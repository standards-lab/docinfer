package docinfer_test

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"iter"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/standards-lab/docinfer"
	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/aitest"
	"github.com/standards-lab/go-ai/format/completions"
	"github.com/standards-lab/go-ai/protocol"
)

const (
	extractModel  = "extraction-model"
	extractTokens = 200
	extractPrompt = "Extract the total."
	extractName   = "invoice_total"
	extractRefuse = "I can't extract from this."
)

var (
	extractSchema = jsontext.Value(`{"type":"object","properties":{"total":{"type":"number"}},"required":["total"],"additionalProperties":false}`)
	theExtract    = docinfer.Extract{Name: extractName, Prompt: extractPrompt, Schema: extractSchema}
	extractPieces = []string{`{"total"`, `: 12`, `.5`, `}`}
	extractJSON   = strings.Join(extractPieces, "")
	extractID     = docinfer.Extraction{Model: extractModel, MaxTokens: extractTokens, Reasoning: protocol.ReasoningEffortLow}.Identity(theExtract)
)

func extractConfig(t *testing.T, e *aitest.Endpoint, edit func(*docinfer.Config)) docinfer.Config {
	t.Helper()
	return runConfig(t, e, func(c *docinfer.Config) {
		c.Extraction = &docinfer.Extraction{
			Client: newClient(t, e.URL(), ai.TranscribeRoute), Model: extractModel, MaxTokens: extractTokens,
			Reasoning: protocol.ReasoningEffortLow,
		}
		if edit != nil {
			edit(c)
		}
	})
}

func extracting(final bool) docinfer.Options {
	return docinfer.Options{Extract: new(theExtract), Final: final}
}

func extractPipeline(t *testing.T, cfg docinfer.Config, times ...time.Duration) *docinfer.Pipeline {
	t.Helper()
	p := newPipeline(t, cfg)
	if len(times) == 0 {
		times = []time.Duration{0}
	}
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var calls atomic.Int64
	docinfer.SetClock(p, func() time.Time {
		i := min(int(calls.Add(1))-1, len(times)-1)
		return epoch.Add(times[i])
	})
	return p
}

func extractRoute(t *testing.T, text string, reply aitest.Reply) func(context.Context, aitest.ChatRequest) aitest.Reply {
	return func(_ context.Context, req aitest.ChatRequest) aitest.Reply {
		checkExtractRequest(t, req, text)
		return reply
	}
}

func checkExtractRequest(t *testing.T, req aitest.ChatRequest, text string) {
	t.Helper()
	if !req.Stream || req.Model != extractModel || req.MaxTokens != extractTokens || req.ReasoningEffort != "low" ||
		req.SchemaName != extractName {
		t.Errorf("request: stream %t, model %q, max tokens %d, effort %q, schema %q; want streamed, %q, %d, low, %q",
			req.Stream, req.Model, req.MaxTokens, req.ReasoningEffort, req.SchemaName, extractModel, extractTokens, extractName)
	}
	var body struct {
		ResponseFormat struct {
			Type       string `json:"type"`
			JSONSchema struct {
				Strict bool            `json:"strict"`
				Schema json.RawMessage `json:"schema"`
			} `json:"json_schema"`
		} `json:"response_format"`
	}
	if err := json.Unmarshal(req.Body, &body); err != nil {
		t.Fatalf("the request's body: %v", err)
	}
	f := body.ResponseFormat
	var schema bytes.Buffer
	if err := json.Compact(&schema, f.JSONSchema.Schema); err != nil || f.Type != "json_schema" || !f.JSONSchema.Strict ||
		schema.String() != string(extractSchema) {
		t.Errorf("response_format: type %q, strict %t, schema %s; want a strict json_schema of %s",
			f.Type, f.JSONSchema.Strict, f.JSONSchema.Schema, extractSchema)
	}
	if len(req.Messages) != 2 {
		t.Fatalf("request of %d messages, want the prompt's and the text's", len(req.Messages))
	}
	if m := req.Messages[0]; m.Role != "system" || m.Text != extractPrompt {
		t.Errorf("first message: role %q, text %q; want system, the prompt", m.Role, m.Text)
	}
	if m := req.Messages[1]; m.Role != "user" || m.Text != text {
		t.Errorf("second message: role %q, text %q; want user, %q", m.Role, m.Text, text)
	}
}

func heldExtraction(t *testing.T, cache docinfer.Cache, text string) *docinfer.Held {
	t.Helper()
	values, err := cache.Get(t.Context(), docinfer.Key(extractID, sha(text)))
	if err != nil {
		t.Fatal(err)
	}
	if values[0] == nil {
		return nil
	}
	var h docinfer.Held
	if err := json.Unmarshal(values[0], &h); err != nil {
		t.Fatalf("the held extraction: %v", err)
	}
	return &h
}

func extractions(o outcome) []docinfer.Inference {
	var out []docinfer.Inference
	for _, inf := range o.inferences {
		if inf.Capability == docinfer.CapabilityExtraction {
			out = append(out, inf)
		}
	}
	return out
}

func oneExtraction(t *testing.T, o outcome, part dispatch.Part, text string) docinfer.Inference {
	t.Helper()
	infs := extractions(o)
	if len(infs) != 1 {
		t.Fatalf("%d extractions, want 1", len(infs))
	}
	inf := infs[0]
	if inf.Part != part.ID || !slices.Equal(inf.ContentSHA256, []string{sha(text)}) {
		t.Errorf("extraction of part %q, content %v; want part %q, text %s", inf.Part, inf.ContentSHA256, part.ID, sha(text))
	}
	return inf
}

func deltas(t *testing.T, o outcome, text string) []string {
	t.Helper()
	var out []string
	for _, p := range o.progress {
		if p.ContentSHA256 != sha(text) {
			t.Errorf("progress of text %s, want %s", p.ContentSHA256, sha(text))
		}
		out = append(out, p.Delta)
	}
	return out
}

func chatRequests(e *aitest.Endpoint) int {
	n := 0
	for _, r := range e.Requests() {
		if strings.HasSuffix(r.Path, "/chat/completions") {
			n++
		}
	}
	return n
}

func checkExtractionCounts(t *testing.T, o outcome, xc docinfer.ExtractionCounts, usage docinfer.Usage) {
	t.Helper()
	if o.counts == nil || o.counts.Extraction == nil || o.counts.Tokens.Extraction == nil {
		t.Fatalf("counts %+v, want extraction counts", o.counts)
	}
	if *o.counts.Extraction != xc || *o.counts.Tokens.Extraction != usage {
		t.Errorf("extraction counts %+v, tokens %+v; want %+v, %+v", *o.counts.Extraction, *o.counts.Tokens.Extraction, xc, usage)
	}
}

func runText(t *testing.T, p *docinfer.Pipeline, opts docinfer.Options) outcome {
	t.Helper()
	content := dispatch.NewMemoryContent()
	return collect(t, p.Run(t.Context(), content, seq(outputInput(t, content, childPart, dispatch.TextOutput, plainText)), opts))
}

func TestRunExtracts(t *testing.T) {
	e := aitest.Serve(t, aitest.Routes{
		ChatCompletions: extractRoute(t, plainText, aitest.ChatReply{Chunks: extractPieces, PromptTokens: 20, CompletionTokens: 9}),
		Embeddings:      embedAll,
	})
	cfg := extractConfig(t, e, nil)
	p := extractPipeline(t, cfg, 0, 500*time.Millisecond, time.Second, 1200*time.Millisecond)
	content := dispatch.NewMemoryContent()
	inputs := func() iter.Seq2[docinfer.Input, error] {
		return seq(outputInput(t, content, childPart, dispatch.TextOutput, plainText), metadataInput(),
			outputInput(t, content, childPart, dispatch.MarkdownOutput, ""))
	}

	o := collect(t, p.Run(t.Context(), content, inputs(), extracting(false)))
	if o.err != nil {
		t.Fatal(o.err)
	}
	inf := oneExtraction(t, o, childPart, plainText)
	if inf.Outcome != docinfer.OutcomeExtracted || inf.Text != extractJSON || inf.TextSHA256 != sha(extractJSON) ||
		inf.Reason != "" || inf.Held || inf.Usage != (docinfer.Usage{Input: 20, Output: 9}) || inf.Failed != nil {
		t.Errorf("extraction %+v, want extracted, its JSON, and the reply's usage", inf)
	}
	if got, want := deltas(t, o, plainText), []string{extractPieces[0], extractPieces[1] + extractPieces[2]}; !slices.Equal(got, want) {
		t.Errorf("progress %q, want %q", got, want)
	}
	if h := heldExtraction(t, cfg.Cache, plainText); h == nil || *h != (docinfer.Held{Text: extractJSON}) {
		t.Errorf("held %+v, want the extraction's JSON", h)
	}
	if n := chatRequests(e); n != 1 {
		t.Errorf("%d extraction requests, want the text output's alone", n)
	}
	checkExtractionCounts(t, o, docinfer.ExtractionCounts{Extracted: 1, Requests: 1}, docinfer.Usage{Input: 20, Output: 9})
	if o.counts.Texts != 2 || o.counts.Metadata != 1 || o.counts.Embedded == 0 {
		t.Errorf("counts %+v, want the texts and the metadata chunked", o.counts)
	}

	o = collect(t, p.Run(t.Context(), content, inputs(), extracting(false)))
	if o.err != nil {
		t.Fatal(o.err)
	}
	if inf := oneExtraction(t, o, childPart, plainText); inf.Outcome != docinfer.OutcomeExtracted || !inf.Held ||
		inf.Text != extractJSON || inf.Usage != (docinfer.Usage{}) {
		t.Errorf("the held extraction %+v, want extracted, held, with no usage", inf)
	}
	if len(o.progress) != 0 {
		t.Errorf("a held extraction yielded progress %+v", o.progress)
	}
	if n := chatRequests(e); n != 1 {
		t.Errorf("%d extraction requests after the second run, want the first run's one", n)
	}
	checkExtractionCounts(t, o, docinfer.ExtractionCounts{Extracted: 1, Held: 1}, docinfer.Usage{})
}

func TestRunExtractsInferredTexts(t *testing.T) {
	var (
		mu    sync.Mutex
		asked []string
	)
	e := aitest.Serve(t, aitest.Routes{
		ChatCompletions: func(_ context.Context, req aitest.ChatRequest) aitest.Reply {
			if !req.Stream {
				return aitest.ChatReply{Text: description}
			}
			text := req.Messages[len(req.Messages)-1].Text
			checkExtractRequest(t, req, text)
			mu.Lock()
			asked = append(asked, text)
			mu.Unlock()
			return aitest.ChatReply{Chunks: []string{`{"total":1}`}}
		},
		Transcriptions: func(context.Context, aitest.TranscriptionRequest) aitest.Reply {
			return aitest.TranscriptionReply{Text: said0}
		},
		Embeddings: embedAll,
	})
	client := newClient(t, e.URL(), ai.TranscribeRoute)
	cfg := extractConfig(t, e, func(c *docinfer.Config) {
		c.Vision = &docinfer.Vision{Client: client, Model: visionModel, MaxTokens: visionTokens}
		c.Transcription = &docinfer.Transcription{Client: client, Model: transcriptionModel}
	})
	content := dispatch.NewMemoryContent()
	inputs := seq(imageInput(t, content, imageBytes, imageType), windowInput(t, content, win{index: 0, clip: "clip 0", said: said0}),
		metadataInput())
	o := collect(t, newPipeline(t, cfg).Run(t.Context(), content, inputs, extracting(false)))
	if o.err != nil {
		t.Fatal(o.err)
	}
	transcriptText := joinedAt(t, []string{said0}).Text()
	slices.Sort(asked)
	if want := []string{description, transcriptText}; !slices.Equal(asked, slices.Sorted(slices.Values(want))) {
		t.Errorf("extracted %q, want the description and the transcript", asked)
	}
	infs := extractions(o)
	if len(infs) != 2 {
		t.Fatalf("%d extractions, want 2", len(infs))
	}
	for _, want := range []struct {
		part dispatch.Part
		text string
	}{{childPart, description}, {audioPart, transcriptText}} {
		if !slices.ContainsFunc(infs, func(inf docinfer.Inference) bool {
			return inf.Part == want.part.ID && slices.Equal(inf.ContentSHA256, []string{sha(want.text)}) &&
				inf.Outcome == docinfer.OutcomeExtracted && inf.Text == `{"total":1}`
		}) {
			t.Errorf("no extraction of %q on part %s among %+v", want.text, want.part.ID, infs)
		}
	}
	checkExtractionCounts(t, o, docinfer.ExtractionCounts{Extracted: 2, Requests: 2}, docinfer.Usage{})
}

func TestRunExtractionProgressStreams(t *testing.T) {
	gate := &aitest.Gate{}
	e := aitest.Serve(t, aitest.Routes{
		ChatCompletions: extractRoute(t, plainText, aitest.ChatReply{Chunks: extractPieces, Hold: gate, HoldAfter: 1}),
		Embeddings:      embedAll,
	})
	p := extractPipeline(t, extractConfig(t, e, nil))
	content := dispatch.NewMemoryContent()
	results := p.Run(t.Context(), content, seq(outputInput(t, content, childPart, dispatch.TextOutput, plainText)), extracting(false))
	first := make(chan struct{})
	done := make(chan outcome, 1)
	go func() {
		var once sync.Once
		done <- collect(t, func(yield func(docinfer.Result, error) bool) {
			for r, err := range results {
				if r.Progress != nil {
					once.Do(func() { close(first) })
				}
				if !yield(r, err) {
					return
				}
			}
		})
	}()
	gate.Await(t, 1)
	receive(t, first, "progress while the stream is held")
	gate.Open()

	o := receive(t, done, "outcome of the run")
	if o.err != nil {
		t.Fatal(o.err)
	}
	if got := deltas(t, o, plainText); !slices.Equal(got, extractPieces[:1]) {
		t.Errorf("progress %q, want %q", got, extractPieces[:1])
	}
	if inf := oneExtraction(t, o, childPart, plainText); inf.Outcome != docinfer.OutcomeExtracted || inf.Text != extractJSON {
		t.Errorf("extraction %+v, want the whole JSON extracted", inf)
	}
}

func TestRunExtractionRefusal(t *testing.T) {
	e := aitest.Serve(t, aitest.Routes{
		ChatCompletions: extractRoute(t, plainText, aitest.ChatReply{Refusal: extractRefuse, PromptTokens: 20, CompletionTokens: 4}),
		Embeddings:      embedAll,
	})
	cfg := extractConfig(t, e, nil)
	p := extractPipeline(t, cfg)
	o := runText(t, p, extracting(false))
	if o.err != nil {
		t.Fatal(o.err)
	}
	if inf := oneExtraction(t, o, childPart, plainText); inf.Outcome != docinfer.OutcomeRefused || inf.Reason != extractRefuse ||
		inf.Text != "" || inf.TextSHA256 != "" || inf.Held {
		t.Errorf("extraction %+v, want refused for the refusal's text", inf)
	}
	if h := heldExtraction(t, cfg.Cache, plainText); h == nil || *h != (docinfer.Held{Refused: extractRefuse}) {
		t.Errorf("held %+v, want the refusal", h)
	}
	checkExtractionCounts(t, o, docinfer.ExtractionCounts{Refused: 1, Requests: 1}, docinfer.Usage{Input: 20, Output: 4})

	o = runText(t, p, extracting(false))
	if inf := oneExtraction(t, o, childPart, plainText); o.err != nil || inf.Outcome != docinfer.OutcomeRefused ||
		inf.Reason != extractRefuse || !inf.Held {
		t.Errorf("the second run: %v, extraction %+v; want the held refusal", o.err, inf)
	}
	checkExtractionCounts(t, o, docinfer.ExtractionCounts{Refused: 1, Held: 1}, docinfer.Usage{})
}

func TestRunExtractionIncomplete(t *testing.T) {
	partial := strings.Join(extractPieces[:2], "")
	e := aitest.Serve(t, aitest.Routes{
		ChatCompletions: extractRoute(t, plainText, aitest.ChatReply{Chunks: extractPieces[:2], Finish: "length", CompletionTokens: extractTokens}),
		Embeddings:      embedAll,
	})
	cfg := extractConfig(t, e, nil)
	p := extractPipeline(t, cfg)
	o := runText(t, p, extracting(false))
	if o.err != nil {
		t.Fatal(o.err)
	}
	if inf := oneExtraction(t, o, childPart, plainText); inf.Outcome != docinfer.OutcomeIncomplete ||
		inf.Reason != string(protocol.ReasonMaxOutputTokens) || inf.Text != partial || inf.TextSHA256 != sha(partial) || inf.Held {
		t.Errorf("extraction %+v, want incomplete for max_output_tokens, its partial text kept", inf)
	}
	if h := heldExtraction(t, cfg.Cache, plainText); h == nil ||
		*h != (docinfer.Held{Text: partial, Incomplete: string(protocol.ReasonMaxOutputTokens)}) {
		t.Errorf("held %+v, want the partial text and its reason", h)
	}
	checkExtractionCounts(t, o, docinfer.ExtractionCounts{Incomplete: 1, Requests: 1}, docinfer.Usage{Output: extractTokens})

	o = runText(t, p, extracting(false))
	if inf := oneExtraction(t, o, childPart, plainText); o.err != nil || inf.Outcome != docinfer.OutcomeIncomplete ||
		inf.Text != partial || !inf.Held {
		t.Errorf("the second run: %v, extraction %+v; want the held partial text", o.err, inf)
	}
}

func extractStopped(t *testing.T, o outcome, e *aitest.Endpoint, cache docinfer.Cache) {
	t.Helper()
	if o.counts != nil || len(extractions(o)) > 0 {
		t.Error("a stopped run yielded an extraction or counts")
	}
	if n := chatRequests(e); n > 1 {
		t.Errorf("%d extraction requests, want at most the one", n)
	}
	if h := heldExtraction(t, cache, plainText); h != nil {
		t.Errorf("a stopped run held %+v", h)
	}
}

func TestRunExtractionBrokenStream(t *testing.T) {
	for _, final := range []bool{false, true} {
		t.Run(fmt.Sprintf("final=%t", final), func(t *testing.T) {
			e := aitest.Serve(t, aitest.Routes{
				ChatCompletions: extractRoute(t, plainText, aitest.ChatReply{Chunks: extractPieces, BreakAfter: 2}),
				Embeddings:      embedAll,
			})
			cfg := extractConfig(t, e, nil)
			o := runText(t, extractPipeline(t, cfg), extracting(final))
			if !errors.Is(o.err, io.ErrUnexpectedEOF) {
				t.Fatalf("Run over a broken stream: %v, want io.ErrUnexpectedEOF", o.err)
			}
			if docinfer.Permanent(o.err) {
				t.Errorf("Permanent(%v) is true", o.err)
			}
			if got := deltas(t, o, plainText); !slices.Equal(got, extractPieces[:1]) {
				t.Errorf("progress %q, want the first delta's, yielded before the break", got)
			}
			extractStopped(t, o, e, cfg.Cache)
		})
	}
}

func TestRunExtractionRefusedStatus(t *testing.T) {
	for _, status := range []int{400, 413, 415, 422} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			e := aitest.Serve(t, aitest.Routes{
				ChatCompletions: extractRoute(t, plainText, aitest.ErrorReply{Status: status, Message: "text refused"}),
				Embeddings:      embedAll,
			})
			cfg := extractConfig(t, e, nil)
			o := runText(t, extractPipeline(t, cfg), extracting(false))
			if o.err != nil {
				t.Fatalf("Run: %v", o.err)
			}
			want := (&ai.APIError{Status: status, Message: "text refused"}).Error()
			if inf := oneExtraction(t, o, childPart, plainText); inf.Outcome != docinfer.OutcomeRefused || inf.Reason != want || inf.Held {
				t.Errorf("extraction %+v, want refused for %q", inf, want)
			}
			if h := heldExtraction(t, cfg.Cache, plainText); h == nil || *h != (docinfer.Held{Refused: want}) {
				t.Errorf("held %+v, want the refusal", h)
			}
			checkExtractionCounts(t, o, docinfer.ExtractionCounts{Refused: 1, Requests: 1}, docinfer.Usage{})
		})
	}
}

func TestRunExtractionPermanent(t *testing.T) {
	for _, status := range []int{401, 403, 404, 405, 409, 418, 451, 501, 505} {
		for _, final := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d final=%t", status, final), func(t *testing.T) {
				e := aitest.Serve(t, aitest.Routes{
					ChatCompletions: extractRoute(t, plainText, aitest.ErrorReply{Status: status, Message: "no"}),
					Embeddings:      embedAll,
				})
				cfg := extractConfig(t, e, nil)
				o := runText(t, extractPipeline(t, cfg), extracting(final))
				if apiErr, ok := errors.AsType[*ai.APIError](o.err); !ok || apiErr.Status != status {
					t.Fatalf("Run: %v, want the %d *ai.APIError", o.err, status)
				}
				if !docinfer.Permanent(o.err) {
					t.Errorf("Permanent(%v) is false", o.err)
				}
				extractStopped(t, o, e, cfg.Cache)
			})
		}
	}
	for _, final := range []bool{false, true} {
		t.Run(fmt.Sprintf("unsupported final=%t", final), func(t *testing.T) {
			e := aitest.Serve(t, aitest.Routes{Embeddings: embedAll})
			client, err := ai.New(e.URL(), http.DefaultClient, completions.New(completions.Options{
				RefusedEfforts: []protocol.ReasoningEffort{protocol.ReasoningEffortLow},
			}), ai.Options{})
			if err != nil {
				t.Fatal(err)
			}
			cfg := extractConfig(t, e, func(c *docinfer.Config) { c.Extraction.Client = client })
			o := runText(t, extractPipeline(t, cfg), extracting(final))
			if !docinfer.Permanent(o.err) {
				t.Errorf("Run: %v, want a permanent error", o.err)
			}
			if n := chatRequests(e); n != 0 {
				t.Errorf("%d extraction requests, want none sent", n)
			}
			extractStopped(t, o, e, cfg.Cache)
		})
	}
}

func TestRunExtractionTransient(t *testing.T) {
	for _, status := range []int{408, 429, 502, 503, 504, 507, 599} {
		for _, final := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d final=%t", status, final), func(t *testing.T) {
				e := aitest.Serve(t, aitest.Routes{
					ChatCompletions: extractRoute(t, plainText, aitest.ErrorReply{Status: status, Message: "later", RetryAfter: "7"}),
					Embeddings:      embedAll,
				})
				cfg := extractConfig(t, e, nil)
				o := runText(t, extractPipeline(t, cfg), extracting(final))
				apiErr, ok := errors.AsType[*ai.APIError](o.err)
				if !ok || apiErr.Status != status {
					t.Fatalf("Run: %v, want the %d *ai.APIError", o.err, status)
				}
				if apiErr.RetryAfter != 7*time.Second {
					t.Errorf("RetryAfter %v, want 7s", apiErr.RetryAfter)
				}
				if docinfer.Permanent(o.err) {
					t.Errorf("Permanent(%v) is true", o.err)
				}
				extractStopped(t, o, e, cfg.Cache)
			})
		}
	}
}

func TestRunExtractionFailed(t *testing.T) {
	cases := []struct {
		name   string
		reply  aitest.Reply
		reason string
	}{
		{"500", aitest.ErrorReply{Status: 500, Message: "broke"}, "500 Internal Server Error: broke"},
		{"stream failed", aitest.RawReply{Frames: []aitest.Frame{
			{Data: `{"error":{"message":"the model broke","type":"server_error"}}`},
		}}, "the model broke"},
		{"undecodable", aitest.RawReply{Frames: []aitest.Frame{{Data: `not JSON`}}}, "completions"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := aitest.Serve(t, aitest.Routes{ChatCompletions: extractRoute(t, plainText, tc.reply), Embeddings: embedAll})
			cfg := extractConfig(t, e, nil)
			o := runText(t, extractPipeline(t, cfg), extracting(false))
			if o.err == nil || docinfer.Permanent(o.err) || errors.Is(o.err, io.ErrUnexpectedEOF) {
				t.Fatalf("Run: %v, want an error neither permanent nor a transport failure", o.err)
			}
			extractStopped(t, o, e, cfg.Cache)

			cfg = extractConfig(t, e, nil)
			o = runText(t, extractPipeline(t, cfg), extracting(true))
			if o.err != nil {
				t.Fatalf("the final Run: %v, want the job to go on", o.err)
			}
			inf := oneExtraction(t, o, childPart, plainText)
			if inf.Outcome != docinfer.OutcomeFailed || !strings.Contains(inf.Reason, tc.reason) || inf.Held || inf.Text != "" {
				t.Errorf("extraction %+v, want failed, its reason naming %q", inf, tc.reason)
			}
			if h := heldExtraction(t, cfg.Cache, plainText); h != nil {
				t.Errorf("a failed extraction was held: %+v", h)
			}
			checkExtractionCounts(t, o, docinfer.ExtractionCounts{Failed: 1, Requests: 1}, docinfer.Usage{})
			if o.counts.Texts != 1 || o.counts.Embedded == 0 {
				t.Errorf("counts %+v, want the text embedded", o.counts)
			}
		})
	}
}

func TestRunJoinsExtraction(t *testing.T) {
	gate := &aitest.Gate{}
	e := aitest.Serve(t, aitest.Routes{
		ChatCompletions: func(ctx context.Context, req aitest.ChatRequest) aitest.Reply {
			checkExtractRequest(t, req, plainText)
			if err := gate.Wait(ctx); err != nil {
				return aitest.ErrorReply{Status: 503, Message: err.Error()}
			}
			return aitest.ChatReply{Chunks: extractPieces, PromptTokens: 20, CompletionTokens: 9}
		},
		Embeddings: embedAll,
	})
	p := extractPipeline(t, extractConfig(t, e, nil))
	waits := make(chan struct{}, 64)
	docinfer.OnWait(p, func() { waits <- struct{}{} })
	content := dispatch.NewMemoryContent()
	in := outputInput(t, content, childPart, dispatch.TextOutput, plainText)

	batched := make(chan struct{})
	leader := make(chan outcome, 1)
	go func() {
		var once sync.Once
		results := func(yield func(docinfer.Result, error) bool) {
			for r, err := range p.Run(t.Context(), content, seq(in), extracting(false)) {
				if r.Batch != nil {
					once.Do(func() { close(batched) })
				}
				if !yield(r, err) {
					return
				}
			}
		}
		leader <- collect(t, results)
	}()
	gate.Await(t, 1)
	receive(t, batched, "batch from the leader")
	waiter := startRun(t, p, content, in, extracting(false))
	awaitWaiters(t, waits, 1)
	gate.Open()

	lo, wo := receive(t, leader, "outcome of the leader"), waiter.outcome(t)
	for _, o := range []outcome{lo, wo} {
		if o.err != nil {
			t.Fatal(o.err)
		}
	}
	if n := chatRequests(e); n != 1 {
		t.Errorf("%d extraction requests, want 1", n)
	}
	if inf := oneExtraction(t, lo, childPart, plainText); inf.Held || inf.Text != extractJSON {
		t.Errorf("the leader's extraction %+v, want made", inf)
	}
	if inf := oneExtraction(t, wo, childPart, plainText); !inf.Held || inf.Text != extractJSON || inf.Usage != (docinfer.Usage{}) {
		t.Errorf("the waiter's extraction %+v, want held, with no usage", inf)
	}
	if len(wo.progress) != 0 {
		t.Errorf("the waiter yielded progress %+v", wo.progress)
	}
	checkExtractionCounts(t, lo, docinfer.ExtractionCounts{Extracted: 1, Requests: 1}, docinfer.Usage{Input: 20, Output: 9})
	checkExtractionCounts(t, wo, docinfer.ExtractionCounts{Extracted: 1, Held: 1}, docinfer.Usage{})
}

func unpulled(t *testing.T) iter.Seq2[docinfer.Input, error] {
	return func(func(docinfer.Input, error) bool) {
		t.Error("Run pulled an input")
	}
}

func TestRunRefusesExtract(t *testing.T) {
	cases := []struct {
		name    string
		extract docinfer.Extract
	}{
		{"no name", docinfer.Extract{Prompt: extractPrompt, Schema: extractSchema}},
		{"name with a space", docinfer.Extract{Name: "invoice total", Prompt: extractPrompt, Schema: extractSchema}},
		{"name too long", docinfer.Extract{Name: strings.Repeat("n", 65), Prompt: extractPrompt, Schema: extractSchema}},
		{"no prompt", docinfer.Extract{Name: extractName, Schema: extractSchema}},
		{"prompt not UTF-8", docinfer.Extract{Name: extractName, Prompt: "total \xff", Schema: extractSchema}},
		{"no schema", docinfer.Extract{Name: extractName, Prompt: extractPrompt}},
		{"schema not an object", docinfer.Extract{Name: extractName, Prompt: extractPrompt, Schema: jsontext.Value(`["total"]`)}},
		{"schema not JSON", docinfer.Extract{Name: extractName, Prompt: extractPrompt, Schema: jsontext.Value(`{"type":`)}},
		{"schema of duplicate members", docinfer.Extract{Name: extractName, Prompt: extractPrompt,
			Schema: jsontext.Value(`{"type":"object","type":"string"}`)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := aitest.Serve(t, aitest.Routes{})
			p := newPipeline(t, extractConfig(t, e, nil))
			o := collect(t, p.Run(t.Context(), dispatch.NewMemoryContent(), unpulled(t), docinfer.Options{Extract: &tc.extract}))
			if !errors.Is(o.err, docinfer.ErrRequest) || !docinfer.Permanent(o.err) {
				t.Errorf("Run: %v, want a permanent error matching ErrRequest", o.err)
			}
			if o.results != 1 || len(e.Requests()) != 0 {
				t.Errorf("%d results, %d requests; want the error alone, nothing asked", o.results, len(e.Requests()))
			}
		})
	}
	for _, name := range []string{"n", strings.Repeat("n-_9", 16)} {
		x := docinfer.Extract{Name: name, Prompt: extractPrompt, Schema: extractSchema}
		e := aitest.Serve(t, aitest.Routes{Embeddings: embedAll})
		p := newPipeline(t, extractConfig(t, e, nil))
		if o := collect(t, p.Run(t.Context(), dispatch.NewMemoryContent(), seq(metadataInput()), docinfer.Options{Extract: &x})); o.err != nil {
			t.Errorf("Run with the name %q: %v", name, o.err)
		}
	}
}

func TestRunExtractionNotConfigured(t *testing.T) {
	e := aitest.Serve(t, aitest.Routes{})
	p := newPipeline(t, runConfig(t, e, nil))
	o := collect(t, p.Run(t.Context(), dispatch.NewMemoryContent(), unpulled(t), extracting(false)))
	if o.err == nil || !docinfer.Permanent(o.err) || !strings.Contains(o.err.Error(), "Config.Extraction") {
		t.Errorf("Run: %v, want a permanent error naming Config.Extraction", o.err)
	}
	if o.results != 1 || len(e.Requests()) != 0 {
		t.Errorf("%d results, %d requests; want the error alone, nothing asked", o.results, len(e.Requests()))
	}
}
