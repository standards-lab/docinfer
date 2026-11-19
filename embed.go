package docinfer

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"

	"github.com/standards-lab/go-ai"
)

// embed chunks an input's source text and completes each chunk, as complete does. It runs on the
// input's own goroutine, and hands the coordinator the count of chunks, then each completed group
// of them.
func (r *run) embed(st *inputState, text string) {
	pieces, err := r.p.cfg.Chunker.Split(text)
	if err != nil {
		r.fail(fmt.Errorf("docinfer: run: chunk part %s: %w", st.part.ID, err))
		return
	}
	items := make([]item, len(pieces))
	for i, c := range pieces {
		items[i] = item{st: st, chunk: Chunk{Digest: sha256Hex([]byte(c.Text)), Chunk: c, Index: i}}
	}
	if !r.send(splitEvent{st: st, n: len(items)}) || len(items) == 0 {
		return
	}
	r.complete(st, items)
}

// key is the cache key, and the claim table's, of the embedding of the chunk digest names.
func (r *run) key(digest string) string { return Key(r.embedID, digest) }

// complete completes items, chunks of st's source text. It claims their keys in the pipeline's
// claim table first, so a result another job is asking for is never asked again: the keys it
// claims it reads from the cache, and asks the model for the misses Config.EmbedBatch texts per
// call; the keys another call holds it waits for. Claiming before the cache read means a key
// released meanwhile is held already, since a claimant puts before it releases.
func (r *run) complete(st *inputState, items []item) {
	keys := make([]string, len(items))
	for i, it := range items {
		keys[i] = r.key(it.chunk.Digest)
	}
	l, own, waits := r.p.claims.claim(keys)
	defer l.release()
	if len(waits) > 0 {
		waited := make([]item, len(waits))
		calls := make([]*claim, len(waits))
		for i, w := range waits {
			waited[i], calls[i] = items[w.i], w.c
		}
		r.spawn(func() { r.await(st, waited, calls) })
	}
	if len(own) == 0 {
		return
	}
	ownKeys := make([]string, len(own))
	for i, j := range own {
		ownKeys[i] = keys[j]
	}

	values, err := r.p.cfg.Cache.Get(r.ctx, ownKeys...)
	if err != nil {
		l.release()
		r.fail(fmt.Errorf("docinfer: run: cache get for part %s: %w", st.part.ID, err))
		return
	}
	var held, missed []item
	for i, v := range values {
		it := items[own[i]]
		h, ok := decodeHeld(v)
		switch {
		case !ok: // a miss, or a value that doesn't decode, which the model's reply replaces
			missed = append(missed, it)
			continue
		case h.Vector != "":
			// decodeHeld decoded the vector already; its length is checked here, after the Get.
			vector, _ := DecodeVector(h.Vector)
			if len(vector) != r.p.cfg.Dimensions {
				err := &dimensionsError{digest: it.chunk.Digest, got: len(vector), want: r.p.cfg.Dimensions, held: true}
				l.answer(ownKeys[i], answer{err: err})
				l.release()
				r.fail(err)
				return
			}
			it.chunk.Vector = h.Vector
		case h.Refused != "":
			it.chunk.Error = h.Refused
		default: // a held value of another capability's shape: a miss
			missed = append(missed, it)
			continue
		}
		l.answer(ownKeys[i], answer{held: &h})
		it.held = true
		held = append(held, it)
	}
	// Each call gets its keys' claims before anything is handed to the coordinator, so no waiter
	// waits on this goroutine's send.
	for start := 0; start < len(missed); start += r.p.cfg.EmbedBatch {
		call := missed[start:min(start+r.p.cfg.EmbedBatch, len(missed))]
		callKeys := make([]string, len(call))
		for i, it := range call {
			callKeys[i] = r.key(it.chunk.Digest)
		}
		sub := l.take(callKeys...)
		r.spawn(func() { r.call(st, call, sub) })
	}
	if len(held) > 0 {
		r.send(doneEvent{st: st, items: held})
	}
}

// await waits for items' embeddings, each claimed by another call, calls[i] items[i]'s, holding
// no Bounds slot, and hands the coordinator those answered, counted held, as a cache read would
// be. A key answered with a permanent failure stops the job with it; the keys whose claimants
// ended without a result are completed again, so this job asks the model itself, unless another
// has claimed them meanwhile. A cancelled job stops waiting at once.
func (r *run) await(st *inputState, items []item, calls []*claim) {
	var answered, again []item
	for i, c := range calls {
		a, err := r.p.claims.wait(r.ctx, c)
		if err != nil {
			return // the job has stopped; the coordinator reports why
		}
		it := items[i]
		switch {
		case a.err != nil:
			r.fail(fmt.Errorf("docinfer: run: embed chunk %s of part %s, as another call asked: %w", it.chunk.Digest, st.part.ID, a.err))
			return
		case a.held != nil:
			it.chunk.Vector, it.chunk.Error, it.held = a.held.Vector, a.held.Refused, true
			answered = append(answered, it)
		default:
			again = append(again, it)
		}
	}
	if len(answered) > 0 && !r.send(doneEvent{st: st, items: answered}) {
		return
	}
	if len(again) > 0 {
		r.complete(st, again)
	}
}

// call asks the embeddings model for items' vectors in one request, under the embeddings bound,
// checks each vector's length, holds each, answers its claim, and hands the items to the
// coordinator. A refused call of more than one text is re-sent a text at a time, and only then.
// l holds items' claims; a key call ends without answering, its job stopped or its call failed
// on a row no other caller shares, is released unanswered, a panic included.
func (r *run) call(st *inputState, items []item, l *lease) {
	defer l.release()
	texts := make([]string, len(items))
	for i, it := range items {
		texts[i] = it.chunk.Text
	}
	var (
		reply *ai.Embeddings
		sent  bool
	)
	err := r.p.cfg.Bounds.do(r.ctx, CapabilityEmbeddings, func(ctx context.Context) error {
		sent = true
		var err error
		reply, err = r.p.cfg.Embeddings.Client.Embed(ctx, &ai.EmbeddingRequest{Model: r.p.cfg.Embeddings.Model, Input: texts})
		return err
	})
	if r.ctx.Err() != nil {
		return // the job has stopped; the coordinator reports why
	}
	requests := 0
	if sent {
		requests = 1
	}
	if err != nil {
		r.callFailed(st, items, requests, err, l)
		return
	}
	for i, v := range reply.Vectors {
		if len(v) != r.p.cfg.Dimensions {
			err := &dimensionsError{digest: items[i].chunk.Digest, got: len(v), want: r.p.cfg.Dimensions}
			l.answer(r.key(items[i].chunk.Digest), answer{err: err})
			l.release()
			r.fail(err)
			return
		}
	}
	for i, v := range reply.Vectors {
		items[i].chunk.Vector = encodeVector(v)
		if !r.hold(l, r.key(items[i].chunk.Digest), "the embedding of chunk "+items[i].chunk.Digest, Held{Vector: items[i].chunk.Vector}) {
			return
		}
	}
	r.send(doneEvent{st: st, items: items, requests: requests, usage: Usage{Input: reply.Usage.PromptTokens}})
}

// callFailed maps a failed embeddings call by its row: a refused call is split, each text's claim
// handed to its own call, or its one text refused, held, and answered; a permanent failure is
// every key's answer, and stops the job; a failed call's texts are recorded failed on the last
// delivery; anything else stops the job. A key not answered is released unanswered, before the
// coordinator hears, so its waiters ask the model themselves.
func (r *run) callFailed(st *inputState, items []item, requests int, err error, l *lease) {
	wrapped := fmt.Errorf("docinfer: run: embed %d texts of part %s: %w", len(items), st.part.ID, err)
	switch classify(err) {
	case rowRefused:
		if len(items) > 1 {
			subs := make([]*lease, len(items))
			for i, it := range items {
				subs[i] = l.take(r.key(it.chunk.Digest))
			}
			// The coordinator hears of the request before the texts it splits into complete.
			if !r.send(doneEvent{st: st, requests: requests}) {
				return
			}
			for i, it := range items {
				r.spawn(func() { r.call(st, []item{it}, subs[i]) })
			}
			return
		}
		items[0].chunk.Error = failureText(err)
		if !r.hold(l, r.key(items[0].chunk.Digest), "the embedding of chunk "+items[0].chunk.Digest, Held{Refused: items[0].chunk.Error}) {
			return
		}
	case rowPermanent:
		for _, it := range items {
			l.answer(r.key(it.chunk.Digest), answer{err: err})
		}
		r.fail(wrapped)
		return
	case rowFailed:
		l.release()
		if !r.opts.Final {
			r.fail(wrapped)
			return
		}
		for i := range items {
			items[i].chunk.Error = failureText(err)
		}
	default:
		l.release()
		r.fail(wrapped)
		return
	}
	r.send(doneEvent{st: st, items: items, requests: requests})
}

// hold puts h under key, then answers key's claim in l with it, so a waiter's answer is what a
// cache read would give. It reports false, having released l and stopped the job, when it can't
// put h, what naming the result in the error.
func (r *run) hold(l *lease, key, what string, h Held) bool {
	b, err := json.Marshal(h)
	if err == nil {
		err = r.p.cfg.Cache.Put(r.ctx, key, b)
	}
	if err != nil {
		l.release()
		r.fail(fmt.Errorf("docinfer: run: hold %s: %w", what, err))
		return false
	}
	l.answer(key, answer{held: &h})
	return true
}

// encodeVector is the one writer of a [Chunk]'s and a [Held]'s vector, as [DecodeVector] reads
// it: standard base64 of each float32's bits, little-endian, in order.
func encodeVector(v []float32) string {
	b := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(b[4*i:], math.Float32bits(f))
	}
	return base64.StdEncoding.EncodeToString(b)
}
