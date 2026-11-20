package job_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"iter"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/standards-lab/docinfer"
	"github.com/standards-lab/docinfer/job"
	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/aitest"
)

// The failure tests stop jobs: by a content miss, by any other error the dispatch or the pipeline
// yields, and by a cancelled context or a loop stopped early. They order a job's steps by holding
// its index record or its embeddings until the test lets them go, never by waiting a while.

// lookup reports whether f's index holds an entry under key.
func lookup(t *testing.T, f *fixture, key dispatch.Key) bool {
	t.Helper()
	_, ok, err := f.index.Lookup(t.Context(), key)
	if err != nil {
		t.Fatal(err)
	}
	return ok
}

// A content miss drops the index entry of its output's part, under the part's SHA-256 and its
// event's stage, and stops the attempt with the miss, alone and last, not permanent. A first run
// prepares the input and its child x.txt; the content store then loses x.txt's output, so a
// second run replays both from the index and misses x.txt's content: x.txt's entry is dropped,
// the input's kept. A redelivery prepares x.txt again, putting its output back, and completes. A
// failed drop is joined to the miss, both matching, and drops nothing.
func TestRunContentMiss(t *testing.T) {
	for _, dropFails := range []bool{false, true} {
		t.Run(map[bool]string{false: "dropped", true: "drop fails"}[dropFails], func(t *testing.T) {
			f := newFixture(t, map[string]string{"x.txt": childText}, serve(t))
			// Two lines, so the input's lines output isn't x.txt's.
			const text = "file x.txt\nwords"
			in := input(t, "a.txt", text)
			first := collect(f.runner.Run(t.Context(), in, docinfer.Options{}), nil)
			first.done(t)
			var lost dispatch.Event // x.txt's output's event
			for _, ev := range first.events() {
				if ev.Output != nil && ev.Part.Name == "x.txt" {
					lost = ev
				}
			}
			if lost.Output == nil {
				t.Fatal("x.txt yielded no output")
			}
			f.content.lose(lost.Output.SHA256)
			dropErr := errors.New("the index refuses the drop")
			if dropFails {
				f.index.fail = map[string]error{"drop": dropErr}
			}

			r := collect(f.runner.Run(t.Context(), in, docinfer.Options{}), nil)
			if len(r.errs) != 1 || !r.last {
				t.Fatalf("errors %v, want one, alone and last", r.errs)
			}
			err := r.errs[0]
			miss, ok := errors.AsType[*docinfer.MissingError](err)
			if !ok || miss.Event.Output == nil || miss.Event.Output.SHA256 != lost.Output.SHA256 {
				t.Fatalf("error %v, want x.txt's content miss", err)
			}
			if !errors.Is(err, fs.ErrNotExist) || docinfer.Permanent(err) {
				t.Errorf("error %v, want one matching fs.ErrNotExist, not permanent", err)
			}
			if got := errors.Is(err, dropErr); got != dropFails {
				t.Errorf("error %v matches the drop's error %v, want %v", err, got, dropFails)
			}
			for _, u := range r.updates {
				if u.Done != nil {
					t.Error("a Done beside the miss")
				}
			}
			if got := lookup(t, f, fixtureKey(childText)); got != dropFails {
				t.Errorf("x.txt's entry held %v after the miss, want %v", got, dropFails)
			}
			if !lookup(t, f, fixtureKey(text)) {
				t.Error("the input's entry was dropped, want x.txt's alone")
			}
			if dropFails {
				return
			}

			// The redelivery: x.txt prepared again, its output put back, the run complete.
			again := collect(f.runner.Run(t.Context(), in, docinfer.Options{}), nil)
			d := again.done(t)
			if n := f.stage.Calls("x.txt"); n != 2 {
				t.Errorf("x.txt prepared %d times, want twice: once at first, once on the redelivery", n)
			}
			if want := map[dispatch.Outcome]int{dispatch.Prepared: 2}; !maps.Equal(d.Parts, want) {
				t.Errorf("the redelivery's parts %v, want %v", d.Parts, want)
			}
		})
	}
}

// Any other error the dispatch or the pipeline yields stops the job, alone and last, keeping what
// docinfer.Permanent reads of it: a mismatched input and a refused request are permanent, a claim
// lost and a store's failure are not, and a go-ai failure is as the failure table marks it.
func TestRunStopsOnError(t *testing.T) {
	storeDown := errors.New("the store is down")
	for _, tc := range []struct {
		name string
		// setup returns the job's fixture and its input.
		setup     func(t *testing.T) (*fixture, dispatch.Input)
		match     func(error) bool
		permanent bool
	}{
		{
			name: "sha256",
			setup: func(t *testing.T) (*fixture, dispatch.Input) {
				in := input(t, "a.txt", "words")
				in.SHA256 = sum("other words")
				return newFixture(t, nil, serve(t)), in
			},
			match:     func(err error) bool { return errors.Is(err, dispatch.ErrSHA256) },
			permanent: true,
		},
		{
			name: "request",
			setup: func(t *testing.T) (*fixture, dispatch.Input) {
				return newFixture(t, nil, serve(t)), dispatch.Input{} // neither Path nor Reader
			},
			match:     func(err error) bool { return errors.Is(err, dispatch.ErrRequest) },
			permanent: true,
		},
		{
			name: "claim lost",
			setup: func(t *testing.T) (*fixture, dispatch.Input) {
				f := newFixture(t, nil, serve(t))
				f.index.fail = map[string]error{"append": fmt.Errorf("the token lapsed: %w", dispatch.ErrClaimLost)}
				return f, input(t, "a.txt", "words")
			},
			match: func(err error) bool { return errors.Is(err, dispatch.ErrClaimLost) },
		},
		{
			name: "index",
			setup: func(t *testing.T) (*fixture, dispatch.Input) {
				f := newFixture(t, nil, serve(t))
				f.index.fail = map[string]error{"claim": storeDown}
				return f, input(t, "a.txt", "words")
			},
			match: func(err error) bool {
				s, ok := errors.AsType[*dispatch.StoreError](err)
				return ok && s.Store == "index" && errors.Is(err, storeDown)
			},
		},
		{
			name: "content",
			setup: func(t *testing.T) (*fixture, dispatch.Input) {
				f := newFixture(t, nil, serve(t))
				f.content.fail = map[string]error{sum("1"): storeDown} // the one-line input's lines output
				return f, input(t, "a.txt", "words")
			},
			match: func(err error) bool {
				s, ok := errors.AsType[*dispatch.StoreError](err)
				return ok && s.Store == "content" && errors.Is(err, storeDown)
			},
		},
		{
			name: "go-ai permanent",
			setup: func(t *testing.T) (*fixture, dispatch.Input) {
				return newFixture(t, nil, failing(t, 401)), input(t, "a.txt", "words")
			},
			match:     func(err error) bool { return apiStatus(err) == 401 },
			permanent: true,
		},
		{
			name: "go-ai transient",
			setup: func(t *testing.T) (*fixture, dispatch.Input) {
				return newFixture(t, nil, failing(t, 503)), input(t, "a.txt", "words")
			},
			match: func(err error) bool { return apiStatus(err) == 503 },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, in := tc.setup(t)
			r := collect(f.runner.Run(t.Context(), in, docinfer.Options{}), nil)
			if len(r.errs) != 1 || !r.last {
				t.Fatalf("errors %v, want one, alone and last", r.errs)
			}
			err := r.errs[0]
			if !tc.match(err) {
				t.Errorf("error %v, not the %s error", err, tc.name)
			}
			if got := docinfer.Permanent(err); got != tc.permanent {
				t.Errorf("Permanent(%v) = %v, want %v", err, got, tc.permanent)
			}
			for _, u := range r.updates {
				if u.Done != nil {
					t.Error("a Done beside the error")
				}
			}
		})
	}
}

// failing returns an endpoint that answers every embedding with an error of status.
func failing(t *testing.T, status int) *aitest.Endpoint {
	return aitest.Serve(t, aitest.Routes{
		Embeddings: func(context.Context, aitest.EmbeddingRequest) aitest.Reply {
			return aitest.ErrorReply{Status: status, Message: "no embeddings today"}
		},
	})
}

// apiStatus returns the status of err's *ai.APIError, or 0.
func apiStatus(err error) int {
	if apiErr, ok := errors.AsType[*ai.APIError](err); ok {
		return apiErr.Status
	}
	return 0
}

// until yields updates' updates until stop is closed: the first update after it is the last, and
// the loop over updates stops early there.
func until(stop <-chan struct{}, updates iter.Seq2[job.Update, error]) iter.Seq2[job.Update, error] {
	return func(yield func(job.Update, error) bool) {
		for u, err := range updates {
			if !yield(u, err) {
				return
			}
			select {
			case <-stop:
				return
			default:
			}
		}
	}
}

// within receives from ch, failing the test if nothing comes in failsafe.
func within[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(failsafe):
		t.Fatalf("%s: nothing in %v", what, failsafe)
		var zero T
		return zero
	}
}

// A job cancelled, or whose loop stops early, mid-flight stops its dispatch, its index claim,
// and its go-ai calls, and removes its files from the dispatcher's working directory, while
// another job on the same Runner goes on and completes. Job A, a Reader spooled into the working
// directory, is held with its input's record waiting and its two embeddings parked at the gate,
// its input's claim held and its part's directory made. Job B, of the same content, waits on A's
// claim. A is then cancelled, yielding ctx.Err() alone and last, or its loop stops at its next
// update, yielding nothing more; either way A's files are gone once its Run returns. B then
// claims the input, prepares it, replays x.txt, which A recorded, and completes with its Done.
// Run under -race, the test proves the two jobs share the Runner safely.
func TestRunStopsMidFlight(t *testing.T) {
	for _, how := range []string{"cancel", "stop"} {
		t.Run(how, func(t *testing.T) {
			var gate aitest.Gate
			e := aitest.Serve(t, aitest.Routes{
				Embeddings: func(ctx context.Context, req aitest.EmbeddingRequest) aitest.Reply {
					_ = gate.Wait(ctx) // a context ended lets the request go, unanswered by the client
					vectors := make([][]float32, len(req.Input))
					for i, text := range req.Input {
						vectors[i] = []float32{float32(len(text)), 1}
					}
					return aitest.EmbeddingsReply{Vectors: vectors, PromptTokens: len(req.Input)}
				},
			})
			f := newFixture(t, map[string]string{"x.txt": childText}, e)
			const text = "words\nfile x.txt"
			key := fixtureKey(text)

			held := make(chan struct{})    // A's record of the input is held
			waiting := make(chan struct{}) // B has begun to claim the input
			var records, claims atomic.Int32
			f.index.onRecord = func(ctx context.Context, k dispatch.Key) {
				if k != key || records.Add(1) != 1 {
					return
				}
				close(held)
				select {
				case <-ctx.Done():
				case <-time.After(failsafe):
					t.Error("job A's record was held past the failsafe")
				}
			}
			f.index.onClaim = func(k dispatch.Key) {
				if k == key && claims.Add(1) == 2 {
					close(waiting)
				}
			}

			ctxA, cancelA := context.WithCancel(t.Context())
			defer cancelA()
			stopA := make(chan struct{})
			aDone, bDone := make(chan run, 1), make(chan run, 1)
			go func() {
				in := dispatch.Input{Reader: strings.NewReader(text), Name: "a.txt"}
				aDone <- collect(until(stopA, f.runner.Run(ctxA, in, docinfer.Options{})), nil)
			}()
			gate.Await(t, 2) // the input's lines and x.txt's
			within(t, held, "job A's record of the input")
			go func() {
				bDone <- collect(f.runner.Run(t.Context(), input(t, "b.txt", text), docinfer.Options{}), nil)
			}()
			within(t, waiting, "job B's claim of the input")

			// A is mid-flight: B, waiting on A's claim, has made nothing yet, so the working
			// directory holds A's files alone.
			entries, err := os.ReadDir(f.dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) == 0 {
				t.Fatal("job A holds no files in the working directory mid-flight")
			}
			switch how {
			case "cancel":
				cancelA()
			case "stop":
				close(stopA)
				gate.Release(1) // an embedding answered makes A's next update, where its loop stops
			}

			a := within(t, aDone, "job A's end")
			for _, u := range a.updates {
				if u.Done != nil {
					t.Error("job A yielded a Done")
				}
			}
			switch how {
			case "cancel":
				if len(a.errs) != 1 || !a.last || a.errs[0] != ctxA.Err() {
					t.Errorf("job A's errors %v, want its context's error %v alone, last", a.errs, ctxA.Err())
				}
			case "stop":
				if len(a.errs) != 0 {
					t.Errorf("job A's errors %v after its loop stopped, want none", a.errs)
				}
			}
			for _, entry := range entries {
				if _, err := os.Stat(filepath.Join(f.dir, entry.Name())); !errors.Is(err, fs.ErrNotExist) {
					t.Errorf("job A's %s is still in the working directory: %v", entry.Name(), err)
				}
			}

			gate.Open()
			b := within(t, bDone, "job B's end")
			d := b.done(t)
			want := map[dispatch.Outcome]int{dispatch.Prepared: 1, dispatch.Seen: 1}
			if !maps.Equal(d.Parts, want) || d.Outputs != 2 {
				t.Errorf("job B's parts %v, outputs %d; want %v, 2", d.Parts, d.Outputs, want)
			}
			if c := d.Counts; c.Texts != 2 || c.Metadata != 1 {
				t.Errorf("job B's counts %+v, want 2 texts and the metadata", c)
			}
			if left, err := os.ReadDir(f.dir); err != nil || len(left) != 0 {
				t.Errorf("the working directory holds %v, %v once both jobs are done, want nothing", left, err)
			}
		})
	}
}
