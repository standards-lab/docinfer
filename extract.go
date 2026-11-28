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
	tl       *timeline
	inf      *Inference
	requests int
	held     int
	usage    Usage
}

func (progressEvent) isEvent() {}
func (extractEvent) isEvent()  {}

func (p *Pipeline) checkExtract(field string, e Extract) error {
	switch {
	case e.Prompt == "":
		return fmt.Errorf("docinfer: run: %w: %s has no prompt", ErrRequest, field)
	case !utf8.ValidString(e.Prompt):
		return fmt.Errorf("docinfer: run: %w: %s's prompt isn't UTF-8", ErrRequest, field)
	}
	probe := extractRequest(&Extraction{Model: "probe"}, e, "probe")
	if err := probe.Validate(); err != nil {
		return fmt.Errorf("docinfer: run: %w: %s: %w", ErrRequest, field, err)
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

type extractCall struct {
	e    Extract
	id   Identity
	text string
	sha  string
	what string
}

type callReply struct {
	h        Held
	held     bool
	failed   string
	requests int
	usage    Usage
}

func (r *run) extract(st *inputState, text string) {
	start := time.Now()
	sha := st.textSHA()
	c, ok := r.ask(extractCall{e: *r.opts.Extract, id: r.extractID, text: text, sha: sha,
		what: fmt.Sprintf("text %s of part %s", sha, st.part.ID)})
	if !ok {
		return
	}
	t := c.result()
	inf := t.inference(st.part.ID, st.source.Origin, []string{sha}, start)
	r.send(extractEvent{st: st, inf: inf, requests: t.requests, usage: t.usage, held: t.held})
}

func (r *run) ask(c extractCall) (callReply, bool) {
	key := Key(c.id, c.sha)
	for {
		l, own, waits := r.p.claims.claim([]string{key})
		if len(own) == 1 {
			return r.askLead(c, key, l)
		}
		a, err := r.p.claims.wait(r.ctx, waits[0].c)
		if err != nil {
			return callReply{}, false
		}
		switch {
		case a.err != nil:
			r.fail(fmt.Errorf("docinfer: run: extract %s, as another call asked: %w", c.what, a.err))
			return callReply{}, false
		case a.held != nil:
			return callReply{h: *a.held, held: true}, true
		}
	}
}

func (r *run) askLead(c extractCall, key string, l *lease) (callReply, bool) {
	defer l.release()
	values, err := r.p.cfg.Cache.Get(r.ctx, key)
	if err != nil {
		l.release()
		r.fail(fmt.Errorf("docinfer: run: cache get for the extraction of %s: %w", c.what, err))
		return callReply{}, false
	}
	if h, ok := decodeHeld(values[0]); ok && h.Vector == "" {
		l.answer(key, answer{held: &h})
		return callReply{h: h, held: true}, true
	}

	x := r.p.cfg.Extraction
	req := extractRequest(x, c.e, c.text)
	var (
		resp *protocol.Response
		sent bool
	)
	err = r.p.cfg.Bounds.do(r.ctx, CapabilityExtraction, func(ctx context.Context) error {
		sent = true
		var acc protocol.Accumulator
		co := coalescer{now: r.p.now}
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
			if delta, ok := co.add(d.Delta); ok && !r.send(progressEvent{&Progress{ContentSHA256: c.sha, Delta: delta}}) {
				return r.ctx.Err()
			}
		}
		resp = acc.Response()
		return nil
	})
	if r.ctx.Err() != nil {
		return callReply{}, false
	}
	reply := callReply{}
	if sent {
		reply.requests = 1
	}
	if resp != nil {
		reply.usage = Usage{Input: resp.Usage.InputTokens, Output: resp.Usage.OutputTokens}
		if resp.Status != protocol.StatusCompleted && resp.Status != protocol.StatusIncomplete {
			err = &failedReplyError{status: resp.Status, reply: resp.Error}
		}
	}
	what := "the extraction of " + c.what
	if err == nil {
		reply.h = heldReply(resp)
		return reply, r.hold(l, key, what, reply.h)
	}

	wrapped := fmt.Errorf("docinfer: run: extract %s: %w", c.what, err)
	switch classify(err) {
	case rowRefused:
		reply.h = Held{Refused: failureText(err)}
		return reply, r.hold(l, key, "the refusal of "+what, reply.h)
	case rowPermanent:
		l.answer(key, answer{err: err})
		r.fail(wrapped)
	case rowFailed:
		l.release()
		if r.opts.Final {
			reply.failed = failureText(err)
			return reply, true
		}
		r.fail(wrapped)
	default:
		l.release()
		r.fail(wrapped)
	}
	return callReply{}, false
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

func (c callReply) heldCalls() int {
	if c.held {
		return 1
	}
	return 0
}

func (c callReply) outcome() (Outcome, string) {
	switch {
	case c.failed != "":
		return OutcomeFailed, c.failed
	case c.h.Refused != "":
		return OutcomeRefused, c.h.Refused
	case c.h.Incomplete != "":
		return OutcomeIncomplete, c.h.Incomplete
	}
	return OutcomeExtracted, ""
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
	xc.Held += e.held
	xc.Requests += e.requests
	r.counts.Tokens.Extraction.Input += e.usage.Input
	r.counts.Tokens.Extraction.Output += e.usage.Output
	if !yield(Result{Inference: e.inf}, nil) {
		return false
	}
	if e.st == nil {
		if e.tl != nil {
			r.releaseTimeline(e.tl)
		}
		r.active--
		return true
	}
	e.st.extracting = false
	if e.st.split && e.st.remaining == 0 {
		r.finishState(e.st)
	}
	return true
}
