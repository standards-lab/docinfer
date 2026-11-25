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

const sourceTranscript = "transcript"

var windowFileNames = map[string]string{
	"audio/wav":  "window.wav",
	"audio/mpeg": "window.mp3",
}

type groupKey struct {
	origin string
	part   string
	stage  dispatch.Identity
}

// See docs/internals.md
type group struct {
	origin string
	part   dispatch.Part
	stage  dispatch.Identity
	start  time.Time

	windows map[int]*window
	count   int
	last    int
	max     int
	plan    bool

	closed  bool
	n       int
	pending int
	joined  bool

	members []int
	settled bool
}

type window struct {
	g                   *group
	ev                  dispatch.Event
	before, after       float64
	hasBefore, hasAfter bool

	outcome Outcome
	text    string
	held    bool
	usage   Usage
}

type windowEvent struct {
	w        *window
	h        Held
	failed   bool
	held     bool
	requests int
	usage    Usage
}

type planEvent struct {
	g     *group
	count int
}

func (windowEvent) isEvent() {}
func (planEvent) isEvent()   {}

func (r *run) groupOf(in Input) *group {
	ev := in.Event
	k := groupKey{in.Origin, ev.Part.ID, ev.Stage}
	if g, ok := r.groups[k]; ok {
		return g
	}
	if r.groups == nil {
		r.groups = map[groupKey]*group{}
	}
	g := &group{
		origin: in.Origin, part: ev.Part, stage: ev.Stage, start: time.Now(),
		windows: map[int]*window{}, count: -1, last: -1, max: -1,
	}
	r.groups[k] = g
	r.groupOrder = append(r.groupOrder, g)
	return g
}

func (r *run) admitWindow(in Input, pos int) error {
	ev := in.Event
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

	g := r.groupOf(in)
	r.addMember(g, pos)
	if g.closed || g.windows[index] != nil || index >= g.bound() {
		r.counts.Transcription.Unplaced++
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

func (r *run) admitPlan(in Input, pos int) {
	ev := in.Event
	g := r.groupOf(in)
	r.addMember(g, pos)
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

func (r *run) addMember(g *group, pos int) {
	if g.settled {
		r.settleInput(pos)
		return
	}
	g.members = append(g.members, pos)
}

func (r *run) settleGroup(g *group) {
	g.settled = true
	for _, pos := range g.members {
		r.settleInput(pos)
	}
	g.members = nil
}

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

func (g *group) bound() int {
	switch {
	case g.count >= 0:
		return g.count
	case g.last >= 0:
		return g.last + 1
	}
	return math.MaxInt
}

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

func (g *group) close() {
	g.closed = true
	g.n = g.bound()
	if g.n == math.MaxInt {
		g.n = g.max + 1
	}
}

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
			return
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

func (r *run) transcribeLead(w *window, key string, l *lease) {
	defer l.release()
	ev := w.ev
	values, err := r.p.cfg.Cache.Get(r.ctx, key)
	if err != nil {
		l.release()
		r.fail(fmt.Errorf("docinfer: run: cache get for window %s of part %s: %w", ev.Output.SHA256, ev.Part.ID, err))
		return
	}
	if h, ok := decodeHeld(values[0]); ok && h.Vector == "" {
		l.answer(key, answer{held: &h})
		r.send(windowEvent{w: w, h: h, held: true})
		return
	}

	t := r.p.cfg.Transcription
	req := &ai.TranscriptionRequest{
		Model: t.Model,
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
		return
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

type contentError struct{ err error }

func (e *contentError) Error() string { return "docinfer: open window content: " + e.err.Error() }
func (e *contentError) Unwrap() error { return e.err }

func incompleteReason(e *ai.IncompleteTranscriptError) string {
	switch {
	case e.Reason != "":
		return string(e.Reason)
	case e.Status != "":
		return string(e.Status)
	}
	return "incomplete"
}

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

func (r *run) settle(g *group, slot bool, yield func(Result, error) bool) (bool, error) {
	if g.joined || !g.closed || g.pending > 0 {
		if slot {
			r.finish()
		}
		return true, nil
	}
	g.joined = true
	if !slot {
		r.active++
	}
	return r.joinGroup(g, !slot, yield)
}

func (r *run) unplace(w *window) {
	tc := r.counts.Transcription
	switch w.outcome {
	case OutcomeFailed:
		tc.Failed--
	case OutcomeRefused:
		tc.Refused--
	case OutcomeIncomplete:
		tc.Incomplete--
	default:
		tc.Transcribed--
	}
	if w.held {
		tc.Held--
	}
	tc.Unplaced++
}

func (r *run) joinGroup(g *group, slotless bool, yield func(Result, error) bool) (bool, error) {
	n := g.n
	for i, w := range g.windows {
		if i >= n {
			r.unplace(w)
		}
	}
	if n == 0 {
		r.finishState(&inputState{g: g, slotless: slotless})
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
		Origin:        g.origin,
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
		source:   Source{Kind: sourceTranscript, Origin: g.origin, SHA256: textSHA, Stage: &stage, Output: first},
		g:        g,
		slotless: slotless,
	}
	if inf.Outcome != OutcomeTranscribed {
		st.source.Incomplete = string(inf.Outcome)
	}
	if err := r.start(st, func() (string, error) { return text, nil }); err != nil {
		return false, err
	}
	return true, nil
}
