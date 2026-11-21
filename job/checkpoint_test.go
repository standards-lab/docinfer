package job_test

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/docinfer"
	"github.com/standards-lab/docinfer/job"
	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/go-ai/aitest"
)

func messages(n int) string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = "message " + strconv.Itoa(i+1)
	}
	return strings.Join(lines, "\n")
}

func (r run) checkpoints() []uint64 {
	var out []uint64
	for _, u := range r.updates {
		if u.Checkpoint != nil {
			out = append(out, u.Checkpoint.After)
		}
	}
	return out
}

func messagesOf(updates []job.Update) []uint64 {
	var out []uint64
	for _, u := range updates {
		if ev := u.Event; ev != nil && ev.Output != nil && ev.After != 0 {
			out = append(out, ev.After)
		}
	}
	return out
}

func checkCovered(t *testing.T, r run) {
	t.Helper()
	for i, u := range r.updates {
		if u.Checkpoint == nil {
			continue
		}
		var outputs []string
		reached := false
		for _, v := range r.updates[:i] {
			if ev := v.Event; ev != nil && ev.Output != nil {
				outputs = append(outputs, ev.Output.SHA256)
				if ev.After == u.Checkpoint.After {
					reached = true
					break
				}
			}
		}
		if !reached {
			t.Errorf("update %d, a checkpoint after %d, precedes its message's event", i, u.Checkpoint.After)
			continue
		}
		batched := map[string]bool{}
		for _, v := range r.updates[:i] {
			if v.Batch != nil {
				for _, s := range v.Batch.Sources {
					batched[sourceSHA(s)] = true
				}
			}
		}
		for _, sha := range outputs {
			if !batched[sha] {
				t.Errorf("update %d, a checkpoint after %d, precedes the batch of the output %s it covers", i, u.Checkpoint.After, sha)
			}
		}
	}
}

func holdPuts(t *testing.T, f *fixture, holds map[string]chan struct{}) {
	f.content.onPut = func(ctx context.Context, sha string) {
		hold, ok := holds[sha]
		if !ok {
			return
		}
		select {
		case <-hold:
		case <-ctx.Done():
		case <-time.After(failsafe):
			t.Errorf("the put of %s was held past the failsafe", sha)
		}
	}
}

func TestRunCheckpointEvery(t *testing.T) {
	for _, tc := range []struct {
		every, messages int
		want            []uint64
	}{
		{every: 1, messages: 3, want: []uint64{1, 2, 3}},
		{every: 2, messages: 4, want: []uint64{2, 4}},
		{every: 2, messages: 5, want: []uint64{2, 4}},
		{every: 3, messages: 4, want: []uint64{3}},
	} {
		t.Run(strconv.Itoa(tc.every)+" of "+strconv.Itoa(tc.messages), func(t *testing.T) {
			f := newFixtureEvery(t, nil, serve(t), tc.every)
			holds := map[string]chan struct{}{}
			for _, after := range tc.want {
				holds[sum("message "+strconv.FormatUint(after+1, 10))] = make(chan struct{})
			}
			holdPuts(t, f, holds)
			r := collect(f.runner.Run(t.Context(), input(t, "store.txt", messages(tc.messages)), docinfer.Options{}), func(u job.Update) {
				if u.Checkpoint != nil {
					if hold, ok := holds[sum("message "+strconv.FormatUint(u.Checkpoint.After+1, 10))]; ok {
						close(hold)
					}
				}
			})
			r.done(t)
			if got := r.checkpoints(); !slices.Equal(got, tc.want) {
				t.Errorf("checkpoints after %v, want %v", got, tc.want)
			}
			checkCovered(t, r)
		})
	}
}

func TestRunCheckpointHeldBack(t *testing.T) {
	var gate aitest.Gate
	e := aitest.Serve(t, aitest.Routes{
		Embeddings: func(ctx context.Context, req aitest.EmbeddingRequest) aitest.Reply {
			if slices.Contains(req.Input, "message 1") {
				held, cancel := context.WithTimeout(ctx, failsafe)
				defer cancel()
				if err := gate.Wait(held); err != nil && ctx.Err() == nil {
					t.Error("message 1's embedding was held past the failsafe: messages 2 and 3 never settled around it")
				}
			}
			vectors := make([][]float32, len(req.Input))
			for i, text := range req.Input {
				vectors[i] = []float32{float32(len(text)), 1}
			}
			return aitest.EmbeddingsReply{Vectors: vectors, PromptTokens: len(req.Input)}
		},
	})
	f := newFixture(t, nil, e)
	batched := map[string]bool{}
	released := false
	var early []uint64
	r := collect(f.runner.Run(t.Context(), input(t, "store.txt", messages(3)), docinfer.Options{}), func(u job.Update) {
		switch {
		case u.Checkpoint != nil && !released:
			early = append(early, u.Checkpoint.After)
		case u.Batch != nil:
			for _, s := range u.Batch.Sources {
				batched[sourceSHA(s)] = true
			}
			if !released && batched[sum("message 2")] && batched[sum("message 3")] {
				if batched[sum("message 1")] {
					t.Error("message 1's batch is out while its embedding is held")
				}
				released = true
				gate.Open()
			}
		}
	})
	r.done(t)
	if !released {
		t.Fatal("messages 2 and 3 were never batched while message 1 was held")
	}
	if len(early) != 0 {
		t.Errorf("checkpoints after %v while message 1 was unsettled, want none", early)
	}
	if got, want := r.checkpoints(), []uint64{3}; !slices.Equal(got, want) {
		t.Errorf("checkpoints after %v, want %v", got, want)
	}
	checkCovered(t, r)
}

func TestRunNoStoreNoCheckpoint(t *testing.T) {
	f := newFixture(t, map[string]string{"x.txt": childText}, serve(t))
	r := collect(f.runner.Run(t.Context(), input(t, "a.txt", "text\nfile x.txt\nwarn a line\nrecord {\"k\":1}"), docinfer.Options{}), nil)
	r.done(t)
	if got := r.checkpoints(); len(got) != 0 {
		t.Errorf("checkpoints after %v, want none", got)
	}
}

func TestRunResume(t *testing.T) {
	for _, crash := range []string{"stop", "cancel"} {
		t.Run(crash, func(t *testing.T) {
			f := newFixtureEvery(t, nil, serve(t), 2)
			hold := make(chan struct{})
			holdPuts(t, f, map[string]chan struct{}{sum("message 3"): hold})
			in := input(t, "store.txt", messages(4))

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var checkpoint *job.Checkpoint
			var before []job.Update
			for u, err := range f.runner.Run(ctx, in, docinfer.Options{}) {
				if err != nil {
					if crash != "cancel" || !errors.Is(err, context.Canceled) {
						t.Fatalf("the first run: %v", err)
					}
					continue
				}
				if checkpoint != nil {
					continue
				}
				before = append(before, u)
				if u.Checkpoint != nil {
					checkpoint = u.Checkpoint
					if crash == "stop" {
						break
					}
					cancel()
				}
			}
			if checkpoint == nil || checkpoint.After != 2 {
				t.Fatalf("the first run's checkpoint %+v, want one after message 2", checkpoint)
			}
			close(hold)

			resumed := in
			resumed.After = checkpoint.After
			r := collect(f.runner.Run(t.Context(), resumed, docinfer.Options{}), nil)
			r.done(t)
			first, second := messagesOf(before), messagesOf(r.updates)
			if !slices.Equal(first, []uint64{1, 2}) || !slices.Equal(second, []uint64{3, 4}) {
				t.Errorf("messages %v up to the checkpoint, then %v resumed; want [1 2], then [3 4]", first, second)
			}
			var parts []dispatch.Event
			for _, ev := range r.events() {
				if own(ev) {
					parts = append(parts, ev)
				}
			}
			if len(parts) != 1 || parts[0].Outcome != dispatch.Prepared {
				t.Errorf("the resumed run's own events %+v, want the store's alone, prepared", parts)
			}
		})
	}
}
