package docinfer_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"iter"
	"math"
	"net/http"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/standards-lab/docinfer"
	"github.com/standards-lab/docinfer/chunk"
	"github.com/standards-lab/docinfer/join"
	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/aitest"
	"github.com/standards-lab/go-ai/format/completions"
)

const (
	embeddingModel  = "embedding-model"
	embeddingServed = "org/embedding-1"
	dimensions      = 2
)

var embeddingID = docinfer.Identity{Capability: docinfer.CapabilityEmbeddings, Model: embeddingServed}

func vectorOf(text string) []float32 {
	return []float32{float32(len(text)), float32(strings.Count(text, " "))}
}

func runes(_ context.Context, text string) (int, error) { return utf8.RuneCountInString(text), nil }

func embedAll(_ context.Context, req aitest.EmbeddingRequest) aitest.Reply {
	vectors := make([][]float32, len(req.Input))
	for i, text := range req.Input {
		vectors[i] = vectorOf(text)
	}
	return aitest.EmbeddingsReply{Vectors: vectors, PromptTokens: 3 * len(req.Input)}
}

func runConfig(t *testing.T, e *aitest.Endpoint, edit func(*docinfer.Config)) docinfer.Config {
	t.Helper()
	cfg := docinfer.Config{
		Embeddings: docinfer.Embeddings{Client: newClient(t, e.URL(), ai.TranscribeRoute), Model: embeddingModel, Served: embeddingServed},
		Chunker:    chunk.Splitter{Bound: 40, Runes: 1 << 20, Count: runes, Model: embeddingServed},
		Dimensions: dimensions,
		Cache:      docinfer.NewMemoryCache(),
		Bounds:     newBounds(t, 1, 1, 1, 4),
		Pending:    4,
		EmbedBatch: 8,
		BatchBytes: 1 << 20,
		Join:       join.Request{WordsPerSecond: 3},
	}
	if edit != nil {
		edit(&cfg)
	}
	return cfg
}

const failsafe = 10 * time.Second

func receive[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()
	var v T
	select {
	case v = <-ch:
	case <-time.After(failsafe):
		t.Fatalf("no %s within %v", what, failsafe)
	}
	return v
}

func newPipeline(t *testing.T, cfg docinfer.Config) *docinfer.Pipeline {
	t.Helper()
	p, err := docinfer.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func encode(v []float32) string {
	b := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(b[4*i:], math.Float32bits(f))
	}
	return base64.StdEncoding.EncodeToString(b)
}

var (
	inputPart = dispatch.Part{ID: "/", Name: "report.docx", Size: 2048, SHA256: strings.Repeat("a", 64)}
	childPart = dispatch.Part{ID: "/word%2Fmedia%2Fpage", Name: "page", Size: 512, SHA256: strings.Repeat("b", 64),
		Parent: "/", Depth: 1, Where: "word/media/page"}
	textStage = dispatch.Identity{Name: "office", Version: "1", Config: "c0"}
)

func outputInput(t *testing.T, content *dispatch.MemoryContent, part dispatch.Part, kind dispatch.OutputKind, text string) docinfer.Input {
	t.Helper()
	sha := sha(text)
	if content != nil {
		if err := content.Put(t.Context(), sha, strings.NewReader(text)); err != nil {
			t.Fatal(err)
		}
	}
	return docinfer.Input{Event: dispatch.Event{
		Part:   part,
		Stage:  textStage,
		Output: &dispatch.Output{Kind: kind, Name: string(kind), MediaType: "text/plain", SHA256: sha},
	}}
}

func metadataInput() docinfer.Input {
	return docinfer.Input{Event: dispatch.Event{Part: inputPart, Outcome: dispatch.Prepared}, Metadata: docinfer.MetadataText(inputPart)}
}

func seq(inputs ...docinfer.Input) iter.Seq2[docinfer.Input, error] {
	return func(yield func(docinfer.Input, error) bool) {
		for _, in := range inputs {
			if !yield(in, nil) {
				return
			}
		}
	}
}

type outcome struct {
	inputs     []docinfer.Input
	inferences []docinfer.Inference
	progress   []docinfer.Progress
	batches    []docinfer.Batch
	settled    []int
	counts     *docinfer.Counts
	err        error
	after      bool
	results    int
	all        []docinfer.Result
}

func collect(t *testing.T, results iter.Seq2[docinfer.Result, error]) outcome {
	t.Helper()
	var o outcome
	from := derivation{}
	unechoed := func(what, sha string) {
		if len(from.bySHA[sha]) == 0 {
			t.Errorf("%s %s before the echo of the input it derives from", what, sha)
		}
	}
	settled := map[int]bool{}
	settledFrom := func(what string, positions []int) {
		if len(positions) > 0 && !slices.ContainsFunc(positions, func(p int) bool { return !settled[p] }) {
			t.Errorf("%s after the settlement of every input it may derive from, %v", what, positions)
		}
	}
	for r, err := range results {
		o.results++
		if o.counts != nil || o.err != nil {
			o.after = true
		}
		if err == nil {
			o.all = append(o.all, r)
			if r.Input != nil {
				from.echo(len(o.inputs), *r.Input)
			} else if what, positions := from.derived(r); positions != nil {
				settledFrom(what, positions)
			}
		}
		switch {
		case err != nil:
			if r != (docinfer.Result{}) {
				t.Errorf("a result beside the error %v", err)
			}
			o.err = err
		case r.Input != nil:
			o.inputs = append(o.inputs, *r.Input)
		case r.Inference != nil:
			if r.Inference.Capability == docinfer.CapabilityExtraction {
				unechoed("extraction of", r.Inference.ContentSHA256[0])
			} else {
				for _, c := range r.Inference.ContentSHA256 {
					if c != "" {
						unechoed("inference of", c)
					}
				}
			}
			o.inferences = append(o.inferences, *r.Inference)
		case r.Progress != nil:
			unechoed("progress of", r.Progress.ContentSHA256)
			if slices.ContainsFunc(o.inferences, func(inf docinfer.Inference) bool {
				return inf.Capability == docinfer.CapabilityExtraction && slices.Equal(inf.ContentSHA256, []string{r.Progress.ContentSHA256})
			}) {
				t.Errorf("progress of text %s after its extraction's inference", r.Progress.ContentSHA256)
			}
			o.progress = append(o.progress, *r.Progress)
		case r.Batch != nil:
			for _, s := range r.Batch.Sources {
				switch {
				case s.Kind == "metadata" && s.Output == nil:
					unechoed("a batch of metadata", s.SHA256)
				case s.Output != nil:
					unechoed("a batch of "+s.Kind, s.Output.SHA256)
				}
				if s.Kind == "vision" && !slices.ContainsFunc(o.inferences, func(inf docinfer.Inference) bool {
					return slices.Equal(inf.ContentSHA256, []string{s.Output.SHA256})
				}) {
					t.Errorf("a batch holds chunks of image %s before its inference", s.Output.SHA256)
				}
			}
			o.batches = append(o.batches, *r.Batch)
		case r.Settled != nil:
			p := *r.Settled
			switch {
			case p < 0 || p >= len(o.inputs):
				t.Errorf("settlement of input %d, of %d echoed", p, len(o.inputs))
			case settled[p]:
				t.Errorf("input %d settled twice", p)
			}
			settled[p] = true
			o.settled = append(o.settled, p)
		case r.Done != nil:
			o.counts = r.Done
			for p := range o.inputs {
				if !settled[p] {
					t.Errorf("input %d, %+v, unsettled at the counts", p, o.inputs[p].Event)
				}
			}
		default:
			t.Errorf("a result with nothing set: %+v", r)
		}
	}
	if o.after {
		t.Error("Run yielded after its counts or its error, which must be last")
	}
	return o
}

type derivation struct {
	bySHA  map[string][]int
	byPart map[string][]int
}

func (d *derivation) echo(p int, in docinfer.Input) {
	if d.bySHA == nil {
		d.bySHA, d.byPart = map[string][]int{}, map[string][]int{}
	}
	switch out := in.Event.Output; {
	case out != nil:
		d.bySHA[out.SHA256] = append(d.bySHA[out.SHA256], p)
		if out.Kind == dispatch.AudioOutput || (out.Kind == dispatch.RecordOutput && out.MediaType == dispatch.MediaAudioPlan) {
			d.byPart[in.Event.Part.ID] = append(d.byPart[in.Event.Part.ID], p)
		}
	case in.Metadata != "":
		s := sha(in.Metadata)
		d.bySHA[s] = append(d.bySHA[s], p)
	}
}

func (d *derivation) derived(r docinfer.Result) (string, []int) {
	var positions []int
	add := func(sha string) {
		for _, p := range d.bySHA[sha] {
			if !slices.Contains(positions, p) {
				positions = append(positions, p)
			}
		}
	}
	switch {
	case r.Inference != nil:
		inf := r.Inference
		if inf.Capability == docinfer.CapabilityTranscription {
			positions = slices.Clone(d.byPart[inf.Part])
		} else {
			add(inf.ContentSHA256[0])
		}
		if inf.TextSHA256 != "" && positions != nil {
			for _, p := range positions {
				if !slices.Contains(d.bySHA[inf.TextSHA256], p) {
					d.bySHA[inf.TextSHA256] = append(d.bySHA[inf.TextSHA256], p)
				}
			}
		}
		return fmt.Sprintf("the %s inference of %v", inf.Capability, inf.ContentSHA256), positions
	case r.Progress != nil:
		add(r.Progress.ContentSHA256)
		return "progress of " + r.Progress.ContentSHA256, positions
	case r.Batch != nil:
		for _, s := range r.Batch.Sources {
			if s.SHA256 != "" {
				add(s.SHA256)
			}
			if s.SHA256 == "" || (s.Kind == "metadata" && s.Output != nil) {
				add(s.Output.SHA256)
			}
		}
		return fmt.Sprintf("a batch of %d sources", len(r.Batch.Sources)), positions
	}
	return "", nil
}

func (o outcome) chunks() []placed {
	var out []placed
	for _, b := range o.batches {
		for _, c := range b.Chunks {
			s := b.Sources[c.Source]
			out = append(out, placed{c, s, b.Parts[s.Part]})
		}
	}
	return out
}

type placed struct {
	chunk  docinfer.Chunk
	source docinfer.Source
	part   dispatch.Part
}

func held(t *testing.T, cache docinfer.Cache, digest string) *docinfer.Held {
	t.Helper()
	values, err := cache.Get(t.Context(), docinfer.Key(embeddingID, digest))
	if err != nil {
		t.Fatal(err)
	}
	if values[0] == nil {
		return nil
	}
	var h docinfer.Held
	if err := json.Unmarshal(values[0], &h); err != nil {
		t.Fatalf("the held value of chunk %s: %v", digest, err)
	}
	return &h
}

func vector(t *testing.T, s string) []float32 {
	t.Helper()
	v, err := docinfer.DecodeVector(s)
	if err != nil {
		t.Fatalf("DecodeVector(%q): %v", s, err)
	}
	return v
}

const (
	plainText = "The first paragraph is short.\n\nThe second paragraph is a little longer than the first."
	markdown  = "# Heading\n\nA Markdown paragraph under it."
)

func TestRunEmbedsTextMarkdownAndMetadata(t *testing.T) {
	e := aitest.Serve(t, aitest.Routes{Embeddings: embedAll})
	cfg := runConfig(t, e, func(c *docinfer.Config) { c.EmbedBatch = 2 })
	p := newPipeline(t, cfg)
	content := dispatch.NewMemoryContent()
	inputs := []docinfer.Input{
		outputInput(t, content, childPart, dispatch.TextOutput, plainText),
		outputInput(t, content, childPart, dispatch.MarkdownOutput, markdown),
		outputInput(t, content, childPart, dispatch.ImageOutput, "an image"),
		outputInput(t, content, childPart, dispatch.AudioOutput, "a window"),
		outputInput(t, content, childPart, dispatch.RecordOutput, "{}"),
		metadataInput(),
	}

	type want struct {
		kind, text string
		part       dispatch.Part
		output     *dispatch.Output
	}
	var wants []want
	for _, in := range []struct {
		kind, text string
		in         docinfer.Input
	}{{"output", plainText, inputs[0]}, {"output", markdown, inputs[1]}, {"metadata", inputs[5].Metadata, inputs[5]}} {
		pieces, err := cfg.Chunker.Split(t.Context(), in.text)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range pieces {
			wants = append(wants, want{in.kind, c.Text, in.in.Event.Part, in.in.Event.Output})
		}
	}
	if len(wants) < 4 {
		t.Fatalf("the fixture's texts cut into %d chunks; the test needs more than one call's", len(wants))
	}

	o := collect(t, p.Run(t.Context(), content, seq(inputs...), docinfer.Options{}))
	if o.err != nil {
		t.Fatalf("Run: %v", o.err)
	}
	got := o.chunks()
	if len(got) != len(wants) {
		t.Fatalf("Run yielded %d chunks, want %d", len(got), len(wants))
	}
	for _, w := range wants {
		i := slices.IndexFunc(got, func(g placed) bool { return g.chunk.Text == w.text && g.source.Kind == w.kind })
		if i < 0 {
			t.Errorf("no %s chunk %q", w.kind, w.text)
			continue
		}
		g := got[i]
		if g.chunk.Digest != sha(w.text) {
			t.Errorf("chunk %q: digest %s, want its text's SHA-256", w.text, g.chunk.Digest)
		}
		if v := vector(t, g.chunk.Vector); !slices.Equal(v, vectorOf(w.text)) {
			t.Errorf("chunk %q: vector %v, want %v", w.text, v, vectorOf(w.text))
		}
		if g.part.ID != w.part.ID {
			t.Errorf("chunk %q: part %s, want %s", w.text, g.part.ID, w.part.ID)
		}
		switch w.kind {
		case "output":
			if g.source.Output == nil || g.source.Output.SHA256 != w.output.SHA256 {
				t.Errorf("chunk %q: source output %+v, want %+v", w.text, g.source.Output, w.output)
			}
			if g.source.SHA256 != "" {
				t.Errorf("chunk %q: an output's source SHA256 %q, want it held in Output.SHA256 alone", w.text, g.source.SHA256)
			}
			if g.source.Stage == nil || *g.source.Stage != textStage {
				t.Errorf("chunk %q: source stage %v, want %v", w.text, g.source.Stage, textStage)
			}
		case "metadata":
			if g.source.SHA256 != sha(inputs[5].Metadata) || g.source.Output != nil {
				t.Errorf("chunk %q: metadata source %+v, want the metadata text's SHA-256 and no output", w.text, g.source)
			}
		}
		if h := held(t, cfg.Cache, g.chunk.Digest); h == nil || h.Vector != g.chunk.Vector {
			t.Errorf("chunk %q: held %+v, want its vector", w.text, h)
		}
	}
	for _, b := range o.batches {
		checkBatch(t, b)
	}

	var texts, tokens int
	for _, req := range e.Requests() {
		var body struct {
			Model string   `json:"model"`
			Input []string `json:"input"`
		}
		if err := json.Unmarshal(req.Body, &body); err != nil {
			t.Fatal(err)
		}
		if body.Model != embeddingModel || len(body.Input) > cfg.EmbedBatch {
			t.Errorf("a request of model %q and %d texts, want %q and at most EmbedBatch %d", body.Model, len(body.Input), embeddingModel, cfg.EmbedBatch)
		}
		texts += len(body.Input)
		tokens += 3 * len(body.Input)
	}
	if texts != len(wants) {
		t.Errorf("the endpoint was asked %d texts, want each chunk's once, %d", texts, len(wants))
	}
	want1 := docinfer.Counts{
		Texts: 2, Metadata: 1, Embedded: len(wants), Requests: len(e.Requests()), Images: 1, Windows: 1,
		Tokens: docinfer.Tokens{Embeddings: docinfer.Usage{Input: tokens}},
	}
	checkCounts(t, o, want1)

	requests := len(e.Requests())
	o = collect(t, p.Run(t.Context(), content, seq(inputs...), docinfer.Options{}))
	if o.err != nil {
		t.Fatalf("the second Run: %v", o.err)
	}
	if n := len(e.Requests()); n != requests {
		t.Errorf("the second run sent %d requests, want none", n-requests)
	}
	checkCounts(t, o, docinfer.Counts{Texts: 2, Metadata: 1, Embedded: len(wants), Held: len(wants), Images: 1, Windows: 1})
}

func checkBatch(t *testing.T, b docinfer.Batch) {
	t.Helper()
	parts := map[string]bool{}
	for _, p := range b.Parts {
		if parts[p.ID] {
			t.Errorf("a batch holds part %s twice", p.ID)
		}
		parts[p.ID] = true
	}
	sources := map[string]bool{}
	for _, s := range b.Sources {
		if s.Part < 0 || s.Part >= len(b.Parts) {
			t.Fatalf("a source's part index %d is outside the batch's %d parts", s.Part, len(b.Parts))
		}
		j, _ := json.Marshal(s)
		if sources[string(j)] {
			t.Errorf("a batch holds source %s twice", j)
		}
		sources[string(j)] = true
	}
	for _, c := range b.Chunks {
		if c.Source < 0 || c.Source >= len(b.Sources) {
			t.Fatalf("a chunk's source index %d is outside the batch's %d sources", c.Source, len(b.Sources))
		}
	}
	same := docinfer.Batch{Chunks: b.Chunks}
	if b.Digest() != same.Digest() {
		t.Error("a batch's Digest depends on more than its chunks")
	}
	other := docinfer.Batch{Chunks: slices.Clone(b.Chunks)}
	other.Chunks[0].Digest += "0"
	if b.Digest() == other.Digest() {
		t.Error("a batch's Digest doesn't change with its chunks' digests")
	}
}

func checkCounts(t *testing.T, o outcome, want docinfer.Counts) {
	t.Helper()
	if o.counts == nil {
		t.Fatal("Run yielded no counts")
	}
	got, _ := json.Marshal(o.counts)
	w, _ := json.Marshal(want)
	if !bytes.Equal(got, w) {
		t.Errorf("counts\n got %s\nwant %s", got, w)
	}
	if n := len(o.chunks()); o.counts.Embedded+o.counts.Rejected != n {
		t.Errorf("Embedded %d + Rejected %d, want the %d chunks of the batches", o.counts.Embedded, o.counts.Rejected, n)
	}
	if o.counts.Extraction != nil || o.counts.Tokens.Extraction != nil {
		t.Errorf("counts of an unasked capability: %s", got)
	}
}

func TestMetadataText(t *testing.T) {
	p := dispatch.Part{ID: "/", Name: "notes.txt", Size: 12, SHA256: strings.Repeat("c", 64)}
	p.ContentType.Kind, p.ContentType.Route = "text", "text"
	p.ContentType.Stored = "text/plain"
	want := "file name: notes.txt\nsize: 12 bytes\nkind: text\nroute: text\nstored MIME type: text/plain\n"
	if got := docinfer.MetadataText(p); got != want {
		t.Errorf("MetadataText\n got %q\nwant %q", got, want)
	}
}

func TestRunHoldsBeforeItYields(t *testing.T) {
	e := aitest.Serve(t, aitest.Routes{Embeddings: embedAll})
	cfg := runConfig(t, e, func(c *docinfer.Config) { c.EmbedBatch = 1 })
	p := newPipeline(t, cfg)
	content := dispatch.NewMemoryContent()
	in := outputInput(t, content, childPart, dispatch.TextOutput, plainText)
	batches := 0
	for r, err := range p.Run(t.Context(), content, seq(in), docinfer.Options{}) {
		if err != nil {
			t.Fatal(err)
		}
		if r.Batch == nil {
			continue
		}
		batches++
		for _, c := range r.Batch.Chunks {
			if h := held(t, cfg.Cache, c.Digest); h == nil || h.Vector != c.Vector {
				t.Errorf("at its batch's yield, chunk %q is held as %+v, want its vector", c.Text, h)
			}
		}
	}
	if batches < 2 {
		t.Errorf("%d batches, want one per call, at least 2", batches)
	}
}

func TestRunPullsWithinPending(t *testing.T) {
	const pending, total = 2, 5
	gate := &aitest.Gate{}
	e := aitest.Serve(t, aitest.Routes{Embeddings: func(ctx context.Context, req aitest.EmbeddingRequest) aitest.Reply {
		if err := gate.Wait(ctx); err != nil {
			return aitest.ErrorReply{Status: 503, Message: err.Error()}
		}
		return embedAll(ctx, req)
	}})
	p := newPipeline(t, runConfig(t, e, func(c *docinfer.Config) {
		c.Pending = pending
		c.Bounds = newBounds(t, 1, 1, 1, total)
	}))
	content := dispatch.NewMemoryContent()
	var inputs []docinfer.Input
	for i := range total {
		inputs = append(inputs, outputInput(t, content, childPart, dispatch.TextOutput, fmt.Sprintf("Text number %d.", i)))
	}

	var pulled, finished, overrun atomic.Int64
	stream := func(yield func(docinfer.Input, error) bool) {
		for _, in := range inputs {
			if pulled.Add(1) > finished.Load()+pending {
				overrun.Add(1)
			}
			if !yield(in, nil) {
				return
			}
		}
	}
	done := make(chan outcome, 1)
	go func() {
		var o outcome
		for r, err := range p.Run(t.Context(), content, stream, docinfer.Options{}) {
			switch {
			case err != nil:
				o.err = err
			case r.Batch != nil:
				finished.Add(1)
				o.batches = append(o.batches, *r.Batch)
			case r.Done != nil:
				o.counts = r.Done
			}
		}
		done <- o
	}()
	for i := range total {
		want := min(pending, total-i)
		gate.Await(t, want)
		if parked := gate.Parked(); parked != want {
			t.Fatalf("after %d inputs finished, %d requests parked, want %d", i, parked, want)
		}
		if n := pulled.Load(); n != int64(i+want) {
			t.Fatalf("after %d inputs finished, %d pulled, want %d", i, n, i+want)
		}
		gate.Release(1)
	}
	o := receive(t, done, "outcome of the run")
	if o.err != nil {
		t.Fatalf("Run: %v", o.err)
	}
	if n := overrun.Load(); n != 0 {
		t.Errorf("%d inputs pulled with Pending inputs unfinished", n)
	}
	if o.counts == nil || o.counts.Embedded != total {
		t.Errorf("counts %+v, want %d embedded", o.counts, total)
	}
}

func TestRunBatchBytes(t *testing.T) {
	text := "Alpha alpha alpha.\n\nBravo bravo bravo.\n\nCharlie charlie.\n\nDelta delta delta."
	e := aitest.Serve(t, aitest.Routes{Embeddings: embedAll})
	run := func(batchBytes int) outcome {
		t.Helper()
		p := newPipeline(t, runConfig(t, e, func(c *docinfer.Config) {
			c.Chunker.Bound = 20
			c.BatchBytes = batchBytes
		}))
		content := dispatch.NewMemoryContent()
		o := collect(t, p.Run(t.Context(), content, seq(outputInput(t, content, childPart, dispatch.TextOutput, text)), docinfer.Options{}))
		if o.err != nil {
			t.Fatalf("Run with BatchBytes %d: %v", batchBytes, o.err)
		}
		return o
	}
	whole := run(1 << 20)
	if len(whole.batches) != 1 || len(whole.batches[0].Chunks) < 3 {
		t.Fatalf("the uncapped run yielded %d batches, want one of at least 3 chunks", len(whole.batches))
	}
	all := whole.batches[0]
	size := func(n int) int {
		b, err := json.Marshal(docinfer.Batch{Parts: all.Parts, Sources: all.Sources, Chunks: all.Chunks[:n]})
		if err != nil {
			t.Fatal(err)
		}
		return len(b)
	}
	cases := []struct {
		name       string
		batchBytes int
		first      int
	}{
		{"exactly two chunks", size(2), 2},
		{"a byte below two chunks", size(2) - 1, 1},
		{"each chunk alone past the cap", 1, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := run(tc.batchBytes)
			if n := len(o.batches[0].Chunks); n != tc.first {
				t.Errorf("the first batch holds %d chunks, want %d", n, tc.first)
			}
			var chunks []docinfer.Chunk
			for i, b := range o.batches {
				j, err := json.Marshal(b)
				if err != nil {
					t.Fatal(err)
				}
				if len(b.Chunks) > 1 && len(j) > tc.batchBytes {
					t.Errorf("batch %d of %d chunks marshals to %d bytes, past the cap %d", i, len(b.Chunks), len(j), tc.batchBytes)
				}
				if i+1 < len(o.batches) {
					next := docinfer.Batch{Parts: b.Parts, Sources: b.Sources, Chunks: append(slices.Clone(b.Chunks), o.batches[i+1].Chunks[0])}
					if k, _ := json.Marshal(next); len(k) <= tc.batchBytes {
						t.Errorf("batch %d was yielded though the next chunk fit under the cap", i)
					}
				}
				checkBatch(t, b)
				for _, c := range b.Chunks {
					c.Source = 0
					chunks = append(chunks, c)
				}
			}
			if !slices.EqualFunc(chunks, all.Chunks, func(a, b docinfer.Chunk) bool { return a == b }) {
				t.Error("the capped batches' chunks aren't the uncapped batch's, in order")
			}
		})
	}
}

func TestRunCancelled(t *testing.T) {
	gate := &aitest.Gate{}
	e := aitest.Serve(t, aitest.Routes{Embeddings: func(ctx context.Context, req aitest.EmbeddingRequest) aitest.Reply {
		if err := gate.Wait(ctx); err != nil {
			return aitest.ErrorReply{Status: 503, Message: err.Error()}
		}
		return embedAll(ctx, req)
	}})
	p := newPipeline(t, runConfig(t, e, nil))
	content := dispatch.NewMemoryContent()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan []error, 1)
	go func() {
		var errs []error
		first := true
		for r, err := range p.Run(ctx, content, seq(outputInput(t, content, childPart, dispatch.TextOutput, plainText)), docinfer.Options{}) {
			if first && r.Input != nil && err == nil {
				first = false
				continue
			}
			if r != (docinfer.Result{}) {
				errs = append(errs, fmt.Errorf("a result %+v", r))
			}
			errs = append(errs, err)
		}
		done <- errs
	}()
	gate.Await(t, 1)
	cancel()
	errs := receive(t, done, "end of the cancelled run")
	if len(errs) != 1 || errs[0] != context.Canceled {
		t.Errorf("a cancelled run yielded %v, want context.Canceled alone", errs)
	}
}

type routeBoundTransport struct {
	routeDone <-chan struct{}
	late      *atomic.Bool
}

func (rt routeBoundTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := http.DefaultTransport.RoundTrip(req)
	if req.Context().Err() == nil {
		return resp, err
	}
	select {
	case <-rt.routeDone:
	case <-time.After(failsafe):
		rt.late.Store(true)
	}
	return resp, err
}

func TestRunWaitsForRequestsInFlight(t *testing.T) {
	const quick, parked = "A text embedded at once.", "A text whose request parks."
	for _, cancelled := range []bool{false, true} {
		name := "stopped ranging"
		if cancelled {
			name = "cancelled"
		}
		t.Run(name, func(t *testing.T) {
			gate := &aitest.Gate{}
			routeDone := make(chan struct{})
			e := aitest.Serve(t, aitest.Routes{Embeddings: func(ctx context.Context, req aitest.EmbeddingRequest) aitest.Reply {
				if !slices.Contains(req.Input, parked) {
					return embedAll(ctx, req)
				}
				defer close(routeDone)
				if err := gate.Wait(ctx); err != nil {
					for range 1000 {
						runtime.Gosched()
					}
					return aitest.ErrorReply{Status: 503, Message: err.Error()}
				}
				return embedAll(ctx, req)
			}})
			var late atomic.Bool
			client, err := ai.New(e.URL(), &http.Client{Transport: routeBoundTransport{routeDone, &late}},
				completions.New(completions.Options{}), ai.Options{})
			if err != nil {
				t.Fatal(err)
			}
			p := newPipeline(t, runConfig(t, e, func(c *docinfer.Config) { c.Embeddings.Client = client }))
			content := dispatch.NewMemoryContent()
			inputs := seq(outputInput(t, content, childPart, dispatch.TextOutput, quick),
				outputInput(t, content, childPart, dispatch.TextOutput, parked))
			routeReturned := func() bool {
				select {
				case <-routeDone:
					return true
				default:
					return false
				}
			}

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var results []error
			for r, err := range p.Run(ctx, content, inputs, docinfer.Options{}) {
				if r.Input != nil {
					continue
				}
				results = append(results, err)
				if err != nil {
					if !routeReturned() {
						t.Error("Run yielded its error with its request in flight, its route function not yet returned")
					}
					continue
				}
				if r.Batch == nil || len(r.Batch.Chunks) != 1 || r.Batch.Chunks[0].Text != quick {
					t.Fatalf("Run yielded %+v, want the quick text's batch", r)
				}
				gate.Await(t, 1)
				if !cancelled {
					break
				}
				cancel()
			}
			if !routeReturned() {
				t.Error("Run returned with its request in flight, its route function not yet returned")
			}
			if late.Load() {
				t.Errorf("the parked request's route didn't return within %v of the job's end", failsafe)
			}
			want := []error{nil}
			if cancelled {
				want = append(want, context.Canceled)
			}
			if !slices.EqualFunc(results, want, errors.Is) {
				t.Errorf("Run yielded %v, want %v: the batch, then context.Canceled when cancelled", results, want)
			}
		})
	}
}

func TestRunStoppedEarly(t *testing.T) {
	e := aitest.Serve(t, aitest.Routes{Embeddings: embedAll})
	p := newPipeline(t, runConfig(t, e, func(c *docinfer.Config) { c.EmbedBatch = 1 }))
	content := dispatch.NewMemoryContent()
	for _, err := range p.Run(t.Context(), content, seq(outputInput(t, content, childPart, dispatch.TextOutput, plainText)), docinfer.Options{}) {
		if err != nil {
			t.Fatal(err)
		}
		break
	}
}

func TestRunMissingContent(t *testing.T) {
	e := aitest.Serve(t, aitest.Routes{Embeddings: embedAll})
	p := newPipeline(t, runConfig(t, e, nil))
	in := outputInput(t, nil, childPart, dispatch.TextOutput, "never put")
	o := collect(t, p.Run(t.Context(), dispatch.NewMemoryContent(), seq(in), docinfer.Options{}))
	missing, ok := errors.AsType[*docinfer.MissingError](o.err)
	if !ok {
		t.Fatalf("Run over missing content: %v, want a *MissingError", o.err)
	}
	if !errors.Is(o.err, fs.ErrNotExist) {
		t.Errorf("%v doesn't match fs.ErrNotExist", o.err)
	}
	if missing.Event.Part.ID != in.Event.Part.ID || missing.Event.Output != in.Event.Output {
		t.Errorf("the MissingError's event %+v, want the input's", missing.Event)
	}
	if docinfer.Permanent(o.err) {
		t.Error("Permanent reports a MissingError")
	}
	if o.counts != nil || len(o.batches) > 0 {
		t.Error("a stopped run yielded batches or counts")
	}
}

func TestRunReadsHeldValues(t *testing.T) {
	const text = "One short chunk."
	digest := sha(text)
	vec := encode([]float32{7, 8})
	run := func(t *testing.T, value string) (outcome, *aitest.Endpoint, docinfer.Cache) {
		t.Helper()
		e := aitest.Serve(t, aitest.Routes{Embeddings: embedAll})
		cfg := runConfig(t, e, nil)
		if err := cfg.Cache.Put(t.Context(), docinfer.Key(embeddingID, digest), []byte(value)); err != nil {
			t.Fatal(err)
		}
		content := dispatch.NewMemoryContent()
		o := collect(t, newPipeline(t, cfg).Run(t.Context(), content, seq(outputInput(t, content, childPart, dispatch.TextOutput, text)), docinfer.Options{}))
		if o.err != nil {
			t.Fatal(o.err)
		}
		return o, e, cfg.Cache
	}
	embedded := docinfer.Counts{Texts: 1, Embedded: 1, Held: 1}
	for _, tc := range []struct {
		value         string
		vector, error string
		counts        docinfer.Counts
	}{
		{` {"vector":"` + vec + `"} `, vec, "", embedded},
		{`{"vector":"` + vec + `","text":"a","incomplete":"max_output_tokens"}`, vec, "", embedded},
		{`{"refused":"no"}`, "", "no", docinfer.Counts{Texts: 1, Rejected: 1, Held: 1}},
	} {
		t.Run("read "+tc.value, func(t *testing.T) {
			o, e, _ := run(t, tc.value)
			if n := len(e.Requests()); n != 0 {
				t.Errorf("%d requests, want the held value read", n)
			}
			if c := o.chunks(); len(c) != 1 || c[0].chunk.Vector != tc.vector || c[0].chunk.Error != tc.error {
				t.Errorf("chunks %+v, want the held vector %q, error %q", c, tc.vector, tc.error)
			}
			checkCounts(t, o, tc.counts)
		})
	}
	for _, value := range []string{
		` `, `null`, `"a cat"`, `[]`, `{`, `{"text":1}`, `{"vector":"` + vec + `"} {}`, `{"vector":"` + vec + `"}x`,
		`{"description":"a cat"}`, `{"vector":"not base64!"}`, `{"vector":"AACA"}`, `{}`, `{"text":"a cat"}`,
	} {
		t.Run("miss "+value, func(t *testing.T) {
			o, e, cache := run(t, value)
			if n := len(e.Requests()); n != 1 {
				t.Errorf("%d requests, want the value asked again", n)
			}
			if h := held(t, cache, digest); h == nil || !slices.Equal(vector(t, h.Vector), vectorOf(text)) {
				t.Errorf("held %+v, want the model's vector in place of the value", h)
			}
			checkCounts(t, o, docinfer.Counts{Texts: 1, Embedded: 1, Requests: 1, Tokens: docinfer.Tokens{Embeddings: docinfer.Usage{Input: 3}}})
		})
	}
}

func TestRunWrongDimensions(t *testing.T) {
	const text = "One short chunk."
	digest := sha(text)

	t.Run("made", func(t *testing.T) {
		e := aitest.Serve(t, aitest.Routes{Embeddings: func(_ context.Context, req aitest.EmbeddingRequest) aitest.Reply {
			return aitest.EmbeddingsReply{Vectors: [][]float32{{1, 2, 3}}}
		}})
		cfg := runConfig(t, e, nil)
		content := dispatch.NewMemoryContent()
		o := collect(t, newPipeline(t, cfg).Run(t.Context(), content, seq(outputInput(t, content, childPart, dispatch.TextOutput, text)), docinfer.Options{}))
		if o.err == nil || !docinfer.Permanent(o.err) {
			t.Fatalf("a made vector of 3 dimensions: %v, want a permanent error", o.err)
		}
		if h := held(t, cfg.Cache, digest); h != nil {
			t.Errorf("the wrong vector was held: %+v", h)
		}
	})
	t.Run("held", func(t *testing.T) {
		e := aitest.Serve(t, aitest.Routes{Embeddings: embedAll})
		cfg := runConfig(t, e, nil)
		value, _ := json.Marshal(docinfer.Held{Vector: encode([]float32{1, 2, 3})})
		if err := cfg.Cache.Put(t.Context(), docinfer.Key(embeddingID, digest), value); err != nil {
			t.Fatal(err)
		}
		content := dispatch.NewMemoryContent()
		o := collect(t, newPipeline(t, cfg).Run(t.Context(), content, seq(outputInput(t, content, childPart, dispatch.TextOutput, text)), docinfer.Options{}))
		if o.err == nil || !docinfer.Permanent(o.err) {
			t.Fatalf("a held vector of 3 dimensions: %v, want a permanent error", o.err)
		}
		if n := len(e.Requests()); n != 0 {
			t.Errorf("%d requests, want none: the held vector is checked after the Get", n)
		}
	})
}

func TestRunInputStreamError(t *testing.T) {
	e := aitest.Serve(t, aitest.Routes{Embeddings: embedAll})
	p := newPipeline(t, runConfig(t, e, nil))
	broken := errors.New("broken stream")
	stream := func(yield func(docinfer.Input, error) bool) { yield(docinfer.Input{}, broken) }
	o := collect(t, p.Run(t.Context(), dispatch.NewMemoryContent(), stream, docinfer.Options{}))
	if !errors.Is(o.err, broken) || o.counts != nil {
		t.Errorf("Run over a broken stream: %v, counts %v; want the stream's error alone", o.err, o.counts)
	}
}
