package docinfer_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/standards-lab/docinfer"
	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/aitest"
)

// The bound tests drive Bounds through docinfer.BoundsDo, the runner every model request goes
// through, with go-ai embedding calls against aitest, so each of the four capabilities' bounds is
// proven by the same call, though Pipeline.Run reaches all four.

// gatedEmbeddings is an aitest embeddings route that parks each request at gate, counting the
// requests inside it at once and the most it has seen.
type gatedEmbeddings struct {
	gate *aitest.Gate
	mu   sync.Mutex
	in   int
	most int
}

func (g *gatedEmbeddings) route(ctx context.Context, req aitest.EmbeddingRequest) aitest.Reply {
	g.mu.Lock()
	g.in++
	g.most = max(g.most, g.in)
	g.mu.Unlock()
	err := g.gate.Wait(ctx)
	// The count drops before the reply is written, so before the caller's slot is freed.
	g.mu.Lock()
	g.in--
	g.mu.Unlock()
	if err != nil {
		return aitest.ErrorReply{Status: 500, Message: err.Error()}
	}
	vectors := make([][]float32, len(req.Input))
	for i := range vectors {
		vectors[i] = []float32{1}
	}
	return aitest.EmbeddingsReply{Vectors: vectors}
}

// mostInside returns the most requests the route has held at once.
func (g *gatedEmbeddings) mostInside() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.most
}

// embed returns a call that embeds one text through client, as BoundsDo runs it.
func embed(client *ai.Client) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		_, err := client.Embed(ctx, &ai.EmbeddingRequest{Model: "m", Input: []string{"text"}})
		return err
	}
}

// Each capability's requests in flight reach its bound and never pass it, across two clients
// standing for two pipelines on one Bounds: requests park at the Gate until the bound is reached,
// and each one released lets exactly one more in.
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
			route := &gatedEmbeddings{gate: &aitest.Gate{}}
			e := aitest.Serve(t, aitest.Routes{Embeddings: route.route})
			clients := []*ai.Client{newClient(t, e.URL(), ai.TranscribeRoute), newClient(t, e.URL(), ai.TranscribeRoute)}

			total := 3*bound + 1
			errs := make(chan error, total)
			for i := range total {
				go func() { errs <- docinfer.BoundsDo(b, t.Context(), capability, embed(clients[i%2])) }()
			}
			for done := range total {
				want := min(bound, total-done)
				route.gate.Await(t, want)
				if parked := route.gate.Parked(); parked != want {
					t.Fatalf("after %d requests done, %d parked, want the bound's %d", done, parked, want)
				}
				route.gate.Release(1)
			}
			for range total {
				if err := <-errs; err != nil {
					t.Errorf("a bounded request: %v", err)
				}
			}
			if most := route.mostInside(); most != bound {
				t.Errorf("at most %d requests in flight at once, want the bound %d", most, bound)
			}
			if n := len(e.Requests()); n != total {
				t.Errorf("the endpoint received %d requests, want %d", n, total)
			}
		})
	}
}

// A capability's bound holds its own slots alone: with vision's one slot held, an embeddings
// request goes on.
func TestBoundsArePerCapability(t *testing.T) {
	b := newBounds(t, 1, 1, 1, 1)
	gated := &gatedEmbeddings{gate: &aitest.Gate{}}
	held := newClient(t, aitest.Serve(t, aitest.Routes{Embeddings: gated.route}).URL(), ai.TranscribeRoute)
	free := newClient(t, aitest.Serve(t, aitest.Routes{Embeddings: (&gatedEmbeddings{gate: openGate()}).route}).URL(), ai.TranscribeRoute)

	errs := make(chan error, 1)
	go func() { errs <- docinfer.BoundsDo(b, t.Context(), docinfer.CapabilityVision, embed(held)) }()
	gated.gate.Await(t, 1)
	// The deadline ends the test, rather than hang it, if vision's held slot were embeddings'.
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if err := docinfer.BoundsDo(b, ctx, docinfer.CapabilityEmbeddings, embed(free)); err != nil {
		t.Errorf("an embeddings request beside a held vision slot: %v", err)
	}
	gated.gate.Open()
	if err := <-errs; err != nil {
		t.Errorf("the held vision request: %v", err)
	}
}

// openGate returns a Gate that parks nothing.
func openGate() *aitest.Gate {
	g := &aitest.Gate{}
	g.Open()
	return g
}

// A request waiting for a slot returns its context's error once it ends, and is never sent.
func TestBoundsWaitEndsWithTheContext(t *testing.T) {
	b := newBounds(t, 1, 1, 1, 1)
	route := &gatedEmbeddings{gate: &aitest.Gate{}}
	e := aitest.Serve(t, aitest.Routes{Embeddings: route.route})
	client := newClient(t, e.URL(), ai.TranscribeRoute)

	errs := make(chan error, 1)
	go func() { errs <- docinfer.BoundsDo(b, t.Context(), docinfer.CapabilityVision, embed(client)) }()
	route.gate.Await(t, 1)
	ctx, cancel := context.WithCancel(t.Context())
	waiting := make(chan error, 1)
	go func() { waiting <- docinfer.BoundsDo(b, ctx, docinfer.CapabilityVision, embed(client)) }()
	cancel()
	if err := <-waiting; !errors.Is(err, context.Canceled) {
		t.Errorf("a request waiting for a slot, its context cancelled: %v, want context.Canceled", err)
	}
	route.gate.Open()
	if err := <-errs; err != nil {
		t.Errorf("the request holding the slot: %v", err)
	}
	if n := len(e.Requests()); n != 1 {
		t.Errorf("the endpoint received %d requests, want only the slot holder's", n)
	}
}

// A call that panics frees its slot, so the next request runs.
func TestBoundsFreeTheSlotOnPanic(t *testing.T) {
	b := newBounds(t, 1, 1, 1, 1)
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("the panicking call didn't panic through BoundsDo")
			}
		}()
		_ = docinfer.BoundsDo(b, t.Context(), docinfer.CapabilityEmbeddings, func(context.Context) error { panic("call") })
	}()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if err := docinfer.BoundsDo(b, ctx, docinfer.CapabilityEmbeddings, func(context.Context) error { return nil }); err != nil {
		t.Errorf("the request after a panic: %v", err)
	}
}
