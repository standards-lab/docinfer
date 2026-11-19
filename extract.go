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

// errNoExtraction is the cause of a run that asks for extraction of a pipeline with no
// Config.Extraction: no redelivery changes the pipeline, so [Permanent] reports it.
var errNoExtraction = errors.New("docinfer: extraction asked of a pipeline without Config.Extraction")

// progressEvery is the least time between two of a source text's progress results.
const progressEvery = time.Second

// progressEvent is an extraction's progress, for the coordinator to yield.
type progressEvent struct{ p *Progress }

// extractEvent is a source text's extraction: its inference, and the requests that made it and
// their usage.
type extractEvent struct {
	st       *inputState
	inf      *Inference
	requests int
	usage    Usage
}

func (progressEvent) isEvent() {}
func (extractEvent) isEvent()  {}

// checkExtract refuses, matching [ErrRequest], an Extract whose prompt is empty or isn't UTF-8,
// or whose name or schema the request's json_schema format refuses: go-ai's rules, a name of 1 to
// 64 letters, digits, _ and -, and a schema that is a JSON object with unique member names. It
// then refuses a run asking for extraction of a pipeline without Config.Extraction, matching
// errNoExtraction.
func (p *Pipeline) checkExtract(e Extract) error {
	switch {
	case e.Prompt == "":
		return fmt.Errorf("docinfer: run: %w: Extract has no prompt", ErrRequest)
	case !utf8.ValidString(e.Prompt):
		return fmt.Errorf("docinfer: run: %w: Extract's prompt isn't UTF-8", ErrRequest)
	}
	// The request's own construction is validated, its model and text stand-ins, so the name's
	// and the schema's rules are go-ai's alone.
	probe := extractRequest(&Extraction{Model: "probe"}, e, "probe")
	if err := probe.Validate(); err != nil {
		return fmt.Errorf("docinfer: run: %w: Extract: %w", ErrRequest, err)
	}
	if p.cfg.Extraction == nil {
		return fmt.Errorf("docinfer: run: %w", errNoExtraction)
	}
	return nil
}

// extractRequest is the request that extracts by e from text with x's settings: the prompt as its
// instructions, one user message of the text, and e's schema as a strict json_schema format.
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
	}
}

// textSHA is st's source text's SHA-256: an output's is its Output.SHA256, held there alone.
func (st *inputState) textSHA() string {
	if st.source.SHA256 != "" {
		return st.source.SHA256
	}
	return st.source.Output.SHA256
}

// extract extracts by the run's Extract from text, st's source text, on the input's goroutine, as
// extractLead does, unless another call has claimed its key: then it waits for that call's
// answer, holding no Bounds slot, and takes it as a cache read, with no progress, or asks again
// itself when the claimant ended without a result. A permanent failure the claimant answered with
// stops the job. A cancelled job stops waiting at once.
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
			return // the job has stopped; the coordinator reports why
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

// extractLead extracts from text, st's source text, its key claimed in l: it reads the cache, and
// on a miss streams the extraction model's reply under the extraction bound, folding its events
// in a protocol.Accumulator and handing the coordinator its text deltas as progress, coalesced,
// then holds the folded reply before it answers the claim. A failure maps by its row, as lead's
// does; a stream that breaks part way is a transport failure, which holds nothing and stops the
// job, the progress already yielded standing. A key extractLead ends without answering is
// released unanswered, a panic included.
func (r *run) extractLead(st *inputState, text string, start time.Time, key string, l *lease) {
	defer l.release()
	sha := st.textSHA()
	values, err := r.p.cfg.Cache.Get(r.ctx, key)
	if err != nil {
		l.release()
		r.fail(fmt.Errorf("docinfer: run: cache get for the extraction of text %s of part %s: %w", sha, st.part.ID, err))
		return
	}
	// A held value that doesn't decode, or holds another capability's vector, is a miss, which
	// the model's reply replaces.
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
		// The sequence ends without an error only after its terminal event.
		resp = acc.Response()
		return nil
	})
	if r.ctx.Err() != nil {
		return // the job has stopped; the coordinator reports why
	}
	requests := 0
	if sent {
		requests = 1
	}
	var usage Usage
	if resp != nil {
		usage = Usage{Input: resp.Usage.InputTokens, Output: resp.Usage.OutputTokens}
		if resp.Status != protocol.StatusCompleted && resp.Status != protocol.StatusIncomplete {
			// A stream that ends in response.failed takes the failed row.
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

// coalescer coalesces an extraction's text deltas into progress: at most one per progressEvery,
// each the text since the last. A delta that arrives within progressEvery of the last progress
// waits for a later delta to carry it; the text a stream ends with unsent is its inference's,
// which follows.
type coalescer struct {
	now     func() time.Time
	last    time.Time // the last progress's
	sent    bool      // a progress has been sent
	pending strings.Builder
}

// add takes delta, and returns the progress to send, the text since the last, and true, when one
// is due: the first, or one progressEvery after the last.
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

// extracted hands the coordinator st's extraction from h, its result, read from the cache or a
// leader when held, or made by requests, which took usage. A completed reply is outcome
// extracted, its text the JSON the schema describes, which nothing decodes; an incomplete one
// keeps its partial text, its Reason why the model stopped; a refusal is outcome refused, its
// Reason the refusal's text.
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

// extraction returns st's extraction's inference of outcome, its time taken since start.
func (r *run) extraction(st *inputState, start time.Time, outcome Outcome, usage Usage) *Inference {
	return &Inference{
		Capability:    CapabilityExtraction,
		Part:          st.part.ID,
		ContentSHA256: []string{st.textSHA()},
		Outcome:       outcome,
		Usage:         usage,
		TookMS:        time.Since(start).Milliseconds(),
	}
}

// extractDone counts a source text's extraction under its outcome, and the requests and tokens
// this job spent on it, yields its inference, and finishes its input once its chunks are complete
// too. It reports false once yield has stopped the job.
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
