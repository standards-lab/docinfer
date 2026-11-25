package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/standards-lab/go-core/process"

	"github.com/standards-lab/go-ai/aitest"

	"github.com/standards-lab/docproc/dispatch"

	"github.com/standards-lab/docinfer"

	"github.com/standards-lab/docinfer/cmd/docinfer/internal/outdir"
)

type recorded[T any] struct {
	mu   sync.Mutex
	reqs []T
}

func (r *recorded[T]) add(req T) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reqs = append(r.reqs, req)
}

func (r *recorded[T]) all() []T {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]T(nil), r.reqs...)
}

func capabilityOf(t *testing.T, m outdir.Manifest, name docinfer.Capability) outdir.Capability {
	t.Helper()
	for _, c := range m.Capabilities {
		if c.Capability == name {
			return c
		}
	}
	t.Fatalf("manifest capabilities = %+v, want %s among them", m.Capabilities, name)
	return outdir.Capability{}
}

func TestRun_E5Configuration(t *testing.T) {
	var embedded recorded[aitest.EmbeddingRequest]
	var tokenized recorded[aitest.TokenizeRequest]
	e := aitest.Serve(t, aitest.Routes{
		Models: modelList(`{"id":"e5","object":"model","root":"intfloat/multilingual-e5-large"}`),
		Embeddings: func(ctx context.Context, req aitest.EmbeddingRequest) aitest.Reply {
			embedded.add(req)
			return unitVectors(ctx, req)
		},
		Tokenize: func(ctx context.Context, req aitest.TokenizeRequest) aitest.Reply {
			tokenized.add(req)
			return wordTokens(ctx, req)
		},
	})
	dir := configDir(t, map[string]string{"config.json": `{
		"endpoints":{
			"gateway":{"base_url":"` + e.URL() + `","format":"completions"},
			"vllm-root":{"base_url":"` + strings.TrimSuffix(e.URL(), "/v1") + `","format":"completions"}},
		"embeddings":{"endpoint":"gateway","model":"e5","dimensions":4,"tokenizer":"vllm-root",
			"input_types":{"document":{"prefix":"passage: "},"query":{"prefix":"query: "}}}}`})
	root := t.TempDir()
	docproc, out := filepath.Join(root, "docproc"), filepath.Join(root, "out")
	textRun(t, docproc, "alpha text", "beta text")
	a, _, stderr, _ := newApp(t)

	code := a.Run(context.Background(), []string{"run", docproc, "--out", out, "--config", dir})

	if code != process.ExitOK {
		t.Fatalf("code = %d, want %d; stderr = %q", code, process.ExitOK, stderr)
	}
	embeds := embedded.all()
	if len(embeds) == 0 {
		t.Fatal("no embeddings request, want the chunks embedded")
	}
	for _, req := range embeds {
		if req.Model != "e5" || len(req.Native) != 0 {
			t.Errorf("embeddings request model %q, native %s, want e5 and no member beside model and input", req.Model, req.Native)
		}
		for _, in := range req.Input {
			if !strings.HasPrefix(in, "passage: ") {
				t.Errorf("embeddings input %q, want the document prefix", in)
			}
		}
	}
	tokens := tokenized.all()
	if len(tokens) == 0 {
		t.Fatal("no tokenize request, want the chunks counted by the tokenizer endpoint")
	}
	for _, req := range tokens {
		if req.Model != "e5" || !strings.HasPrefix(req.Prompt, "passage: ") {
			t.Errorf("tokenize request model %q, prompt %q, want e5 and the document prefix", req.Model, req.Prompt)
		}
	}
	for _, r := range e.Requests() {
		if strings.HasSuffix(r.Path, "tokenize") && r.Path != "/tokenize" {
			t.Errorf("tokenize request to %s, want the server root's /tokenize", r.Path)
		}
	}
	_, m := openOut(t, out)
	c := capabilityOf(t, m, docinfer.CapabilityEmbeddings)
	if c.Model != "intfloat/multilingual-e5-large" || c.RequestModel != "e5" {
		t.Errorf("embeddings model %q, request_model %q, want the listing's root and e5", c.Model, c.RequestModel)
	}
	if want := `"prefix":"passage: "`; !strings.Contains(c.Settings, want) {
		t.Errorf("embeddings settings = %s, want the document convention, %s", c.Settings, want)
	}
	if c.ChunkSize != 480 {
		t.Errorf("embeddings chunk_size = %d, want the starting 480 tokens", c.ChunkSize)
	}
	if s := m.Space; s == nil || s.Chunker != `{"version":"2","bound":480,"runes":8183,"model":"intfloat/multilingual-e5-large"}` {
		t.Errorf("manifest embedding space = %+v, want the chunker's bound, 8192 runes less the prefix's 9, and served model", s)
	}
}

func TestRun_ServedModel(t *testing.T) {
	const unrooted, rooted = `{"id":"embed-model","object":"model"}`, `{"id":"embed-model","object":"model","root":"org/embed"}`
	tests := []struct {
		name    string
		listing string
		served  string
		want    string
		stop    string
	}{
		{name: "from the listing's root", listing: rooted, want: "org/embed"},
		{name: "from served_model", listing: unrooted, served: "org/configured", want: "org/configured"},
		{name: "the listing's root and served_model agree", listing: rooted, served: "org/embed", want: "org/embed"},
		{name: "served_model with no listing", served: "org/configured", want: "org/configured"},
		{
			name: "the listing's root and served_model disagree", listing: rooted, served: "org/other",
			stop: `docinfer run: embeddings: endpoint "gb10" lists model "embed-model" with root "org/embed", and served_model is "org/other": set embeddings.served_model to "org/embed", or leave it unset to take the listing's`,
		},
		{
			name: "neither", listing: unrooted,
			stop: `docinfer run: embeddings: endpoint "gb10" lists no root for model "embed-model", and served_model is unset: set embeddings.served_model to the model the deployment serves under "embed-model"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			routes := aitest.Routes{Embeddings: unitVectors, Tokenize: wordTokens}
			if tt.listing != "" {
				routes.Models = modelList(tt.listing)
			}
			e := aitest.Serve(t, routes)
			cfg := inferConfig(e.URL(), false)
			cfg.Embeddings.ServedModel = tt.served
			root := t.TempDir()
			docproc, out := filepath.Join(root, "docproc"), filepath.Join(root, "out")
			textRun(t, docproc, "alpha text")
			a, stderr := inferApp(t, cfg, new(bytes.Buffer))

			code := a.Run(context.Background(), []string{"run", docproc, "--out", out})

			if tt.stop != "" {
				if code != process.ExitFailure {
					t.Errorf("code = %d, want %d", code, process.ExitFailure)
				}
				if !strings.HasPrefix(stderr.String(), tt.stop) {
					t.Errorf("stderr = %q, want it to start with %q", stderr, tt.stop)
				}
				for _, r := range e.Requests() {
					if r.Path != "/v1/models" {
						t.Errorf("request to %s, want none but the model listing", r.Path)
					}
				}
				if _, err := os.Stat(out); !os.IsNotExist(err) {
					t.Errorf("--out's directory: %v, want none made", err)
				}
				return
			}
			if code != process.ExitOK {
				t.Fatalf("code = %d, want %d; stderr = %q", code, process.ExitOK, stderr)
			}
			_, m := openOut(t, out)
			if c := capabilityOf(t, m, docinfer.CapabilityEmbeddings); c.Model != tt.want || c.RequestModel != "embed-model" {
				t.Errorf("embeddings model %q, request_model %q, want %q and embed-model", c.Model, c.RequestModel, tt.want)
			}
		})
	}
}

func TestRun_VisionSendsItsNative(t *testing.T) {
	for _, tt := range []struct {
		name, native, want string
	}{
		{"unset starts at temperature 0", "", `{"temperature":0}`},
		{"configured replaces the start", `{"top_p": 0.5}`, `{"top_p":0.5}`},
		{"empty sends none", `{}`, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var asked recorded[aitest.ChatRequest]
			e := aitest.Serve(t, aitest.Routes{
				Models:     listed,
				Embeddings: unitVectors,
				Tokenize:   wordTokens,
				ChatCompletions: func(_ context.Context, req aitest.ChatRequest) aitest.Reply {
					asked.add(req)
					return aitest.ChatReply{Text: "a red square"}
				},
			})
			cfg := inferConfig(e.URL(), true)
			if tt.native != "" {
				cfg.Vision.Native = json.RawMessage(tt.native)
			}
			root := t.TempDir()
			docproc, out := filepath.Join(root, "docproc"), filepath.Join(root, "out")
			dr := newDocprocRun(t, docproc)
			part := inputPart("a.png", []byte("png-bytes"))
			dr.output("/in/a.png", part, dispatch.ImageOutput, "image/png", []byte("image-a"))
			dr.part("/in/a.png", part, 1)
			dr.end()
			a, stderr := inferApp(t, cfg, new(bytes.Buffer))

			code := a.Run(context.Background(), []string{"run", docproc, "--out", out})

			if code != process.ExitOK {
				t.Fatalf("code = %d, want %d; stderr = %q", code, process.ExitOK, stderr)
			}
			reqs := asked.all()
			if len(reqs) != 1 {
				t.Fatalf("vision requests = %d, want 1", len(reqs))
			}
			body := string(reqs[0].Body)
			for _, member := range []string{`"temperature"`, `"top_p"`} {
				if strings.Contains(body, member) != strings.Contains(tt.want, member) {
					t.Errorf("vision request body = %s, want native %q's members alone", body, tt.want)
				}
			}
			_, m := openOut(t, out)
			settings := capabilityOf(t, m, docinfer.CapabilityVision).Settings
			if tt.want == "" && strings.Contains(settings, `"native"`) || tt.want != "" && !strings.Contains(settings, `"native":`+tt.want) {
				t.Errorf("vision settings = %s, want native %q", settings, tt.want)
			}
		})
	}
}
