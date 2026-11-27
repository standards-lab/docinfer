package docinfer

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/go-ai/protocol"
)

const (
	sourceVision  = "vision"
	documentStage = "document"
)

func (r *run) describe(in Input, pos int) {
	ev := in.Event
	start := time.Now()
	key := Key(*r.p.profile.Vision, ev.Output.SHA256)
	for {
		l, own, waits := r.p.claims.claim([]string{key})
		if len(own) == 1 {
			r.lead(in, pos, start, key, l)
			return
		}
		a, err := r.p.claims.wait(r.ctx, waits[0].c)
		if err != nil {
			return
		}
		switch {
		case a.err != nil:
			r.fail(fmt.Errorf("docinfer: run: describe image %s of part %s, as another call asked: %w",
				ev.Output.SHA256, ev.Part.ID, a.err))
			return
		case a.held != nil:
			r.described(in, pos, start, *a.held, true, 0, Usage{})
			return
		}
	}
}

func (r *run) lead(in Input, pos int, start time.Time, key string, l *lease) {
	ev := in.Event
	defer l.release()
	values, err := r.p.cfg.Cache.Get(r.ctx, key)
	if err != nil {
		l.release()
		r.fail(fmt.Errorf("docinfer: run: cache get for image %s of part %s: %w", ev.Output.SHA256, ev.Part.ID, err))
		return
	}
	if h, ok := decodeHeld(values[0]); ok && h.Vector == "" {
		l.answer(key, answer{held: &h})
		r.described(in, pos, start, h, true, 0, Usage{})
		return
	}

	data, err := r.read(ev)
	if err != nil {
		l.release()
		r.fail(err)
		return
	}
	v := r.p.cfg.Vision
	data = fit(data, v.MaxPixels)
	content := []protocol.ContentPart{protocol.InputImage{ImageURL: protocol.DataURL(ev.Output.MediaType, data)}}
	if v.Prompt != "" {
		content = append([]protocol.ContentPart{protocol.InputText{Text: v.Prompt}}, content...)
	}
	req := &protocol.Request{
		Model:           v.Model,
		Input:           []protocol.Item{protocol.Message{Role: protocol.RoleUser, Content: content}},
		Reasoning:       protocol.ReasoningParam{Effort: v.Reasoning},
		MaxOutputTokens: v.MaxTokens,
		Native:          v.Native,
	}
	var (
		resp *protocol.Response
		sent bool
	)
	err = r.p.cfg.Bounds.do(r.ctx, CapabilityVision, func(ctx context.Context) error {
		sent = true
		var err error
		resp, err = v.Client.Generate(ctx, req)
		return err
	})
	if r.ctx.Err() != nil {
		return
	}
	requests := 0
	if sent {
		requests = 1
	}
	var usage Usage
	if resp != nil {
		usage = Usage{Input: resp.Usage.InputTokens, Output: resp.Usage.OutputTokens}
		if resp.Status != protocol.StatusCompleted && resp.Status != protocol.StatusIncomplete {
			err = &failedReplyError{status: resp.Status, reply: resp.Error}
		}
	}
	if err == nil {
		h := heldReply(resp)
		if !r.hold(l, key, "the description of image "+ev.Output.SHA256, h) {
			return
		}
		r.described(in, pos, start, h, false, requests, usage)
		return
	}

	wrapped := fmt.Errorf("docinfer: run: describe image %s of part %s: %w", ev.Output.SHA256, ev.Part.ID, err)
	switch classify(err) {
	case rowRefused:
		h := Held{Refused: failureText(err)}
		if !r.hold(l, key, "the refusal of image "+ev.Output.SHA256, h) {
			return
		}
		r.described(in, pos, start, h, false, requests, usage)
	case rowPermanent:
		l.answer(key, answer{err: err})
		r.fail(wrapped)
	case rowFailed:
		l.release()
		if !r.opts.Final {
			r.fail(wrapped)
			return
		}
		inf := r.inference(in, start, OutcomeFailed, usage)
		inf.Reason = failureText(err)
		r.send(inferenceEvent{inf: inf, requests: requests, usage: usage, pos: pos, last: true})
	default:
		l.release()
		r.fail(wrapped)
	}
}

type failedReplyError struct {
	status protocol.Status
	reply  *protocol.ResponseError
}

func (e *failedReplyError) Error() string {
	s := fmt.Sprintf("docinfer: the reply's status is %q", e.status)
	if e.reply != nil {
		s += fmt.Sprintf(": %s (%s)", e.reply.Message, e.reply.Code)
	}
	return s
}

func heldReply(resp *protocol.Response) Held {
	text := resp.OutputText()
	if resp.Status == protocol.StatusIncomplete {
		h := Held{Text: text, Incomplete: "incomplete"}
		if d := resp.IncompleteDetails; d != nil && d.Reason != "" {
			h.Incomplete = string(d.Reason)
		}
		return trimLoop(h)
	}
	if text == "" {
		if refusal := refusalText(resp); refusal != "" {
			return Held{Refused: refusal}
		}
	}
	return Held{Text: text}
}

func refusalText(resp *protocol.Response) string {
	var b strings.Builder
	for _, it := range resp.Output {
		m, ok := it.(protocol.Message)
		if !ok {
			continue
		}
		for _, part := range m.Content {
			if p, ok := part.(protocol.Refusal); ok {
				b.WriteString(p.Refusal)
			}
		}
	}
	return b.String()
}

func (r *run) described(in Input, pos int, start time.Time, h Held, held bool, requests int, usage Usage) {
	ev := in.Event
	var inf *Inference
	switch {
	case h.Refused != "":
		inf = r.inference(in, start, OutcomeRefused, usage)
		inf.Reason = h.Refused
	case h.Incomplete != "":
		inf = r.inference(in, start, OutcomeIncomplete, usage)
		inf.Reason = h.Incomplete
	default:
		inf = r.inference(in, start, OutcomeDescribed, usage)
	}
	inf.Held = held
	if inf.Outcome == OutcomeRefused {
		r.send(inferenceEvent{inf: inf, requests: requests, usage: usage, pos: pos, last: true})
		return
	}
	inf.Text = h.Text
	textSHA := sha256Hex([]byte(h.Text))
	if h.Text != "" {
		inf.TextSHA256 = textSHA
	}
	stage := ev.Stage
	st := &inputState{
		part:   ev.Part,
		pos:    pos,
		source: Source{Kind: sourceVision, Origin: in.Origin, SHA256: textSHA, Incomplete: h.Incomplete, Stage: &stage, Output: new(*ev.Output)},
	}
	b, err := marshal(st.part)
	if err != nil {
		r.fail(err)
		return
	}
	st.partJSON = b
	if !r.send(inferenceEvent{inf: inf, requests: requests, usage: usage}) {
		return
	}
	r.embed(st, h.Text)
}

func (r *run) inference(in Input, start time.Time, outcome Outcome, usage Usage) *Inference {
	ev := in.Event
	return &Inference{
		Capability:    CapabilityVision,
		Part:          ev.Part.ID,
		Origin:        in.Origin,
		ContentSHA256: []string{ev.Output.SHA256},
		Outcome:       outcome,
		Usage:         usage,
		TookMS:        time.Since(start).Milliseconds(),
	}
}

func (r *run) countVision(e inferenceEvent) {
	vc := r.counts.Vision
	switch e.inf.Outcome {
	case OutcomeDescribed:
		vc.Described++
	case OutcomeIncomplete:
		vc.Incomplete++
		if e.inf.Text == "" {
			vc.Empty++
		}
	case OutcomeRefused:
		vc.Refused++
	case OutcomeFailed:
		vc.Failed++
	}
	if e.inf.Held {
		vc.Held++
	}
	vc.Requests += e.requests
	r.counts.Tokens.Vision.Input += e.usage.Input
	r.counts.Tokens.Vision.Output += e.usage.Output
}

func layerWords(text string) int {
	return len(strings.FieldsFunc(text, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }))
}

type partKey struct {
	origin string
	part   string
}

type pageKey struct {
	stage dispatch.Identity
	page  string
}

type layer struct {
	done  chan struct{}
	words int
}

func (l *layer) read(text string) {
	l.words = layerWords(text)
	close(l.done)
}

func (r *run) layerKey(in Input) (partKey, pageKey, bool) {
	ev := in.Event
	if v := r.p.cfg.Vision; v == nil || v.LayerWords == 0 || ev.Stage.Name != documentStage {
		return partKey{}, pageKey{}, false
	}
	page := ev.Output.Meta[dispatch.MetaPage]
	return partKey{origin: in.Origin, part: ev.Part.ID}, pageKey{stage: ev.Stage, page: page}, page != ""
}

func (r *run) pageLayer(in Input) *layer {
	if in.Event.Output.Kind != dispatch.TextOutput {
		return nil
	}
	pk, k, ok := r.layerKey(in)
	if !ok {
		return nil
	}
	l := &layer{done: make(chan struct{})}
	if r.layers == nil {
		r.layers = map[partKey]map[pageKey]*layer{}
	}
	if r.layers[pk] == nil {
		r.layers[pk] = map[pageKey]*layer{}
	}
	r.layers[pk][k] = l
	return l
}

func (r *run) matchLayer(in Input) *layer {
	pk, k, ok := r.layerKey(in)
	if !ok {
		return nil
	}
	l := r.layers[pk][k]
	if l == nil {
		return nil
	}
	delete(r.layers[pk], k)
	if len(r.layers[pk]) == 0 {
		delete(r.layers, pk)
	}
	return l
}

func (r *run) releaseLayers(in Input) {
	if ev := in.Event; ev.Output == nil && ev.Warning == nil {
		delete(r.layers, partKey{origin: in.Origin, part: ev.Part.ID})
	}
}

func (r *run) layered(in Input, pos int, l *layer) {
	select {
	case <-l.done:
	case <-r.ctx.Done():
		return
	}
	if l.words >= r.p.cfg.Vision.LayerWords {
		r.send(skipEvent{pos})
		return
	}
	r.describe(in, pos)
}
