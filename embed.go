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

// embed chunks an input's source text and completes each chunk: its vector or refusal read from
// the cache, or asked of the model Config.EmbedBatch texts per call. It runs on the input's own
// goroutine, and hands the coordinator the count of chunks, then each completed group of them.
func (r *run) embed(st *inputState, text string) {
	pieces, err := r.p.cfg.Chunker.Split(text)
	if err != nil {
		r.fail(fmt.Errorf("docinfer: run: chunk part %s: %w", st.part.ID, err))
		return
	}
	items := make([]item, len(pieces))
	keys := make([]string, len(pieces))
	for i, c := range pieces {
		digest := sha256Hex([]byte(c.Text))
		items[i] = item{st: st, chunk: Chunk{Digest: digest, Chunk: c, Index: i}}
		keys[i] = Key(r.embedID, digest)
	}
	if !r.send(splitEvent{st: st, n: len(items)}) || len(items) == 0 {
		return
	}

	values, err := r.p.cfg.Cache.Get(r.ctx, keys...)
	if err != nil {
		r.fail(fmt.Errorf("docinfer: run: cache get for part %s: %w", st.part.ID, err))
		return
	}
	var held, missed []item
	for i, v := range values {
		it := items[i]
		h, ok := decodeHeld(v)
		switch {
		case !ok: // a miss, or a value that doesn't decode, which the model's reply replaces
			missed = append(missed, it)
			continue
		case h.Vector != "":
			// decodeHeld decoded the vector already; its length is checked here, after the Get.
			vector, _ := DecodeVector(h.Vector)
			if len(vector) != r.p.cfg.Dimensions {
				r.fail(&dimensionsError{digest: it.chunk.Digest, got: len(vector), want: r.p.cfg.Dimensions, held: true})
				return
			}
			it.chunk.Vector = h.Vector
		case h.Refused != "":
			it.chunk.Error = h.Refused
		default: // a held value of another capability's shape: a miss
			missed = append(missed, it)
			continue
		}
		it.held = true
		held = append(held, it)
	}
	if len(held) > 0 && !r.send(doneEvent{st: st, items: held}) {
		return
	}
	for start := 0; start < len(missed); start += r.p.cfg.EmbedBatch {
		call := missed[start:min(start+r.p.cfg.EmbedBatch, len(missed))]
		r.wg.Go(func() { r.call(st, call) })
	}
}

// call asks the embeddings model for items' vectors in one request, under the embeddings bound,
// checks each vector's length, holds each, and hands the items to the coordinator. A refused
// call of more than one text is re-sent a text at a time, and only then.
func (r *run) call(st *inputState, items []item) {
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
		r.callFailed(st, items, requests, err)
		return
	}
	for i, v := range reply.Vectors {
		if len(v) != r.p.cfg.Dimensions {
			r.fail(&dimensionsError{digest: items[i].chunk.Digest, got: len(v), want: r.p.cfg.Dimensions})
			return
		}
	}
	for i, v := range reply.Vectors {
		items[i].chunk.Vector = encodeVector(v)
		if !r.put(items[i].chunk.Digest, Held{Vector: items[i].chunk.Vector}) {
			return
		}
	}
	r.send(doneEvent{st: st, items: items, requests: requests, usage: Usage{Input: reply.Usage.PromptTokens}})
}

// callFailed maps a failed embeddings call by its row: a refused call is split, or its one text
// refused and held; a failed call's texts are recorded failed on the last delivery; anything
// else stops the job.
func (r *run) callFailed(st *inputState, items []item, requests int, err error) {
	wrapped := fmt.Errorf("docinfer: run: embed %d texts of part %s: %w", len(items), st.part.ID, err)
	switch classify(err) {
	case rowRefused:
		if len(items) > 1 {
			if !r.send(doneEvent{st: st, requests: requests}) {
				return
			}
			for _, it := range items {
				r.wg.Go(func() { r.call(st, []item{it}) })
			}
			return
		}
		items[0].chunk.Error = failureText(err)
		if !r.put(items[0].chunk.Digest, Held{Refused: items[0].chunk.Error}) {
			return
		}
	case rowFailed:
		if !r.opts.Final {
			r.fail(wrapped)
			return
		}
		for i := range items {
			items[i].chunk.Error = failureText(err)
		}
	default:
		r.fail(wrapped)
		return
	}
	r.send(doneEvent{st: st, items: items, requests: requests})
}

// put holds h as the embedding of the chunk digest names, reporting false, having stopped the job,
// when it can't.
func (r *run) put(digest string, h Held) bool {
	b, err := json.Marshal(h)
	if err == nil {
		err = r.p.cfg.Cache.Put(r.ctx, Key(r.embedID, digest), b)
	}
	if err != nil {
		r.fail(fmt.Errorf("docinfer: run: hold the embedding of chunk %s: %w", digest, err))
		return false
	}
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
