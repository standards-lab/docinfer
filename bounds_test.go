package docinfer_test

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"maps"
	"net/http"
	"slices"
	"sync"
	"testing"

	"github.com/standards-lab/docinfer"
	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/aitest"
	"github.com/standards-lab/go-ai/format/completions"
)

type boundedRoute struct {
	gate aitest.Gate

	mu   sync.Mutex
	in   int
	most int
	sent int
}

func (b *boundedRoute) enter(ctx context.Context) error {
	b.mu.Lock()
	b.in++
	b.sent++
	b.most = max(b.most, b.in)
	b.mu.Unlock()
	err := b.gate.Wait(ctx)
	b.mu.Lock()
	b.in--
	b.mu.Unlock()
	return err
}

func (b *boundedRoute) counts() (most, sent int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.most, b.sent
}

func boundEndpoint(t *testing.T, capability docinfer.Capability, route *boundedRoute) *aitest.Endpoint {
	t.Helper()
	gated := func(c docinfer.Capability, ctx context.Context) aitest.Reply {
		if c != capability {
			return nil
		}
		if err := route.enter(ctx); err != nil {
			return aitest.ErrorReply{Status: 503, Message: err.Error()}
		}
		return nil
	}
	return aitest.Serve(t, aitest.Routes{
		ChatCompletions: func(ctx context.Context, req aitest.ChatRequest) aitest.Reply {
			if req.Stream {
				if r := gated(docinfer.CapabilityExtraction, ctx); r != nil {
					return r
				}
				return aitest.ChatReply{Chunks: extractPieces}
			}
			if r := gated(docinfer.CapabilityVision, ctx); r != nil {
				return r
			}
			return aitest.ChatReply{Text: description}
		},
		Transcriptions: func(ctx context.Context, _ aitest.TranscriptionRequest) aitest.Reply {
			if r := gated(docinfer.CapabilityTranscription, ctx); r != nil {
				return r
			}
			return aitest.TranscriptionReply{Text: said0}
		},
		Embeddings: func(ctx context.Context, req aitest.EmbeddingRequest) aitest.Reply {
			if r := gated(docinfer.CapabilityEmbeddings, ctx); r != nil {
				return r
			}
			return embedAll(ctx, req)
		},
	})
}

func boundConfig(t *testing.T, e *aitest.Endpoint, bounds *docinfer.Bounds) docinfer.Config {
	t.Helper()
	return runConfig(t, e, func(c *docinfer.Config) {
		c.Vision = &docinfer.Vision{Client: newClient(t, e.URL(), ai.TranscribeRoute), Model: visionModel, Served: visionServed, Prompt: visionPrompt, MaxTokens: visionTokens}
		c.Transcription = &docinfer.Transcription{Client: newClient(t, e.URL(), ai.TranscribeRoute), Model: transcriptionModel, Served: transcriptionServed, Language: transcriptionLang}
		c.Extraction = &docinfer.Extraction{Client: newClient(t, e.URL(), ai.TranscribeRoute), Model: extractModel, Served: extractServed, MaxTokens: extractTokens, WindowRunes: extractWindow}
		c.Bounds = bounds
		c.Pending = 64
		c.EmbedBatch = 1
	})
}

func boundInput(t *testing.T, content *dispatch.MemoryContent, capability docinfer.Capability, i int) docinfer.Input {
	t.Helper()
	switch capability {
	case docinfer.CapabilityVision:
		return imageInput(t, content, fmt.Appendf(nil, "image %d", i), imageType)
	case docinfer.CapabilityTranscription:
		in := windowInput(t, content, win{index: 0, clip: fmt.Sprintf("clip %d", i)})
		in.Event.Part.ID = fmt.Sprintf("/talk-%d.mp3", i)
		return in
	default:
		return outputInput(t, content, childPart, dispatch.TextOutput, fmt.Sprintf("Text number %d.", i))
	}
}

func runJob(t *testing.T, ctx context.Context, p *docinfer.Pipeline, content dispatch.Content, inputs iter.Seq2[docinfer.Input, error], opts docinfer.Options) <-chan outcome {
	done := make(chan outcome, 1)
	go func() { done <- collect(t, p.Run(ctx, content, inputs, opts)) }()
	return done
}

func slotWaits(b *docinfer.Bounds, capability docinfer.Capability) <-chan struct{} {
	waits := make(chan struct{}, 64)
	docinfer.OnSlotWait(b, func(c docinfer.Capability) {
		if c == capability {
			waits <- struct{}{}
		}
	})
	return waits
}

func TestBoundsCapRequestsInFlight(t *testing.T) {
	bounds := map[docinfer.Capability]int{
		docinfer.CapabilityVision:        2,
		docinfer.CapabilityTranscription: 3,
		docinfer.CapabilityExtraction:    1,
		docinfer.CapabilityEmbeddings:    4,
	}
	for capability, bound := range bounds {
		t.Run(string(capability), func(t *testing.T) {
			b := newBounds(t, bounds[docinfer.CapabilityVision], bounds[docinfer.CapabilityTranscription],
				bounds[docinfer.CapabilityExtraction], bounds[docinfer.CapabilityEmbeddings])
			waits := slotWaits(b, capability)
			route := &boundedRoute{}
			e := boundEndpoint(t, capability, route)
			var opts docinfer.Options
			if capability == docinfer.CapabilityExtraction {
				opts = extracting(false)
			}

			total := 3*bound + 1
			content := dispatch.NewMemoryContent()
			inputs := make([][]docinfer.Input, 2)
			for i := range total {
				inputs[i%2] = append(inputs[i%2], boundInput(t, content, capability, i))
			}
			var jobs []<-chan outcome
			for _, in := range inputs {
				p := newPipeline(t, boundConfig(t, e, b))
				jobs = append(jobs, runJob(t, t.Context(), p, content, seq(in...), opts))
			}
			route.gate.Await(t, bound)
			awaitWaiters(t, waits, total-bound)
			for done := range total {
				want := min(bound, total-done)
				route.gate.Await(t, want)
				if parked := route.gate.Parked(); parked != want {
					t.Fatalf("after %d requests done, %d parked, want the bound's %d", done, parked, want)
				}
				route.gate.Release(1)
			}
			for _, job := range jobs {
				if o := receive(t, job, "outcome of a job"); o.err != nil {
					t.Errorf("a bounded job: %v", o.err)
				}
			}
			if most, sent := route.counts(); most != bound || sent != total {
				t.Errorf("%d %s requests, at most %d at once; want %d, at most the bound %d", sent, capability, most, total, bound)
			}
		})
	}
}

func TestBoundsArePerCapability(t *testing.T) {
	capabilities := []docinfer.Capability{
		docinfer.CapabilityVision, docinfer.CapabilityTranscription, docinfer.CapabilityExtraction, docinfer.CapabilityEmbeddings,
	}
	for _, held := range capabilities {
		t.Run(string(held), func(t *testing.T) {
			route := &boundedRoute{}
			e := boundEndpoint(t, held, route)
			p := newPipeline(t, boundConfig(t, e, newBounds(t, 1, 1, 1, 1)))
			content := dispatch.NewMemoryContent()
			inputs := seq(boundInput(t, content, docinfer.CapabilityVision, 0), boundInput(t, content, docinfer.CapabilityTranscription, 0),
				boundInput(t, content, docinfer.CapabilityEmbeddings, 0))

			made := make(chan docinfer.Capability, 64)
			results := func(yield func(docinfer.Result, error) bool) {
				for r, err := range p.Run(t.Context(), content, inputs, extracting(false)) {
					switch {
					case r.Inference != nil:
						made <- r.Inference.Capability
					case r.Batch != nil && slices.ContainsFunc(r.Batch.Sources, func(s docinfer.Source) bool { return s.Kind == "output" }):
						made <- docinfer.CapabilityEmbeddings
					}
					if !yield(r, err) {
						return
					}
				}
			}
			done := make(chan outcome, 1)
			go func() { done <- collect(t, results) }()
			route.gate.Await(t, 1)
			others := map[docinfer.Capability]bool{}
			for _, c := range capabilities {
				if c != held {
					others[c] = true
				}
			}
			for len(others) > 0 {
				delete(others, receive(t, made, fmt.Sprintf("result of %v while %s's slot is held", slices.Sorted(maps.Keys(others)), held)))
			}
			route.gate.Open()
			if o := receive(t, done, "outcome of the job"); o.err != nil {
				t.Errorf("Run: %v", o.err)
			}
		})
	}
}

func TestBoundsWaitEndsWithTheContext(t *testing.T) {
	route := &boundedRoute{}
	e := boundEndpoint(t, docinfer.CapabilityEmbeddings, route)
	b := newBounds(t, 1, 1, 1, 1)
	waits := slotWaits(b, docinfer.CapabilityEmbeddings)
	content := dispatch.NewMemoryContent()

	holder := runJob(t, t.Context(), newPipeline(t, boundConfig(t, e, b)), content,
		seq(boundInput(t, content, docinfer.CapabilityEmbeddings, 0)), docinfer.Options{})
	route.gate.Await(t, 1)
	ctx, cancel := context.WithCancel(t.Context())
	waiter := runJob(t, ctx, newPipeline(t, boundConfig(t, e, b)), content,
		seq(boundInput(t, content, docinfer.CapabilityEmbeddings, 1)), docinfer.Options{})
	awaitWaiters(t, waits, 1)
	cancel()
	if o := receive(t, waiter, "outcome of the cancelled job"); !errors.Is(o.err, context.Canceled) {
		t.Errorf("a job waiting for a slot, its context cancelled: %v, want context.Canceled", o.err)
	}
	route.gate.Open()
	if o := receive(t, holder, "outcome of the job holding the slot"); o.err != nil {
		t.Errorf("the job holding the slot: %v", o.err)
	}
	if _, sent := route.counts(); sent != 1 {
		t.Errorf("the endpoint received %d embeddings requests, want only the slot holder's", sent)
	}
}

type panicTransport struct{}

var errTransportDefect = errors.New("round trip: a defect in the host's transport")

func (panicTransport) RoundTrip(*http.Request) (*http.Response, error) { panic(errTransportDefect) }

func TestBoundsFreeTheSlotOnPanic(t *testing.T) {
	e := boundEndpoint(t, "", nil)
	b := newBounds(t, 1, 1, 1, 1)
	content := dispatch.NewMemoryContent()
	in := seq(boundInput(t, content, docinfer.CapabilityEmbeddings, 0))

	client, err := ai.New(e.URL(), &http.Client{Transport: panicTransport{}}, completions.New(completions.Options{}), ai.Options{})
	if err != nil {
		t.Fatal(err)
	}
	cfg := boundConfig(t, e, b)
	cfg.Embeddings.Client = client
	var panicked any
	func() {
		defer func() { panicked = recover() }()
		collect(t, newPipeline(t, cfg).Run(t.Context(), content, in, docinfer.Options{}))
	}()
	if v := panicked; v != errTransportDefect {
		t.Fatalf("Run over a panicking transport panicked with %v, want the transport's %v", v, errTransportDefect)
	}
	next := runJob(t, t.Context(), newPipeline(t, boundConfig(t, e, b)), content, in, docinfer.Options{})
	if o := receive(t, next, "outcome of the job after the panic"); o.err != nil {
		t.Errorf("the job after the panic: %v", o.err)
	}
}
