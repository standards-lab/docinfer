package app_test

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/go-cli-sdk"
	"github.com/standards-lab/go-core/process"

	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/aitest"

	"github.com/standards-lab/docinfer/cmd/docinfer/internal/app"
)

const unusedURL = "http://127.0.0.1:1/v1"

func configDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func embeddingsJSON(baseURL string) string {
	return `{"endpoints":{"gb10":{"base_url":"` + baseURL + `","format":"completions"}},
		"embeddings":{"endpoint":"gb10","model":"embed-model","dimensions":768}}`
}

type probed struct {
	cfg           app.Config
	httpClients   map[string]*http.Client
	vision        *ai.Client
	transcription *ai.Client
}

func probeConfig(t *testing.T, configJSON string) probed {
	t.Helper()
	dir := configDir(t, map[string]string{"config.json": configJSON})
	a, _, stderr, _ := newApp(t)
	var p probed
	probeNodes(a, func(inv *cli.Invocation) {
		n := a.Nodes()
		p = probed{
			cfg:           inv.Get(n.Config),
			httpClients:   inv.Get(n.HTTPClients),
			vision:        inv.Get(n.VisionClient),
			transcription: inv.Get(n.TranscriptionClient),
		}
	})

	if code := a.Run(context.Background(), []string{"probe", "--config", dir}); code != process.ExitOK {
		t.Fatalf("code = %d, want %d; stderr = %q", code, process.ExitOK, stderr)
	}
	return p
}

func responseHeaderTimeout(t *testing.T, p probed, endpoint string) time.Duration {
	t.Helper()
	hc := p.httpClients[endpoint]
	if hc == nil {
		t.Fatalf("no HTTP client for endpoint %q", endpoint)
	}
	return hc.Transport.(*http.Transport).ResponseHeaderTimeout
}

func TestConfig_StartingValues(t *testing.T) {
	p := probeConfig(t, `{"endpoints":{"gb10":{"base_url":"`+unusedURL+`","format":"completions"}},
		"vision":{"endpoint":"gb10","model":"vision-model"},
		"transcription":{"endpoint":"gb10","model":"audio-model"},
		"embeddings":{"endpoint":"gb10","model":"embed-model","dimensions":768}}`)

	if got := responseHeaderTimeout(t, p, "gb10"); got != 180*time.Second {
		t.Errorf("transport ResponseHeaderTimeout = %v, want 180s", got)
	}
	e := p.cfg.Endpoints["gb10"]
	if got := [3]any{*e.Retry.MaxAttempts, e.Retry.BaseDelay.Duration(), e.Retry.MaxDelay.Duration()}; got != [3]any{3, 500 * time.Millisecond, 30 * time.Second} {
		t.Errorf("retry = %v, want 3 attempts, 500ms base delay, 30s max delay", got)
	}
	v, tr, em, run := p.cfg.Vision, p.cfg.Transcription, p.cfg.Embeddings, p.cfg.Run
	for name, got := range map[string][2]int{
		"vision max_tokens":              {*v.MaxTokens, 2048},
		"vision concurrency":             {*v.Concurrency, 4},
		"transcription words_per_second": {*tr.WordsPerSecond, 8},
		"transcription concurrency":      {*tr.Concurrency, 4},
		"embeddings batch":               {*em.Batch, 16},
		"embeddings chunk_size":          {*em.ChunkSize, 2000},
		"embeddings concurrency":         {*em.Concurrency, 4},
		"run pending":                    {*run.Pending, 16},
		"run batch_bytes":                {*run.BatchBytes, 524288},
	} {
		if got[0] != got[1] {
			t.Errorf("%s = %d, want %d", name, got[0], got[1])
		}
	}
	if v.Reasoning != "none" {
		t.Errorf("vision reasoning = %q, want none", v.Reasoning)
	}
	if v.Prompt == "" {
		t.Error("vision prompt is empty, want the starting prompt")
	}
	if got := p.transcription.TranscriptionMode(); got != ai.TranscribeRoute {
		t.Errorf("transcription client's mode = %v, want route", got)
	}
}

func TestConfig_ResponseHeaderTimeoutOverride(t *testing.T) {
	t.Setenv("DOCINFER_ENDPOINTS_GB10_RESPONSE_HEADER_TIMEOUT", "5s")

	p := probeConfig(t, embeddingsJSON(unusedURL))

	if got := responseHeaderTimeout(t, p, "gb10"); got != 5*time.Second {
		t.Errorf("transport ResponseHeaderTimeout = %v, want the override's 5s", got)
	}
}

func TestConfig_OverridesSetEverySection(t *testing.T) {
	for name, value := range map[string]string{
		"DOCINFER_ENDPOINTS_GB10_RETRY_MAX_ATTEMPTS": "5",
		"DOCINFER_VISION_ENDPOINT":                   "gb10",
		"DOCINFER_VISION_MODEL":                      "vision-model",
		"DOCINFER_VISION_REASONING":                  "max",
		"DOCINFER_TRANSCRIPTION_ENDPOINT":            "gb10",
		"DOCINFER_TRANSCRIPTION_MODEL":               "audio-model",
		"DOCINFER_TRANSCRIPTION_MODE":                "chat",
		"DOCINFER_EMBEDDINGS_BATCH":                  "32",
		"DOCINFER_RUN_PENDING":                       "2",
	} {
		t.Setenv(name, value)
	}

	p := probeConfig(t, embeddingsJSON(unusedURL))

	if got := *p.cfg.Endpoints["gb10"].Retry.MaxAttempts; got != 5 {
		t.Errorf("retry max_attempts = %d, want 5", got)
	}
	if p.vision == nil || p.cfg.Vision.Reasoning != "max" {
		t.Errorf("vision = %+v, want the overrides' capability, reasoning max", p.cfg.Vision)
	}
	if p.transcription == nil || p.transcription.TranscriptionMode() != ai.TranscribeChat {
		t.Errorf("transcription client = %v, want one in chat mode", p.transcription)
	}
	if got := *p.cfg.Embeddings.Batch; got != 32 {
		t.Errorf("embeddings batch = %d, want 32", got)
	}
	if got := *p.cfg.Run.Pending; got != 2 {
		t.Errorf("run pending = %d, want 2", got)
	}
}

func TestConfig_ChatModeIsTranscriptionsOwn(t *testing.T) {
	p := probeConfig(t, `{"endpoints":{"gb10":{"base_url":"`+unusedURL+`","format":"completions"}},
		"vision":{"endpoint":"gb10","model":"vision-model"},
		"transcription":{"endpoint":"gb10","model":"audio-model","mode":"chat"},
		"embeddings":{"endpoint":"gb10","model":"embed-model","dimensions":768}}`)

	if got := p.transcription.TranscriptionMode(); got != ai.TranscribeChat {
		t.Errorf("transcription client's mode = %v, want chat", got)
	}
	if got := p.vision.TranscriptionMode(); got != ai.TranscribeRoute {
		t.Errorf("vision client's mode = %v, want route", got)
	}
}

func TestConfig_DirectoryFromTheEnvironment(t *testing.T) {
	e := aitest.Serve(t, aitest.Routes{Models: modelList(`{"id":"embed-model","object":"model"}`)})
	t.Setenv("DOCINFER_CONFIG", configDir(t, map[string]string{"config.json": embeddingsJSON(e.URL())}))
	a, stdout, stderr, _ := newApp(t)

	code := a.Run(context.Background(), runArgs)

	if code != process.ExitOK {
		t.Errorf("code = %d, want %d; stderr = %q", code, process.ExitOK, stderr)
	}
	if !strings.Contains(stdout.String(), "embed-model  listed\n") {
		t.Errorf("stdout = %q, want the embeddings row listed", stdout)
	}
}

func TestConfig_FlagWinsOverTheEnvironment(t *testing.T) {
	t.Setenv("DOCINFER_CONFIG", filepath.Join(t.TempDir(), "absent"))

	p := probeConfig(t, embeddingsJSON(unusedURL))

	if p.cfg.Embeddings.Model != "embed-model" {
		t.Errorf("embeddings model = %q, want the --config directory's", p.cfg.Embeddings.Model)
	}
}

func TestConfig_ErrorsFailTheBuild(t *testing.T) {
	file := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(file, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	withEndpoint := func(endpoint, sections string) string {
		return `{"endpoints":{"gb10":` + endpoint + `},` + sections + `}`
	}
	embeddings := `"embeddings":{"endpoint":"gb10","model":"embed-model","dimensions":768}`
	completions := `{"base_url":"` + unusedURL + `","format":"completions"}`
	tests := []struct {
		name   string
		config string
		dir    string
		env    map[string]string
		want   string
	}{
		{name: "no directory", want: "no configuration directory: set --config or DOCINFER_CONFIG"},
		{name: "not a directory", dir: file, want: "is no directory"},
		{name: "unknown key", config: withEndpoint(completions, embeddings+`,"extraction":{}`), want: "extraction"},
		{name: "no embeddings endpoint", config: withEndpoint(completions, `"embeddings":{"model":"m","dimensions":768}`), want: "embeddings: no endpoint"},
		{name: "unknown endpoint", config: withEndpoint(completions, `"embeddings":{"endpoint":"other","model":"m","dimensions":768}`), want: `embeddings: endpoint "other" is not one of endpoints`},
		{name: "no dimensions", config: withEndpoint(completions, `"embeddings":{"endpoint":"gb10","model":"m"}`), want: "embeddings: no dimensions"},
		{name: "count below 1", config: withEndpoint(completions, embeddings+`,"vision":{"endpoint":"gb10","model":"v","concurrency":0}`), want: "vision: concurrency 0 is below 1"},
		{name: "unknown format", config: withEndpoint(`{"base_url":"`+unusedURL+`","format":"grpc"}`, embeddings), want: `endpoints "gb10": format "grpc" is not completions, messages, or responses`},
		{name: "unknown profile member", config: withEndpoint(`{"base_url":"`+unusedURL+`","format":"completions","profile":{"completions":{"bogus":true}}}`, embeddings), want: `endpoints "gb10": profile: `},
		{name: "chat without completions", config: withEndpoint(`{"base_url":"`+unusedURL+`","format":"messages"}`, embeddings+`,"transcription":{"endpoint":"gb10","model":"a","mode":"chat"}`), want: `transcription: mode chat needs the completions format, and endpoint "gb10"'s is "messages"`},
		{name: "unknown mode", config: withEndpoint(completions, embeddings+`,"transcription":{"endpoint":"gb10","model":"a","mode":"stream"}`), want: "stream"},
		{name: "unknown reasoning", config: withEndpoint(completions, embeddings+`,"vision":{"endpoint":"gb10","model":"v","reasoning":"extreme"}`), want: "extreme"},
		{name: "override not a count", config: withEndpoint(completions, embeddings), env: map[string]string{"DOCINFER_RUN_PENDING": "many"}, want: "DOCINFER_RUN_PENDING"},
		{name: "negative response header timeout", config: withEndpoint(completions, embeddings), env: map[string]string{"DOCINFER_ENDPOINTS_GB10_RESPONSE_HEADER_TIMEOUT": "-1s"}, want: `endpoints "gb10": response_header_timeout -1s is below zero`},
		{name: "endpoints sharing overrides", config: `{"endpoints":{"a-b":` + completions + `,"a_b":` + completions + `},"embeddings":{"endpoint":"a-b","model":"m","dimensions":768}}`, want: `endpoints "a-b" and "a_b" compose the same overrides, DOCINFER_ENDPOINTS_A_B_*`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("DOCINFER_CONFIG", "")
			for name, value := range tt.env {
				t.Setenv(name, value)
			}
			args := slices.Clone(runArgs)
			switch {
			case tt.dir != "":
				args = append(args, "--config", tt.dir)
			case tt.config != "":
				args = append(args, "--config", configDir(t, map[string]string{"config.json": tt.config}))
			}
			a, stdout, stderr, built := newApp(t)

			code := a.Run(context.Background(), args)

			if code != process.ExitFailure {
				t.Errorf("code = %d, want %d", code, process.ExitFailure)
			}
			if prefix := "docinfer run: config: "; !strings.HasPrefix(stderr.String(), prefix) || !strings.Contains(stderr.String(), tt.want) {
				t.Errorf("stderr = %q, want it to start with %q and hold %q", stderr, prefix, tt.want)
			}
			if stdout.Len() != 0 {
				t.Errorf("stdout = %q, want empty: no model table", stdout)
			}
			for _, node := range built.Log() {
				if strings.HasSuffix(node, "client") || node == "http clients" {
					t.Errorf("nodes built = %q, want no client", built.Log())
					break
				}
			}
		})
	}
}

func TestConfig_ClientErrorNamedForItsEndpoint(t *testing.T) {
	dir := configDir(t, map[string]string{"config.json": embeddingsJSON("ftp://gb10/v1")})
	a, stdout, stderr, _ := newApp(t)

	code := a.Run(context.Background(), append(slices.Clone(runArgs), "--config", dir))

	if code != process.ExitFailure {
		t.Errorf("code = %d, want %d", code, process.ExitFailure)
	}
	if want := `docinfer run: embeddings client: endpoint "gb10": `; !strings.HasPrefix(stderr.String(), want) {
		t.Errorf("stderr = %q, want it to start with %q", stderr, want)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty: no model table", stdout)
	}
}
