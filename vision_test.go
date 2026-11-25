package docinfer_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/docinfer"
	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/aitest"
	"github.com/standards-lab/go-ai/format"
	"github.com/standards-lab/go-ai/format/completions"
	"github.com/standards-lab/go-ai/format/responses"
	"github.com/standards-lab/go-ai/protocol"
)

const (
	visionModel  = "vision-model"
	visionServed = "org/vision-1"
	visionPrompt = "Describe the image."
	visionTokens = 300
	description  = "A red square on a white page.\n\nA small grey caption below."
	refusal      = "I can't describe this image."
)

var (
	imageBytes = []byte("\x89PNG\r\n\x1a\n not really a png")
	imageType  = "image/png"
)

func visionConfig(t *testing.T, e *aitest.Endpoint, wireFormat format.Format, edit func(*docinfer.Config)) docinfer.Config {
	t.Helper()
	if wireFormat == nil {
		wireFormat = completions.New(completions.Options{})
	}
	client, err := ai.New(e.URL(), http.DefaultClient, wireFormat, ai.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return runConfig(t, e, func(c *docinfer.Config) {
		c.Vision = &docinfer.Vision{
			Client: client, Model: visionModel, Served: visionServed, Prompt: visionPrompt, MaxTokens: visionTokens,
			Reasoning: protocol.ReasoningEffortLow,
		}
		if edit != nil {
			edit(c)
		}
	})
}

var visionID = docinfer.Vision{
	Model: visionModel, Served: visionServed, Prompt: visionPrompt, MaxTokens: visionTokens, Reasoning: protocol.ReasoningEffortLow,
}.Identity()

func imageInput(t *testing.T, content *dispatch.MemoryContent, data []byte, mediaType string) docinfer.Input {
	t.Helper()
	sum := sha(string(data))
	if content != nil {
		if err := content.Put(t.Context(), sum, bytes.NewReader(data)); err != nil {
			t.Fatal(err)
		}
	}
	return docinfer.Input{Event: dispatch.Event{
		Part:   childPart,
		Stage:  textStage,
		Output: &dispatch.Output{Kind: dispatch.ImageOutput, Name: "image", MediaType: mediaType, SHA256: sum},
	}}
}

func chatRoute(t *testing.T, reply aitest.Reply) func(context.Context, aitest.ChatRequest) aitest.Reply {
	return func(_ context.Context, req aitest.ChatRequest) aitest.Reply {
		checkVisionRequest(t, req)
		return reply
	}
}

func checkVisionRequest(t *testing.T, req aitest.ChatRequest) {
	t.Helper()
	if req.Model != visionModel || req.MaxTokens != visionTokens || req.ReasoningEffort != "low" || req.Stream {
		t.Errorf("request: model %q, max tokens %d, effort %q, stream %t; want %q, %d, low, unstreamed",
			req.Model, req.MaxTokens, req.ReasoningEffort, req.Stream, visionModel, visionTokens)
	}
	if len(req.Messages) != 1 {
		t.Fatalf("request of %d messages, want 1", len(req.Messages))
	}
	m := req.Messages[0]
	if m.Role != "user" || m.Text != visionPrompt || len(m.Images) != 1 {
		t.Fatalf("message: role %q, text %q, %d images; want user, %q, 1", m.Role, m.Text, len(m.Images), visionPrompt)
	}
	if img := m.Images[0]; img.MediaType != imageType || !bytes.Equal(img.Data, imageBytes) || img.URL != "" {
		t.Errorf("image: media type %q, %d bytes, URL %q; want a data URL of %s and the image's bytes",
			img.MediaType, len(img.Data), img.URL, imageType)
	}
}

func heldImage(t *testing.T, cache docinfer.Cache) *docinfer.Held {
	t.Helper()
	values, err := cache.Get(t.Context(), docinfer.Key(visionID, sha(string(imageBytes))))
	if err != nil {
		t.Fatal(err)
	}
	if values[0] == nil {
		return nil
	}
	var h docinfer.Held
	if err := json.Unmarshal(values[0], &h); err != nil {
		t.Fatalf("the held description: %v", err)
	}
	return &h
}

func oneInference(t *testing.T, o outcome) docinfer.Inference {
	t.Helper()
	if len(o.inferences) != 1 {
		t.Fatalf("%d inferences, want 1", len(o.inferences))
	}
	inf := o.inferences[0]
	if inf.Capability != docinfer.CapabilityVision || inf.Part != childPart.ID ||
		!slices.Equal(inf.ContentSHA256, []string{sha(string(imageBytes))}) {
		t.Errorf("inference of capability %q, part %q, content %v; want the test image's", inf.Capability, inf.Part, inf.ContentSHA256)
	}
	return inf
}

func visionCounts(vc docinfer.VisionCounts, vision docinfer.Usage, embedded, held int) docinfer.Counts {
	c := docinfer.Counts{
		Images: 1, Vision: &vc, Embedded: embedded, Held: held,
		Tokens: docinfer.Tokens{Vision: &vision, Embeddings: docinfer.Usage{Input: 3 * (embedded - held)}},
	}
	if embedded > held {
		c.Requests = 1
	}
	return c
}

func TestRunDescribesImage(t *testing.T) {
	e := aitest.Serve(t, aitest.Routes{
		ChatCompletions: chatRoute(t, aitest.ChatReply{Text: description, PromptTokens: 11, CompletionTokens: 7}),
		Embeddings:      embedAll,
	})
	cfg := visionConfig(t, e, nil, nil)
	p := newPipeline(t, cfg)
	content := dispatch.NewMemoryContent()
	in := imageInput(t, content, imageBytes, imageType)

	o := collect(t, p.Run(t.Context(), content, seq(in), docinfer.Options{}))
	if o.err != nil {
		t.Fatal(o.err)
	}
	inf := oneInference(t, o)
	if inf.Outcome != docinfer.OutcomeDescribed || inf.Text != description || inf.TextSHA256 != sha(description) ||
		inf.Reason != "" || inf.Held || inf.Usage != (docinfer.Usage{Input: 11, Output: 7}) {
		t.Errorf("inference %+v, want described, its text, and the reply's usage", inf)
	}
	if h := heldImage(t, cfg.Cache); h == nil || !reflect.DeepEqual(*h, docinfer.Held{Text: description}) {
		t.Errorf("held %+v, want the description", h)
	}
	pieces, _ := cfg.Chunker.Split(t.Context(), description)
	chunks := o.chunks()
	if len(chunks) != len(pieces) || len(pieces) < 2 {
		t.Fatalf("%d chunks, want the description's %d", len(chunks), len(pieces))
	}
	for i, c := range chunks {
		s := c.source
		if c.chunk.Text != pieces[i].Text || c.chunk.Index != i || c.chunk.Vector == "" {
			t.Errorf("chunk %d: %+v, want %q embedded", i, c.chunk, pieces[i].Text)
		}
		if s.Kind != "vision" || s.SHA256 != sha(description) || s.Incomplete != "" ||
			s.Stage == nil || *s.Stage != textStage || s.Output == nil || !reflect.DeepEqual(*s.Output, *in.Event.Output) {
			t.Errorf("chunk %d's source %+v, want kind vision of the description, the image its output", i, s)
		}
		if c.part.ID != childPart.ID {
			t.Errorf("chunk %d's part %q, want %q", i, c.part.ID, childPart.ID)
		}
	}
	checkCounts(t, o, visionCounts(docinfer.VisionCounts{Described: 1, Requests: 1}, docinfer.Usage{Input: 11, Output: 7}, 2, 0))

	requests := len(e.Requests())
	o = collect(t, p.Run(t.Context(), dispatch.NewMemoryContent(), seq(in), docinfer.Options{}))
	if o.err != nil {
		t.Fatalf("the second run, over no content: %v", o.err)
	}
	if n := len(e.Requests()); n != requests {
		t.Errorf("the second run sent %d requests, want none", n-requests)
	}
	inf = oneInference(t, o)
	if inf.Outcome != docinfer.OutcomeDescribed || inf.Text != description || !inf.Held || inf.Usage != (docinfer.Usage{}) {
		t.Errorf("the held inference %+v, want described, held, with no usage", inf)
	}
	checkCounts(t, o, visionCounts(docinfer.VisionCounts{Described: 1, Held: 1}, docinfer.Usage{}, 2, 2))
}

func TestRunJoinsImage(t *testing.T) {
	gate := &aitest.Gate{}
	e := aitest.Serve(t, aitest.Routes{
		ChatCompletions: func(ctx context.Context, req aitest.ChatRequest) aitest.Reply {
			checkVisionRequest(t, req)
			if err := gate.Wait(ctx); err != nil {
				return aitest.ErrorReply{Status: 503, Message: err.Error()}
			}
			return aitest.ChatReply{Text: description, PromptTokens: 11, CompletionTokens: 7}
		},
		Embeddings: embedAll,
	})
	p := newPipeline(t, visionConfig(t, e, nil, nil))
	waits := make(chan struct{}, 64)
	docinfer.OnWait(p, func() { waits <- struct{}{} })
	content := dispatch.NewMemoryContent()
	in := imageInput(t, content, imageBytes, imageType)

	leader := startRun(t, p, content, in, docinfer.Options{})
	gate.Await(t, 1)
	waiter := startRun(t, p, content, in, docinfer.Options{})
	awaitWaiters(t, waits, 1)
	gate.Open()

	lo, wo := leader.outcome(t), waiter.outcome(t)
	for _, o := range []outcome{lo, wo} {
		if o.err != nil {
			t.Fatal(o.err)
		}
	}
	chats := 0
	for _, r := range e.Requests() {
		if strings.HasSuffix(r.Path, "/chat/completions") {
			chats++
		}
	}
	if chats != 1 {
		t.Errorf("%d vision requests, want 1", chats)
	}
	if inf := oneInference(t, lo); inf.Held || inf.Text != description {
		t.Errorf("the leader's inference %+v, want made", inf)
	}
	if inf := oneInference(t, wo); !inf.Held || inf.Text != description || inf.Usage != (docinfer.Usage{}) {
		t.Errorf("the waiter's inference %+v, want held, with no usage", inf)
	}
	if wo.counts.Vision == nil || *wo.counts.Vision != (docinfer.VisionCounts{Described: 1, Held: 1}) ||
		*wo.counts.Tokens.Vision != (docinfer.Usage{}) {
		t.Errorf("the waiter's vision counts %+v, tokens %+v; want described and held, no tokens", wo.counts.Vision, wo.counts.Tokens.Vision)
	}
	if lo.counts.Vision == nil || *lo.counts.Vision != (docinfer.VisionCounts{Described: 1, Requests: 1}) {
		t.Errorf("the leader's vision counts %+v, want described by its one request", lo.counts.Vision)
	}
}

func TestRunImageRefusal(t *testing.T) {
	e := aitest.Serve(t, aitest.Routes{
		ChatCompletions: chatRoute(t, aitest.ChatReply{Refusal: refusal, PromptTokens: 11, CompletionTokens: 3}),
		Embeddings:      embedAll,
	})
	cfg := visionConfig(t, e, nil, nil)
	p := newPipeline(t, cfg)
	content := dispatch.NewMemoryContent()
	in := imageInput(t, content, imageBytes, imageType)

	o := collect(t, p.Run(t.Context(), content, seq(in), docinfer.Options{}))
	if o.err != nil {
		t.Fatal(o.err)
	}
	inf := oneInference(t, o)
	if inf.Outcome != docinfer.OutcomeRefused || inf.Reason != refusal || inf.Text != "" || inf.TextSHA256 != "" {
		t.Errorf("inference %+v, want refused, its reason the refusal", inf)
	}
	if h := heldImage(t, cfg.Cache); h == nil || !reflect.DeepEqual(*h, docinfer.Held{Refused: refusal}) {
		t.Errorf("held %+v, want the refusal", h)
	}
	if len(o.batches) > 0 {
		t.Errorf("a refused image yielded %d batches", len(o.batches))
	}
	checkCounts(t, o, visionCounts(docinfer.VisionCounts{Refused: 1, Requests: 1}, docinfer.Usage{Input: 11, Output: 3}, 0, 0))

	o = collect(t, p.Run(t.Context(), content, seq(in), docinfer.Options{}))
	if o.err != nil {
		t.Fatal(o.err)
	}
	if inf := oneInference(t, o); inf.Outcome != docinfer.OutcomeRefused || inf.Reason != refusal || !inf.Held {
		t.Errorf("the held refusal read back as %+v", inf)
	}
	checkCounts(t, o, visionCounts(docinfer.VisionCounts{Refused: 1, Held: 1}, docinfer.Usage{}, 0, 0))
}

func TestRunImageIncomplete(t *testing.T) {
	cases := []struct {
		finish, reason, text string
		chunks               int
		counts               docinfer.VisionCounts
	}{
		{"length", "max_output_tokens", description, 2, docinfer.VisionCounts{Incomplete: 1, Requests: 1}},
		{"content_filter", "content_filter", "", 0, docinfer.VisionCounts{Incomplete: 1, Empty: 1, Requests: 1}},
	}
	for _, tc := range cases {
		t.Run(tc.finish, func(t *testing.T) {
			e := aitest.Serve(t, aitest.Routes{
				ChatCompletions: chatRoute(t, aitest.ChatReply{Text: tc.text, Finish: tc.finish, PromptTokens: 11, CompletionTokens: 9}),
				Embeddings:      embedAll,
			})
			cfg := visionConfig(t, e, nil, nil)
			content := dispatch.NewMemoryContent()
			o := collect(t, newPipeline(t, cfg).Run(t.Context(), content, seq(imageInput(t, content, imageBytes, imageType)), docinfer.Options{}))
			if o.err != nil {
				t.Fatal(o.err)
			}
			inf := oneInference(t, o)
			if inf.Outcome != docinfer.OutcomeIncomplete || inf.Reason != tc.reason || inf.Text != tc.text {
				t.Errorf("inference %+v, want incomplete for %s, its text kept", inf, tc.reason)
			}
			if h := heldImage(t, cfg.Cache); h == nil || !reflect.DeepEqual(*h, docinfer.Held{Text: tc.text, Incomplete: tc.reason}) {
				t.Errorf("held %+v, want the text and its reason", h)
			}
			chunks := o.chunks()
			for _, c := range chunks {
				if c.source.Kind != "vision" || c.source.Incomplete != tc.reason {
					t.Errorf("chunk source %+v, want kind vision, incomplete for %s", c.source, tc.reason)
				}
			}
			if len(chunks) != tc.chunks {
				t.Errorf("%d chunks, want %d", len(chunks), tc.chunks)
			}
			checkCounts(t, o, visionCounts(tc.counts, docinfer.Usage{Input: 11, Output: 9}, tc.chunks, 0))
		})
	}
}

func TestRunImageWithoutMediaType(t *testing.T) {
	e := aitest.Serve(t, aitest.Routes{ChatCompletions: chatRoute(t, aitest.ChatReply{Text: description}), Embeddings: embedAll})
	content := dispatch.NewMemoryContent()
	in := imageInput(t, content, imageBytes, "")

	o := collect(t, newPipeline(t, visionConfig(t, e, nil, nil)).Run(t.Context(), content, seq(in), docinfer.Options{Final: true}))
	if o.err == nil || !docinfer.Permanent(o.err) {
		t.Fatalf("Run over an image without a media type: %v, want a permanent error", o.err)
	}
	if n := len(e.Requests()); n != 0 {
		t.Errorf("%d requests, want none", n)
	}

	o = collect(t, newPipeline(t, runConfig(t, e, nil)).Run(t.Context(), content, seq(in), docinfer.Options{}))
	if o.err != nil {
		t.Fatalf("Run with vision skipped: %v", o.err)
	}
	checkCounts(t, o, docinfer.Counts{Images: 1})
}

func noMediaTypeError(t *testing.T) error {
	t.Helper()
	e := aitest.Serve(t, aitest.Routes{})
	content := dispatch.NewMemoryContent()
	o := collect(t, newPipeline(t, visionConfig(t, e, nil, nil)).Run(t.Context(), content, seq(imageInput(t, content, imageBytes, "")), docinfer.Options{}))
	if o.err == nil {
		t.Fatal("Run over an image without a media type yielded no error")
	}
	return o.err
}

func runImage(t *testing.T, cfg docinfer.Config, opts docinfer.Options) outcome {
	t.Helper()
	content := dispatch.NewMemoryContent()
	return collect(t, newPipeline(t, cfg).Run(t.Context(), content, seq(imageInput(t, content, imageBytes, imageType)), opts))
}

func imageStopped(t *testing.T, o outcome, e *aitest.Endpoint, cache docinfer.Cache) {
	t.Helper()
	if o.counts != nil || len(o.batches) > 0 || len(o.inferences) > 0 {
		t.Error("a stopped run yielded inferences, batches, or counts")
	}
	if n := len(e.Requests()); n > 1 {
		t.Errorf("%d requests, want at most the one", n)
	}
	if h := heldImage(t, cache); h != nil {
		t.Errorf("a stopped run held %+v", h)
	}
}

func TestRunImageRefusedStatus(t *testing.T) {
	for _, status := range []int{400, 413, 415, 422} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			e := aitest.Serve(t, aitest.Routes{ChatCompletions: chatRoute(t, aitest.ErrorReply{Status: status, Message: "image refused"})})
			cfg := visionConfig(t, e, nil, nil)
			o := runImage(t, cfg, docinfer.Options{})
			if o.err != nil {
				t.Fatalf("Run: %v", o.err)
			}
			want := (&ai.APIError{Status: status, Message: "image refused"}).Error()
			if inf := oneInference(t, o); inf.Outcome != docinfer.OutcomeRefused || inf.Reason != want || inf.Held {
				t.Errorf("inference %+v, want refused for %q", inf, want)
			}
			if h := heldImage(t, cfg.Cache); h == nil || !reflect.DeepEqual(*h, docinfer.Held{Refused: want}) {
				t.Errorf("held %+v, want the refusal", h)
			}
			checkCounts(t, o, visionCounts(docinfer.VisionCounts{Refused: 1, Requests: 1}, docinfer.Usage{}, 0, 0))
		})
	}
}

func TestRunImagePermanent(t *testing.T) {
	for _, status := range []int{401, 403, 404, 405, 409, 418, 451, 501, 505} {
		for _, final := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d final=%t", status, final), func(t *testing.T) {
				e := aitest.Serve(t, aitest.Routes{ChatCompletions: chatRoute(t, aitest.ErrorReply{Status: status, Message: "no"})})
				cfg := visionConfig(t, e, nil, nil)
				o := runImage(t, cfg, docinfer.Options{Final: final})
				if apiErr, ok := errors.AsType[*ai.APIError](o.err); !ok || apiErr.Status != status {
					t.Fatalf("Run: %v, want the %d *ai.APIError", o.err, status)
				}
				if !docinfer.Permanent(o.err) {
					t.Errorf("Permanent(%v) is false", o.err)
				}
				imageStopped(t, o, e, cfg.Cache)
			})
		}
	}
	for _, final := range []bool{false, true} {
		t.Run(fmt.Sprintf("unsupported final=%t", final), func(t *testing.T) {
			e := aitest.Serve(t, aitest.Routes{ChatCompletions: chatRoute(t, aitest.ChatReply{Text: description})})
			wireFormat := completions.New(completions.Options{RefusedEfforts: []protocol.ReasoningEffort{protocol.ReasoningEffortLow}})
			cfg := visionConfig(t, e, wireFormat, nil)
			o := runImage(t, cfg, docinfer.Options{Final: final})
			if _, ok := errors.AsType[*format.UnsupportedError](o.err); !ok {
				t.Fatalf("Run: %v, want a *format.UnsupportedError", o.err)
			}
			if !docinfer.Permanent(o.err) {
				t.Errorf("Permanent(%v) is false", o.err)
			}
			if n := len(e.Requests()); n != 0 {
				t.Errorf("%d requests, want none sent", n)
			}
			imageStopped(t, o, e, cfg.Cache)
		})
	}
}

func TestRunImageTransient(t *testing.T) {
	for _, status := range []int{408, 429, 502, 503, 504, 507, 599} {
		for _, final := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d final=%t", status, final), func(t *testing.T) {
				e := aitest.Serve(t, aitest.Routes{ChatCompletions: chatRoute(t, aitest.ErrorReply{Status: status, Message: "later", RetryAfter: "7"})})
				cfg := visionConfig(t, e, nil, nil)
				o := runImage(t, cfg, docinfer.Options{Final: final})
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
				imageStopped(t, o, e, cfg.Cache)
			})
		}
	}
}

func TestRunImageTransportFailure(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closed := "http://" + l.Addr().String() + "/v1"
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	for _, final := range []bool{false, true} {
		t.Run(fmt.Sprintf("closed final=%t", final), func(t *testing.T) {
			e := aitest.Serve(t, aitest.Routes{})
			cfg := visionConfig(t, e, nil, func(c *docinfer.Config) {
				c.Vision.Client = newClient(t, closed, ai.TranscribeRoute)
			})
			o := runImage(t, cfg, docinfer.Options{Final: final})
			if _, ok := errors.AsType[*url.Error](o.err); !ok {
				t.Fatalf("Run against a closed port: %v, want a *url.Error", o.err)
			}
			if docinfer.Permanent(o.err) {
				t.Errorf("Permanent(%v) is true", o.err)
			}
			imageStopped(t, o, e, cfg.Cache)
		})
		t.Run(fmt.Sprintf("broken final=%t", final), func(t *testing.T) {
			broken := aitest.RawReply{Header: http.Header{"Content-Length": {"4096"}}, Body: []byte(`{"id":"chatcmpl-1","choices":[`)}
			e := aitest.Serve(t, aitest.Routes{ChatCompletions: chatRoute(t, broken)})
			cfg := visionConfig(t, e, nil, nil)
			o := runImage(t, cfg, docinfer.Options{Final: final})
			if o.err == nil || !strings.Contains(o.err.Error(), "unexpected EOF") {
				t.Fatalf("Run over a broken reply: %v, want io.ErrUnexpectedEOF", o.err)
			}
			if docinfer.Permanent(o.err) {
				t.Errorf("Permanent(%v) is true", o.err)
			}
			imageStopped(t, o, e, cfg.Cache)
		})
	}
}

func TestRunImageFailed(t *testing.T) {
	failed := []byte(`{"id":"resp-1","object":"response","model":"vision-model","status":"failed",` +
		`"error":{"code":"server_error","message":"the model broke"},"output":[]}`)
	cases := []struct {
		name       string
		routes     aitest.Routes
		wireFormat format.Format
		reason     string
	}{
		{"500", aitest.Routes{ChatCompletions: chatRoute(t, aitest.ErrorReply{Status: 500, Message: "broke"})}, nil, "500 Internal Server Error: broke"},
		{"undecodable", aitest.Routes{ChatCompletions: chatRoute(t, aitest.RawReply{Body: []byte(`not JSON`)})}, nil, "completions"},
		{"status failed", aitest.Routes{Raw: map[string]func(context.Context, aitest.RawRequest) aitest.Reply{
			"responses": func(context.Context, aitest.RawRequest) aitest.Reply { return aitest.RawReply{Body: failed} },
		}}, responses.New(responses.Options{}), "the model broke"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.routes.Embeddings = embedAll
			e := aitest.Serve(t, tc.routes)
			cfg := visionConfig(t, e, tc.wireFormat, nil)
			o := runImage(t, cfg, docinfer.Options{})
			if o.err == nil || docinfer.Permanent(o.err) {
				t.Fatalf("Run: %v, want an error not permanent", o.err)
			}
			imageStopped(t, o, e, cfg.Cache)

			cfg = visionConfig(t, e, tc.wireFormat, nil)
			content := dispatch.NewMemoryContent()
			inputs := seq(imageInput(t, content, imageBytes, imageType), outputInput(t, content, childPart, dispatch.TextOutput, plainText))
			o = collect(t, newPipeline(t, cfg).Run(t.Context(), content, inputs, docinfer.Options{Final: true}))
			if o.err != nil {
				t.Fatalf("the final Run: %v, want the job to go on", o.err)
			}
			inf := oneInference(t, o)
			if inf.Outcome != docinfer.OutcomeFailed || !strings.Contains(inf.Reason, tc.reason) || inf.Held || inf.Text != "" {
				t.Errorf("inference %+v, want failed, its reason naming %q", inf, tc.reason)
			}
			if h := heldImage(t, cfg.Cache); h != nil {
				t.Errorf("a failed image was held: %+v", h)
			}
			if o.counts == nil || o.counts.Texts != 1 || o.counts.Embedded == 0 ||
				*o.counts.Vision != (docinfer.VisionCounts{Failed: 1, Requests: 1}) {
				t.Errorf("counts %+v, want the image failed and the text embedded", o.counts)
			}
		})
	}
}

func TestRunKeysByServedModel(t *testing.T) {
	var models []string
	e := aitest.Serve(t, aitest.Routes{
		ChatCompletions: func(_ context.Context, req aitest.ChatRequest) aitest.Reply {
			models = append(models, req.Model)
			return aitest.ChatReply{Text: description}
		},
		Embeddings: embedAll,
	})
	cache := docinfer.NewMemoryCache()
	content := dispatch.NewMemoryContent()
	in := imageInput(t, content, imageBytes, imageType)
	run := func(model, served string) docinfer.VisionCounts {
		t.Helper()
		cfg := visionConfig(t, e, nil, func(c *docinfer.Config) {
			c.Vision.Model, c.Vision.Served = model, served
			c.Cache = cache
		})
		o := collect(t, newPipeline(t, cfg).Run(t.Context(), content, seq(in), docinfer.Options{}))
		if o.err != nil {
			t.Fatal(o.err)
		}
		return *o.counts.Vision
	}
	for _, tc := range []struct {
		name, model, served string
		want                docinfer.VisionCounts
		sent                []string
	}{
		{"first", visionModel, visionServed, docinfer.VisionCounts{Described: 1, Requests: 1}, []string{visionModel}},
		{"another model served under the alias", visionModel, "org/vision-2", docinfer.VisionCounts{Described: 1, Requests: 1}, []string{visionModel, visionModel}},
		{"the model served under another alias", "another-alias", visionServed, docinfer.VisionCounts{Described: 1, Held: 1}, []string{visionModel, visionModel}},
	} {
		if got := run(tc.model, tc.served); got != tc.want {
			t.Errorf("%s: vision counts %+v, want %+v", tc.name, got, tc.want)
		}
		if !slices.Equal(models, tc.sent) {
			t.Errorf("%s: requests named %q, want %q", tc.name, models, tc.sent)
		}
	}
	if h := heldImage(t, cache); h == nil || !reflect.DeepEqual(*h, docinfer.Held{Text: description}) {
		t.Errorf("held under the served model %+v, want the description", h)
	}
}
