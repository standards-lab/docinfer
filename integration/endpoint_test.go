//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"iter"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/standards-lab/docinfer"
	"github.com/standards-lab/docinfer/chunk"
	"github.com/standards-lab/docinfer/join"
	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/format/completions"
)

type role string

const (
	agent         role = "AI_AGENT_BASE_URL"
	vision        role = "AI_VISION_BASE_URL"
	audio         role = "AI_AUDIO_BASE_URL"
	embedding     role = "AI_EMBEDDING_BASE_URL"
	transcription role = "AI_TRANSCRIPTION_BASE_URL"
	tokenize      role = "AI_TOKENIZE_BASE_URL"
)

const (
	visionTokens     = 256
	extractionTokens = 1024
	chunkBound       = 400
	chunkRunes       = 8000
	pending          = 2
	embedBatch       = 8
	batchBytes       = 1 << 16
	wordsPerSecond   = 3
)

const runTimeout = 5 * time.Minute

func endpoint(t *testing.T, r role, opts ai.Options) (c *ai.Client, model, served string) {
	t.Helper()
	base := os.Getenv(string(r))
	if base == "" {
		t.Skipf("%s is unset: set it to the endpoint's API root, such as http://localhost:8000/v1", r)
	}
	c, err := newClient(string(r), base, completions.New(completions.Options{}), opts, os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	if model := namedModel(string(r), os.Getenv); model != "" {
		return c, model, model
	}
	ctx, cancel := context.WithTimeout(t.Context(), runTimeout)
	defer cancel()
	models, err := c.Models(ctx)
	if err != nil {
		t.Fatalf("Models of %s: %v", r, err)
	}
	if len(models) != 1 || models[0].ID == "" {
		t.Fatalf("%s lists %+v, want exactly one model, or its model named in %s", r, models, modelVariable(string(r)))
	}
	var listed struct {
		Root string `json:"root"`
	}
	if err := json.Unmarshal(models[0].Native, &listed); err != nil {
		t.Fatalf("%s lists %s: %v", r, models[0].Native, err)
	}
	if listed.Root == "" {
		return c, models[0].ID, models[0].ID
	}
	return c, models[0].ID, listed.Root
}

func embeddings(t *testing.T) (docinfer.Embeddings, int) {
	t.Helper()
	c, model, served := endpoint(t, embedding, ai.Options{})
	ctx, cancel := context.WithTimeout(t.Context(), runTimeout)
	defer cancel()
	probe, err := c.Embed(ctx, &ai.EmbeddingRequest{Model: model, Input: []string{"A probe of the embedding space."}})
	if err != nil {
		t.Fatalf("Embed of the probe: %v", err)
	}
	if len(probe.Vectors) != 1 || len(probe.Vectors[0]) == 0 {
		t.Fatalf("Embed of the probe gave %d vectors, want one of at least one dimension", len(probe.Vectors))
	}
	return docinfer.Embeddings{Client: c, Model: model, Served: served}, len(probe.Vectors[0])
}

func tokenizer(t *testing.T, emb docinfer.Embeddings) *ai.Client {
	t.Helper()
	base := os.Getenv(string(tokenize))
	if base == "" {
		return emb.Client
	}
	c, err := newClient(string(tokenize), base, completions.New(completions.Options{}), ai.Options{}, os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func newPipeline(t *testing.T, emb docinfer.Embeddings, dims int, edit func(*docinfer.Config)) *docinfer.Pipeline {
	t.Helper()
	counter := tokenizer(t, emb)
	cache, err := docinfer.NewFileCache(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	bounds, err := docinfer.NewBounds(1, 1, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	cfg := docinfer.Config{
		Embeddings: emb,
		Chunker: chunk.Splitter{
			Bound: chunkBound,
			Runes: chunkRunes,
			Count: func(ctx context.Context, text string) (int, error) {
				tokens, err := counter.Tokenize(ctx, &ai.TokenizeRequest{Model: emb.Model, Text: text, InputType: ai.InputDocument})
				if err != nil {
					return 0, err
				}
				return tokens.Count, nil
			},
			Model: emb.Served,
		},
		Dimensions: dims,
		Cache:      cache,
		Bounds:     bounds,
		Pending:    pending,
		EmbedBatch: embedBatch,
		BatchBytes: batchBytes,
		Join:       join.Request{WordsPerSecond: wordsPerSecond},
	}
	if edit != nil {
		edit(&cfg)
	}
	p, err := docinfer.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

var stage = dispatch.Identity{Name: "integration", Version: "1", Config: "c0"}

func put(t *testing.T, content *dispatch.MemoryContent, data []byte) string {
	t.Helper()
	sum := sha256.Sum256(data)
	sha := hex.EncodeToString(sum[:])
	if err := content.Put(t.Context(), sha, bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
	return sha
}

func readTestdata(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

type outcome struct {
	inferences []docinfer.Inference
	progress   []docinfer.Progress
	batches    []docinfer.Batch
	counts     docinfer.Counts
}

func run(t *testing.T, p *docinfer.Pipeline, content dispatch.Content, opts docinfer.Options, inputs ...docinfer.Input) outcome {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), runTimeout)
	defer cancel()
	var in iter.Seq2[docinfer.Input, error] = func(yield func(docinfer.Input, error) bool) {
		for _, i := range inputs {
			if !yield(i, nil) {
				return
			}
		}
	}
	var o outcome
	done := false
	for r, err := range p.Run(ctx, content, in, opts) {
		switch {
		case err != nil:
			t.Fatalf("Run: %v", err)
		case r.Inference != nil:
			o.inferences = append(o.inferences, *r.Inference)
		case r.Progress != nil:
			o.progress = append(o.progress, *r.Progress)
		case r.Batch != nil:
			o.batches = append(o.batches, *r.Batch)
		case r.Done != nil:
			o.counts, done = *r.Done, true
		}
	}
	if !done {
		t.Fatal("Run ended without its counts")
	}
	return o
}

func (o outcome) inference(t *testing.T, c docinfer.Capability) docinfer.Inference {
	t.Helper()
	if len(o.inferences) != 1 || o.inferences[0].Capability != c {
		t.Fatalf("Run yielded inferences %+v, want one of %s", o.inferences, c)
	}
	return o.inferences[0]
}

func checkChunks(t *testing.T, o outcome, dims int, kinds ...string) int {
	t.Helper()
	n := 0
	seen := map[string]bool{}
	for _, b := range o.batches {
		for _, c := range b.Chunks {
			n++
			seen[b.Sources[c.Source].Kind] = true
			if c.Error != "" {
				t.Errorf("chunk %d of %q has error %q, want a vector", c.Index, c.Text, c.Error)
				continue
			}
			v, err := docinfer.DecodeVector(c.Vector)
			if err != nil {
				t.Errorf("chunk %d of %q: %v", c.Index, c.Text, err)
				continue
			}
			if len(v) != dims {
				t.Errorf("chunk %d of %q has %d dimensions, want the configured %d", c.Index, c.Text, len(v), dims)
			}
		}
	}
	for _, k := range kinds {
		if !seen[k] {
			t.Errorf("no chunk of a source of kind %s among %d chunks", k, n)
		}
	}
	if n != o.counts.Embedded || o.counts.Rejected != 0 {
		t.Errorf("Counts has %d embedded and %d rejected, want the %d chunks yielded embedded", o.counts.Embedded, o.counts.Rejected, n)
	}
	return n
}

func checkHeld(t *testing.T, first, second outcome) {
	t.Helper()
	if len(second.inferences) != len(first.inferences) {
		t.Fatalf("the second run yielded %d inferences, the first %d", len(second.inferences), len(first.inferences))
	}
	for i, inf := range second.inferences {
		if !inf.Held || inf.Text != first.inferences[i].Text || inf.Outcome != first.inferences[i].Outcome {
			t.Errorf("the second run's inference %d = %+v, want the first's %+v, held", i, inf, first.inferences[i])
		}
	}
	if len(second.progress) != 0 {
		t.Errorf("the second run yielded %d progress, want none from a held extraction", len(second.progress))
	}
	c := second.counts
	if c.Embedded != first.counts.Embedded || c.Held != c.Embedded || c.Requests != 0 || c.Tokens.Embeddings != (docinfer.Usage{}) {
		t.Errorf("the second run's counts %+v, want the first's %d chunks held, no request, no token", c, first.counts.Embedded)
	}
	if v := c.Vision; v != nil && (v.Held != v.Described+v.Incomplete+v.Refused || v.Requests != 0) {
		t.Errorf("the second run's vision counts %+v, want every image held, no request", *v)
	}
	if tc := c.Transcription; tc != nil && (tc.Held != tc.Transcribed+tc.Incomplete+tc.Refused || tc.Requests != 0) {
		t.Errorf("the second run's transcription counts %+v, want every window held, no request", *tc)
	}
	if x := c.Extraction; x != nil && (x.Held != x.Extracted+x.Incomplete+x.Refused || x.Requests != 0) {
		t.Errorf("the second run's extraction counts %+v, want every text held, no request", *x)
	}
}
