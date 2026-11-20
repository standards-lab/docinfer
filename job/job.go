package job

import (
	"fmt"

	"github.com/standards-lab/docinfer"
	"github.com/standards-lab/docproc/dispatch"
)

// Config is what a [Runner] runs each job with. Nothing has a default.
type Config struct {
	Dispatcher      *dispatch.Dispatcher // required
	Pipeline        *docinfer.Pipeline   // required
	CheckpointEvery int                  // at least 1: messages between a store's checkpoints
}

// Runner runs jobs, each through its Config's dispatcher and pipeline. It is safe for concurrent
// use: one Runner runs many jobs at once.
type Runner struct {
	cfg Config
}

// New returns a Runner of cfg. A nil Dispatcher or Pipeline is the caller's mistake, and panics
// naming the field. It refuses a CheckpointEvery below 1, matching docinfer.ErrRequest: job
// declares no error of its own.
func New(cfg Config) (*Runner, error) {
	if cfg.Dispatcher == nil {
		panic("job: New: nil Dispatcher")
	}
	if cfg.Pipeline == nil {
		panic("job: New: nil Pipeline")
	}
	if cfg.CheckpointEvery < 1 {
		return nil, fmt.Errorf("job: New: %w: CheckpointEvery %d is below 1", docinfer.ErrRequest, cfg.CheckpointEvery)
	}
	return &Runner{cfg: cfg}, nil
}

// Update is one thing a run yields; one field is set. It is the Go side's one-of alone: each
// field's type is its own JSON shape, so Update has none.
type Update struct {
	Event      *dispatch.Event // a part's own, an output's, or a warning's
	Inference  *docinfer.Inference
	Progress   *docinfer.Progress
	Batch      *docinfer.Batch
	Checkpoint *Checkpoint
	Done       *Done
}

// Checkpoint is a store's cursor, a low watermark: a host resumes a redelivered job from the last
// one by setting dispatch.Input.After to its After.
type Checkpoint struct {
	// After is the last message whose inputs, and every earlier input, have settled.
	After uint64 `json:"after"`
}

// Done ends one attempt's updates. Each part ID is counted once, by its last own event, since a
// replay after a content miss prepares the part again and makes its earlier event out of date.
type Done struct {
	Parts   map[dispatch.Outcome]int `json:"parts"`   // by outcome, every depth
	Outputs int                      `json:"outputs"` // each part's Outputs, summed
	Counts  docinfer.Counts          `json:"counts"`  // the pipeline's, unchanged
}
