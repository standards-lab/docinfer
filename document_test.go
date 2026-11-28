package docinfer_test

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/standards-lab/docinfer"
	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/aitest"
	"github.com/standards-lab/go-ai/protocol"
)

func documenting(final bool) docinfer.Options {
	return docinfer.Options{Document: new(theExtract), Final: final}
}

func documentRoute(t *testing.T, text string, reply aitest.Reply) func(context.Context, aitest.ChatRequest) aitest.Reply {
	return func(_ context.Context, req aitest.ChatRequest) aitest.Reply {
		checkExtractRequest(t, req, text)
		if strings.Contains(string(req.Body), "report.docx") {
			t.Error("the document's request holds the metadata text")
		}
		return reply
	}
}

func documentInputs(t *testing.T, content *dispatch.MemoryContent) func() []docinfer.Input {
	return func() []docinfer.Input {
		return []docinfer.Input{
			outputInput(t, content, childPart, dispatch.TextOutput, plainText),
			metadataInput(),
			outputInput(t, content, childPart, dispatch.MarkdownOutput, ""),
			outputInput(t, content, childPart, dispatch.MarkdownOutput, markdown),
		}
	}
}

func theDocument(t *testing.T, o outcome) docinfer.Inference {
	t.Helper()
	var (
		found []docinfer.Inference
		at    int
	)
	for i, r := range o.all {
		if r.Inference != nil && r.Inference.Scope == docinfer.ScopeDocument {
			found, at = append(found, *r.Inference), i
		}
	}
	if len(found) != 1 {
		t.Fatalf("%d scoped inferences, want the document's alone", len(found))
	}
	if at != len(o.all)-2 || o.all[len(o.all)-1].Done == nil {
		t.Errorf("the document's inference is result %d of %d, want the last before the counts", at, len(o.all))
	}
	inf := found[0]
	if inf.Scope != docinfer.ScopeDocument || inf.Capability != docinfer.CapabilityExtraction || inf.Part != inputPart.ID {
		t.Errorf("document inference: scope %q, capability %q, part %q; want document, extraction, %q",
			inf.Scope, inf.Capability, inf.Part, inputPart.ID)
	}
	return inf
}

func TestRunExtractsDocument(t *testing.T) {
	joined := plainText + "\n\n" + markdown
	e := aitest.Serve(t, aitest.Routes{
		ChatCompletions: documentRoute(t, joined, aitest.ChatReply{Chunks: extractPieces, PromptTokens: 30, CompletionTokens: 9}),
		Embeddings:      embedAll,
	})
	cfg := extractConfig(t, e, nil)
	p := extractPipeline(t, cfg, 0, 500*time.Millisecond, time.Second, 1200*time.Millisecond)
	content := dispatch.NewMemoryContent()
	inputs := documentInputs(t, content)

	o := collect(t, p.Run(t.Context(), content, seq(inputs()...), documenting(false)))
	if o.err != nil {
		t.Fatal(o.err)
	}
	inf := theDocument(t, o)
	if !slices.Equal(inf.ContentSHA256, []string{sha(plainText), sha(markdown)}) || inf.Outcome != docinfer.OutcomeExtracted ||
		inf.Text != extractJSON || inf.TextSHA256 != sha(extractJSON) || inf.Held || inf.Usage != (docinfer.Usage{Input: 30, Output: 9}) {
		t.Errorf("document inference %+v, want extracted from the two texts in pull order", inf)
	}
	if got, want := deltas(t, o, joined), []string{extractPieces[0], extractPieces[1] + extractPieces[2]}; !slices.Equal(got, want) {
		t.Errorf("progress %q, want %q", got, want)
	}
	if h := heldExtraction(t, cfg.Cache, joined); h == nil || !reflect.DeepEqual(*h, docinfer.Held{Text: extractJSON}) {
		t.Errorf("held %+v, want the document's JSON", h)
	}
	if n := chatRequests(e); n != 1 {
		t.Errorf("%d extraction requests, want the document's alone", n)
	}
	checkExtractionCounts(t, o, docinfer.ExtractionCounts{Extracted: 1, Requests: 1}, docinfer.Usage{Input: 30, Output: 9})

	o = collect(t, p.Run(t.Context(), content, seq(inputs()...), documenting(false)))
	if o.err != nil {
		t.Fatal(o.err)
	}
	if inf := theDocument(t, o); inf.Outcome != docinfer.OutcomeExtracted || !inf.Held || inf.Text != extractJSON ||
		inf.Usage != (docinfer.Usage{}) {
		t.Errorf("the held document %+v, want extracted, held, with no usage", inf)
	}
	if len(o.progress) != 0 {
		t.Errorf("a held document yielded progress %+v", o.progress)
	}
	if n := chatRequests(e); n != 1 {
		t.Errorf("%d extraction requests after the second run, want the first run's one", n)
	}
	checkExtractionCounts(t, o, docinfer.ExtractionCounts{Extracted: 1, Held: 1}, docinfer.Usage{})
}

func TestRunDocumentReadsInferredTexts(t *testing.T) {
	transcriptText := joinedAt(t, []string{said0}).Text()
	joined := strings.Join([]string{plainText, description, transcriptText}, "\n\n")
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
		c.Vision = &docinfer.Vision{Client: client, Model: visionModel, Served: visionServed, MaxTokens: visionTokens}
		c.Transcription = &docinfer.Transcription{Client: client, Model: transcriptionModel, Served: transcriptionServed}
	})
	content := dispatch.NewMemoryContent()
	inputs := seq(metadataInput(), outputInput(t, content, childPart, dispatch.TextOutput, plainText),
		imageInput(t, content, imageBytes, imageType), windowInput(t, content, win{index: 0, clip: "clip 0", said: said0}))
	opts := docinfer.Options{Extract: new(theExtract), Document: new(theExtract)}
	o := collect(t, newPipeline(t, cfg).Run(t.Context(), content, inputs, opts))
	if o.err != nil {
		t.Fatal(o.err)
	}
	if !slices.Contains(asked, joined) {
		t.Errorf("extracted %q, none the three texts joined in pull order", asked)
	}
	inf := theDocument(t, o)
	if want := []string{sha(plainText), sha(description), sha(transcriptText)}; !slices.Equal(inf.ContentSHA256, want) ||
		inf.Outcome != docinfer.OutcomeExtracted || inf.Text != `{"total":1}` {
		t.Errorf("document inference %+v, want extracted from %v", inf, want)
	}
	if n := len(extractions(o)); n != 4 {
		t.Errorf("%d extractions, want the three sources' and the document's", n)
	}
	checkExtractionCounts(t, o, docinfer.ExtractionCounts{Extracted: 4, Requests: 4}, docinfer.Usage{})
}

func TestRunDocumentOfNoText(t *testing.T) {
	e := aitest.Serve(t, aitest.Routes{Embeddings: embedAll})
	p := newPipeline(t, extractConfig(t, e, nil))
	content := dispatch.NewMemoryContent()
	inputs := seq(metadataInput(), outputInput(t, content, childPart, dispatch.TextOutput, ""))
	o := collect(t, p.Run(t.Context(), content, inputs, documenting(false)))
	if o.err != nil {
		t.Fatal(o.err)
	}
	if n := len(extractions(o)); n != 0 || chatRequests(e) != 0 {
		t.Errorf("%d extractions, %d requests; want neither", n, chatRequests(e))
	}
	checkExtractionCounts(t, o, docinfer.ExtractionCounts{}, docinfer.Usage{})
}

func TestRunDocumentFailureTable(t *testing.T) {
	partial := strings.Join(extractPieces[:2], "")
	refusedStatus := (&ai.APIError{Status: 422, Message: "text refused"}).Error()
	cases := []struct {
		name   string
		reply  aitest.Reply
		final  bool
		want   docinfer.Outcome
		reason string
		text   string
		held   *docinfer.Held
		usage  docinfer.Usage
	}{
		{"refusal", aitest.ChatReply{Refusal: extractRefuse, PromptTokens: 20, CompletionTokens: 4}, false,
			docinfer.OutcomeRefused, extractRefuse, "", &docinfer.Held{Refused: extractRefuse}, docinfer.Usage{Input: 20, Output: 4}},
		{"refused status", aitest.ErrorReply{Status: 422, Message: "text refused"}, false,
			docinfer.OutcomeRefused, refusedStatus, "", &docinfer.Held{Refused: refusedStatus}, docinfer.Usage{}},
		{"incomplete", aitest.ChatReply{Chunks: extractPieces[:2], Finish: "length", CompletionTokens: extractTokens}, false,
			docinfer.OutcomeIncomplete, string(protocol.ReasonMaxOutputTokens), partial,
			&docinfer.Held{Text: partial, Incomplete: string(protocol.ReasonMaxOutputTokens)}, docinfer.Usage{Output: extractTokens}},
		{"failed final", aitest.ErrorReply{Status: 500, Message: "broke"}, true,
			docinfer.OutcomeFailed, "500 Internal Server Error: broke", "", nil, docinfer.Usage{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := aitest.Serve(t, aitest.Routes{ChatCompletions: documentRoute(t, plainText, tc.reply), Embeddings: embedAll})
			cfg := extractConfig(t, e, nil)
			content := dispatch.NewMemoryContent()
			inputs := seq(metadataInput(), outputInput(t, content, childPart, dispatch.TextOutput, plainText))
			o := collect(t, extractPipeline(t, cfg).Run(t.Context(), content, inputs, documenting(tc.final)))
			if o.err != nil {
				t.Fatal(o.err)
			}
			inf := theDocument(t, o)
			if inf.Outcome != tc.want || !strings.Contains(inf.Reason, tc.reason) || inf.Text != tc.text || inf.Held {
				t.Errorf("document inference %+v, want %s for %q, text %q", inf, tc.want, tc.reason, tc.text)
			}
			if h := heldExtraction(t, cfg.Cache, plainText); !reflect.DeepEqual(h, tc.held) {
				t.Errorf("held %+v, want %+v", h, tc.held)
			}
			var xc docinfer.ExtractionCounts
			switch tc.want {
			case docinfer.OutcomeRefused:
				xc.Refused = 1
			case docinfer.OutcomeIncomplete:
				xc.Incomplete = 1
			case docinfer.OutcomeFailed:
				xc.Failed = 1
			}
			xc.Requests = 1
			checkExtractionCounts(t, o, xc, tc.usage)
		})
	}
}

func TestRunDocumentFailedStops(t *testing.T) {
	e := aitest.Serve(t, aitest.Routes{
		ChatCompletions: documentRoute(t, plainText, aitest.ErrorReply{Status: 500, Message: "broke"}),
		Embeddings:      embedAll,
	})
	cfg := extractConfig(t, e, nil)
	content := dispatch.NewMemoryContent()
	inputs := seq(metadataInput(), outputInput(t, content, childPart, dispatch.TextOutput, plainText))
	o := collect(t, extractPipeline(t, cfg).Run(t.Context(), content, inputs, documenting(false)))
	if o.err == nil || docinfer.Permanent(o.err) {
		t.Fatalf("Run: %v, want an error that isn't permanent", o.err)
	}
	if o.counts != nil || len(extractions(o)) > 0 {
		t.Error("a stopped run yielded the document or counts")
	}
	if h := heldExtraction(t, cfg.Cache, plainText); h != nil {
		t.Errorf("a stopped run held %+v", h)
	}
}
