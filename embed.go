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
	extract := r.opts.Extract != nil && st.source.Kind != sourceMetadata && text != ""
	if !r.send(splitEvent{st: st, n: len(items), extract: extract}) {
		return
	}
	if len(items) > 0 {
		r.complete(st, items)
	}
	if extract {
		r.extract(st, text)
	}
}

func (r *run) key(digest string) string { return Key(r.embedID, digest) }

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
		case !ok:
			missed = append(missed, it)
			continue
		case h.Vector != "":
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
		default:
			missed = append(missed, it)
			continue
		}
		l.answer(ownKeys[i], answer{held: &h})
		it.held = true
		held = append(held, it)
	}
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

func (r *run) await(st *inputState, items []item, calls []*claim) {
	var answered, again []item
	for i, c := range calls {
		a, err := r.p.claims.wait(r.ctx, c)
		if err != nil {
			return
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
		reply, err = r.p.cfg.Embeddings.Client.Embed(ctx, &ai.EmbeddingRequest{
			Model: r.p.cfg.Embeddings.Model, Input: texts, InputType: ai.InputDocument,
		})
		return err
	})
	if r.ctx.Err() != nil {
		return
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

func (r *run) callFailed(st *inputState, items []item, requests int, err error, l *lease) {
	wrapped := fmt.Errorf("docinfer: run: embed %d texts of part %s: %w", len(items), st.part.ID, err)
	switch classify(err) {
	case rowRefused:
		if len(items) > 1 {
			subs := make([]*lease, len(items))
			for i, it := range items {
				subs[i] = l.take(r.key(it.chunk.Digest))
			}
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

func encodeVector(v []float32) string {
	b := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(b[4*i:], math.Float32bits(f))
	}
	return base64.StdEncoding.EncodeToString(b)
}
