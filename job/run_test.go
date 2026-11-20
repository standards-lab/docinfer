package job_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/standards-lab/docinfer"
	"github.com/standards-lab/docinfer/chunk"
	"github.com/standards-lab/docinfer/job"
	"github.com/standards-lab/docinfer/join"
	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/dispatch/dispatchtest"
	"github.com/standards-lab/docproc/route"
	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/aitest"
	"github.com/standards-lab/go-ai/format/completions"
)

// The run tests drive New and Runner.Run over a dispatcher of docproc's fixture stage, its
// MemoryIndex and MemoryContent, and a pipeline against go-ai's aitest. Each job's input is
// lines of the fixture stage's directives.

const (
	embeddingModel = "embedding-model"
	extractModel   = "extraction-model"
	// refusedText is the one source text the extraction route refuses.
	refusedText = "message 2"
	// failsafe bounds every wait a test makes on what a job does: a stall is a finding.
	failsafe = 10 * time.Second
)

// theExtract is the tests' Extract, which the extraction route answers.
var theExtract = docinfer.Extract{
	Name:   "echo",
	Prompt: "Echo the text.",
	Schema: jsontext.Value(`{"type":"object","properties":{"text":{"type":"string"}},"required":["text"],"additionalProperties":false}`),
}

// sum returns s's SHA-256, lowercase hex.
func sum(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// extraction is the extraction route's text for a source text.
func extraction(text string) string { return `{"text":` + strconv.Quote(text) + `}` }

// serve returns an endpoint that embeds every text as a vector of its length and word count, and
// extracts each source text as extraction's JSON of it, streamed in two pieces, but refuses
// refusedText.
func serve(t *testing.T) *aitest.Endpoint {
	return aitest.Serve(t, aitest.Routes{
		Embeddings: func(_ context.Context, req aitest.EmbeddingRequest) aitest.Reply {
			vectors := make([][]float32, len(req.Input))
			for i, text := range req.Input {
				vectors[i] = []float32{float32(len(text)), float32(strings.Count(text, " "))}
			}
			return aitest.EmbeddingsReply{Vectors: vectors, PromptTokens: 3 * len(req.Input)}
		},
		ChatCompletions: func(_ context.Context, req aitest.ChatRequest) aitest.Reply {
			text := req.Messages[len(req.Messages)-1].Text
			if text == refusedText {
				return aitest.ChatReply{Refusal: "no", PromptTokens: 4}
			}
			whole := extraction(text)
			return aitest.ChatReply{Chunks: []string{whole[:5], whole[5:]}, PromptTokens: 4, CompletionTokens: 2}
		},
	})
}

// newPipeline returns a pipeline of embeddings and extraction on e's routes, a batch per embedded
// chunk, and a chunk per source text.
func newPipeline(t *testing.T, e *aitest.Endpoint) *docinfer.Pipeline {
	t.Helper()
	c, err := ai.New(e.URL(), http.DefaultClient, completions.New(completions.Options{}), ai.Options{})
	if err != nil {
		t.Fatal(err)
	}
	bounds, err := docinfer.NewBounds(1, 1, 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	p, err := docinfer.New(docinfer.Config{
		Extraction: &docinfer.Extraction{Client: c, Model: extractModel, MaxTokens: 100},
		Embeddings: docinfer.Embeddings{Client: c, Model: embeddingModel},
		Chunker:    chunk.Splitter{Bound: 1000},
		Dimensions: 2,
		Cache:      docinfer.NewMemoryCache(),
		Bounds:     bounds,
		Pending:    16,
		EmbedBatch: 1,
		BatchBytes: 1,
		Join:       join.Request{WordsPerSecond: 3},
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// hookIndex is an index that calls its hooks, when set, as a claim or a record of key begins, and
// fails each method fail names, "claim", "append", or "drop", with its error.
type hookIndex struct {
	dispatch.Index
	onClaim  func(dispatch.Key)
	onRecord func(ctx context.Context, key dispatch.Key)
	fail     map[string]error
}

func (h *hookIndex) Claim(ctx context.Context, key dispatch.Key) (string, time.Duration, bool, error) {
	if h.onClaim != nil {
		h.onClaim(key)
	}
	if err := h.fail["claim"]; err != nil {
		return "", 0, false, err
	}
	return h.Index.Claim(ctx, key)
}

func (h *hookIndex) Append(ctx context.Context, key dispatch.Key, token string, r dispatch.Recorded) error {
	if err := h.fail["append"]; err != nil {
		return err
	}
	return h.Index.Append(ctx, key, token, r)
}

func (h *hookIndex) Record(ctx context.Context, key dispatch.Key, token string, e dispatch.Entry) error {
	if h.onRecord != nil {
		h.onRecord(ctx, key)
	}
	return h.Index.Record(ctx, key, token, e)
}

func (h *hookIndex) Drop(ctx context.Context, key dispatch.Key) error {
	if err := h.fail["drop"]; err != nil {
		return err
	}
	return h.Index.Drop(ctx, key)
}

// putContent is a content store that keeps the SHA-256 of each put, in order, fails a put of a
// SHA-256 fail names with its error, and calls onPut, when set, as each put begins. A content
// lost, by lose, misses on Open until it is put again.
type putContent struct {
	dispatch.Content
	fail  map[string]error
	onPut func(ctx context.Context, sha string)

	mu   sync.Mutex
	puts []string
	lost map[string]bool
}

func (c *putContent) Put(ctx context.Context, sha string, r io.Reader) error {
	if c.onPut != nil {
		c.onPut(ctx, sha)
	}
	if err := c.fail[sha]; err != nil {
		return err
	}
	c.mu.Lock()
	c.puts = append(c.puts, sha)
	delete(c.lost, sha)
	c.mu.Unlock()
	return c.Content.Put(ctx, sha, r)
}

func (c *putContent) Open(ctx context.Context, sha string) (io.ReadCloser, error) {
	c.mu.Lock()
	lost := c.lost[sha]
	c.mu.Unlock()
	if lost {
		return nil, fmt.Errorf("putContent: %s is lost: %w", sha, fs.ErrNotExist)
	}
	return c.Content.Open(ctx, sha)
}

// lose makes the content sha names miss until it is put again, as a store that has lost it.
func (c *putContent) lose(sha string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.lost == nil {
		c.lost = map[string]bool{}
	}
	c.lost[sha] = true
}

// Puts returns the SHA-256 of each put so far.
func (c *putContent) Puts() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.puts)
}

// fixture is a job's runner, over a dispatcher of the fixture stage on route.Text, and the stores
// it shares with the dispatcher.
type fixture struct {
	runner  *job.Runner
	stage   *dispatchtest.Stage
	index   *hookIndex
	content *putContent
	dir     string // the dispatcher's working directory
}

// newFixture returns a fixture over world, the content the stage's "file" directives find, and
// a pipeline on e's routes, checkpointing every message.
func newFixture(t *testing.T, world map[string]string, e *aitest.Endpoint) *fixture {
	t.Helper()
	return newFixtureEvery(t, world, e, 1)
}

// newFixtureEvery returns newFixture's fixture, checkpointing every every messages.
func newFixtureEvery(t *testing.T, world map[string]string, e *aitest.Endpoint, every int) *fixture {
	t.Helper()
	f := &fixture{
		stage:   dispatchtest.NewStage(world),
		index:   &hookIndex{Index: dispatch.NewMemoryIndex(0)},
		content: &putContent{Content: dispatch.NewMemoryContent()},
		dir:     t.TempDir(),
	}
	d, err := dispatch.New(dispatch.Config{
		Stages: dispatch.Stages{route.Text: f.stage}, Index: f.index, Content: f.content, Dir: f.dir, Puts: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	f.runner, err = job.New(job.Config{Dispatcher: d, Pipeline: newPipeline(t, e), CheckpointEvery: every})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// input writes text as a file named name, returning the dispatch input of it.
func input(t *testing.T, name, text string) dispatch.Input {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return dispatch.Input{Path: path}
}

// fixtureKey returns the fixture stage's index key of content.
func fixtureKey(content string) dispatch.Key {
	return dispatch.Key{SHA256: sum(content), Stage: dispatchtest.NewStage(nil).Identity()}
}

// run is what one Run yielded: its updates, in order, and its errors.
type run struct {
	updates []job.Update
	errs    []error
	// last is whether an error, if any, came alone and last.
	last bool
}

// collect ranges over updates, calling each, when set, with each update as it is yielded.
func collect(updates func(func(job.Update, error) bool), each func(job.Update)) run {
	var r run
	r.last = true
	for u, err := range updates {
		if len(r.errs) > 0 {
			r.last = false
		}
		if err != nil {
			if u != (job.Update{}) {
				r.last = false
			}
			r.errs = append(r.errs, err)
			continue
		}
		if each != nil {
			each(u)
		}
		r.updates = append(r.updates, u)
	}
	return r
}

// done returns r's Done, failing the test unless r ended with it, its one Done, and no error.
func (r run) done(t *testing.T) job.Done {
	t.Helper()
	if len(r.errs) != 0 {
		t.Fatalf("Run: %v", r.errs)
	}
	for i, u := range r.updates {
		if u.Done != nil && i != len(r.updates)-1 {
			t.Fatalf("a Done at update %d of %d, want it last alone", i, len(r.updates))
		}
	}
	if len(r.updates) == 0 || r.updates[len(r.updates)-1].Done == nil {
		t.Fatal("Run ended without a Done")
	}
	return *r.updates[len(r.updates)-1].Done
}

// events returns r's events, in order.
func (r run) events() []dispatch.Event {
	var out []dispatch.Event
	for _, u := range r.updates {
		if u.Event != nil {
			out = append(out, *u.Event)
		}
	}
	return out
}

// sourceSHA returns s's text's SHA-256: an output's is its Output's.
func sourceSHA(s docinfer.Source) string {
	if s.Output != nil {
		return s.Output.SHA256
	}
	return s.SHA256
}

// own reports whether ev is a part's own event.
func own(ev dispatch.Event) bool { return ev.Output == nil && ev.Warning == nil }

// The job's input: messages, a warning, a found part, an unread child, and a record, which the
// pipeline passes through.
const (
	inputText = "message 1\nwarn a line read badly\nfile x.txt\nunread locked.bin\nrecord {\"k\":1}\nmessage 2"
	childText = "leaf x"
)

// Run yields every dispatch event as an Update.Event, in the pipeline's order: an output's event
// before its inference and the batches holding its chunks, a part's own event after its outputs',
// warnings', and children's. The input's message 1 is extracted and embedded before the input's
// own event, which the index holds back until both are out, so no output waits on its part's
// event. The depth-0 part's own event carries its metadata text, chunked as kind metadata; every
// other event is fed as it is, the warning, the unread child, and the record among them. Each
// inference's text is in the content store as its update is yielded; the refused extraction puts
// nothing. Done comes last: each part by its outcome, their outputs summed, and the pipeline's
// counts.
func TestRunOrder(t *testing.T) {
	f := newFixture(t, map[string]string{"x.txt": childText}, serve(t))
	message1 := sum("message 1")
	release := make(chan struct{})
	f.index.onRecord = func(_ context.Context, key dispatch.Key) {
		if key == fixtureKey(inputText) {
			select {
			case <-release:
			case <-time.After(failsafe):
			}
		}
	}
	var inferred, batched bool
	var once sync.Once
	ctx := t.Context()
	r := collect(f.runner.Run(ctx, input(t, "a.txt", inputText), docinfer.Options{Extract: &theExtract}), func(u job.Update) {
		switch {
		case u.Inference != nil:
			inferred = inferred || u.Inference.ContentSHA256[0] == message1
			// The text is in the content store as its update is yielded.
			if sha := u.Inference.TextSHA256; sha != "" {
				rc, err := f.content.Open(ctx, sha)
				if err != nil {
					t.Errorf("the text of %s's inference isn't in the content store at its update: %v", u.Inference.Part, err)
					return
				}
				b, err := io.ReadAll(rc)
				if err != nil || string(b) != u.Inference.Text {
					t.Errorf("the content store holds %q, %v under the inference's TextSHA256, want %q", b, err, u.Inference.Text)
				}
				if err := rc.Close(); err != nil {
					t.Error(err)
				}
			}
		case u.Batch != nil:
			batched = batched || slices.ContainsFunc(u.Batch.Sources, func(s docinfer.Source) bool { return sourceSHA(s) == message1 })
		}
		if inferred && batched {
			once.Do(func() { close(release) })
		}
	})
	d := r.done(t)

	// Every event, fed as it is: the outputs, the warning, the unread child's, and the record's.
	type step struct {
		name    string
		output  string
		warning bool
		outcome dispatch.Outcome
	}
	var steps []step
	for _, ev := range r.events() {
		s := step{name: ev.Part.Name, warning: ev.Warning != nil, outcome: ev.Outcome}
		if ev.Output != nil {
			s.output = ev.Output.Name
		}
		steps = append(steps, s)
	}
	want := []step{
		{name: "a.txt", output: "lines"},
		{name: "a.txt", output: "message 1"},
		{name: "a.txt", warning: true},
		{name: "x.txt", output: "lines"},
		{name: "x.txt", outcome: dispatch.Prepared},
		{name: "locked.bin", outcome: dispatch.Encrypted},
		{name: "a.txt", output: "record 5"},
		{name: "a.txt", output: "message 2"},
		{name: "a.txt", outcome: dispatch.Prepared},
	}
	if !slices.Equal(steps, want) {
		t.Errorf("events:\n got %+v\nwant %+v", steps, want)
	}

	// The order: each inference and batch after the events it derives from, each part's own event
	// after its outputs', warnings', and children's.
	echoed := map[string]bool{}
	ownAt := map[string]int{}
	var input dispatch.Part
	for i, u := range r.updates {
		switch {
		case u.Event != nil && u.Event.Output != nil:
			echoed[u.Event.Output.SHA256] = true
		case u.Event != nil && own(*u.Event):
			ownAt[u.Event.Part.ID] = i
			if u.Event.Part.Depth == 0 {
				input = u.Event.Part
				echoed[sum(docinfer.MetadataText(input))] = true
			}
		case u.Inference != nil:
			if !echoed[u.Inference.ContentSHA256[0]] {
				t.Errorf("update %d, an inference of %s, precedes its output's event", i, u.Inference.ContentSHA256[0])
			}
		case u.Batch != nil:
			for _, s := range u.Batch.Sources {
				if !echoed[sourceSHA(s)] {
					t.Errorf("update %d, a batch of %s %s, precedes the event it derives from", i, s.Kind, sourceSHA(s))
				}
			}
		}
	}
	for i, u := range r.updates {
		if ev := u.Event; ev != nil {
			if at, ok := ownAt[ev.Part.ID]; ok && i > at {
				t.Errorf("update %d, an event of %s, follows the part's own event", i, ev.Part.Name)
			}
			if at, ok := ownAt[ev.Part.Parent]; ok && i > at {
				t.Errorf("update %d, an event of %s, follows its parent's own event", i, ev.Part.Name)
			}
		}
	}
	if !inferred || !batched {
		t.Fatalf("message 1 inferred %v, batched %v: want both before the input's own event", inferred, batched)
	}

	// The metadata text, chunked as kind metadata, is the depth-0 part's.
	var metadata []string
	for _, u := range r.updates {
		if u.Batch == nil {
			continue
		}
		for _, c := range u.Batch.Chunks {
			if u.Batch.Sources[c.Source].Kind == "metadata" {
				metadata = append(metadata, c.Text)
			}
		}
	}
	// The chunker trims the text's trailing line feed.
	if wantText := strings.TrimSpace(docinfer.MetadataText(input)); !slices.Equal(metadata, []string{wantText}) {
		t.Errorf("metadata chunks %q, want the input's text %q", metadata, wantText)
	}

	// Each inference's text put once, under its TextSHA256; the refused one puts nothing.
	outputs := map[string]bool{}
	for _, ev := range r.events() {
		if ev.Output != nil {
			outputs[ev.Output.SHA256] = true
		}
	}
	var texts, refused []string
	for _, u := range r.updates {
		if inf := u.Inference; inf != nil {
			if inf.TextSHA256 != "" {
				texts = append(texts, inf.TextSHA256)
			}
			if inf.Outcome == docinfer.OutcomeRefused {
				refused = append(refused, inf.ContentSHA256[0])
			}
		}
	}
	var put []string
	for _, sha := range f.content.Puts() {
		if !outputs[sha] {
			put = append(put, sha)
		}
	}
	slices.Sort(texts)
	slices.Sort(put)
	if len(texts) != 3 || !slices.Equal(put, texts) {
		t.Errorf("puts beside the outputs' %v, want each of the 3 extractions' texts %v", put, texts)
	}
	if !slices.Equal(refused, []string{sum(refusedText)}) {
		t.Errorf("refused extractions of %v, want message 2's alone", refused)
	}

	// Done: the input and x.txt prepared, the unread child encrypted; their outputs summed, the
	// input's lines, two messages, and record, and x.txt's lines; the pipeline's counts.
	wantParts := map[dispatch.Outcome]int{dispatch.Prepared: 2, dispatch.Encrypted: 1}
	if !maps.Equal(d.Parts, wantParts) || d.Outputs != 5 {
		t.Errorf("Done parts %v, outputs %d; want %v, 5", d.Parts, d.Outputs, wantParts)
	}
	if c := d.Counts; c.Texts != 4 || c.Metadata != 1 || c.Extraction == nil || c.Extraction.Extracted != 3 || c.Extraction.Refused != 1 {
		t.Errorf("Done counts %+v, want 4 texts, the metadata, 3 extracted and 1 refused", c)
	}
}

// Options pass through to the pipeline: a run without Extract asks for no extraction and counts
// none, and one with it extracts.
func TestRunPassesOptions(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts docinfer.Options
		want int
	}{
		{"none", docinfer.Options{}, 0},
		{"extract", docinfer.Options{Extract: &theExtract}, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := serve(t)
			f := newFixture(t, nil, e)
			r := collect(f.runner.Run(t.Context(), input(t, "a.txt", "message 1"), tc.opts), nil)
			d := r.done(t)
			extractions := 0
			for _, u := range r.updates {
				if u.Inference != nil && u.Inference.Capability == docinfer.CapabilityExtraction {
					extractions++
				}
			}
			if extractions != tc.want || (d.Counts.Extraction == nil) != (tc.want == 0) {
				t.Errorf("%d extractions, counts %+v; want %d", extractions, d.Counts.Extraction, tc.want)
			}
		})
	}
}

// A refused Extract fails before the dispatch starts: the error alone, matching ErrRequest and
// permanent, and the stage never called.
func TestRunRefusedExtract(t *testing.T) {
	e := serve(t)
	f := newFixture(t, nil, e)
	bad := docinfer.Extract{Name: "", Prompt: theExtract.Prompt, Schema: theExtract.Schema}
	r := collect(f.runner.Run(t.Context(), input(t, "a.txt", "message 1"), docinfer.Options{Extract: &bad}), nil)
	if len(r.updates) != 0 || len(r.errs) != 1 || !r.last {
		t.Fatalf("%d updates and errors %v, want the error alone", len(r.updates), r.errs)
	}
	if !errors.Is(r.errs[0], docinfer.ErrRequest) || !docinfer.Permanent(r.errs[0]) {
		t.Errorf("error %v, want a permanent docinfer.ErrRequest", r.errs[0])
	}
	if n := f.stage.Calls("a.txt"); n != 0 {
		t.Errorf("the stage prepared the input %d times, want never", n)
	}
	if n := len(e.Requests()); n != 0 {
		t.Errorf("%d requests, want none", n)
	}
}

// A failed put of an inference's text stops the job with the put's error, alone and last, not
// permanent; the inference's update is never yielded.
func TestRunPutFails(t *testing.T) {
	f := newFixture(t, nil, serve(t))
	failed := sum(extraction("message 1"))
	full := errors.New("the store is full")
	f.content.fail = map[string]error{failed: full}
	r := collect(f.runner.Run(t.Context(), input(t, "a.txt", "message 1"), docinfer.Options{Extract: &theExtract}), nil)
	if len(r.errs) != 1 || !r.last {
		t.Fatalf("errors %v, want one, alone and last", r.errs)
	}
	if !errors.Is(r.errs[0], full) || docinfer.Permanent(r.errs[0]) {
		t.Errorf("error %v, want the put's, not permanent", r.errs[0])
	}
	for _, u := range r.updates {
		if u.Inference != nil && u.Inference.TextSHA256 == failed {
			t.Error("the inference whose text failed to put was yielded")
		}
		if u.Done != nil {
			t.Error("a Done beside the error")
		}
	}
}

// Done counts each part ID once, by its last own event. A replay misses a dropped entry under
// b.txt after x.txt's events have replayed; the input, prepared again, finds x.txt's entry gone
// too, so x.txt's own event repeats, seen then prepared, and is counted once, prepared.
func TestRunDoneByLastEvent(t *testing.T) {
	f := newFixture(t, map[string]string{"x.txt": childText, "b.txt": "file c.txt", "c.txt": "leaf c"}, serve(t))
	const text = "file x.txt\nfile b.txt"
	first := collect(f.runner.Run(t.Context(), input(t, "a.txt", text), docinfer.Options{}), nil)
	first.done(t)

	if err := f.index.Drop(t.Context(), fixtureKey("leaf c")); err != nil {
		t.Fatal(err)
	}
	var drop sync.Once
	f.index.onClaim = func(key dispatch.Key) {
		if key == fixtureKey(text) {
			drop.Do(func() {
				if err := f.index.Drop(context.Background(), fixtureKey(childText)); err != nil {
					t.Error(err)
				}
			})
		}
	}
	r := collect(f.runner.Run(t.Context(), input(t, "a.txt", text), docinfer.Options{}), nil)
	d := r.done(t)
	var x []dispatch.Outcome
	for _, ev := range r.events() {
		if own(ev) && ev.Part.Name == "x.txt" {
			x = append(x, ev.Outcome)
		}
	}
	if want := []dispatch.Outcome{dispatch.Seen, dispatch.Prepared}; !slices.Equal(x, want) {
		t.Fatalf("x.txt's own events %v, want %v", x, want)
	}
	wantParts := map[dispatch.Outcome]int{dispatch.Prepared: 4}
	if !maps.Equal(d.Parts, wantParts) || d.Outputs != 4 {
		t.Errorf("Done parts %v, outputs %d; want %v, 4: each part once, by its last event", d.Parts, d.Outputs, wantParts)
	}
	if d.Counts.Texts != 6 {
		t.Errorf("Done counts %d texts, want the pipeline's 6, the input's and x.txt's lines twice", d.Counts.Texts)
	}
}
