package run

import (
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"math"
	"path/filepath"
	"strings"
	"sync"

	"github.com/standards-lab/docproc/rundir"
	"github.com/standards-lab/docproc/rundir/schema"

	"github.com/standards-lab/docinfer"
	"github.com/standards-lab/docinfer/profile"

	"github.com/standards-lab/docinfer/cmd/docinfer/internal/outdir"
)

const notConfigured = "not configured"

type target struct {
	docproc string
	out     string
	run     string
	extract *docinfer.Extract
}

func infer(ctx context.Context, w io.Writer, cfg Config, models []profile.Model, t target) (string, error) {
	src, err := rundir.Open(t.docproc)
	if err != nil {
		return "", err
	}
	id := t.run
	if id == "" {
		if id, err = src.Latest(); err != nil {
			return "", err
		}
	}
	dm, err := src.Manifest(id)
	if err != nil {
		return "", err
	}
	if dm.End.IsZero() {
		return "", fmt.Errorf("docproc run %s: %w", id, rundir.ErrNotEnded)
	}
	rel, err := relative(t.out, t.docproc)
	if err != nil {
		return "", err
	}
	out, err := outdir.Create(t.out)
	if err != nil {
		return "", err
	}
	p, err := pipeline(cfg, models, out.Cache())
	if err != nil {
		return "", err
	}
	m := manifest(cfg, p.Profile(), outdir.Docproc{Dir: rel, Run: id})
	r, err := out.Begin(m)
	if err != nil {
		return "", err
	}
	if err := writeBegin(w, r.ID(), id); err != nil {
		return "", errors.Join(err, r.End(m))
	}

	f := &feed{}
	pr := newProgress(w, f)
	sp := newSpace(cfg, p.Profile())
	var counts *docinfer.Counts
	var stop error
	for res, err := range p.Run(ctx, src.Content(), f.inputs(src.Record(id)), docinfer.Options{Extract: t.extract, Final: true}) {
		if err == nil {
			err = take(ctx, res, out, r, pr, sp)
		}
		if err != nil {
			stop = err
			break
		}
		if res.Done != nil {
			counts = res.Done
		}
	}
	m.Inputs, m.Outputs = f.read()
	m.Space = sp.result()
	if counts != nil {
		m.Counts = *counts
	}
	if stop != nil {
		m.Error = stop.Error()
	}
	if err := r.End(m); err != nil {
		return "", errors.Join(stop, err)
	}
	if stop != nil {
		return "", stop
	}
	return r.ID(), writeEnd(w, r.ID(), m)
}

func take(ctx context.Context, res docinfer.Result, out *outdir.Dir, r *outdir.Run, pr *progress, sp *space) error {
	switch {
	case res.Input != nil:
		pr.echo(*res.Input)
	case res.Inference != nil:
		inf := res.Inference
		if inf.TextSHA256 != "" {
			if err := out.Texts().Put(ctx, inf.TextSHA256, strings.NewReader(inf.Text)); err != nil {
				return fmt.Errorf("put the text %s of part %s's %s: %w", inf.TextSHA256, inf.Part, inf.Capability, err)
			}
		}
		if err := r.Inference(outdir.Inference{Input: inf.Origin, Inference: *inf}); err != nil {
			return err
		}
		pr.inference(*inf)
	case res.Batch != nil:
		if err := r.Batch(*res.Batch, partInputs(*res.Batch)); err != nil {
			return err
		}
		if err := sp.measure(*res.Batch); err != nil {
			return err
		}
		pr.batch(*res.Batch)
	case res.Settled != nil:
		return pr.settle(*res.Settled)
	}
	return nil
}

func relative(out, docproc string) (string, error) {
	absOut, err := filepath.Abs(out)
	if err != nil {
		return "", err
	}
	absDocproc, err := filepath.Abs(docproc)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(absOut, absDocproc)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

func pipeline(cfg Config, models []profile.Model, cache docinfer.Cache) (*docinfer.Pipeline, error) {
	bound := func(c docinfer.Capability) int {
		if n, ok := cfg.Concurrency[c]; ok {
			return n
		}
		return 1
	}
	bounds, err := docinfer.NewBounds(
		bound(docinfer.CapabilityVision),
		bound(docinfer.CapabilityTranscription),
		bound(docinfer.CapabilityExtraction),
		bound(docinfer.CapabilityEmbeddings),
	)
	if err != nil {
		return nil, err
	}
	return profile.New(profile.Config{
		Settings: cfg.Settings,
		Clients:  cfg.Clients,
		Models:   models,
		Bounds:   bounds,
		Cache:    cache,
	})
}

func manifest(cfg Config, pr docinfer.Profile, d outdir.Docproc) outdir.Manifest {
	m := outdir.Manifest{Docinfer: cfg.Build, Docproc: d, Inputs: []outdir.Input{}}
	s := cfg.Settings
	requested := map[docinfer.Capability]string{docinfer.CapabilityEmbeddings: s.Embeddings.Model}
	if v := s.Vision; v != nil {
		requested[docinfer.CapabilityVision] = v.Model
	}
	if t := s.Transcription; t != nil {
		requested[docinfer.CapabilityTranscription] = t.Model
	}
	if x := s.Extraction; x != nil {
		requested[docinfer.CapabilityExtraction] = x.Model
	}
	capability := func(id docinfer.Identity) outdir.Capability {
		e := cfg.Endpoints[id.Capability]
		return outdir.Capability{
			Identity:     id,
			RequestModel: requested[id.Capability],
			Endpoint:     e.Name,
			BaseURL:      e.BaseURL,
			Format:       e.Format,
			Concurrency:  cfg.Concurrency[id.Capability],
		}
	}
	for _, c := range []struct {
		name docinfer.Capability
		id   *docinfer.Identity
	}{
		{docinfer.CapabilityVision, pr.Vision},
		{docinfer.CapabilityTranscription, pr.Transcription},
	} {
		if c.id == nil {
			m.Skipped = append(m.Skipped, outdir.Skipped{Name: c.name, Reason: notConfigured})
			continue
		}
		mc := capability(*c.id)
		if c.name == docinfer.CapabilityVision {
			mc.LayerWords = pr.VisionLayerWords
		}
		m.Capabilities = append(m.Capabilities, mc)
	}
	if pr.Extraction != nil {
		m.Capabilities = append(m.Capabilities, capability(*pr.Extraction))
	}
	embeddings := capability(pr.Embeddings)
	embeddings.Batch, embeddings.ChunkSize = *s.Embeddings.Batch, *s.Embeddings.ChunkSize
	m.Capabilities = append(m.Capabilities, embeddings)
	return m
}

type fed struct {
	path string
	last bool
}

type feed struct {
	mu      sync.Mutex
	fed     []fed
	listed  []outdir.Input
	outputs int
}

func (f *feed) inputs(record iter.Seq2[schema.Event, error]) iter.Seq2[docinfer.Input, error] {
	return func(yield func(docinfer.Input, error) bool) {
		for e, err := range record {
			if err != nil {
				yield(docinfer.Input{}, err)
				return
			}
			if !yield(f.take(e), nil) {
				return
			}
		}
	}
}

func (f *feed) take(e schema.Event) docinfer.Input {
	in := docinfer.Input{Event: e.Event, Origin: e.Input}
	last := false
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case e.Kind == schema.OutputEvent:
		f.outputs++
	case e.Kind == schema.PartEvent && e.Part.Depth == 0:
		in.Metadata = docinfer.MetadataText(e.Part)
		f.listed = append(f.listed, outdir.Input{Path: e.Input, SHA256: e.Part.SHA256})
		last = true
	case e.Kind == schema.ErrorEvent:
		f.listed = append(f.listed, outdir.Input{Path: e.Input, Error: e.Error})
		last = true
	}
	f.fed = append(f.fed, fed{path: e.Input, last: last})
	return in
}

func (f *feed) at(pos int) fed {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.fed[pos]
}

func (f *feed) read() ([]outdir.Input, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	inputs := append([]outdir.Input{}, f.listed...)
	return inputs, f.outputs
}

type space struct {
	s    outdir.Space
	seen map[string]bool
}

func newSpace(cfg Config, pr docinfer.Profile) *space {
	return &space{
		s: outdir.Space{
			Endpoint:   cfg.Endpoints[docinfer.CapabilityEmbeddings].Name,
			Model:      pr.Embeddings.Model,
			Dimensions: pr.Dimensions,
			Tolerance:  outdir.UnitTolerance,
			Unit:       true,
			Chunker:    pr.Chunker,
		},
		seen: map[string]bool{},
	}
}

func (sp *space) measure(b docinfer.Batch) error {
	for _, c := range b.Chunks {
		if c.Vector == "" || sp.seen[c.Digest] {
			continue
		}
		sp.seen[c.Digest] = true
		v, err := docinfer.DecodeVector(c.Vector)
		if err != nil {
			return fmt.Errorf("embeddings: vector of chunk %s: %w", c.Digest, err)
		}
		var sq float64
		for _, x := range v {
			sq += float64(x) * float64(x)
		}
		norm := math.Sqrt(sq)
		if sp.s.Vectors == 0 {
			sp.s.MinNorm, sp.s.MaxNorm = norm, norm
		}
		sp.s.MinNorm, sp.s.MaxNorm = min(sp.s.MinNorm, norm), max(sp.s.MaxNorm, norm)
		sp.s.Unit = sp.s.Unit && math.Abs(norm-1) <= outdir.UnitTolerance
		sp.s.Vectors++
	}
	return nil
}

func (sp *space) result() *outdir.Space {
	s := sp.s
	s.Unit = s.Unit && s.Vectors > 0
	return &s
}
