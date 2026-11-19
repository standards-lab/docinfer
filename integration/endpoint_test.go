//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
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

// role is an endpoint a scenario runs on, named by the environment variable that holds its API
// root: go-ai's tier's roles, by the same variables.
type role string

// The roles, each its variable.
const (
	agent         role = "AI_AGENT_BASE_URL"
	vision        role = "AI_VISION_BASE_URL"
	audio         role = "AI_AUDIO_BASE_URL"
	embedding     role = "AI_EMBEDDING_BASE_URL"
	transcription role = "AI_TRANSCRIPTION_BASE_URL"
)

// The tier's own small values for what docinfer gives no default: each MaxTokens, the chunker's
// bound, the counts, the bounds, and the join's words a second.
const (
	// visionTokens is the vision request's MaxTokens: room for a sentence.
	visionTokens = 256
	// extractionTokens is the extraction request's MaxTokens: a reasoning model, as the agent is,
	// reasons ahead of its answer within the same bound.
	extractionTokens = 1024
	chunkBound       = 400
	pending          = 2
	embedBatch       = 8
	batchBytes       = 1 << 16
	wordsPerSecond   = 3
)

// runTimeout bounds one Run: its few requests, each of which, on a reasoning model, takes tens
// of seconds. The endpoints have their models loaded, so this covers generation only.
const runTimeout = 5 * time.Minute

// endpoint returns a Client of r's endpoint, speaking Chat Completions, adjusted by opts and by
// r's credentials, AI_<ROLE>_TOKEN and AI_<ROLE>_HEADER, as newClient reads them, and the model to
// send to: the one AI_<ROLE>_MODEL names when set, else the one model the endpoint lists. It skips
// t when r's variable is unset, naming it, and fails t when a credential doesn't parse, New
// refuses the URL or the credentials, or, with no model named, the list can't be had or names
// other than exactly one model; no failure prints a token or header value.
func endpoint(t *testing.T, r role, opts ai.Options) (*ai.Client, string) {
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
		return c, model
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
	return c, models[0].ID
}

// embeddings returns the pipeline's Embeddings on the embedding role's endpoint, which every
// pipeline requires, and the dimensions of its model's vectors, taken from the vector of one
// probe text as go-ai's TestEmbed takes them, so no variable states them. It skips t, naming
// AI_EMBEDDING_BASE_URL, when that is unset.
func embeddings(t *testing.T) (docinfer.Embeddings, int) {
	t.Helper()
	c, model := endpoint(t, embedding, ai.Options{})
	ctx, cancel := context.WithTimeout(t.Context(), runTimeout)
	defer cancel()
	probe, err := c.Embed(ctx, &ai.EmbeddingRequest{Model: model, Input: []string{"A probe of the embedding space."}})
	if err != nil {
		t.Fatalf("Embed of the probe: %v", err)
	}
	if len(probe.Vectors) != 1 || len(probe.Vectors[0]) == 0 {
		t.Fatalf("Embed of the probe gave %d vectors, want one of at least one dimension", len(probe.Vectors))
	}
	return docinfer.Embeddings{Client: c, Model: model}, len(probe.Vectors[0])
}

// newPipeline returns the Pipeline of emb, of dims dimensions, over a FileCache in a directory of
// t's own, with the tier's small values, and edit's capabilities.
func newPipeline(t *testing.T, emb docinfer.Embeddings, dims int, edit func(*docinfer.Config)) *docinfer.Pipeline {
	t.Helper()
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
		Chunker:    chunk.Splitter{Bound: chunkBound},
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

// stage is the stage every input's output comes from.
var stage = dispatch.Identity{Name: "integration", Version: "1", Config: "c0"}

// put holds data in content as the content its SHA-256 names, and returns that SHA-256.
func put(t *testing.T, content *dispatch.MemoryContent, data []byte) string {
	t.Helper()
	sum := sha256.Sum256(data)
	sha := hex.EncodeToString(sum[:])
	if err := content.Put(t.Context(), sha, bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
	return sha
}

// readTestdata returns the bytes of testdata's file name.
func readTestdata(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// outcome is what one Run yielded: its inferences, progress, and batches, in order, and its
// counts.
type outcome struct {
	inferences []docinfer.Inference
	progress   []docinfer.Progress
	batches    []docinfer.Batch
	counts     docinfer.Counts
}

// run runs one job of inputs on p over content with opts, bounded by runTimeout, and returns what
// it yielded, failing t on the job's error or a job that ends without its counts.
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

// inference returns o's one inference, failing t unless o yielded exactly one, of capability c.
func (o outcome) inference(t *testing.T, c docinfer.Capability) docinfer.Inference {
	t.Helper()
	if len(o.inferences) != 1 || o.inferences[0].Capability != c {
		t.Fatalf("Run yielded inferences %+v, want one of %s", o.inferences, c)
	}
	return o.inferences[0]
}

// checkChunks fails t unless o's batches hold a chunk of a source of each of kinds, and every
// chunk has a vector of dims dimensions, as DecodeVector reads it, and no error. It returns the
// chunks' number.
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

// checkHeld fails t unless second, a second run over first's content, is held whole: its
// inferences are first's, each Held, with the same text; it has no progress; its chunks are
// first's number, each held; and it asks no model.
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
