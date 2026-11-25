package docinfer_test

import (
	"context"
	"encoding/json"
	"encoding/json/jsontext"
	"sync"
	"testing"

	"github.com/standards-lab/docinfer"
	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/aitest"
)

var sampling = jsontext.Value(`{"temperature":0.2,"top_k":64}`)

type bodies struct {
	mu   sync.Mutex
	seen []map[string]jsontext.Value
}

func (b *bodies) record(t *testing.T, req aitest.ChatRequest) {
	t.Helper()
	var members map[string]jsontext.Value
	if err := json.Unmarshal(req.Body, &members); err != nil {
		t.Errorf("the request's body: %v", err)
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.seen = append(b.seen, members)
}

func (b *bodies) one(t *testing.T) map[string]jsontext.Value {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.seen) != 1 {
		t.Fatalf("%d chat requests, want 1", len(b.seen))
	}
	return b.seen[0]
}

func checkSampling(t *testing.T, body map[string]jsontext.Value, native jsontext.Value) {
	t.Helper()
	if len(native) == 0 {
		for _, name := range []string{"temperature", "top_k"} {
			if v, ok := body[name]; ok {
				t.Errorf("the request sent %s %s, with no Native", name, v)
			}
		}
		return
	}
	if string(body["temperature"]) != "0.2" || string(body["top_k"]) != "64" {
		t.Errorf("the request's temperature %s and top_k %s; want Native's 0.2 and 64", body["temperature"], body["top_k"])
	}
}

func TestRunSendsVisionNative(t *testing.T) {
	for _, native := range []jsontext.Value{sampling, nil} {
		t.Run(string(native), func(t *testing.T) {
			var b bodies
			e := aitest.Serve(t, aitest.Routes{
				ChatCompletions: func(_ context.Context, req aitest.ChatRequest) aitest.Reply {
					checkVisionRequest(t, req)
					b.record(t, req)
					return aitest.ChatReply{Text: description}
				},
				Embeddings: embedAll,
			})
			cfg := visionConfig(t, e, nil, func(c *docinfer.Config) { c.Vision.Native = native })
			p := newPipeline(t, cfg)
			content := dispatch.NewMemoryContent()
			o := collect(t, p.Run(t.Context(), content, seq(imageInput(t, content, imageBytes, imageType)), docinfer.Options{}))
			if o.err != nil {
				t.Fatal(o.err)
			}
			checkSampling(t, b.one(t), native)

			values, err := cfg.Cache.Get(t.Context(), docinfer.Key(cfg.Vision.Identity(), sha(string(imageBytes))))
			if err != nil {
				t.Fatal(err)
			}
			if values[0] == nil {
				t.Error("the description isn't held under the identity with Native")
			}
		})
	}
}

func TestRunSendsExtractionNative(t *testing.T) {
	for _, native := range []jsontext.Value{sampling, nil} {
		t.Run(string(native), func(t *testing.T) {
			var b bodies
			e := aitest.Serve(t, aitest.Routes{
				ChatCompletions: func(_ context.Context, req aitest.ChatRequest) aitest.Reply {
					checkExtractRequest(t, req, plainText)
					b.record(t, req)
					return aitest.ChatReply{Chunks: extractPieces}
				},
				Embeddings: embedAll,
			})
			cfg := extractConfig(t, e, func(c *docinfer.Config) { c.Extraction.Native = native })
			p := extractPipeline(t, cfg)
			content := dispatch.NewMemoryContent()
			in := outputInput(t, content, childPart, dispatch.TextOutput, plainText)
			o := collect(t, p.Run(t.Context(), content, seq(in), extracting(false)))
			if o.err != nil {
				t.Fatal(o.err)
			}
			checkSampling(t, b.one(t), native)

			values, err := cfg.Cache.Get(t.Context(), docinfer.Key(cfg.Extraction.Identity(theExtract), sha(plainText)))
			if err != nil {
				t.Fatal(err)
			}
			if values[0] == nil {
				t.Error("the extraction isn't held under the identity with Native")
			}
		})
	}
}

func TestNativeKeys(t *testing.T) {
	route := newClient(t, "http://localhost/v1", ai.TranscribeRoute)
	identities := map[docinfer.Capability]func(native jsontext.Value) docinfer.Identity{
		docinfer.CapabilityVision: func(native jsontext.Value) docinfer.Identity {
			return docinfer.Vision{Client: route, Model: "m", Served: "s", MaxTokens: 1, Native: native}.Identity()
		},
		docinfer.CapabilityExtraction: func(native jsontext.Value) docinfer.Identity {
			return docinfer.Extraction{Client: route, Model: "m", Served: "s", MaxTokens: 1, Native: native}.Identity(theExtract)
		},
	}
	base := `{"temperature":0.2,"top_k":64,"options":{"min_p":0.05,"stop":["\n"]}}`
	same := []string{
		"{ \"temperature\": 0.2,\n\t\"top_k\": 64, \"options\": {\"min_p\": 0.05, \"stop\": [ \"\\n\" ]} }",
		`{"options":{"stop":["\n"],"min_p":0.05},"top_k":64,"temperature":0.2}`,
		`{"temperature":0.2,"top_k":64,"options":{"min_p":0.05,"stop":["\u000a"]}}`,
	}
	differ := []string{
		``,
		`{}`,
		`{"temperature":0.3,"top_k":64,"options":{"min_p":0.05,"stop":["\n"]}}`,
		`{"temperature":0.20,"top_k":64,"options":{"min_p":0.05,"stop":["\n"]}}`,
		`{"temperature":0.2,"top_k":64,"options":{"min_p":0.05,"stop":["\n"]},"seed":9007199254740993}`,
		`{"temperature":0.2,"top_k":64,"options":{"min_p":0.05,"stop":["\n"]},"seed":9007199254740992}`,
	}
	content := sha("content")
	for capability, identity := range identities {
		key := docinfer.Key(identity(jsontext.Value(base)), content)
		for _, s := range same {
			if docinfer.Key(identity(jsontext.Value(s)), content) != key {
				t.Errorf("%s: Native %s keys apart from %s", capability, s, base)
			}
		}
		seen := map[string]string{key: base}
		for _, s := range differ {
			k := docinfer.Key(identity(jsontext.Value(s)), content)
			if prior, ok := seen[k]; ok {
				t.Errorf("%s: Native %q keys the same as %q", capability, s, prior)
			}
			seen[k] = s
		}
	}
}

func TestIdentityLeavesNativeAsGiven(t *testing.T) {
	native := jsontext.Value(`{ "top_k": 64, "temperature": 0.2 }`)
	given := string(native)
	v := docinfer.Vision{Model: "m", Served: "s", MaxTokens: 1, Native: native}
	want := `{"request":"2","prompt":"","max_tokens":1,"effort":"","native":{"temperature":0.2,"top_k":64}}`
	if got := v.Identity().Settings; got != want {
		t.Errorf("settings %s, want %s", got, want)
	}
	if string(native) != given {
		t.Errorf("Identity rewrote Native to %s", native)
	}
}

func TestProfileHoldsNative(t *testing.T) {
	cfg := validConfig(t)
	vision, extraction := jsontext.Value(`{"top_k": 64, "temperature": 0.2}`), jsontext.Value(`{"temperature":0}`)
	cfg.Vision.Native, cfg.Extraction.Native = vision, extraction
	p, err := docinfer.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	copy(vision, `{"top_k": 32`)
	copy(extraction, `{"temperature":1}`)
	pr := p.Profile()
	if want := `{"request":"2","prompt":"Describe the image.","max_tokens":2,"effort":"low","native":{"temperature":0.2,"top_k":64}}`; pr.Vision.Settings != want {
		t.Errorf("Profile.Vision's settings %s, want %s", pr.Vision.Settings, want)
	}
	if want := `{"request":"2","max_tokens":2,"effort":"medium","native":{"temperature":0}}`; pr.Extraction.Settings != want {
		t.Errorf("Profile.Extraction's settings %s, want %s", pr.Extraction.Settings, want)
	}
}

func TestNewRefusesNative(t *testing.T) {
	bad := []string{`[]`, `"temperature"`, `0.2`, `null`, `{"temperature":0.2,"temperature":0.3}`, `{"temperature":}`, `{`}
	fields := []struct {
		field string
		set   func(*docinfer.Config, jsontext.Value)
	}{
		{"Vision.Native", func(c *docinfer.Config, v jsontext.Value) { c.Vision.Native = v }},
		{"Extraction.Native", func(c *docinfer.Config, v jsontext.Value) { c.Extraction.Native = v }},
	}
	for _, f := range fields {
		for _, native := range bad {
			t.Run(f.field+"="+native, func(t *testing.T) {
				cfg := validConfig(t)
				f.set(&cfg, jsontext.Value(native))
				refused(t, cfg, f.field)
			})
		}
		t.Run(f.field+"=empty", func(t *testing.T) {
			cfg := validConfig(t)
			f.set(&cfg, jsontext.Value(""))
			if _, err := docinfer.New(cfg); err != nil {
				t.Errorf("New with an empty %s: %v", f.field, err)
			}
		})
	}
}
