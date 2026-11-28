package docinfer

import (
	"context"
	"encoding/json/jsontext"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/standards-lab/docinfer/chunk"
	"github.com/standards-lab/go-ai/protocol"
)

const combineRequest = "1"

const combineInstruction = "Each numbered part below is a partial extraction, made by the same request, " +
	"from one of the consecutive parts of one document, in the document's order. Merge them into one reply " +
	"that follows the schema, as one extraction of the whole document would: keep every value the parts " +
	"find, join what continues across parts, and give each value once."

const combineSeparator = "\n\n"

type combineSettings struct {
	Request      string                   `json:"request"`
	Combine      string                   `json:"combine"`
	Name         string                   `json:"name"`
	PromptSHA256 string                   `json:"prompt_sha256"`
	SchemaSHA256 string                   `json:"schema_sha256"`
	MaxTokens    int                      `json:"max_tokens"`
	Effort       protocol.ReasoningEffort `json:"effort"`
	Native       jsontext.Value           `json:"native,omitempty"`
}

func (x Extraction) combineIdentity(e Extract) Identity {
	return Identity{
		Capability: CapabilityExtraction,
		Model:      x.Served,
		Settings: encodeSettings(combineSettings{
			extractionRequest, combineRequest, e.Name, sha256Hex([]byte(e.Prompt)), sha256Hex(e.Schema), x.MaxTokens,
			x.Reasoning, canonical(x.Native),
		}),
	}
}

type textResult struct {
	text     string
	failed   []int
	calls    int
	held     int
	requests int
	usage    Usage
	reasons  map[Outcome]string
}

func (t *textResult) add(c callReply) Outcome {
	t.calls++
	t.held += c.heldCalls()
	t.requests += c.requests
	t.usage.Input += c.usage.Input
	t.usage.Output += c.usage.Output
	o, reason := c.outcome()
	if o == OutcomeExtracted {
		return o
	}
	if t.reasons == nil {
		t.reasons = map[Outcome]string{}
	}
	if _, ok := t.reasons[o]; !ok {
		t.reasons[o] = reason
	}
	return o
}

func (c callReply) result() textResult {
	var t textResult
	t.add(c)
	t.text = c.h.Text
	return t
}

func (t textResult) outcome() (Outcome, string) {
	for _, o := range []Outcome{OutcomeFailed, OutcomeRefused, OutcomeIncomplete} {
		if reason, ok := t.reasons[o]; ok {
			return o, reason
		}
	}
	return OutcomeExtracted, ""
}

func (t textResult) inference(part, origin string, shas []string, start time.Time) *Inference {
	inf := &Inference{
		Capability:    CapabilityExtraction,
		Part:          part,
		Origin:        origin,
		ContentSHA256: shas,
		Failed:        t.failed,
		Held:          t.calls > 0 && t.held == t.calls,
		Usage:         t.usage,
		TookMS:        time.Since(start).Milliseconds(),
	}
	inf.Outcome, inf.Reason = t.outcome()
	if t.text != "" {
		inf.Text, inf.TextSHA256 = t.text, sha256Hex([]byte(t.text))
	}
	return inf
}

func (r *run) extractText(e Extract, id Identity, text, what string) (textResult, bool) {
	x := r.p.cfg.Extraction
	var t textResult
	if utf8.RuneCountInString(text) <= x.WindowRunes {
		c, ok := r.ask(extractCall{e: e, id: id, text: text, sha: sha256Hex([]byte(text)), what: what})
		return c.result(), ok
	}
	s := chunk.Splitter{Bound: x.WindowRunes, Runes: x.WindowRunes, Count: countRunes}
	windows, err := s.Split(r.ctx, text)
	if err != nil {
		r.fail(fmt.Errorf("docinfer: run: window %s: %w", what, err))
		return textResult{}, false
	}
	var replies []string
	for i, w := range windows {
		sha := sha256Hex([]byte(w.Text))
		c, ok := r.ask(extractCall{e: e, id: id, text: w.Text, sha: sha,
			what: fmt.Sprintf("window %d, %s, of %s", i, sha, what)})
		if !ok {
			return textResult{}, false
		}
		if t.add(c) != OutcomeExtracted {
			t.failed = append(t.failed, i)
		}
		if c.h.Text != "" {
			replies = append(replies, c.h.Text)
		}
	}
	combine := Extract{Name: e.Name, Prompt: combineInstruction, Schema: e.Schema}
	cid := x.combineIdentity(e)
	for len(replies) > 1 {
		var next []string
		for _, run := range combineRuns(replies, x.WindowRunes) {
			if len(run) == 1 && utf8.RuneCountInString(run[0]) <= x.WindowRunes {
				next = append(next, run[0])
				continue
			}
			in := combineInput(run)
			sha := sha256Hex([]byte(in))
			c, ok := r.ask(extractCall{e: combine, id: cid, text: in, sha: sha,
				what: fmt.Sprintf("the combined replies %s of %s", sha, what)})
			if !ok {
				return textResult{}, false
			}
			t.add(c)
			if c.h.Text != "" {
				next = append(next, c.h.Text)
			}
		}
		replies = next
	}
	if len(replies) == 1 {
		t.text = replies[0]
	}
	return t, true
}

func countRunes(_ context.Context, text string) (int, error) {
	return utf8.RuneCountInString(text), nil
}

func combineSection(i int, reply string) string {
	return "Part " + strconv.Itoa(i+1) + ":\n" + reply
}

func combineInput(replies []string) string {
	sections := make([]string, len(replies))
	for i, reply := range replies {
		sections[i] = combineSection(i, reply)
	}
	return strings.Join(sections, combineSeparator)
}

func combineRuns(replies []string, bound int) [][]string {
	sep := utf8.RuneCountInString(combineSeparator)
	var runs [][]string
	for start := 0; start < len(replies); {
		end := start + 1
		n := utf8.RuneCountInString(combineSection(0, replies[start]))
		for end < len(replies) {
			m := n + sep + utf8.RuneCountInString(combineSection(end-start, replies[end]))
			if m > bound {
				break
			}
			n, end = m, end+1
		}
		runs = append(runs, replies[start:end])
		start = end
	}
	if len(runs) == len(replies) {
		return [][]string{replies}
	}
	return runs
}
