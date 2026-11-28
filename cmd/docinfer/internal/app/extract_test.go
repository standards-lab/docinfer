package app_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/standards-lab/go-core/process"

	"github.com/standards-lab/go-ai/aitest"

	"github.com/standards-lab/docinfer"

	"github.com/standards-lab/docinfer/cmd/docinfer/internal/app"
)

const titleExtract = `{"name":"title","prompt":"Name the text's title.",
	"schema":{"type":"object","properties":{"title":{"type":"string"}},"required":["title"],"additionalProperties":false}}`

func extractConfig(baseURL string) app.Config {
	cfg := inferConfig(baseURL, false)
	cfg.Extraction = &app.Extraction{Endpoint: "gb10", Model: "agent-model", MaxTokens: new(256)}
	return cfg
}

func extractFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "extract.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRun_Extract(t *testing.T) {
	var asked recorded[aitest.ChatRequest]
	e := aitest.Serve(t, aitest.Routes{
		Models: modelList(
			`{"id":"agent-model","object":"model","root":"org/agent","status":{"value":"loaded"}}`,
			`{"id":"embed-model","object":"model","status":{"value":"loaded"}}`,
		),
		ChatCompletions: func(_ context.Context, req aitest.ChatRequest) aitest.Reply {
			asked.add(req)
			return aitest.ChatReply{Chunks: []string{`{"title":`, `"alpha"}`}, PromptTokens: 12, CompletionTokens: 5}
		},
		Embeddings: unitVectors,
		Tokenize:   wordTokens,
	})
	root := t.TempDir()
	docproc, out := filepath.Join(root, "docproc"), filepath.Join(root, "out")
	textRun(t, docproc, "alpha text")
	stdout := new(bytes.Buffer)
	a, stderr := inferApp(t, extractConfig(e.URL()), stdout)

	code := a.Run(context.Background(), []string{"run", docproc, "--out", out, "--extract", extractFile(t, titleExtract)})

	if code != process.ExitOK {
		t.Fatalf("code = %d, want %d; stderr = %q", code, process.ExitOK, stderr)
	}
	reqs := asked.all()
	if len(reqs) != 1 {
		t.Fatalf("chat requests = %d, want 1, the source text's extraction", len(reqs))
	}
	r := reqs[0]
	if !r.Stream || r.Model != "agent-model" || r.MaxTokens != 256 || r.SchemaName != "title" {
		t.Errorf("request: stream %t, model %q, max tokens %d, schema %q; want streamed, agent-model, 256, title",
			r.Stream, r.Model, r.MaxTokens, r.SchemaName)
	}
	if !slices.ContainsFunc(r.Messages, func(m aitest.ChatMessage) bool { return m.Text == "Name the text's title." }) ||
		!slices.ContainsFunc(r.Messages, func(m aitest.ChatMessage) bool { return m.Text == "alpha text" }) {
		t.Errorf("request messages = %+v, want the extract's prompt and the source text", r.Messages)
	}
	if !slices.ContainsFunc(strings.Split(stdout.String(), "\n"), func(line string) bool {
		return slices.Equal(strings.Fields(line), []string{"extraction", "gb10", "agent-model", "loaded"})
	}) {
		t.Errorf("stdout = %q, want the extraction row loaded", stdout)
	}
	if want := "extraction: extracted 1, incomplete 0, refused 0, failed 0, held 0; requests 1\n"; !strings.Contains(stdout.String(), want) {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}

	d, m := openOut(t, out)
	if c := capabilityOf(t, m, docinfer.CapabilityExtraction); c.Model != "org/agent" || c.RequestModel != "agent-model" || c.Endpoint != "gb10" || c.Concurrency != 4 {
		t.Errorf("manifest extraction = %+v, want the listing's root, agent-model, gb10, and concurrency 4", c)
	}
	var names []docinfer.Capability
	for _, c := range m.Capabilities {
		names = append(names, c.Capability)
	}
	if want := []docinfer.Capability{docinfer.CapabilityExtraction, docinfer.CapabilityEmbeddings}; !slices.Equal(names, want) {
		t.Errorf("manifest capabilities = %v, want %v", names, want)
	}
	var extracted []string
	for _, inf := range collect(t, d.Inferences(m.Run)) {
		if inf.Capability == docinfer.CapabilityExtraction {
			extracted = append(extracted, string(inf.Outcome)+" "+inf.Input)
		}
	}
	if want := []string{"extracted /in/a.txt"}; !slices.Equal(extracted, want) {
		t.Errorf("extraction inferences = %q, want %q", extracted, want)
	}
}

func TestRun_ConfiguredExtractionRunsNoneWithoutExtract(t *testing.T) {
	e := aitest.Serve(t, aitest.Routes{
		Models: modelList(
			`{"id":"agent-model","object":"model","root":"org/agent"}`,
			`{"id":"embed-model","object":"model","status":{"value":"loaded"}}`,
		),
		Embeddings: unitVectors,
		Tokenize:   wordTokens,
	})
	root := t.TempDir()
	docproc, out := filepath.Join(root, "docproc"), filepath.Join(root, "out")
	textRun(t, docproc, "alpha text")
	stdout := new(bytes.Buffer)
	a, stderr := inferApp(t, extractConfig(e.URL()), stdout)

	code := a.Run(context.Background(), []string{"run", docproc, "--out", out})

	if code != process.ExitOK {
		t.Fatalf("code = %d, want %d; stderr = %q", code, process.ExitOK, stderr)
	}
	for _, r := range e.Requests() {
		if r.Path == "/v1/chat/completions" {
			t.Errorf("request to %s, want no extraction without --extract", r.Path)
		}
	}
	if strings.Contains(stdout.String(), "extraction: extracted") {
		t.Errorf("stdout = %q, want no extraction counts", stdout)
	}
}

func TestRun_ExtractRefusedBeforeTheModelCheck(t *testing.T) {
	tests := []struct {
		name    string
		cfg     func(baseURL string) app.Config
		extract string
		want    string
	}{
		{
			name: "no extraction configured", cfg: func(u string) app.Config { return inferConfig(u, false) }, extract: titleExtract,
			want: "docinfer run: --extract %s: no extraction configured: set extraction's endpoint and model",
		},
		{name: "an unknown member", cfg: extractConfig, extract: `{"name":"title","prompt":"p","schema":{},"strict":true}`, want: "docinfer run: --extract %s: "},
		{name: "not JSON", cfg: extractConfig, extract: `title`, want: "docinfer run: --extract %s: "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := aitest.Serve(t, aitest.Routes{})
			out := filepath.Join(t.TempDir(), "out")
			file := extractFile(t, tt.extract)
			args := append(runArgs(t)[:2], "--out", out, "--extract", file)
			a, stderr := inferApp(t, tt.cfg(e.URL()), new(bytes.Buffer))

			code := a.Run(context.Background(), args)

			if code != process.ExitFailure {
				t.Errorf("code = %d, want %d", code, process.ExitFailure)
			}
			if want := strings.Replace(tt.want, "%s", file, 1); !strings.HasPrefix(stderr.String(), want) {
				t.Errorf("stderr = %q, want it to start with %q", stderr, want)
			}
			if n := len(e.Requests()); n != 0 {
				t.Errorf("requests = %d, want none", n)
			}
			if _, err := os.Stat(out); !os.IsNotExist(err) {
				t.Errorf("--out's directory: %v, want none made", err)
			}
		})
	}
}

func TestRun_ExtractNamingNoFileIsAUsageError(t *testing.T) {
	a, _, stderr, built := newApp(t)

	code := a.Run(context.Background(), append(runArgs(t), "--extract", ""))

	if code != process.ExitUsage {
		t.Errorf("code = %d, want %d", code, process.ExitUsage)
	}
	if !strings.Contains(stderr.String(), "--extract names no file") {
		t.Errorf("stderr = %q, want the usage error", stderr)
	}
	if log := built.Log(); len(log) != 0 {
		t.Errorf("nodes built = %q, want none", log)
	}
}
