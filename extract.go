package docinfer

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/standards-lab/go-ai/protocol"
)

var errNoExtraction = errors.New("docinfer: extraction asked of a pipeline without Config.Extraction")

const progressEvery = time.Second

type progressEvent struct{ p *Progress }

type extractEvent struct {
	st       *inputState
	inf      *Inference
	requests int
	usage    Usage
}

func (progressEvent) isEvent() {}
func (extractEvent) isEvent()  {}

func (p *Pipeline) checkExtract(e Extract) error {
	switch {
	case e.Prompt == "":
		return fmt.Errorf("docinfer: run: %w: Extract has no prompt", ErrRequest)
	case !utf8.ValidString(e.Prompt):
		return fmt.Errorf("docinfer: run: %w: Extract's prompt isn't UTF-8", ErrRequest)
	}
	probe := extractRequest(&Extraction{Model: "probe"}, e, "probe")
	if err := probe.Validate(); err != nil {
		return fmt.Errorf("docinfer: run: %w: Extract: %w", ErrRequest, err)
	}
	if p.cfg.Extraction == nil {
		return fmt.Errorf("docinfer: run: %w", errNoExtraction)
	}
	return nil
}

func extractRequest(x *Extraction, e Extract, text string) *protocol.Request {
	return &protocol.Request{
		Model:        x.Model,
		Instructions: e.Prompt,
		Input: []protocol.Item{protocol.Message{
			Role: protocol.RoleUser, Content: []protocol.ContentPart{protocol.InputText{Text: text}},
		}},
		Text:            protocol.TextParam{Format: protocol.JSONSchemaFormat{Name: e.Name, Schema: e.Schema, Strict: true}},
		Reasoning:       protocol.ReasoningParam{Effort: x.Reasoning},
		MaxOutputTokens: x.MaxTokens,
		Native:          x.Native,
	}
}

func (st *inputState) textSHA() string {
	if st.source.SHA256 != "" {
		return st.source.SHA256
	}
	return st.source.Output.SHA256
}

func (r *run) extract(st *inputState, text string) {
	start := time.Now()
	key := Key(r.extractID, st.textSHA())
	for {
		l, own, waits := r.p.claims.claim([]string{key})
		if len(own) == 1 {
			r.extractLead(st, text, start, key, l)
			return
		}
		a, err := r.p.claims.wait(r.ctx, waits[0].c)
		if err != nil {
			return
		}
		switch {
		case a.err != nil:
			r.fail(fmt.Errorf("docinfer: run: extract text %s of part %s, as another call asked: %w",
				st.textSHA(), st.part.ID, a.err))
			return
		case a.held != nil:
			r.extracted(st, start, *a.held, true, 0, Usage{})
			return
		}
	}
}

func (r *run) extractLead(st *inputState, text string, start time.Time, key string, l *lease) {
	defer l.release()
	sha := st.textSHA()
	values, err := r.p.cfg.Cache.Get(r.ctx, key)
	if err != nil {
		l.release()
		r.fail(fmt.Errorf("docinfer: run: cache get for the extraction of text %s of part %s: %w", sha, st.part.ID, err))
		return
	}
	if h, ok := decodeHeld(values[0]); ok && h.Vector == "" {
		l.answer(key, answer{held: &h})
		r.extracted(st, start, h, true, 0, Usage{})
		return
	}

	x := r.p.cfg.Extraction
	req := extractRequest(x, *r.opts.Extract, text)
	var (
		resp *protocol.Response
		sent bool
	)
	err = r.p.cfg.Bounds.do(r.ctx, CapabilityExtraction, func(ctx context.Context) error {
		sent = true
		var acc protocol.Accumulator
		c := coalescer{now: r.p.now}
		for e, err := range x.Client.GenerateStream(ctx, req) {
			if err != nil {
				return err
			}
			if err := acc.Add(e); err != nil {
				return err
			}
			d, ok := e.(protocol.OutputTextDelta)
			if !ok {
				continue
			}
			if delta, ok := c.add(d.Delta); ok && !r.send(progressEvent{&Progress{ContentSHA256: sha, Delta: delta}}) {
				return r.ctx.Err()
			}
		}
		resp = acc.Response()
		return nil
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
	what := "the extraction of text " + sha
	if err == nil {
		h := heldReply(resp)
		if !r.hold(l, key, what, h) {
			return
		}
		r.extracted(st, start, h, false, requests, usage)
		return
	}

	wrapped := fmt.Errorf("docinfer: run: extract text %s of part %s: %w", sha, st.part.ID, err)
	switch classify(err) {
	case rowRefused:
		h := Held{Refused: failureText(err)}
		if !r.hold(l, key, "the refusal of "+what, h) {
			return
		}
		r.extracted(st, start, h, false, requests, usage)
	case rowPermanent:
		l.answer(key, answer{err: err})
		r.fail(wrapped)
	case rowFailed:
		l.release()
		if !r.opts.Final {
			r.fail(wrapped)
			return
		}
		inf := r.extraction(st, start, OutcomeFailed, usage)
		inf.Reason = failureText(err)
		r.send(extractEvent{st: st, inf: inf, requests: requests, usage: usage})
	default:
		l.release()
		r.fail(wrapped)
	}
}

type coalescer struct {
	now     func() time.Time
	last    time.Time
	sent    bool
	pending strings.Builder
}

func (c *coalescer) add(delta string) (string, bool) {
	c.pending.WriteString(delta)
	if c.pending.Len() == 0 {
		return "", false
	}
	t := c.now()
	if c.sent && t.Sub(c.last) < progressEvery {
		return "", false
	}
	c.sent, c.last = true, t
	s := c.pending.String()
	c.pending.Reset()
	return s, true
}

func (r *run) extracted(st *inputState, start time.Time, h Held, held bool, requests int, usage Usage) {
	var inf *Inference
	switch {
	case h.Refused != "":
		inf = r.extraction(st, start, OutcomeRefused, usage)
		inf.Reason = h.Refused
	case h.Incomplete != "":
		inf = r.extraction(st, start, OutcomeIncomplete, usage)
		inf.Reason = h.Incomplete
	default:
		inf = r.extraction(st, start, OutcomeExtracted, usage)
	}
	inf.Held = held
	if inf.Outcome != OutcomeRefused && h.Text != "" {
		inf.Text, inf.TextSHA256 = h.Text, sha256Hex([]byte(h.Text))
	}
	r.send(extractEvent{st: st, inf: inf, requests: requests, usage: usage})
}

func (r *run) extraction(st *inputState, start time.Time, outcome Outcome, usage Usage) *Inference {
	return &Inference{
		Capability:    CapabilityExtraction,
		Part:          st.part.ID,
		Origin:        st.source.Origin,
		ContentSHA256: []string{st.textSHA()},
		Outcome:       outcome,
		Usage:         usage,
		TookMS:        time.Since(start).Milliseconds(),
	}
}

func (r *run) extractDone(e extractEvent, yield func(Result, error) bool) bool {
	xc := r.counts.Extraction
	switch e.inf.Outcome {
	case OutcomeExtracted:
		xc.Extracted++
	case OutcomeIncomplete:
		xc.Incomplete++
	case OutcomeRefused:
		xc.Refused++
	case OutcomeFailed:
		xc.Failed++
	}
	if e.inf.Held {
		xc.Held++
	}
	xc.Requests += e.requests
	r.counts.Tokens.Extraction.Input += e.usage.Input
	r.counts.Tokens.Extraction.Output += e.usage.Output
	if !yield(Result{Inference: e.inf}, nil) {
		return false
	}
	e.st.extracting = false
	if e.st.split && e.st.remaining == 0 {
		r.finishState(e.st)
	}
	return true
}
