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

// The checkpoint tests dispatch a store of the fixture stage's "message ID" lines, each a message
// whose output carries its ID as its After, and infer over it with embeddings alone, so each text
// output's one batch is everything derived from it. They order the job's steps by holding a
// message's content put or its embedding until the test lets it go, never by waiting a while.

// messages returns a store of n messages, "message 1" to "message n", one a line.
func messages(n int) string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = "message " + strconv.Itoa(i+1)
	}
	return strings.Join(lines, "\n")
}

// checkpoints returns the After of each of r's checkpoints, in order.
func (r run) checkpoints() []uint64 {
	var out []uint64
	for _, u := range r.updates {
		if u.Checkpoint != nil {
			out = append(out, u.Checkpoint.After)
		}
	}
	return out
}

// messagesOf returns the After of each message event among updates, in order.
func messagesOf(updates []job.Update) []uint64 {
	var out []uint64
	for _, u := range updates {
		if ev := u.Event; ev != nil && ev.Output != nil && ev.After != 0 {
			out = append(out, ev.After)
		}
	}
	return out
}

// checkCovered fails the test unless each of r's checkpoints follows everything derived from
// the inputs it covers: every output's event up to its message's, in pull order, and the batch
// of each such output's text.
func checkCovered(t *testing.T, r run) {
	t.Helper()
	for i, u := range r.updates {
		if u.Checkpoint == nil {
			continue
		}
		var outputs []string // the outputs the checkpoint covers, by SHA-256
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

// holdPuts makes f's content hold the put of each message whose SHA-256 holds names until its
// channel is closed or the put's context ends; the test fails if neither happens in failsafe.
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

// A checkpoint is yielded once the watermark has passed at least CheckpointEvery messages since
// the last, or since the start, at its last message's After, after everything derived from the
// inputs it covers. The store's message after each expected checkpoint is held from the dispatch
// until that checkpoint is yielded, so each run's checkpoints are exactly these; no checkpoint
// ends the job beyond the rule's, so a tail of fewer messages makes none.
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
			// The message after each expected checkpoint, held until the checkpoint's yielded.
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

// A slow earlier input holds the watermark back while later messages settle. Message 1's
// embedding is held until messages 2's and 3's batches are out, and so they have settled: the
// pipeline settles an input right after the results that settle it, before it takes its next
// event. No checkpoint comes while message 1 is held, not even of message 3's After, the highest
// seen; once it's let go, one checkpoint of message 3 covers all three.
func TestRunCheckpointHeldBack(t *testing.T) {
	var gate aitest.Gate
	e := aitest.Serve(t, aitest.Routes{
		Embeddings: func(ctx context.Context, req aitest.EmbeddingRequest) aitest.Reply {
			if slices.Contains(req.Input, "message 1") {
				// A context ended lets the request go, unanswered by the client; the failsafe bounds
				// a hold that messages 2 and 3 never settle around, a finding.
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
	var early []uint64 // checkpoints while message 1 is held
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

// An input with no store, no event carrying an After, yields no checkpoint, whatever its outputs,
// warnings, and parts.
func TestRunNoStoreNoCheckpoint(t *testing.T) {
	f := newFixture(t, map[string]string{"x.txt": childText}, serve(t))
	r := collect(f.runner.Run(t.Context(), input(t, "a.txt", "text\nfile x.txt\nwarn a line\nrecord {\"k\":1}"), docinfer.Options{}), nil)
	r.done(t)
	if got := r.checkpoints(); len(got) != 0 {
		t.Errorf("checkpoints after %v, want none", got)
	}
}

// A dispatch resumed with Input.After set to a checkpoint's After yields only the messages after
// it. A first run is stopped at its checkpoint after message 2, its consumer stopping early or its
// context cancelled, message 3 held from its dispatch meanwhile; a resumed run over the same index
// and content then yields messages 3 and 4. The messages the first run yielded up to its
// checkpoint and the resumed run's are each message once: none lost, none before the checkpoint
// repeated.
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
			var before []job.Update // the first run's updates up to its checkpoint
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
