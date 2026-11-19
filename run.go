package docinfer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"iter"
	"strconv"
	"strings"
	"sync"

	"github.com/standards-lab/docproc/dispatch"
)

// Options are one run's.
type Options struct {
	Extract *Extract // a caller's prompt and schema; nil extracts nothing
	// Final marks the job's last delivery: an item a model fails on with the failure table's
	// failed row (a 500, a failed or undecodable reply, any other error a call returns) is
	// recorded failed, never held, and the job goes on, rather than the job stopping for a
	// redelivery.
	Final bool
}

// Input is one of a job's inputs to [Pipeline.Run]: an output's event, or the input's metadata
// text.
type Input struct {
	// Event is an output's event: a text or Markdown output is chunked; image, audio, and record
	// outputs are described, transcribed, or read as their capabilities say. Its content is
	// opened by its Output.SHA256 from Run's content, only on a cache miss or to chunk its text.
	// For a Metadata input, Event is the depth-0 part's own event.
	Event dispatch.Event
	// Metadata is the input's metadata text, [MetadataText]'s, read when Event has no Output.
	Metadata string
}

// Result is one thing [Pipeline.Run] made. One field is set.
type Result struct {
	Inference *Inference
	Progress  *Progress
	Batch     *Batch
	Done      *Counts // last
}

// MissingError is [Pipeline.Run]'s error for an input whose content Run's content misses: Event
// is the input's, and Err is Content.Open's, which matches fs.ErrNotExist. [Permanent] doesn't
// report it, since the store may hold the content on a redelivery.
type MissingError struct {
	Event dispatch.Event
	Err   error
}

func (e *MissingError) Error() string {
	sha := ""
	if e.Event.Output != nil {
		sha = e.Event.Output.SHA256
	}
	return fmt.Sprintf("docinfer: content %s of part %s is missing: %v", sha, e.Event.Part.ID, e.Err)
}

func (e *MissingError) Unwrap() error { return e.Err }

// MetadataText is an input's metadata text, from its depth-0 part: the one builder of it. It is a
// readable line per field routing and the input gave, "label: value", in a fixed order, a field
// with no value left out: the part's name, size, and content type's kind, route, route reason,
// and stored MIME type.
func MetadataText(p dispatch.Part) string {
	var b strings.Builder
	line := func(label, value string) {
		if value != "" {
			b.WriteString(label + ": " + value + "\n")
		}
	}
	line("file name", p.Name)
	line("size", strconv.FormatInt(p.Size, 10)+" bytes")
	line("kind", string(p.ContentType.Kind))
	line("route", string(p.ContentType.Route))
	line("route reason", p.ContentType.RouteReason)
	line("stored MIME type", p.ContentType.Stored)
	return b.String()
}

// Source kinds: what a chunk's text is.
const (
	sourceOutput   = "output"
	sourceMetadata = "metadata"
)

// errNotYet is the cause of a run that asks for what this version of the pipeline doesn't build
// yet: an image with Vision set, a window with Transcription set, or an Extract.
var errNotYet = errors.New("docinfer: not yet built")

// Run infers over one job's inputs, opening their content from content, and yields what it makes
// as it makes it: each [Inference], each extraction's [Progress], each [Batch] once its chunks'
// embeddings complete, and the job's [Counts] last.
//
// A text or Markdown output is chunked as a source of kind output, and a Metadata input as one of
// kind metadata. Each chunk's embedding is keyed by the embeddings model and the chunk's SHA-256
// alone, read from the cache, or asked of the model Config.EmbedBatch texts per call and held
// before anything derived from it is yielded. A held value that doesn't decode is a miss, which
// the model's reply replaces; a vector, held or made, whose length isn't Config.Dimensions stops
// the job, [Permanent]. Content is opened only on a miss, or to chunk an output's text; a content
// store's miss stops the job with a [MissingError].
//
// Run pulls an input only while fewer than Config.Pending of the job's inputs are admitted and
// not yet finished, so a slow model holds the job's inputs back. A batch is yielded when its next
// chunk, with the part and source it adds, would take its marshalled JSON past
// Config.BatchBytes, or when no embedded chunk is waiting; a chunk alone past the cap is a batch
// of one.
//
// A go-ai failure maps by the failure table: a refused status, 400, 413, 415, or 422, re-sends
// an embedding call a text at a time, and a text so refused is its chunk's Error, held; a
// permanent or transient failure stops the job with the error, wrapped; a failed one stops it
// too, except on Options.Final, where its chunks' Error is recorded, never held, and the job
// goes on. An error stops the job: Run cancels its requests, waits for them, and yields the error
// alone, last. A cancelled context yields ctx.Err() alone, last.
//
// The pipeline joins work in flight across its jobs by Key: a chunk's embedding another job is
// asking for, read from neither the cache nor the model, is waited for, holding no Bounds slot,
// and counted Held. A waited-for refusal is the chunk's Error, and a permanent failure stops this
// job too; a job that ends without a result, cancelled, failing otherwise, or panicking, leaves
// its waiters to ask the model themselves, and a cancelled job stops waiting at once.
//
// yield is called from the goroutine ranging over Run alone, and Run waits for its requests in
// flight, and for its current pull of inputs, before it returns, stopped early or not. A panic on
// one of the job's goroutines, a Cache's or the input stream's, stops the job, and Run panics with
// its value on the goroutine ranging over it once the job's other goroutines have ended.
func (p *Pipeline) Run(ctx context.Context, content dispatch.Content, inputs iter.Seq2[Input, error], opts Options) iter.Seq2[Result, error] {
	return func(yield func(Result, error) bool) {
		if opts.Extract != nil {
			yield(Result{}, fmt.Errorf("docinfer: run: extraction: %w", errNotYet))
			return
		}
		jobCtx, cancel := context.WithCancel(ctx)
		r := &run{
			p:       p,
			ctx:     jobCtx,
			cancel:  cancel,
			content: content,
			opts:    opts,
			events:  make(chan event),
			slots:   make(chan struct{}, p.cfg.Pending),
			embedID: p.cfg.Embeddings.Identity(),
		}
		defer func() {
			cancel()
			r.wg.Wait()
			r.repanic()
		}()
		r.spawn(func() { r.feed(inputs) })
		err := r.coordinate(ctx, yield)
		if err == nil {
			return
		}
		// The job stops: its requests are cancelled and waited for before the error, last.
		cancel()
		r.wg.Wait()
		r.repanic()
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		yield(Result{}, err)
	}
}

// run is one job's Run: the state its goroutines share, and the coordinator's own.
type run struct {
	p       *Pipeline
	ctx     context.Context // the job's: cancelled when Run stops, which ends every request
	cancel  context.CancelFunc
	content dispatch.Content
	opts    Options
	embedID Identity
	// events carries every goroutine's news to the coordinator, Run's own goroutine, which alone
	// yields.
	events chan event
	// slots holds a place per input admitted and not yet finished, Config.Pending in all: the
	// feeder takes one before it pulls an input, and the coordinator frees it once the input is
	// finished.
	slots chan struct{}
	wg    sync.WaitGroup

	// panicMu guards panicked and panicValue: the first panic on one of the job's goroutines,
	// which Run panics with once they have ended.
	panicMu    sync.Mutex
	panicked   bool
	panicValue any

	// The coordinator's alone.
	counts  Counts
	waiting []item // embedded chunks not yet in a yielded batch, in the order they completed
	active  int    // inputs admitted and not yet finished
}

// event is one goroutine's news to the coordinator: one of its types below.
type event interface{ isEvent() }

// inputEvent is an input the feeder pulled, its slot held.
type inputEvent struct{ in Input }

// endEvent is the end of the inputs, err the input stream's error, if any.
type endEvent struct{ err error }

// splitEvent is an input's text cut into n chunks.
type splitEvent struct {
	st *inputState
	n  int
}

// doneEvent is an input's chunks complete, embedded or rejected, and the requests that made them.
type doneEvent struct {
	st       *inputState
	items    []item
	requests int
	usage    Usage
}

// errEvent is an error that stops the job.
type errEvent struct{ err error }

func (inputEvent) isEvent() {}
func (endEvent) isEvent()   {}
func (splitEvent) isEvent() {}
func (doneEvent) isEvent()  {}
func (errEvent) isEvent()   {}

// inputState is one admitted input's, as its source text: what its chunks come from, and, the
// coordinator's alone, how many of them are yet to complete.
type inputState struct {
	part     dispatch.Part
	partJSON []byte // the part's JSON, a batch's Parts element
	source   Source // its Part index set per batch

	split     bool // the coordinator has its count of chunks
	remaining int
}

// item is one chunk on its way to a batch: its source text's input, the chunk, its Source index
// set per batch, and whether it was read from the cache.
type item struct {
	st    *inputState
	chunk Chunk
	held  bool
}

// send hands e to the coordinator, reporting false once the job has stopped.
func (r *run) send(e event) bool {
	select {
	case r.events <- e:
		return true
	case <-r.ctx.Done():
		return false
	}
}

// fail hands err to the coordinator, which stops the job with it.
func (r *run) fail(err error) { r.send(errEvent{err}) }

// spawn runs f on a goroutine of the job's, which Run waits for. A panic in f is recovered, once
// f's deferred calls, a lease's release among them, have run: it stops the job, and Run panics
// with its value on its own goroutine, so the panic reaches Run's caller rather than ending the
// process.
func (r *run) spawn(f func()) {
	r.wg.Go(func() {
		defer func() {
			if v := recover(); v != nil {
				r.panicMu.Lock()
				if !r.panicked {
					r.panicked, r.panicValue = true, v
				}
				r.panicMu.Unlock()
				r.cancel()
			}
		}()
		f()
	})
}

// repanic panics with the value a goroutine of the job's panicked with, if one did, once. Run
// calls it after waiting for the job's goroutines.
func (r *run) repanic() {
	r.panicMu.Lock()
	v, ok := r.panicValue, r.panicked
	r.panicValue, r.panicked = nil, false
	r.panicMu.Unlock()
	if ok {
		panic(v)
	}
}

// feed pulls the inputs, each only once a slot is free, and hands them to the coordinator. It
// takes a slot before each pull, so no more than Config.Pending inputs are admitted and not
// finished, and the input stream is never read ahead of that.
func (r *run) feed(inputs iter.Seq2[Input, error]) {
	acquire := func() bool {
		select {
		case r.slots <- struct{}{}:
			return true
		case <-r.ctx.Done():
			return false
		}
	}
	if !acquire() {
		return
	}
	for in, err := range inputs {
		if err != nil {
			r.send(endEvent{fmt.Errorf("docinfer: run: inputs: %w", err)})
			return
		}
		if !r.send(inputEvent{in}) || !acquire() {
			return
		}
	}
	r.send(endEvent{})
}

// coordinate runs the job on Run's goroutine: it admits each input, counts what completes, and
// yields batches as they fill, then the counts. It returns the error that stops the job, or nil
// once it has yielded the counts or yield has stopped it.
func (r *run) coordinate(ctx context.Context, yield func(Result, error) bool) error {
	ended := false
	for !ended || r.active > 0 {
		var e event
		select {
		case e = <-r.events:
		case <-ctx.Done():
			return ctx.Err()
		case <-r.ctx.Done(): // a goroutine of the job's panicked
			return r.ctx.Err()
		}
		// Nothing derived from the job is yielded once its context has ended.
		if err := ctx.Err(); err != nil {
			return err
		}
		switch e := e.(type) {
		case inputEvent:
			if err := r.admit(e.in); err != nil {
				return err
			}
		case endEvent:
			if e.err != nil {
				return e.err
			}
			ended = true
		case splitEvent:
			e.st.split, e.st.remaining = true, e.st.remaining+e.n
			r.countSource(e.st)
			if e.st.remaining == 0 {
				r.finish()
			}
		case doneEvent:
			r.counts.Requests += e.requests
			r.counts.Tokens.Embeddings.Input += e.usage.Input
			r.counts.Tokens.Embeddings.Output += e.usage.Output
			for _, it := range e.items {
				if it.chunk.Vector != "" {
					r.counts.Embedded++
				} else {
					r.counts.Rejected++
				}
				if it.held {
					r.counts.Held++
				}
			}
			r.waiting = append(r.waiting, e.items...)
			e.st.remaining -= len(e.items)
			ok, err := r.flush(yield)
			if err != nil || !ok {
				return err
			}
			if e.st.split && e.st.remaining == 0 && len(e.items) > 0 {
				r.finish()
			}
		case errEvent:
			return e.err
		}
	}
	counts := r.counts
	yield(Result{Done: &counts}, nil)
	return nil
}

// countSource counts an input's source text once it is chunked.
func (r *run) countSource(st *inputState) {
	if st.source.Kind == sourceMetadata {
		r.counts.Metadata++
	} else {
		r.counts.Texts++
	}
}

// finish frees a finished input's slot, so the feeder may pull the next.
func (r *run) finish() {
	r.active--
	<-r.slots
}

// admit starts an input's work by its kind, as dispatch's constants name it, or finishes an input
// with nothing to do at once. It returns an error that stops the job.
func (r *run) admit(in Input) error {
	r.active++
	out := in.Event.Output
	if out == nil {
		if in.Metadata == "" {
			r.finish()
			return nil
		}
		st := &inputState{
			part:   in.Event.Part,
			source: Source{Kind: sourceMetadata, SHA256: sha256Hex([]byte(in.Metadata))},
		}
		return r.start(st, func() (string, error) { return in.Metadata, nil })
	}
	switch out.Kind {
	case dispatch.TextOutput, dispatch.MarkdownOutput:
		stage := in.Event.Stage
		st := &inputState{
			part: in.Event.Part,
			// An output's text SHA-256 is its Output.SHA256, held there alone.
			source: Source{Kind: sourceOutput, Stage: &stage, Output: new(*out)},
		}
		return r.start(st, func() (string, error) { return r.text(in.Event) })
	case dispatch.ImageOutput:
		r.counts.Images++
		if r.p.cfg.Vision != nil {
			return fmt.Errorf("docinfer: run: vision of image %s of part %s: %w", out.SHA256, in.Event.Part.ID, errNotYet)
		}
	case dispatch.AudioOutput:
		r.counts.Windows++
		if r.p.cfg.Transcription != nil {
			return fmt.Errorf("docinfer: run: transcription of window %s of part %s: %w", out.SHA256, in.Event.Part.ID, errNotYet)
		}
	}
	// A skipped image or window, a record, or an output of another kind: nothing to do.
	r.finish()
	return nil
}

// start runs an input's source text, as text gives it, through chunking and embedding on a
// goroutine of its own.
func (r *run) start(st *inputState, text func() (string, error)) error {
	b, err := marshal(st.part)
	if err != nil {
		return err
	}
	st.partJSON = b
	r.spawn(func() {
		s, err := text()
		if err != nil {
			r.fail(err)
			return
		}
		r.embed(st, s)
	})
	return nil
}

// text reads an output's text from Run's content, by its SHA-256.
func (r *run) text(ev dispatch.Event) (string, error) {
	rc, err := r.content.Open(r.ctx, ev.Output.SHA256)
	if errors.Is(err, fs.ErrNotExist) {
		return "", &MissingError{Event: ev, Err: err}
	}
	if err != nil {
		return "", fmt.Errorf("docinfer: run: open content %s of part %s: %w", ev.Output.SHA256, ev.Part.ID, err)
	}
	b, err := io.ReadAll(rc)
	if closeErr := rc.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return "", fmt.Errorf("docinfer: run: read content %s of part %s: %w", ev.Output.SHA256, ev.Part.ID, err)
	}
	return string(b), nil
}
