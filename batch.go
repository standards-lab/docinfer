package docinfer

import (
	"encoding/json"
	"fmt"

	"github.com/standards-lab/docproc/dispatch"
)

// emptyBatchSize is the length of an empty batch's JSON, {"parts":[],"sources":[],"chunks":[]}.
var emptyBatchSize = len(`{"parts":[],"sources":[],"chunks":[]}`)

// batchBuilder builds one batch, keeping the length its JSON would marshal to: an element's JSON
// is the same within the batch as alone, so each addition costs its element's length and a comma
// after the first of its kind.
type batchBuilder struct {
	batch   Batch
	size    int
	parts   map[string]int      // a part's index in the batch, by its ID
	sources map[*inputState]int // a source's index in the batch, by its input
}

func newBatchBuilder() *batchBuilder {
	return &batchBuilder{
		batch:   Batch{Parts: []dispatch.Part{}, Sources: []Source{}, Chunks: []Chunk{}},
		size:    emptyBatchSize,
		parts:   map[string]int{},
		sources: map[*inputState]int{},
	}
}

// addition is what adding an item would add to a batch: its part and source, when the batch
// doesn't hold them, its chunk, and the bytes they take.
type addition struct {
	part   bool
	source *Source
	chunk  Chunk
	size   int
}

// plan returns what adding it would add to b.
func (b *batchBuilder) plan(it item) (addition, error) {
	var a addition
	partIndex, ok := b.parts[it.st.part.ID]
	if !ok {
		a.part = true
		partIndex = len(b.batch.Parts)
		a.size += len(it.st.partJSON) + comma(len(b.batch.Parts))
	}
	sourceIndex, ok := b.sources[it.st]
	if !ok {
		s := it.st.source
		s.Part = partIndex
		j, err := marshal(s)
		if err != nil {
			return addition{}, err
		}
		a.source = &s
		sourceIndex = len(b.batch.Sources)
		a.size += len(j) + comma(len(b.batch.Sources))
	}
	a.chunk = it.chunk
	a.chunk.Source = sourceIndex
	j, err := marshal(a.chunk)
	if err != nil {
		return addition{}, err
	}
	a.size += len(j) + comma(len(b.batch.Chunks))
	return a, nil
}

// add adds a to b, as plan made it for it.
func (b *batchBuilder) add(it item, a addition) {
	if a.part {
		b.parts[it.st.part.ID] = len(b.batch.Parts)
		b.batch.Parts = append(b.batch.Parts, it.st.part)
	}
	if a.source != nil {
		b.sources[it.st] = len(b.batch.Sources)
		b.batch.Sources = append(b.batch.Sources, *a.source)
	}
	b.batch.Chunks = append(b.batch.Chunks, a.chunk)
	b.size += a.size
}

// comma is the comma an array's next element takes after n elements.
func comma(n int) int {
	if n == 0 {
		return 0
	}
	return 1
}

// marshal returns v's JSON, as encoding/json marshals it within a batch.
func marshal(v any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("docinfer: run: marshal %T: %w", v, err)
	}
	return b, nil
}

// flush yields the waiting chunks as batches, in order, each as full as Config.BatchBytes allows,
// so no embedded chunk is left waiting. It reports false when yield stopped the run.
func (r *run) flush(yield func(Result, error) bool) (bool, error) {
	b := newBatchBuilder()
	for _, it := range r.waiting {
		a, err := b.plan(it)
		if err != nil {
			return false, err
		}
		if len(b.batch.Chunks) > 0 && b.size+a.size > r.p.cfg.BatchBytes {
			if !yield(Result{Batch: &b.batch}, nil) {
				return false, nil
			}
			b = newBatchBuilder()
			if a, err = b.plan(it); err != nil {
				return false, err
			}
		}
		b.add(it, a)
	}
	r.waiting = r.waiting[:0]
	if len(b.batch.Chunks) > 0 {
		return yield(Result{Batch: &b.batch}, nil), nil
	}
	return true, nil
}
