package docinfer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"iter"
	"strconv"
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
	// Event is an output's event: a text or Markdown output, and a metadata record, is chunked;
	// image, audio, and other record outputs are described, transcribed, or read as their
	// capabilities say. Its content is opened by its Output.SHA256 from Run's content, only on a
	// cache miss or to chunk its text.
	// For a Metadata input, Event is the depth-0 part's own event.
	Event dispatch.Event
	// Metadata is the input's metadata text, [MetadataText]'s, read when Event has no Output.
	Metadata string
	// Origin names the input, as the host knows it, that Event's part was found in: a part is one
	// part by its Origin and its ID together, so two inputs' parts of one ID, each "/" for its
	// input, are apart. Run echoes it on what it derives from the part, each [Inference] and
	// [Source]. A job of one input may leave it "".
	Origin string
}

// Result is one thing [Pipeline.Run] made. One field is set.
type Result struct {
	// Input is an input, echoed as Run admits it, in pull order, before anything derived from it.
	Input     *Input
	Inference *Inference
	Progress  *Progress
	Batch     *Batch
	// Settled is an input's position in pull order, from 0, yielded once everything derived from
	// that input is out.
	Settled *int
	Done    *Counts // last
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
// and stored MIME type. [Pipeline.Run] renders an input's metadata records itself.
func MetadataText(p dispatch.Part) string {
	var l lines
	l.add("file name", p.Name)
	l.add("size", strconv.FormatInt(p.Size, 10)+" bytes")
	l.add("kind", string(p.ContentType.Kind))
	l.add("route", string(p.ContentType.Route))
	l.add("route reason", p.ContentType.RouteReason)
	l.add("stored MIME type", p.ContentType.Stored)
	return l.String()
}

const (
	sourceOutput   = "output"
	sourceMetadata = "metadata"
)

var errInputDefect = errors.New("docinfer: the input's defect")

// Run infers over one job's inputs, opening their content from content, and yields what it makes
// as it makes it: each [Input] as it admits it, each [Inference], each extraction's [Progress],
// each [Batch] once its chunks' embeddings complete, and the job's [Counts] last.
//
// Each input is echoed as Result.Input exactly once, in pull order, before anything derived from
// it: its inference, its progress, and the batches holding its chunks. An input that is neither an
// output nor a metadata text, such as a warning or a part's own event, is passed through: echoed,
// then settled, with no work and nothing counted; so is a record output that is neither a
// metadata record nor, with Config.Transcription set, an audio plan record.
//
// Each input settles exactly once, before the counts, as Result.Settled, its position in pull
// order from 0, once everything derived from it is out: a text, Markdown, or metadata input's
// batches and extraction; an image's inference, batches, and extraction; for a window, and an audio
// plan record, its group's transcript inference, that transcript's batches, and its extraction, so
// neither settles before its group is joined, at the end of the inputs at the latest; and an input
// that needs no work, a skipped output or a passed-through input, at once, after its echo; a page
// image that skips vision, once its page's text is read.
// Settlement is not in pull order: a window waits on its group while a later text settles. A job
// that stops settles nothing more.
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
// With Vision.LayerWords above 0, a page image skips vision when its page's text layer holds at
// least LayerWords words. The image is matched to its page's text output: one of the document
// stage's, the stage docproc names "document", of the image's Origin, part, stage, and
// dispatch.MetaPage, pulled before the image with no event of their part's own, one with neither
// Output nor Warning set, pulled between them; of several such texts, the last pulled. A text
// matches one image at most. A skipped image is sent no request, yields no [Inference], holds
// nothing, and is counted Skipped in [VisionCounts]; the page's text is chunked, and extracted, as
// any text output. An image with no such text, one whose text holds fewer words, and one of another
// stage, are described.
//
// A metadata record is a record output of dispatch.MediaOfficeMetadata, dispatch.MediaPDFMetadata,
// dispatch.MediaMediaMetadata, or dispatch.MediaPictureMetadata; of dispatch.MediaMail whose Meta
// holds dispatch.MetaSubject, a message; or of dispatch.MediaOneNote whose Meta holds
// dispatch.MetaTitle, a page. It is a metadata text of its own: its content is opened, decoded as
// docproc's metadata.Office, metadata.PDF, metadata.Media, metadata.Picture, email.Message, or
// onenote.Page, and rendered as a line per field, "label: value", in a fixed order, a field with
// no value left out, a date in RFC 3339 in UTC; its text is chunked as a source of kind metadata,
// its Output the record output, its SHA256 the text's, and never extracted. An office record
// renders each core and custom property by its stored key, in stored order, then the extended
// properties Company and Manager, then the format it was converted from; a PDF record renders its
// information dictionary's title, author, subject, keywords, creation and modification dates, and
// custom keys, then its XMP title, creators, description, and subjects, each only when the
// dictionary doesn't hold the same value; a media record renders its duration in seconds, then the
// container's tags, then each stream's tags, each tag by its stored key, in stored order, but the
// tags naming the encoder, container brand, handler, vendor, bit rate, byte or frame counts, or
// the writing tool; a picture record renders its capture time, make, model, and GPS latitude and
// longitude in decimal degrees. A message renders its sender, its To, CC, and BCC
// recipients, each its name and its SMTP address, else its address, in angle brackets, its submit
// time, else its delivery time, as sent, and its subject; a page its title, author, and creation
// and modification times. A record's text holds no file lines, no body, no transport headers,
// threading ids, attachments, or media, no XMP packet, no EXIF dump, no codecs, rates, sizes,
// orientation, or format names, and no tool names. A mail store's or folder's record, a
// notebook's, section group's, or section's, and a conversation's pass through. A record that
// renders no line is no source, and settles once read; a record whose content doesn't decode stops
// the job, [Permanent], the error naming its SHA-256 and part.
//
// With Config.Transcription set, an audio output is a window, transcribed from its content, sent as
// window.wav for audio/wav or window.mp3 for audio/mpeg, its content opened by each attempt; a
// window of another media type, or one without a MetaWindow index, stops the job, [Permanent]. A
// window's transcript is keyed by transcription's Identity and the window's SHA-256, read from the
// cache, or asked of the model, and held: a refusal, and an incomplete transcript
// (*ai.IncompleteTranscriptError), are held as the window's outcome. Windows are grouped by their
// input's Origin, their part's ID, and their stage, so a group is one input's part's windows from
// one stage, and placed by MetaWindow, the first event of an index winning. A window not placed,
// its index already placed, past its group's bound, or after its group closed, is never
// transcribed, and is counted Unplaced in [TranscriptionCounts]; so is a window placed before its
// group's plan record whose index is at or past the plan's count, once the group is joined without
// it, its request and tokens still counted. A group ends at the first of: the window count of its
// audio plan record, a record output of dispatch.MediaAudioPlan, whose content is read for it; its
// last window, the one without MetaSeamAfter, once every index before it is in; and the end of the
// inputs. A video plan record ends none. Once every window placed in it is in, the group is joined
// by Config.Join, each seam's seconds the earlier window's MetaSeamAfter or else the later one's
// MetaSeamBefore, and its transcript is yielded as one [Inference], then chunked as a source of
// kind transcript. A window refused, failed, or never cut joins as "", and one incomplete joins as
// its held text, "" when it has none; each is listed in the inference's Failed. The transcript is
// outcome transcribed when every window was, else failed when one failed, refused when one was
// refused, and incomplete otherwise. A window holds its input's slot only until its own transcript
// is in, so a group of more windows than Config.Pending never waits on inputs it can't pull.
//
// With Options.Extract set, Run refuses, matching [ErrRequest], an Extract whose prompt is empty or
// isn't UTF-8, or whose name or schema go-ai's json_schema format refuses, and stops, [Permanent],
// when the pipeline has no Config.Extraction, each before it reads an input. A metadata text, an
// input's or a record's, is never extracted. Every other source text, an output's text, an image's
// description, or a transcript, is then extracted once, unless it is empty: keyed by extraction's
// Identity for the Extract and the text's SHA-256, read from the cache, or streamed from the model
// by GenerateStream with the prompt as instructions and the text as the user message, the Extract's
// name and schema a strict json_schema format, and folded by a protocol.Accumulator. Each text
// delta is yielded as [Progress] as it streams, at most one a second per source text, each carrying
// the text since the last; the text a stream ends with unsent is its inference's. A held extraction
// yields no progress. The folded reply is held before its [Inference] is yielded: a completed one
// is outcome extracted, its text the reply's JSON, which nothing decodes; an incomplete one keeps
// its partial text, its Reason why the model stopped; one with a refusal and no text is refused. A
// stream that breaks part way holds nothing and stops the job, transient, the progress it yielded
// standing. An input finishes once its chunks are complete and its extraction is in.
//
// Each chunk's embedding is keyed by the embeddings model and the chunk's SHA-256 alone, read
// from the cache, or asked of the model Config.EmbedBatch texts per call and held before anything
// derived from it is yielded. A held value that doesn't decode is a miss, which
// the model's reply replaces; a vector, held or made, whose length isn't Config.Dimensions stops
// the job, [Permanent]. Content is opened only on a miss, or to chunk an output's text or a
// metadata record's; a content store's miss stops the job with a [MissingError].
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
// extraction's outcome failed, is recorded, never held, and the job goes on. A failure of the
// chunker's count stops the job, with no chunk of its text, as the failure table says. An error
// stops the job: Run cancels its requests, waits for them, and yields the error alone, last. A
// cancelled context yields ctx.Err() alone, last.
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
			chunkID: Identity{Capability: CapabilityChunk, Model: p.cfg.Chunker.Model, Settings: p.cfg.Chunker.Settings()},
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
		cancel()
		r.wg.Wait()
		r.repanic()
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		yield(Result{}, err)
	}
}

// See docs/internals.md
type run struct {
	p         *Pipeline
	ctx       context.Context
	cancel    context.CancelFunc
	content   dispatch.Content
	opts      Options
	embedID   Identity
	chunkID   Identity
	extractID Identity
	events    chan event
	slots     chan struct{}
	wg        sync.WaitGroup

	panicMu    sync.Mutex
	panicked   bool
	panicValue any

	counts      Counts
	waiting     []item
	active      int
	groups      map[groupKey]*group
	groupOrder  []*group
	inputsEnded bool
	pulled      int
	settled     []int
	layers      map[partKey]map[pageKey]*layer
}

type event interface{ isEvent() }

type inputEvent struct{ in Input }

type endEvent struct{ err error }

type splitEvent struct {
	st      *inputState
	n       int
	extract bool
}

type doneEvent struct {
	st       *inputState
	items    []item
	requests int
	usage    Usage
}

type inferenceEvent struct {
	inf      *Inference
	requests int
	usage    Usage
	pos      int
	last     bool
}

type emptyEvent struct{ st *inputState }

type skipEvent struct{ pos int }

type errEvent struct{ err error }

func (inputEvent) isEvent()     {}
func (endEvent) isEvent()       {}
func (splitEvent) isEvent()     {}
func (doneEvent) isEvent()      {}
func (inferenceEvent) isEvent() {}
func (emptyEvent) isEvent()     {}
func (skipEvent) isEvent()      {}
func (errEvent) isEvent()       {}

type inputState struct {
	part     dispatch.Part
	partJSON []byte
	source   Source
	pos      int
	g        *group

	split      bool
	remaining  int
	extracting bool
	slotless   bool
}

type item struct {
	st    *inputState
	chunk Chunk
	held  bool
}

func (r *run) send(e event) bool {
	select {
	case r.events <- e:
		return true
	case <-r.ctx.Done():
		return false
	}
}

func (r *run) fail(err error) { r.send(errEvent{err}) }

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

func (r *run) repanic() {
	r.panicMu.Lock()
	v, ok := r.panicValue, r.panicked
	r.panicValue, r.panicked = nil, false
	r.panicMu.Unlock()
	if ok {
		panic(v)
	}
}

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

func (r *run) coordinate(ctx context.Context, yield func(Result, error) bool) error {
	ended := false
	for !ended || r.active > 0 {
		var e event
		select {
		case e = <-r.events:
		case <-ctx.Done():
			return ctx.Err()
		case <-r.ctx.Done():
			return r.ctx.Err()
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		switch e := e.(type) {
		case inputEvent:
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
				r.settleInput(e.pos)
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
		case emptyEvent:
			r.finishState(e.st)
		case skipEvent:
			r.counts.Vision.Skipped++
			r.finish()
			r.settleInput(e.pos)
		case errEvent:
			return e.err
		}
		if len(r.settled) > 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		for _, pos := range r.settled {
			if !yield(Result{Settled: &pos}, nil) {
				return nil
			}
		}
		r.settled = r.settled[:0]
	}
	counts := r.counts
	yield(Result{Done: &counts}, nil)
	return nil
}

func (r *run) countSource(st *inputState) {
	switch st.source.Kind {
	case sourceMetadata:
		r.counts.Metadata++
	case sourceOutput:
		r.counts.Texts++
	}
}

func (r *run) finish() {
	r.active--
	<-r.slots
}

func (r *run) settleInput(pos int) { r.settled = append(r.settled, pos) }

func (r *run) finishState(st *inputState) {
	if st.g != nil {
		r.settleGroup(st.g)
	} else {
		r.settleInput(st.pos)
	}
	if st.slotless {
		r.active--
		return
	}
	r.finish()
}

func (r *run) admit(in Input) error {
	r.active++
	pos := r.pulled
	r.pulled++
	out := in.Event.Output
	if out == nil {
		r.releaseLayers(in)
		if in.Metadata == "" {
			r.finish()
			r.settleInput(pos)
			return nil
		}
		st := &inputState{
			part:   in.Event.Part,
			pos:    pos,
			source: Source{Kind: sourceMetadata, Origin: in.Origin, SHA256: sha256Hex([]byte(in.Metadata))},
		}
		return r.start(st, func() (string, error) { return in.Metadata, nil })
	}
	switch out.Kind {
	case dispatch.TextOutput, dispatch.MarkdownOutput:
		stage := in.Event.Stage
		st := &inputState{
			part:   in.Event.Part,
			pos:    pos,
			source: Source{Kind: sourceOutput, Origin: in.Origin, Stage: &stage, Output: new(*out)},
		}
		text := func() (string, error) { return r.text(in.Event) }
		if l := r.pageLayer(in); l != nil {
			text = func() (string, error) {
				s, err := r.text(in.Event)
				if err == nil {
					l.read(s)
				}
				return s, err
			}
		}
		return r.start(st, text)
	case dispatch.ImageOutput:
		r.counts.Images++
		if r.p.cfg.Vision != nil {
			if out.MediaType == "" {
				return fmt.Errorf("docinfer: run: image %s of part %s has no media type: %w", out.SHA256, in.Event.Part.ID, errInputDefect)
			}
			if l := r.matchLayer(in); l != nil {
				r.spawn(func() { r.layered(in, pos, l) })
				return nil
			}
			r.spawn(func() { r.describe(in, pos) })
			return nil
		}
	case dispatch.AudioOutput:
		r.counts.Windows++
		if r.p.cfg.Transcription != nil {
			return r.admitWindow(in, pos)
		}
	case dispatch.RecordOutput:
		if r.p.cfg.Transcription != nil && out.MediaType == dispatch.MediaAudioPlan {
			r.admitPlan(in, pos)
			return nil
		}
		if render := recordRender(out); render != nil {
			return r.admitRecord(in, pos, render)
		}
	}
	r.finish()
	r.settleInput(pos)
	return nil
}

func (r *run) admitRecord(in Input, pos int, render func([]byte) (string, error)) error {
	ev := in.Event
	stage := ev.Stage
	st := &inputState{
		part:   ev.Part,
		pos:    pos,
		source: Source{Kind: sourceMetadata, Origin: in.Origin, Stage: &stage, Output: new(*ev.Output)},
	}
	b, err := marshal(st.part)
	if err != nil {
		return err
	}
	st.partJSON = b
	r.spawn(func() {
		content, err := r.read(ev)
		if err != nil {
			r.fail(err)
			return
		}
		text, err := render(content)
		if err != nil {
			r.fail(fmt.Errorf("docinfer: run: metadata record %s of part %s: %v: %w", ev.Output.SHA256, ev.Part.ID, err, errInputDefect))
			return
		}
		if text == "" {
			r.send(emptyEvent{st})
			return
		}
		st.source.SHA256 = sha256Hex([]byte(text))
		r.embed(st, text)
	})
	return nil
}

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

func (r *run) text(ev dispatch.Event) (string, error) {
	b, err := r.read(ev)
	return string(b), err
}

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
