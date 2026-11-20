package job_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/standards-lab/docinfer"
	"github.com/standards-lab/docinfer/chunk"
	"github.com/standards-lab/docinfer/job"
	"github.com/standards-lab/docinfer/join"
	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/format/completions"
)

// validConfig returns a Config New accepts: a dispatcher over memory stores, a pipeline of
// embeddings alone, and a CheckpointEvery of 2, 1 more than the least New accepts.
func validConfig(t *testing.T) job.Config {
	t.Helper()
	d, err := dispatch.New(dispatch.Config{
		Index:   dispatch.NewMemoryIndex(0),
		Content: dispatch.NewMemoryContent(),
		Dir:     t.TempDir(),
		Puts:    1,
	})
	if err != nil {
		t.Fatal(err)
	}
	c, err := ai.New("http://localhost/v1", http.DefaultClient, completions.New(completions.Options{}), ai.Options{})
	if err != nil {
		t.Fatal(err)
	}
	bounds, err := docinfer.NewBounds(1, 1, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	p, err := docinfer.New(docinfer.Config{
		Embeddings: docinfer.Embeddings{Client: c, Model: "embedding-model"},
		Chunker:    chunk.Splitter{Bound: 1},
		Dimensions: 1,
		Cache:      docinfer.NewMemoryCache(),
		Bounds:     bounds,
		Pending:    1,
		EmbedBatch: 1,
		BatchBytes: 1,
		Join:       join.Request{WordsPerSecond: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	return job.Config{Dispatcher: d, Pipeline: p, CheckpointEvery: 2}
}

func TestNewAcceptsLeastValues(t *testing.T) {
	cfg := validConfig(t)
	cfg.CheckpointEvery = 1
	r, err := job.New(cfg)
	if err != nil || r == nil {
		t.Fatalf("New with CheckpointEvery 1: %v, %v", r, err)
	}
}

// New panics naming a nil Dispatcher or Pipeline, a wiring the caller left out.
func TestNewPanicsNamingTheField(t *testing.T) {
	for _, tc := range []struct {
		field string
		unset func(*job.Config)
	}{
		{"Dispatcher", func(c *job.Config) { c.Dispatcher = nil }},
		{"Pipeline", func(c *job.Config) { c.Pipeline = nil }},
	} {
		t.Run(tc.field, func(t *testing.T) {
			cfg := validConfig(t)
			tc.unset(&cfg)
			defer func() {
				r := recover()
				if r == nil {
					t.Fatalf("New with a nil %s didn't panic", tc.field)
				}
				if msg := fmt.Sprint(r); !strings.Contains(msg, "nil "+tc.field) {
					t.Errorf("New with a nil %s panicked with %q, which doesn't name it", tc.field, msg)
				}
			}()
			_, _ = job.New(cfg)
		})
	}
}

// New refuses a CheckpointEvery below 1, 0 and -1 alike, matching docinfer.ErrRequest and naming
// the field: there is no default.
func TestNewRefusesCheckpointEveryBelowOne(t *testing.T) {
	for _, n := range []int{0, -1} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			cfg := validConfig(t)
			cfg.CheckpointEvery = n
			r, err := job.New(cfg)
			if !errors.Is(err, docinfer.ErrRequest) {
				t.Fatalf("New with CheckpointEvery %d: %v, want docinfer.ErrRequest", n, err)
			}
			if r != nil {
				t.Errorf("New with CheckpointEvery %d returned a Runner beside its error", n)
			}
			if !strings.Contains(err.Error(), "CheckpointEvery") {
				t.Errorf("New with CheckpointEvery %d: %q doesn't name it", n, err)
			}
		})
	}
}

// Checkpoint and Done encode under docinfer.Version "1": these strings are pinned, since a host
// stores and forwards them, and may only ever be added to.
func TestJSON(t *testing.T) {
	for _, tc := range []struct {
		name string
		v    any
		want string
	}{
		{"Checkpoint", job.Checkpoint{After: 18446744073709551615}, `{"after":18446744073709551615}`},
		{"Checkpoint zero", job.Checkpoint{}, `{"after":0}`},
		{
			"Done",
			job.Done{
				Parts:   map[dispatch.Outcome]int{dispatch.Prepared: 2, dispatch.Quarantined: 1, dispatch.Seen: 3},
				Outputs: 4,
				Counts:  docinfer.Counts{Texts: 1, Embedded: 5, Tokens: docinfer.Tokens{Embeddings: docinfer.Usage{Input: 6}}},
			},
			`{"parts":{"prepared":2,"quarantined":1,"seen":3},"outputs":4,` +
				`"counts":{"texts":1,"metadata":0,"embedded":5,"rejected":0,"held":0,"requests":0,"images":0,` +
				`"windows":0,"tokens":{"embeddings":{"input":6,"output":0}}}}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, err := json.Marshal(tc.v)
			if err != nil {
				t.Fatal(err)
			}
			if string(b) != tc.want {
				t.Errorf("%s encodes as\n%s, want\n%s", tc.name, b, tc.want)
			}
		})
	}
}
