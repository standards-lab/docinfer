package docinfer_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/standards-lab/docinfer"
	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/aitest"
)

const (
	joined   = "Alpha bravo charlie delta."
	overlaps = joined + "\n\nEcho foxtrot golf hotel.\n\nIndia juliet kilo lima."
	unshared = "Mike november oscar papa."
)

type joinEndpoint struct {
	*aitest.Endpoint
	gate *aitest.Gate

	mu       sync.Mutex
	requests [][]string
}

func serveJoin(t *testing.T, reply func(context.Context, aitest.EmbeddingRequest) aitest.Reply) *joinEndpoint {
	t.Helper()
	if reply == nil {
		reply = embedAll
	}
	e := &joinEndpoint{gate: &aitest.Gate{}}
	e.Endpoint = aitest.Serve(t, aitest.Routes{Embeddings: func(ctx context.Context, req aitest.EmbeddingRequest) aitest.Reply {
		e.mu.Lock()
		e.requests = append(e.requests, req.Input)
		e.mu.Unlock()
		if err := e.gate.Wait(ctx); err != nil {
			return aitest.ErrorReply{Status: 503, Message: err.Error()}
		}
		return reply(ctx, req)
	}})
	return e
}

func (e *joinEndpoint) asked() [][]string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return slices.Clone(e.requests)
}

func joinPipeline(t *testing.T, e *joinEndpoint, bound int, edit func(*docinfer.Config)) (*docinfer.Pipeline, <-chan struct{}) {
	t.Helper()
	p := newPipeline(t, runConfig(t, e.Endpoint, func(c *docinfer.Config) {
		c.Bounds = newBounds(t, 1, 1, 1, bound)
		if edit != nil {
			edit(c)
		}
	}))
	waits := make(chan struct{}, 64)
	docinfer.OnWait(p, func() { waits <- struct{}{} })
	return p, waits
}

func awaitWaiters(t *testing.T, waits <-chan struct{}, n int) {
	t.Helper()
	timeout := time.After(failsafe)
	for i := range n {
		select {
		case <-waits:
		case <-timeout:
			t.Fatalf("%d of %d waiters waiting", i, n)
		}
	}
}

type job struct {
	cancel   context.CancelFunc
	done     chan outcome
	panicked chan any
}

func startJob(t *testing.T, p *docinfer.Pipeline, content *dispatch.MemoryContent, text string) *job {
	t.Helper()
	return startRun(t, p, content, outputInput(t, content, childPart, dispatch.TextOutput, text), docinfer.Options{})
}

func startRun(t *testing.T, p *docinfer.Pipeline, content *dispatch.MemoryContent, in docinfer.Input, opts docinfer.Options) *job {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	j := &job{cancel: cancel, done: make(chan outcome, 1), panicked: make(chan any, 1)}
	go func() {
		defer func() {
			if v := recover(); v != nil {
				j.panicked <- v
			}
		}()
		j.done <- collect(t, p.Run(ctx, content, seq(in), opts))
	}()
	return j
}

func (j *job) outcome(t *testing.T) outcome {
	t.Helper()
	return receive(t, j.done, "outcome of a job")
}

func vectors(o outcome) map[string]string {
	m := map[string]string{}
	for _, c := range o.chunks() {
		m[c.chunk.Text] = c.chunk.Vector + c.chunk.Error
	}
	return m
}

func TestJoinOneRequestPerKey(t *testing.T) {
	e := serveJoin(t, nil)
	p, waits := joinPipeline(t, e, 2, nil)
	content := dispatch.NewMemoryContent()

	leader := startJob(t, p, content, plainText)
	e.gate.Await(t, 1)
	waiter := startJob(t, p, content, plainText)
	awaitWaiters(t, waits, 1)
	other := startJob(t, p, content, unshared)
	e.gate.Await(t, 2)
	e.gate.Open()

	lo, wo, oo := leader.outcome(t), waiter.outcome(t), other.outcome(t)
	for _, o := range []outcome{lo, wo, oo} {
		if o.err != nil {
			t.Fatalf("Run: %v", o.err)
		}
	}
	asked := e.asked()
	if len(asked) != 2 {
		t.Fatalf("%d requests %q, want 2: the leader's and the other job's", len(asked), asked)
	}
	seen := map[string]int{}
	for _, texts := range asked {
		for _, text := range texts {
			seen[text]++
		}
	}
	for text, n := range seen {
		if n != 1 {
			t.Errorf("%q asked %d times, want once", text, n)
		}
	}
	checkCounts(t, lo, docinfer.Counts{Texts: 1, Embedded: 3, Requests: 1, Tokens: docinfer.Tokens{Embeddings: docinfer.Usage{Input: 9}}})
	checkCounts(t, wo, docinfer.Counts{Texts: 1, Embedded: 3, Held: 3})
	if lv, wv := vectors(lo), vectors(wo); !mapsEqual(lv, wv) {
		t.Errorf("the waiter's chunks %v aren't the leader's %v", wv, lv)
	}
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}

func TestJoinLeaderCancelled(t *testing.T) {
	e := serveJoin(t, nil)
	p, waits := joinPipeline(t, e, 2, nil)
	content := dispatch.NewMemoryContent()

	leader := startJob(t, p, content, joined)
	e.gate.Await(t, 1)
	waiter := startJob(t, p, content, joined)
	awaitWaiters(t, waits, 1)
	leader.cancel()
	if o := leader.outcome(t); !errors.Is(o.err, context.Canceled) {
		t.Fatalf("the cancelled leader yielded %v, want context.Canceled", o.err)
	}
	e.gate.Open()

	wo := waiter.outcome(t)
	if wo.err != nil {
		t.Fatalf("the waiter's Run: %v", wo.err)
	}
	checkCounts(t, wo, docinfer.Counts{Texts: 1, Embedded: 1, Requests: 1, Tokens: docinfer.Tokens{Embeddings: docinfer.Usage{Input: 3}}})
	if asked := e.asked(); len(asked) != 2 {
		t.Errorf("%d requests %q, want 2: the cancelled leader's and the waiter's", len(asked), asked)
	}
}

func TestJoinLeaderTransientFailure(t *testing.T) {
	e := serveJoin(t, func(ctx context.Context, req aitest.EmbeddingRequest) aitest.Reply {
		if req.Arrival == 1 {
			return aitest.ErrorReply{Status: 503, Message: "overloaded"}
		}
		return embedAll(ctx, req)
	})
	p, waits := joinPipeline(t, e, 2, nil)
	content := dispatch.NewMemoryContent()

	leader := startJob(t, p, content, joined)
	e.gate.Await(t, 1)
	waiter := startJob(t, p, content, joined)
	awaitWaiters(t, waits, 1)
	e.gate.Open()

	lo, wo := leader.outcome(t), waiter.outcome(t)
	if apiErr, ok := errors.AsType[*ai.APIError](lo.err); !ok || apiErr.Status != 503 || docinfer.Permanent(lo.err) {
		t.Errorf("the leader yielded %v, want its transient 503", lo.err)
	}
	if wo.err != nil {
		t.Fatalf("the waiter's Run: %v", wo.err)
	}
	checkCounts(t, wo, docinfer.Counts{Texts: 1, Embedded: 1, Requests: 1, Tokens: docinfer.Tokens{Embeddings: docinfer.Usage{Input: 3}}})
	if asked := e.asked(); len(asked) != 2 {
		t.Errorf("%d requests %q, want 2: the leader's and the waiter's", len(asked), asked)
	}
}

func TestJoinWaiterCancelled(t *testing.T) {
	e := serveJoin(t, nil)
	p, waits := joinPipeline(t, e, 2, nil)
	content := dispatch.NewMemoryContent()

	leader := startJob(t, p, content, joined)
	e.gate.Await(t, 1)
	waiter := startJob(t, p, content, joined)
	awaitWaiters(t, waits, 1)
	waiter.cancel()
	wo := waiter.outcome(t)
	if !errors.Is(wo.err, context.Canceled) || len(wo.inputs) != 1 || wo.results != 2 {
		t.Errorf("the cancelled waiter yielded %d results, %d echoes, %v, want its input's echo, then context.Canceled alone",
			wo.results, len(wo.inputs), wo.err)
	}
	if n := e.gate.Parked(); n != 1 {
		t.Errorf("%d requests parked once the waiter returned, want the leader's 1", n)
	}
	e.gate.Open()

	lo := leader.outcome(t)
	if lo.err != nil {
		t.Fatalf("the leader's Run: %v", lo.err)
	}
	checkCounts(t, lo, docinfer.Counts{Texts: 1, Embedded: 1, Requests: 1, Tokens: docinfer.Tokens{Embeddings: docinfer.Usage{Input: 3}}})
}

type panicPut struct {
	*docinfer.MemoryCache
	once atomic.Bool
}

var errPutDefect = errors.New("put: a defect in the host's store")

func (c *panicPut) Put(ctx context.Context, key string, value []byte) error {
	if c.once.CompareAndSwap(false, true) {
		panic(errPutDefect)
	}
	return c.MemoryCache.Put(ctx, key, value)
}

func TestJoinLeaderPanics(t *testing.T) {
	e := serveJoin(t, nil)
	cache := &panicPut{MemoryCache: docinfer.NewMemoryCache()}
	p, waits := joinPipeline(t, e, 2, func(c *docinfer.Config) { c.Cache = cache })
	content := dispatch.NewMemoryContent()

	leader := startJob(t, p, content, joined)
	e.gate.Await(t, 1)
	waiter := startJob(t, p, content, joined)
	awaitWaiters(t, waits, 1)
	e.gate.Open()

	if v := receive(t, leader.panicked, "panic of the leader"); v != errPutDefect {
		t.Errorf("the leader's Run panicked with %v, want the cache's %v", v, errPutDefect)
	}
	wo := waiter.outcome(t)
	if wo.err != nil {
		t.Fatalf("the waiter's Run: %v", wo.err)
	}
	checkCounts(t, wo, docinfer.Counts{Texts: 1, Embedded: 1, Requests: 1, Tokens: docinfer.Tokens{Embeddings: docinfer.Usage{Input: 3}}})
	if asked := e.asked(); len(asked) != 2 {
		t.Errorf("%d requests %q, want 2: the panicking leader's and the waiter's", len(asked), asked)
	}
}

func TestJoinBatchPartlyInFlight(t *testing.T) {
	e := serveJoin(t, nil)
	p, waits := joinPipeline(t, e, 2, nil)
	content := dispatch.NewMemoryContent()

	leader := startJob(t, p, content, joined)
	e.gate.Await(t, 1)
	batch := startJob(t, p, content, overlaps)
	awaitWaiters(t, waits, 1)
	e.gate.Await(t, 2)
	asked := e.asked()
	want := [][]string{{joined}, {"Echo foxtrot golf hotel.", "India juliet kilo lima."}}
	if !slices.EqualFunc(asked, want, slices.Equal) {
		t.Errorf("requests %q, want %q: the overlapping batch asks only for what isn't in flight", asked, want)
	}
	e.gate.Open()

	lo, bo := leader.outcome(t), batch.outcome(t)
	if lo.err != nil || bo.err != nil {
		t.Fatalf("Run: %v, %v", lo.err, bo.err)
	}
	checkCounts(t, bo, docinfer.Counts{Texts: 1, Embedded: 3, Held: 1, Requests: 1, Tokens: docinfer.Tokens{Embeddings: docinfer.Usage{Input: 6}}})
	if lv, bv := vectors(lo), vectors(bo); bv[joined] != lv[joined] || bv[joined] == "" {
		t.Errorf("the batch's waited-for chunk is %q, want the leader's %q", bv[joined], lv[joined])
	}
}

func TestJoinSharesRefusalAndPermanentFailure(t *testing.T) {
	cases := []struct {
		name   string
		status int
		check  func(t *testing.T, leader, waiter outcome)
	}{
		{"refusal", 422, func(t *testing.T, lo, wo outcome) {
			if lo.err != nil || wo.err != nil {
				t.Fatalf("Run: %v, %v", lo.err, wo.err)
			}
			checkCounts(t, lo, docinfer.Counts{Texts: 1, Rejected: 1, Requests: 1})
			checkCounts(t, wo, docinfer.Counts{Texts: 1, Rejected: 1, Held: 1})
			if lv, wv := vectors(lo), vectors(wo); wv[joined] == "" || wv[joined] != lv[joined] {
				t.Errorf("the waiter's chunk error %q, want the leader's %q", wv[joined], lv[joined])
			}
		}},
		{"permanent failure", 401, func(t *testing.T, lo, wo outcome) {
			for _, o := range []outcome{lo, wo} {
				if apiErr, ok := errors.AsType[*ai.APIError](o.err); !ok || apiErr.Status != 401 || !docinfer.Permanent(o.err) {
					t.Errorf("a job yielded %v, want the permanent 401", o.err)
				}
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := serveJoin(t, func(context.Context, aitest.EmbeddingRequest) aitest.Reply {
				return aitest.ErrorReply{Status: tc.status, Message: "no"}
			})
			p, waits := joinPipeline(t, e, 2, nil)
			content := dispatch.NewMemoryContent()

			leader := startJob(t, p, content, joined)
			e.gate.Await(t, 1)
			waiter := startJob(t, p, content, joined)
			awaitWaiters(t, waits, 1)
			e.gate.Open()

			tc.check(t, leader.outcome(t), waiter.outcome(t))
			if asked := e.asked(); len(asked) != 1 {
				t.Errorf("%d requests %q, want the leader's alone", len(asked), asked)
			}
		})
	}
}
