package docinfer

import (
	"encoding/json"
	"fmt"

	"github.com/standards-lab/docproc/dispatch"
)

var emptyBatchSize = len(`{"parts":[],"sources":[],"chunks":[]}`)

// See docs/internals.md
type batchBuilder struct {
	batch   Batch
	size    int
	parts   map[string]int
	sources map[*inputState]int
}

func newBatchBuilder() *batchBuilder {
	return &batchBuilder{
		batch:   Batch{Parts: []dispatch.Part{}, Sources: []Source{}, Chunks: []Chunk{}},
		size:    emptyBatchSize,
		parts:   map[string]int{},
		sources: map[*inputState]int{},
	}
}

type addition struct {
	part   bool
	source *Source
	chunk  Chunk
	size   int
}

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

func comma(n int) int {
	if n == 0 {
		return 0
	}
	return 1
}

func marshal(v any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("docinfer: run: marshal %T: %w", v, err)
	}
	return b, nil
}

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
