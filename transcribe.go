package docinfer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/standards-lab/docinfer/join"
	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/go-ai"
)

// sourceTranscript is the source kind of a part's joined transcript.
const sourceTranscript = "transcript"

// windowFileNames are the FileName a window is sent as, by its Output.MediaType: the extension is
// what a chat-mode Transcribe reads the clip's encoding from, and the route's file part's name.
var windowFileNames = map[string]string{
	"audio/wav":  "window.wav",
	"audio/mpeg": "window.mp3",
}

// groupKey names a window group: a part's windows from one stage.
type groupKey struct {
	part  string
	stage dispatch.Identity
}

// group is one part's windows from one stage, the coordinator's alone: the windows placed by
// index, what ends the group, and whether it has been joined.
type group struct {
	part  dispatch.Part
	stage dispatch.Identity
	start time.Time // its first event's admission, from which its inference's time is taken

	windows map[int]*window // placed, by MetaWindow, the first event of an index winning
	count   int             // its audio plan record's window count; -1 until one is read
	last    int             // the lowest index placed without MetaSeamAfter; -1 for none
	max     int             // the highest index placed; -1 for none
	plan    bool            // an audio plan record has been admitted for it, its count read or not

	closed  bool // its windows are all in: no window is placed after
	n       int  // once closed, the windows it joins, indexes 0 to n-1
	pending int  // placed windows whose transcription isn't in
	joined  bool
}

// window is one placed window: its group, its event, its seams, and, once its transcription is
// in, what it came to.
type window struct {
	g                   *group
	ev                  dispatch.Event
	before, after       float64 // the seconds of its seams, MetaSeamBefore's and MetaSeamAfter's
	hasBefore, hasAfter bool

	outcome Outcome // transcribed, incomplete, refused, or failed
	text    string  // a transcribed window's
	held    bool    // read from the cache or a leader
	usage   Usage   // this job's requests' for it
}

// windowEvent is a window's transcription: h its held result, or failed set when it failed on the
// last delivery, never held; held when it was read from the cache or a leader; and the requests
// this job sent for it and their usage.
type windowEvent struct {
	w        *window
	h        Held
	failed   bool
	held     bool
	requests int
	usage    Usage
}

// planEvent is an audio plan record's window count, for its group.
type planEvent struct {
	g     *group
	count int
}

func (windowEvent) isEvent() {}
func (planEvent) isEvent()   {}

// groupOf returns the group of ev's part and stage, made on its first event.
func (r *run) groupOf(ev dispatch.Event) *group {
	k := groupKey{ev.Part.ID, ev.Stage}
	if g, ok := r.groups[k]; ok {
		return g
	}
	if r.groups == nil {
		r.groups = map[groupKey]*group{}
	}
	g := &group{
		part: ev.Part, stage: ev.Stage, start: time.Now(),
		windows: map[int]*window{}, count: -1, last: -1, max: -1,
	}
	r.groups[k] = g
	r.groupOrder = append(r.groupOrder, g)
	return g
}

// admitWindow places a window in its group and starts its transcription, or finishes it at once
// when its group has it already, by index, or has ended without it. A window of a media type
// other than audio/wav or audio/mpeg, or whose Meta doesn't place it, is the input's defect, an
// error that stops the job.
func (r *run) admitWindow(ev dispatch.Event) error {
	out := ev.Output
	if _, ok := windowFileNames[out.MediaType]; !ok {
		return fmt.Errorf("docinfer: run: window %s of part %s has media type %q, not audio/wav or audio/mpeg: %w",
			out.SHA256, ev.Part.ID, out.MediaType, errInputDefect)
	}
	w := &window{ev: ev}
	index, err := strconv.Atoi(out.Meta[dispatch.MetaWindow])
	if err == nil && index < 0 {
		err = errors.New("negative")
	}
	if err != nil {
		return fmt.Errorf("docinfer: run: window %s of part %s: Meta %s %q: %w",
			out.SHA256, ev.Part.ID, dispatch.MetaWindow, out.Meta[dispatch.MetaWindow], errInputDefect)
	}
	for _, s := range []struct {
		key     string
		seconds *float64
		has     *bool
	}{{dispatch.MetaSeamBefore, &w.before, &w.hasBefore}, {dispatch.MetaSeamAfter, &w.after, &w.hasAfter}} {
		v, ok := out.Meta[s.key]
		if !ok {
			continue
		}
		if *s.seconds, err = seamSeconds(v); err != nil {
			return fmt.Errorf("docinfer: run: window %s of part %s: Meta %s %q: %w",
				out.SHA256, ev.Part.ID, s.key, v, errInputDefect)
		}
		*s.has = true
	}

	g := r.groupOf(ev)
	if g.closed || g.windows[index] != nil || index >= g.bound() {
		// A repeated event of a placed window, or a window past the group's end: the first event
		// of an index wins.
		r.finish()
		return nil
	}
	w.g = g
	g.windows[index] = w
	g.pending++
	g.max = max(g.max, index)
	if !w.hasAfter && (g.last < 0 || index < g.last) {
		g.last = index
	}
	if g.complete() {
		g.close()
	}
	r.spawn(func() { r.transcribe(w) })
	return nil
}

// admitPlan admits an audio plan record for its group's window count, read from its content on a
// goroutine of its own; a group that has ended, or has a count already, needs none, and the
// record is finished at once.
func (r *run) admitPlan(ev dispatch.Event) {
	g := r.groupOf(ev)
	if g.closed || g.plan {
		r.finish()
		return
	}
	g.plan = true
	r.spawn(func() {
		b, err := r.read(ev)
		if err != nil {
			r.fail(err)
			return
		}
		// The plan's windows are counted, and nothing else of it is read.
		var plan struct {
			Windows []json.RawMessage `json:"windows"`
		}
		if err := json.Unmarshal(b, &plan); err != nil || plan.Windows == nil {
			if err == nil {
				err = errors.New("it has no windows")
			}
			r.fail(fmt.Errorf("docinfer: run: audio plan %s of part %s: %v: %w", ev.Output.SHA256, ev.Part.ID, err, errInputDefect))
			return
		}
		r.send(planEvent{g: g, count: len(plan.Windows)})
	})
}

// seamSeconds returns the seconds of a seam's span, "<start>-<end>" in seconds, as docproc's
// MetaSeamBefore and MetaSeamAfter give it.
func seamSeconds(v string) (float64, error) {
	a, b, ok := strings.Cut(v, "-")
	if !ok {
		return 0, errors.New("not <start>-<end>")
	}
	start, err := strconv.ParseFloat(a, 64)
	if err != nil {
		return 0, err
	}
	end, err := strconv.ParseFloat(b, 64)
	if err != nil {
		return 0, err
	}
	if math.IsInf(end, 0) || start < 0 || end < start {
		return 0, errors.New("not a span of the source")
	}
	return end - start, nil
}

// bound is the index no window of g reaches: its plan's count, or one past its last window, as
// far as they are known.
func (g *group) bound() int {
	switch {
	case g.count >= 0:
		return g.count
	case g.last >= 0:
		return g.last + 1
	}
	return math.MaxInt
}

// complete reports whether g's windows are all in: every index below its plan's count, or, with
// no count read, every index up to its last window. A plan's count, the windows it lists, decides
// over a window without MetaSeamAfter.
func (g *group) complete() bool {
	n := g.bound()
	if n == math.MaxInt {
		return false
	}
	for i := range n {
		if g.windows[i] == nil {
			return false
		}
	}
	return true
}

// close ends g: no window is placed after, and it joins its windows below its plan's count, or up
// to its last window, or, at the end of Run's inputs with neither known, up to the highest index
// placed. An index not placed by then was never cut.
func (g *group) close() {
	g.closed = true
	g.n = g.bound()
	if g.n == math.MaxInt {
		g.n = g.max + 1
	}
}

// transcribe transcribes w on its own goroutine, as transcribeLead does, unless another call has
// claimed its key: then it waits for that call's answer, holding no Bounds slot, and takes it as
// a cache read, or asks again itself when the claimant ended without a result. A permanent
// failure the claimant answered with stops the job. A cancelled job stops waiting at once.
func (r *run) transcribe(w *window) {
	ev := w.ev
	key := Key(*r.p.profile.Transcription, ev.Output.SHA256)
	for {
		l, own, waits := r.p.claims.claim([]string{key})
		if len(own) == 1 {
			r.transcribeLead(w, key, l)
			return
		}
		a, err := r.p.claims.wait(r.ctx, waits[0].c)
		if err != nil {
			return // the job has stopped; the coordinator reports why
		}
		switch {
		case a.err != nil:
			r.fail(fmt.Errorf("docinfer: run: transcribe window %s of part %s, as another call asked: %w",
				ev.Output.SHA256, ev.Part.ID, a.err))
			return
		case a.held != nil:
			r.send(windowEvent{w: w, h: *a.held, held: true})
			return
		}
	}
}

// transcribeLead transcribes w, its key claimed in l: it reads the cache, and on a miss asks the
// transcription model under the transcription bound, the window's content opened by each
// attempt, then holds the result before it answers the claim. A failure maps by its row: an
// incomplete transcript and a refusal are held as the window's outcome; a permanent failure is
// the claim's answer, and stops the job; a failed call is recorded failed on the last delivery,
// never held, and otherwise stops the job, as a transient failure and a content store's do. A key
// transcribeLead ends without answering is released unanswered, a panic included.
func (r *run) transcribeLead(w *window, key string, l *lease) {
	defer l.release()
	ev := w.ev
	values, err := r.p.cfg.Cache.Get(r.ctx, key)
	if err != nil {
		l.release()
		r.fail(fmt.Errorf("docinfer: run: cache get for window %s of part %s: %w", ev.Output.SHA256, ev.Part.ID, err))
		return
	}
	// A held value that doesn't decode, or holds another capability's vector, is a miss, which
	// the model's reply replaces.
	if h, ok := decodeHeld(values[0]); ok && h.Vector == "" {
		l.answer(key, answer{held: &h})
		r.send(windowEvent{w: w, h: h, held: true})
		return
	}

	t := r.p.cfg.Transcription
	req := &ai.TranscriptionRequest{
		Model: t.Model,
		// Content is opened only on a miss, once per attempt.
		Open: func(ctx context.Context) (io.ReadCloser, error) {
			rc, err := r.content.Open(ctx, ev.Output.SHA256)
			if errors.Is(err, fs.ErrNotExist) {
				return nil, &MissingError{Event: ev, Err: err}
			}
			if err != nil {
				return nil, &contentError{err}
			}
			return rc, nil
		},
		FileName: windowFileNames[ev.Output.MediaType],
		Language: t.Language,
		Prompt:   t.Prompt,
	}
	var (
		reply *ai.Transcription
		sent  bool
	)
	err = r.p.cfg.Bounds.do(r.ctx, CapabilityTranscription, func(ctx context.Context) error {
		sent = true
		var err error
		reply, err = t.Client.Transcribe(ctx, req)
		return err
	})
	if r.ctx.Err() != nil {
		return // the job has stopped; the coordinator reports why
	}
	requests := 0
	if sent {
		requests = 1
	}
	what := "window " + ev.Output.SHA256
	if err == nil {
		usage := Usage{Input: reply.Usage.InputTokens, Output: reply.Usage.OutputTokens}
		h := Held{Text: reply.Text}
		if !r.hold(l, key, "the transcript of "+what, h) {
			return
		}
		r.send(windowEvent{w: w, h: h, requests: requests, usage: usage})
		return
	}

	wrapped := fmt.Errorf("docinfer: run: transcribe window %s of part %s: %w", ev.Output.SHA256, ev.Part.ID, err)
	_, missing := errors.AsType[*MissingError](err)
	_, store := errors.AsType[*contentError](err)
	if missing || store {
		// The content store's failure, not the model's: no row holds it.
		l.release()
		r.fail(wrapped)
		return
	}
	if inc, ok := errors.AsType[*ai.IncompleteTranscriptError](err); ok {
		h := Held{Incomplete: incompleteReason(inc)}
		if !r.hold(l, key, "the incomplete transcript of "+what, h) {
			return
		}
		r.send(windowEvent{w: w, h: h, requests: requests})
		return
	}
	switch classify(err) {
	case rowRefused:
		h := Held{Refused: failureText(err)}
		if !r.hold(l, key, "the refusal of "+what, h) {
			return
		}
		r.send(windowEvent{w: w, h: h, requests: requests})
	case rowPermanent:
		l.answer(key, answer{err: err})
		r.fail(wrapped)
	case rowFailed:
		l.release()
		if !r.opts.Final {
			r.fail(wrapped)
			return
		}
		r.send(windowEvent{w: w, failed: true, requests: requests})
	default:
		l.release()
		r.fail(wrapped)
	}
}

// contentError is a content store's failure to open a window, other than its miss: it stops the
// job, whatever the delivery.
type contentError struct{ err error }

func (e *contentError) Error() string { return "docinfer: open window content: " + e.err.Error() }
func (e *contentError) Unwrap() error { return e.err }

// incompleteReason is what an incomplete transcript is held with: its reason, or else its
// status, or else "incomplete", so a held incomplete transcript is never read as a transcribed
// empty one.
func incompleteReason(e *ai.IncompleteTranscriptError) string {
	switch {
	case e.Reason != "":
		return string(e.Reason)
	case e.Status != "":
		return string(e.Status)
	}
	return "incomplete"
}

// windowDone records a window's transcription, counting it under its outcome, and joins its
// group once the group has ended and every window placed in it is in. It reports false, or an
// error, as coordinate's yield does: the job then stops.
func (r *run) windowDone(e windowEvent, yield func(Result, error) bool) (bool, error) {
	w := e.w
	w.held, w.usage = e.held, e.usage
	tc := r.counts.Transcription
	switch {
	case e.failed:
		w.outcome = OutcomeFailed
		tc.Failed++
	case e.h.Refused != "":
		w.outcome = OutcomeRefused
		tc.Refused++
	case e.h.Incomplete != "":
		w.outcome = OutcomeIncomplete
		tc.Incomplete++
	default:
		w.outcome, w.text = OutcomeTranscribed, e.h.Text
		tc.Transcribed++
	}
	if e.held {
		tc.Held++
	}
	tc.Requests += e.requests
	r.counts.Tokens.Transcription.Input += e.usage.Input
	r.counts.Tokens.Transcription.Output += e.usage.Output
	w.g.pending--
	return r.settle(w.g, true, yield)
}

// planDone takes an audio plan record's window count for its group, unless the group has ended
// meanwhile, and joins the group when that ends it with every window in. A group the end of the
// inputs left open for its count ends now.
func (r *run) planDone(e planEvent, yield func(Result, error) bool) (bool, error) {
	g := e.g
	if !g.closed {
		g.count = e.count
		if r.inputsEnded || g.complete() {
			g.close()
		}
	}
	return r.settle(g, true, yield)
}

// endGroups ends every group still open at the end of Run's inputs, each joined once its windows
// placed are in: an index not placed by then was never cut. A group whose plan record's count is
// still being read ends when it is, so its windows past the last one placed are listed too.
func (r *run) endGroups(yield func(Result, error) bool) (bool, error) {
	r.inputsEnded = true
	for _, g := range r.groupOrder {
		if g.plan && g.count < 0 && !g.closed {
			continue
		}
		if !g.closed {
			g.close()
		}
		if ok, err := r.settle(g, false, yield); !ok || err != nil {
			return ok, err
		}
	}
	return true, nil
}

// settle joins g if it has ended with every window placed in it in. slot is whether the caller
// holds an input's slot, a window's or a plan record's: a joined group's transcript takes it on,
// so its chunks count against Config.Pending as their input's would, and otherwise the input is
// finished. A group joined at the end of Run's inputs, with no input's slot, takes none: no input
// waits on it. A window holds its slot only until its own transcription is in, never for its
// group's later windows, so a group longer than Config.Pending can't stall the inputs it waits
// for.
func (r *run) settle(g *group, slot bool, yield func(Result, error) bool) (bool, error) {
	if g.joined || !g.closed || g.pending > 0 {
		if slot {
			r.finish()
		}
		return true, nil
	}
	g.joined = true
	if !slot {
		r.active++ // the transcript's own, holding no slot
	}
	return r.joinGroup(g, !slot, yield)
}

// joinGroup joins g's window transcripts, yields its transcript's inference, and chunks and
// embeds the transcript as a source of kind transcript. A window refused, failed, incomplete, or
// never cut joins as "" and is listed in Failed. Each seam's seconds are the earlier window's
// MetaSeamAfter, or else the later window's MetaSeamBefore, or else 0, the least bound.
func (r *run) joinGroup(g *group, slotless bool, yield func(Result, error) bool) (bool, error) {
	n := g.n
	if n == 0 {
		// A plan of no windows: nothing to join.
		r.finishState(&inputState{slotless: slotless})
		return true, nil
	}
	texts := make([]string, n)
	shas := make([]string, n)
	seams := make([]float64, n-1)
	var (
		failed                []int
		first                 *dispatch.Output
		anyFailed, anyRefused bool
		usage                 Usage
	)
	// The transcript is held when no window of it was asked of a model, and none is missing.
	held := true
	for i := range n {
		w := g.windows[i]
		if w == nil {
			failed, held = append(failed, i), false
			continue
		}
		shas[i] = w.ev.Output.SHA256
		if first == nil {
			first = new(*w.ev.Output)
		}
		held = held && w.held
		usage.Input += w.usage.Input
		usage.Output += w.usage.Output
		switch w.outcome {
		case OutcomeTranscribed:
			texts[i] = w.text
		case OutcomeFailed:
			failed, anyFailed = append(failed, i), true
		case OutcomeRefused:
			failed, anyRefused = append(failed, i), true
		default:
			failed = append(failed, i)
		}
	}
	for i := range n - 1 {
		switch a, b := g.windows[i], g.windows[i+1]; {
		case a != nil && a.hasAfter:
			seams[i] = a.after
		case b != nil && b.hasBefore:
			seams[i] = b.before
		}
	}
	t, err := join.Join(texts, seams, r.p.cfg.Join)
	if err != nil {
		return false, fmt.Errorf("docinfer: run: join the windows of part %s: %w", g.part.ID, err)
	}

	inf := &Inference{
		Capability:    CapabilityTranscription,
		Part:          g.part.ID,
		ContentSHA256: shas,
		Failed:        failed,
		Held:          held,
		Seams:         t.Seams,
		Usage:         usage,
		TookMS:        time.Since(g.start).Milliseconds(),
	}
	switch {
	case len(failed) == 0:
		inf.Outcome = OutcomeTranscribed
	case anyFailed:
		inf.Outcome = OutcomeFailed
	case anyRefused:
		inf.Outcome = OutcomeRefused
	default:
		inf.Outcome = OutcomeIncomplete
	}
	text := t.Text()
	inf.Text = text
	textSHA := sha256Hex([]byte(text))
	if text != "" {
		inf.TextSHA256 = textSHA
	}
	tc := r.counts.Transcription
	tc.Transcripts++
	for _, s := range t.Seams {
		if s.Matched {
			tc.SeamsMatched++
		} else {
			tc.SeamsUnmatched++
		}
	}
	if !yield(Result{Inference: inf}, nil) {
		return false, nil
	}

	stage := g.stage
	st := &inputState{
		part:     g.part,
		source:   Source{Kind: sourceTranscript, SHA256: textSHA, Stage: &stage, Output: first},
		slotless: slotless,
	}
	if inf.Outcome != OutcomeTranscribed {
		// Its chunks are cut from a transcript with windows missing: why is its outcome.
		st.source.Incomplete = string(inf.Outcome)
	}
	if err := r.start(st, func() (string, error) { return text, nil }); err != nil {
		return false, err
	}
	return true, nil
}
