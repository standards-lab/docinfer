package job

import (
	"context"
	"fmt"
	"iter"
	"strings"

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

// Run dispatches in through the Config's dispatcher and infers over what it prepares through the
// Config's pipeline, under opts and ctx, yielding the job's updates as made and its [Done] last.
//
// Each dispatch event is fed to the pipeline as one input: the depth-0 part's own event with
// docinfer.MetadataText's text of that part, and every other event as it is. Run ranges over the
// pipeline's Run alone, which pulls the dispatch from its input side, so yield is called from the
// caller's goroutine alone. Updates keep the pipeline's order: each event as the pipeline echoes
// it, before anything derived from it, and each inference, progress, and batch as made, so an
// output's event precedes its inference and the batches holding its chunks, and a part's own
// event follows its outputs', warnings', and children's. No output is held back until its part's
// event: a consumer drops a quarantined or failed part's results by that part's own event.
//
// An inference's Text is put into the dispatcher's content store under its TextSHA256 before its
// update; one with no TextSHA256 puts nothing. An error stops the job, yielded alone and last once
// the pipeline and the dispatch have stopped: a refused opts.Extract, before the dispatch starts,
// and an error of the dispatch, the pipeline, or a put, which docinfer.Permanent doesn't report.
//
// A store's input yields [Checkpoint]s at its low watermark. A message is the stretch of inputs,
// in pull order, that ends at an output event with a non-zero After; the watermark is the last
// message whose inputs, and every earlier input, have settled, which settlement out of pull order
// can hold back while later messages settle. Once the watermark has passed at least
// CheckpointEvery messages since the last checkpoint, or since the start, Run yields a
// Checkpoint of its message's After, after everything derived from the inputs it covers: never
// the highest After seen, and never one ahead of an unsettled earlier input. An input with no
// store yields none, and the job's end adds none: a finished job needs no cursor. A dispatch
// resumed with dispatch.Input.After set to a checkpoint's After yields only the messages after it.
func (r *Runner) Run(ctx context.Context, in dispatch.Input, opts docinfer.Options) iter.Seq2[Update, error] {
	return func(yield func(Update, error) bool) {
		// The job's own context, cancelled as the job stops, so a dispatch the pipeline's input
		// side is blocked in ends rather than holds the pipeline's stop.
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		content := r.cfg.Dispatcher.Content()
		parts := map[string]dispatch.Event{} // each part's last own event, by part ID
		mark := watermark{every: r.cfg.CheckpointEvery}
		var counts *docinfer.Counts
		var stop error
	results:
		for res, err := range r.cfg.Pipeline.Run(ctx, content, r.inputs(ctx, in), opts) {
			var u Update
			switch {
			case err != nil:
				stop = err
				break results
			case res.Input != nil:
				ev := res.Input.Event
				if own(ev) {
					parts[ev.Part.ID] = ev
				}
				mark.echo(ev)
				u.Event = &ev
			case res.Inference != nil:
				if err := put(ctx, content, res.Inference); err != nil {
					stop = err
					cancel() // as for a loop stopped early, below
					break results
				}
				u.Inference = res.Inference
			case res.Progress != nil:
				u.Progress = res.Progress
			case res.Batch != nil:
				u.Batch = res.Batch
			case res.Done != nil:
				counts = res.Done
				continue
			default: // a settlement, which may move the watermark far enough for a checkpoint
				after, ok := mark.settle(*res.Settled)
				if !ok {
					continue
				}
				u.Checkpoint = &Checkpoint{After: after}
			}
			if !yield(u, nil) {
				// Cancelled first, so the pipeline's stop doesn't wait on a dispatch blocked
				// short of its next event.
				cancel()
				return
			}
		}
		// The loop's end has stopped the pipeline, and with it the dispatch, before the error is
		// yielded, alone and last.
		if stop != nil {
			yield(Update{}, stop)
			return
		}
		if counts == nil { // the pipeline always ends with its counts or an error
			return
		}
		yield(Update{Done: done(parts, *counts)}, nil)
	}
}

// watermark is a job's low watermark over its inputs, in pull order: the end of the last message
// whose inputs, and every earlier input, have settled. A message is the stretch of inputs that
// ends at an output event with a non-zero After, a store's cursor.
type watermark struct {
	every int // messages the watermark passes between checkpoints
	// afters holds each echoed input's After, by its position in pull order: a message's last
	// output's cursor, or zero.
	afters []uint64
	// settled holds whether each input has settled, by position; an input settles after its echo.
	settled []bool
	// low is the position of the first input not yet settled: every input before it has.
	low int
	// passed counts the messages the watermark has passed since the last checkpoint, or since
	// the start, and last is the After of the last of them.
	passed int
	last   uint64
}

// echo records ev, the next input in pull order.
func (w *watermark) echo(ev dispatch.Event) {
	var after uint64
	if ev.Output != nil {
		after = ev.After
	}
	w.afters = append(w.afters, after)
	w.settled = append(w.settled, false)
}

// settle records that the input at pos has settled, returning the After of a checkpoint when
// the watermark has now passed at least every messages since the last. One settlement that
// passes several messages makes one checkpoint, at the last of them; an input with no store
// passes no message, and makes none.
func (w *watermark) settle(pos int) (uint64, bool) {
	w.settled[pos] = true
	for w.low < len(w.settled) && w.settled[w.low] {
		if after := w.afters[w.low]; after != 0 {
			w.passed++
			w.last = after
		}
		w.low++
	}
	if w.passed < w.every {
		return 0, false
	}
	w.passed = 0
	return w.last, true
}

// inputs dispatches in, yielding each event as the pipeline's input: the depth-0 part's own event
// with its metadata text, every other event as it is, and the dispatch's error alone, last.
func (r *Runner) inputs(ctx context.Context, in dispatch.Input) iter.Seq2[docinfer.Input, error] {
	return func(yield func(docinfer.Input, error) bool) {
		for ev, err := range r.cfg.Dispatcher.Dispatch(ctx, in) {
			if err != nil {
				yield(docinfer.Input{}, err)
				return
			}
			input := docinfer.Input{Event: ev}
			if own(ev) && ev.Part.Depth == 0 {
				input.Metadata = docinfer.MetadataText(ev.Part)
			}
			if !yield(input, nil) {
				return
			}
		}
	}
}

// own reports whether ev is a part's own event: neither an output's nor a warning's.
func own(ev dispatch.Event) bool { return ev.Output == nil && ev.Warning == nil }

// put puts inf's Text into content under its TextSHA256, putting nothing when it has none.
func put(ctx context.Context, content dispatch.Content, inf *docinfer.Inference) error {
	if inf.TextSHA256 == "" {
		return nil
	}
	if err := content.Put(ctx, inf.TextSHA256, strings.NewReader(inf.Text)); err != nil {
		return fmt.Errorf("job: put the text %s of part %s's %s: %w", inf.TextSHA256, inf.Part, inf.Capability, err)
	}
	return nil
}

// done returns the attempt's Done of parts, each part's last own event, and the pipeline's counts.
func done(parts map[string]dispatch.Event, counts docinfer.Counts) *Done {
	d := &Done{Parts: map[dispatch.Outcome]int{}, Counts: counts}
	for _, ev := range parts {
		d.Parts[ev.Outcome]++
		d.Outputs += ev.Outputs
	}
	return d
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
