package docinfer_test

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/standards-lab/docinfer"
	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/go-ai/aitest"
	"github.com/standards-lab/go-ai/protocol"
)

const windowBound = 64

var (
	windowParas = []string{
		"The first window of the document holds the total one.",
		"The second window of the document holds the total two.",
		"The third window of the document holds the total three.",
	}
	windowText = strings.Join(windowParas, "\n\n")
	combineID  = combineIdentity(extractID)
)

const combineVersion = "1"

func combineIdentity(id docinfer.Identity) docinfer.Identity {
	id.Settings = strings.Replace(id.Settings, `{"request":"2",`, `{"request":"2","combine":"`+combineVersion+`",`, 1)
	return id
}

func padded(tag string, n int) string {
	return tag + strings.Repeat("x", n-utf8.RuneCountInString(tag))
}

func parts(replies ...string) string {
	sections := make([]string, len(replies))
	for i, r := range replies {
		sections[i] = fmt.Sprintf("Part %d:\n%s", i+1, r)
	}
	return strings.Join(sections, "\n\n")
}

type windowServer struct {
	t            *testing.T
	window       func(i int) aitest.Reply
	combine      func(k int, input string) aitest.Reply
	mu           sync.Mutex
	windows      []string
	combines     []string
	instructions []string
}

func (s *windowServer) route(_ context.Context, req aitest.ChatRequest) aitest.Reply {
	text := req.Messages[len(req.Messages)-1].Text
	s.mu.Lock()
	defer s.mu.Unlock()
	if instruction := req.Messages[0].Text; instruction != extractPrompt {
		if !req.Stream || req.Model != extractModel || req.SchemaName != extractName || len(req.Messages) != 2 || instruction == "" {
			s.t.Errorf("combine request: stream %t, model %q, schema %q, %d messages, instruction %q; "+
				"want streamed, %q, %q, 2, docinfer's own", req.Stream, req.Model, req.SchemaName, len(req.Messages), instruction,
				extractModel, extractName)
		}
		s.instructions = append(s.instructions, instruction)
		s.combines = append(s.combines, text)
		return s.combine(len(s.combines)-1, text)
	}
	checkExtractRequest(s.t, req, text)
	i := slices.Index(windowParas, text)
	if i < 0 {
		s.t.Errorf("window request of %q, none of the paragraphs", text)
		return aitest.ErrorReply{Status: 400, Message: "no such window"}
	}
	s.windows = append(s.windows, text)
	return s.window(i)
}

func windowReplies(replies ...string) func(int) aitest.Reply {
	return func(i int) aitest.Reply { return aitest.ChatReply{Chunks: []string{replies[i]}} }
}

func combineReplies(n int) func(int, string) aitest.Reply {
	return func(k int, _ string) aitest.Reply { return aitest.ChatReply{Chunks: []string{combined(k, n)}} }
}

func combined(k, n int) string { return padded(fmt.Sprintf("c%d", k), n) }

func runWindows(t *testing.T, s *windowServer, bound int, final bool) (outcome, *aitest.Endpoint, docinfer.Config) {
	t.Helper()
	s.t = t
	e := aitest.Serve(t, aitest.Routes{ChatCompletions: s.route, Embeddings: embedAll})
	cfg := extractConfig(t, e, func(c *docinfer.Config) { c.Extraction.WindowRunes = bound })
	content := dispatch.NewMemoryContent()
	o := collect(t, extractPipeline(t, cfg).Run(t.Context(), content,
		seq(metadataInput(), outputInput(t, content, childPart, dispatch.TextOutput, windowText)), documenting(final)))
	if o.err != nil {
		t.Fatal(o.err)
	}
	return o, e, cfg
}

func heldUnder(t *testing.T, cache docinfer.Cache, id docinfer.Identity, text string) *docinfer.Held {
	t.Helper()
	values, err := cache.Get(t.Context(), docinfer.Key(id, sha(text)))
	if err != nil {
		t.Fatal(err)
	}
	if values[0] == nil {
		return nil
	}
	var h docinfer.Held
	if err := json.Unmarshal(values[0], &h); err != nil {
		t.Fatalf("the held value under %s: %v", sha(text), err)
	}
	return &h
}

func TestRunDocumentWindows(t *testing.T) {
	replies := []string{`{"total":1}`, `{"total":2}`, `{"total":3}`}
	final := `{"total":6}`
	s := &windowServer{
		window: func(i int) aitest.Reply {
			return aitest.ChatReply{Chunks: []string{replies[i]}, PromptTokens: 10 + i, CompletionTokens: 3}
		},
		combine: func(int, string) aitest.Reply {
			return aitest.ChatReply{Chunks: []string{final}, PromptTokens: 40, CompletionTokens: 5}
		},
	}
	o, e, cfg := runWindows(t, s, windowBound, false)
	if !slices.Equal(s.windows, windowParas) {
		t.Errorf("windows asked %q, want the paragraphs in order", s.windows)
	}
	input := parts(replies...)
	if !slices.Equal(s.combines, []string{input}) {
		t.Errorf("combines asked %q, want one of the replies in order, %q", s.combines, input)
	}
	usage := docinfer.Usage{Input: 10 + 11 + 12 + 40, Output: 3*3 + 5}
	inf := theDocument(t, o)
	if !slices.Equal(inf.ContentSHA256, []string{sha(windowText)}) || inf.Outcome != docinfer.OutcomeExtracted ||
		inf.Text != final || inf.TextSHA256 != sha(final) || inf.Failed != nil || inf.Held || inf.Usage != usage {
		t.Errorf("document inference %+v, want extracted as the combine's reply, usage %+v", inf, usage)
	}
	if n := len(extractions(o)); n != 1 {
		t.Errorf("%d extractions, want the document's alone", n)
	}
	var shas []string
	for _, p := range o.progress {
		shas = append(shas, p.ContentSHA256)
	}
	if want := []string{sha(windowParas[0]), sha(windowParas[1]), sha(windowParas[2]), sha(input)}; !slices.Equal(shas, want) {
		t.Errorf("progress of %v, want each call's input, %v", shas, want)
	}
	for i, w := range windowParas {
		if h := heldUnder(t, cfg.Cache, extractID, w); h == nil || h.Text != replies[i] {
			t.Errorf("window %d held %+v, want %q", i, h, replies[i])
		}
	}
	if h := heldUnder(t, cfg.Cache, combineID, input); h == nil || h.Text != final {
		t.Errorf("combine held %+v, want %q", h, final)
	}
	checkExtractionCounts(t, o, docinfer.ExtractionCounts{Extracted: 1, Requests: 4}, usage)

	content := dispatch.NewMemoryContent()
	o = collect(t, extractPipeline(t, cfg).Run(t.Context(), content,
		seq(metadataInput(), outputInput(t, content, childPart, dispatch.TextOutput, windowText)), documenting(false)))
	if o.err != nil {
		t.Fatal(o.err)
	}
	if inf := theDocument(t, o); inf.Outcome != docinfer.OutcomeExtracted || !inf.Held || inf.Text != final ||
		inf.Usage != (docinfer.Usage{}) {
		t.Errorf("the held document %+v, want extracted, held, with no usage", inf)
	}
	if len(o.progress) != 0 {
		t.Errorf("a held document yielded progress %+v", o.progress)
	}
	if n := chatRequests(e); n != 4 {
		t.Errorf("%d requests after the cached run, want the first run's 4", n)
	}
	checkExtractionCounts(t, o, docinfer.ExtractionCounts{Extracted: 1, Held: 4}, docinfer.Usage{})
}

func TestRunDocumentWithinWindow(t *testing.T) {
	n := utf8.RuneCountInString(windowText)
	for _, tc := range []struct {
		bound, requests int
	}{{n, 1}, {n - 1, 3}} {
		t.Run(fmt.Sprint(tc.bound), func(t *testing.T) {
			var (
				mu    sync.Mutex
				asked []string
			)
			e := aitest.Serve(t, aitest.Routes{
				ChatCompletions: func(_ context.Context, req aitest.ChatRequest) aitest.Reply {
					mu.Lock()
					defer mu.Unlock()
					asked = append(asked, req.Messages[len(req.Messages)-1].Text)
					return aitest.ChatReply{Chunks: []string{`{"total":1}`}}
				},
				Embeddings: embedAll,
			})
			cfg := extractConfig(t, e, func(c *docinfer.Config) { c.Extraction.WindowRunes = tc.bound })
			content := dispatch.NewMemoryContent()
			o := collect(t, extractPipeline(t, cfg).Run(t.Context(), content,
				seq(metadataInput(), outputInput(t, content, childPart, dispatch.TextOutput, windowText)), documenting(false)))
			if o.err != nil {
				t.Fatal(o.err)
			}
			if len(asked) != tc.requests {
				t.Errorf("%d requests, want %d", len(asked), tc.requests)
			}
			if tc.requests == 1 && asked[0] != windowText {
				t.Errorf("asked %q, want the document text whole", asked[0])
			}
			checkExtractionCounts(t, o, docinfer.ExtractionCounts{Extracted: 1, Requests: tc.requests}, docinfer.Usage{})
		})
	}
}

func TestRunDocumentCombineRounds(t *testing.T) {
	short := func(i int) string { return padded(fmt.Sprintf("w%d", i), 20) }
	wide := func(i int) string { return padded(fmt.Sprintf("w%d", i), 30) }
	over := padded("w0", windowBound+6)
	cases := []struct {
		name     string
		replies  []string
		combines []string
		final    string
	}{
		{
			"a run and a reply left, then their run",
			[]string{short(0), short(1), short(2)},
			[]string{parts(short(0), short(1)), parts(combined(0, 11), short(2))},
			combined(1, 11),
		},
		{
			"no run of two, so one run of all",
			[]string{wide(0), wide(1), wide(2)},
			[]string{parts(wide(0), wide(1), wide(2))},
			combined(0, 11),
		},
		{
			"a reply over the bound alone",
			[]string{over, padded("w1", 11), padded("w2", 11)},
			[]string{parts(over), parts(padded("w1", 11), padded("w2", 11)), parts(combined(0, 11), combined(1, 11))},
			combined(2, 11),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &windowServer{window: windowReplies(tc.replies...), combine: combineReplies(11)}
			o, _, _ := runWindows(t, s, windowBound, false)
			if !slices.Equal(s.combines, tc.combines) {
				t.Errorf("combines asked %q, want %q", s.combines, tc.combines)
			}
			if inf := theDocument(t, o); inf.Outcome != docinfer.OutcomeExtracted || inf.Text != tc.final {
				t.Errorf("document inference %+v, want extracted as %q", inf, tc.final)
			}
			n := len(windowParas) + len(tc.combines)
			checkExtractionCounts(t, o, docinfer.ExtractionCounts{Extracted: 1, Requests: n}, docinfer.Usage{})
		})
	}
}

func TestRunDocumentWindowFailures(t *testing.T) {
	ok := []string{`{"total":1}`, `{"total":2}`, `{"total":3}`}
	partial := `{"total":`
	refused := aitest.ChatReply{Refusal: extractRefuse}
	failed := aitest.ErrorReply{Status: 500, Message: "broke"}
	incomplete := aitest.ChatReply{Chunks: []string{partial}, Finish: "length"}
	replying := func(kinds map[int]aitest.Reply) func(int) aitest.Reply {
		return func(i int) aitest.Reply {
			if r, found := kinds[i]; found {
				return r
			}
			return aitest.ChatReply{Chunks: []string{ok[i]}}
		}
	}
	maxTokens := string(protocol.ReasonMaxOutputTokens)
	cases := []struct {
		name     string
		windows  map[int]aitest.Reply
		combine  aitest.Reply
		want     docinfer.Outcome
		reason   string
		failed   []int
		combines []string
		text     string
		counts   docinfer.ExtractionCounts
	}{
		{"refused", map[int]aitest.Reply{1: refused}, nil, docinfer.OutcomeRefused, extractRefuse, []int{1},
			[]string{parts(ok[0], ok[2])}, combined(0, 11), docinfer.ExtractionCounts{Refused: 1, Requests: 4}},
		{"failed", map[int]aitest.Reply{1: failed}, nil, docinfer.OutcomeFailed, "broke", []int{1},
			[]string{parts(ok[0], ok[2])}, combined(0, 11), docinfer.ExtractionCounts{Failed: 1, Requests: 4}},
		{"incomplete", map[int]aitest.Reply{1: incomplete}, nil, docinfer.OutcomeIncomplete, maxTokens, []int{1},
			[]string{parts(ok[0], partial, ok[2])}, combined(0, 11), docinfer.ExtractionCounts{Incomplete: 1, Requests: 4}},
		{"failed over refused over incomplete", map[int]aitest.Reply{0: incomplete, 1: refused, 2: failed}, nil,
			docinfer.OutcomeFailed, "broke", []int{0, 1, 2}, nil, partial, docinfer.ExtractionCounts{Failed: 1, Requests: 3}},
		{"refused over incomplete", map[int]aitest.Reply{0: refused, 2: incomplete}, nil,
			docinfer.OutcomeRefused, extractRefuse, []int{0, 2}, []string{parts(ok[1], partial)}, combined(0, 11),
			docinfer.ExtractionCounts{Refused: 1, Requests: 4}},
		{"no window replies", map[int]aitest.Reply{0: refused, 1: refused, 2: refused}, nil,
			docinfer.OutcomeRefused, extractRefuse, []int{0, 1, 2}, nil, "", docinfer.ExtractionCounts{Refused: 1, Requests: 3}},
		{"combine refused", nil, refused, docinfer.OutcomeRefused, extractRefuse, nil,
			[]string{parts(ok...)}, "", docinfer.ExtractionCounts{Refused: 1, Requests: 4}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &windowServer{window: replying(tc.windows), combine: combineReplies(11)}
			if tc.combine != nil {
				s.combine = func(int, string) aitest.Reply { return tc.combine }
			}
			o, _, _ := runWindows(t, s, windowBound, true)
			if !slices.Equal(s.combines, tc.combines) {
				t.Errorf("combines asked %q, want %q", s.combines, tc.combines)
			}
			inf := theDocument(t, o)
			if inf.Outcome != tc.want || !strings.Contains(inf.Reason, tc.reason) || !reflect.DeepEqual(inf.Failed, tc.failed) ||
				inf.Text != tc.text {
				t.Errorf("document inference %+v, want %s for %q, failed %v, text %q", inf, tc.want, tc.reason, tc.failed, tc.text)
			}
			checkExtractionCounts(t, o, tc.counts, docinfer.Usage{})
		})
	}
}
