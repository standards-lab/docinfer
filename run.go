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

// Input is one of a job's inputs to [Pipeline.Run]: an output's event, the input's metadata
// text, or any other event, which Run passes through.
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
	// Input is an input, echoed as Run admits it, in pull order, before anything derived from it.
	Input     *Input
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

// errInputDefect is the cause of an input no redelivery mends, which [Permanent] reports: an image
// output without a MediaType, a window of a media type other than audio/wav or audio/mpeg, or
// whose Meta doesn't place it, and an audio plan record that doesn't decode.
var errInputDefect = errors.New("docinfer: the input's defect")

// Run infers over one job's inputs, opening their content from content, and yields what it makes
// as it makes it: each [Input] as it admits it, each [Inference], each extraction's [Progress],
// each [Batch] once its chunks' embeddings complete, and the job's [Counts] last.
//
// Each input is echoed as Result.Input exactly once, in pull order, before anything derived from
// it: its inference, its progress, and the batches holding its chunks. An input that is neither an
// output nor a metadata text, such as a warning or a part's own event, is passed through: echoed,
// with no work and nothing counted.
//
// A text or Markdown output is chunked as a source of kind output, and a Metadata input as one of
// kind metadata. With Config.Vision set, an image output is described from its content, sent as a
// data URL of its Output.MediaType beside Vision.Prompt, and its text chunked as a source of kind
// vision; an image output without a MediaType stops the job, [Permanent]. Its description is keyed
// by vision's Identity and the image's SHA-256, read from the cache, or asked of the model, and
// held before its [Inference] is yielded, which precedes the batches of its chunks. A completed
// reply is outcome described; one with a refusal and no text, refused, held, its Reason the
// refusal's text; an incomplete one keeps its text, held, its Reason, and its chunks' Source's
// Incomplete, why the model stopped.
//
// With Config.Transcription set, an audio output is a window, transcribed from its content, sent
// as window.wav for audio/wav or window.mp3 for audio/mpeg, its content opened by each attempt;
// a window of another media type, or one without a MetaWindow index, stops the job, [Permanent].
// A window's transcript is keyed by transcription's Identity and the window's SHA-256, read from
// the cache, or asked of the model, and held: a refusal, and an incomplete transcript
// (*ai.IncompleteTranscriptError), are held as the window's outcome. Windows are grouped by their
// part's ID and their stage, and placed by MetaWindow, the first event of an index winning. A
// group ends at the first of: the window count of its audio plan record, a record output of
// dispatch.MediaAudioPlan, whose content is read for it; its last window, the one without
// MetaSeamAfter, once every index before it is in; and the end of the inputs. A video plan
// record ends none. Once every window placed in it is in, the group is joined by Config.Join,
// each seam's seconds the earlier window's MetaSeamAfter or else the later one's MetaSeamBefore,
// and its transcript is yielded as one [Inference], then chunked as a source of kind transcript.
// A window refused, failed, incomplete, or never cut joins as "" and is listed in the
// inference's Failed; the transcript is outcome transcribed when every window was, else failed
// when one failed, refused when one was refused, and incomplete otherwise. A window holds its
// input's slot only until its own transcript is in, so a group of more windows than
// Config.Pending never waits on inputs it can't pull.
//
// With Options.Extract set, Run refuses, matching [ErrRequest], an Extract whose prompt is empty
// or isn't UTF-8, or whose name or schema go-ai's json_schema format refuses, and stops,
// [Permanent], when the pipeline has no Config.Extraction, each before it reads an input. Each source text but
// metadata, an output's text, an image's description, and a transcript, is then extracted once,
// unless it is empty: keyed by extraction's Identity for the Extract and the text's SHA-256, read
// from the cache, or streamed from the model by GenerateStream with the prompt as instructions and
// the text as the user message, the Extract's name and schema a strict json_schema format, and
// folded by a protocol.Accumulator. Each text delta is yielded as [Progress] as it streams, at
// most one a second per source text, each carrying the text since the last; the text a stream ends
// with unsent is its inference's. A held extraction yields no progress. The folded reply is held
// before its [Inference] is yielded: a completed one is outcome extracted, its text the reply's
// JSON, which nothing decodes; an incomplete one keeps its partial text, its Reason why the model
// stopped; one with a refusal and no text is refused. A stream that breaks part way holds nothing
// and stops the job, transient, the progress it yielded standing. An input finishes once its
// chunks are complete and its extraction is in.
//
// Each chunk's embedding is keyed by the embeddings model and the chunk's SHA-256 alone, read
// from the cache, or asked of the model Config.EmbedBatch texts per call and held before anything
// derived from it is yielded. A held value that doesn't decode is a miss, which
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
// an embedding call a text at a time, and a text so refused is its chunk's Error, held, as an
// image or extraction so refused is outcome refused, held, its Reason the error's text; a
// permanent or transient failure stops the job with the error, wrapped; a failed one stops it
// too, except on Options.Final, where its chunks' Error, or its image's, window's, or
// extraction's outcome failed, is recorded, never held, and the job goes on. An error stops the
// job: Run cancels its requests, waits for them, and yields the error alone, last. A cancelled
// context yields ctx.Err() alone, last.
//
// The pipeline joins work in flight across its jobs by Key: a chunk's embedding or an image's
// description another job is asking for, read from neither the cache nor the model, is waited
// for, holding no Bounds slot, and counted Held. A window's transcript, and a source text's
// extraction, are joined the same way. A waited-for refusal is the chunk's Error, or the image's,
// window's, or extraction's outcome, and a permanent failure stops this job too; a job that ends
// without a result, cancelled, failing otherwise, or panicking, leaves its waiters to ask the
// model themselves, and a cancelled job stops waiting at once.
//
// yield is called from the goroutine ranging over Run alone, and Run waits for its requests in
// flight, and for its current pull of inputs, before it returns, stopped early or not. A panic on
// one of the job's goroutines, a Cache's or the input stream's, stops the job, and Run panics with
// its value on the goroutine ranging over it once the job's other goroutines have ended.
func (p *Pipeline) Run(ctx context.Context, content dispatch.Content, inputs iter.Seq2[Input, error], opts Options) iter.Seq2[Result, error] {
	return func(yield func(Result, error) bool) {
		// An Extract is checked before an input is read, so a refused one reads nothing.
		if opts.Extract != nil {
			if err := p.checkExtract(*opts.Extract); err != nil {
				yield(Result{}, err)
				return
			}
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
		if p.cfg.Vision != nil {
			r.counts.Vision, r.counts.Tokens.Vision = &VisionCounts{}, &Usage{}
		}
		if p.cfg.Transcription != nil {
			r.counts.Transcription, r.counts.Tokens.Transcription = &TranscriptionCounts{}, &Usage{}
		}
		if opts.Extract != nil {
			r.extractID = p.cfg.Extraction.Identity(*opts.Extract)
			r.counts.Extraction, r.counts.Tokens.Extraction = &ExtractionCounts{}, &Usage{}
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
	// extractID is extraction's identity for the run's Extract, which keys its extractions; set
	// only when the run asks for extraction.
	extractID Identity
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
	// active counts the inputs admitted and not yet finished, and the transcripts joined at the
	// end of the inputs, which hold no slot, not yet chunked and embedded.
	active int
	// groups are the job's window groups, by part and stage, and groupOrder the same in the order
	// their first events were admitted.
	groups      map[groupKey]*group
	groupOrder  []*group
	inputsEnded bool // the input stream has ended: a group still open ends once its count is in
}

// event is one goroutine's news to the coordinator: one of its types below.
type event interface{ isEvent() }

// inputEvent is an input the feeder pulled, its slot held.
type inputEvent struct{ in Input }

// endEvent is the end of the inputs, err the input stream's error, if any.
type endEvent struct{ err error }

// splitEvent is an input's text cut into n chunks, extract set when the text is extracted too,
// after its chunks are under way.
type splitEvent struct {
	st      *inputState
	n       int
	extract bool
}

// doneEvent is an input's chunks complete, embedded or rejected, and the requests that made them.
type doneEvent struct {
	st       *inputState
	items    []item
	requests int
	usage    Usage
}

// inferenceEvent is an image's inference, and the requests that made it and their usage. last
// is set when the input finishes with it, no text following to chunk.
type inferenceEvent struct {
	inf      *Inference
	requests int
	usage    Usage
	last     bool
}

// errEvent is an error that stops the job.
type errEvent struct{ err error }

func (inputEvent) isEvent()     {}
func (endEvent) isEvent()       {}
func (splitEvent) isEvent()     {}
func (doneEvent) isEvent()      {}
func (inferenceEvent) isEvent() {}
func (errEvent) isEvent()       {}

// inputState is one admitted input's, as its source text: what its chunks come from, and, the
// coordinator's alone, how many of them are yet to complete.
type inputState struct {
	part     dispatch.Part
	partJSON []byte // the part's JSON, a batch's Parts element
	source   Source // its Part index set per batch

	split     bool // the coordinator has its count of chunks
	remaining int
	// extracting is set while the text's extraction is under way: the input finishes once it is
	// in and its chunks are complete.
	extracting bool
	// slotless is a transcript's joined at the end of the inputs, which holds no input's slot.
	slotless bool
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
			// The echo is yielded here, before admit starts the input's work, so nothing derived
			// from it can reach the coordinator ahead of it.
			if !yield(Result{Input: &e.in}, nil) {
				return nil
			}
			if err := r.admit(e.in); err != nil {
				return err
			}
		case endEvent:
			if e.err != nil {
				return e.err
			}
			ended = true
			if ok, err := r.endGroups(yield); err != nil || !ok {
				return err
			}
		case splitEvent:
			e.st.split, e.st.remaining, e.st.extracting = true, e.st.remaining+e.n, e.extract
			r.countSource(e.st)
			if e.st.remaining == 0 && !e.st.extracting {
				r.finishState(e.st)
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
			if e.st.split && e.st.remaining == 0 && len(e.items) > 0 && !e.st.extracting {
				r.finishState(e.st)
			}
		case inferenceEvent:
			r.countVision(e)
			if !yield(Result{Inference: e.inf}, nil) {
				return nil
			}
			if e.last {
				r.finish()
			}
		case progressEvent:
			if !yield(Result{Progress: e.p}, nil) {
				return nil
			}
		case extractEvent:
			if ok := r.extractDone(e, yield); !ok {
				return nil
			}
		case windowEvent:
			if ok, err := r.windowDone(e, yield); err != nil || !ok {
				return err
			}
		case planEvent:
			if ok, err := r.planDone(e, yield); err != nil || !ok {
				return err
			}
		case errEvent:
			return e.err
		}
	}
	counts := r.counts
	yield(Result{Done: &counts}, nil)
	return nil
}

// countSource counts an input's source text once it is chunked: an image's text is counted with
// its inference.
func (r *run) countSource(st *inputState) {
	switch st.source.Kind {
	case sourceMetadata:
		r.counts.Metadata++
	case sourceOutput:
		r.counts.Texts++
	}
}

// finish frees a finished input's slot, so the feeder may pull the next.
func (r *run) finish() {
	r.active--
	<-r.slots
}

// finishState finishes the input whose source text st is, or, for a transcript joined at the end
// of the inputs, the transcript, which holds no slot.
func (r *run) finishState(st *inputState) {
	if st.slotless {
		r.active--
		return
	}
	r.finish()
}

// admit starts an input's work by its kind, as dispatch's constants name it, or finishes an input
// with nothing to do at once. It returns an error that stops the job.
func (r *run) admit(in Input) error {
	r.active++
	out := in.Event.Output
	if out == nil {
		if in.Metadata == "" {
			// Neither an output nor a metadata text, a warning or a part's own event: passed
			// through, its echo all it yields, nothing counted.
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
			if out.MediaType == "" {
				return fmt.Errorf("docinfer: run: image %s of part %s has no media type: %w", out.SHA256, in.Event.Part.ID, errInputDefect)
			}
			r.spawn(func() { r.describe(in.Event) })
			return nil
		}
	case dispatch.AudioOutput:
		r.counts.Windows++
		if r.p.cfg.Transcription != nil {
			return r.admitWindow(in.Event)
		}
	case dispatch.RecordOutput:
		// An audio plan record counts its group's windows; a video plan's holds none, and ends no
		// group.
		if r.p.cfg.Transcription != nil && out.MediaType == dispatch.MediaAudioPlan {
			r.admitPlan(in.Event)
			return nil
		}
	}
	// A skipped image or window, another record, or an output of another kind: nothing to do.
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
	b, err := r.read(ev)
	return string(b), err
}

// read reads an output's content from Run's content, by its SHA-256.
func (r *run) read(ev dispatch.Event) ([]byte, error) {
	rc, err := r.content.Open(r.ctx, ev.Output.SHA256)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, &MissingError{Event: ev, Err: err}
	}
	if err != nil {
		return nil, fmt.Errorf("docinfer: run: open content %s of part %s: %w", ev.Output.SHA256, ev.Part.ID, err)
	}
	b, err := io.ReadAll(rc)
	if closeErr := rc.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return nil, fmt.Errorf("docinfer: run: read content %s of part %s: %w", ev.Output.SHA256, ev.Part.ID, err)
	}
	return b, nil
}
