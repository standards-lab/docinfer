package app_test

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
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
		"embeddings":{"endpoint":"gb10","model":"embed-model","served_model":"embed-model","dimensions":768,"input_types":{},"tokenizer":"gb10"}}`
}

type probed struct {
	cfg           app.Config
	httpClients   map[string]*http.Client
	vision        *ai.Client
	transcription *ai.Client
	embeddings    *ai.Client
	tokenizer     *ai.Client
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
			embeddings:    inv.Get(n.EmbeddingsClient),
			tokenizer:     inv.Get(n.TokenizerClient),
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
	p := probeConfig(t, `{"endpoints":{"gb10":{"base_url":"`+unusedURL+`","format":"completions"},
			"seeing":{"base_url":"`+unusedURL+`","format":"completions"}},
		"vision":{"endpoint":"seeing","model":"vision-model"},
		"transcription":{"endpoint":"gb10","model":"audio-model"},
		"embeddings":{"endpoint":"gb10","model":"embed-model","dimensions":768,"input_types":{},"tokenizer":"gb10"}}`)

	if got := responseHeaderTimeout(t, p, "gb10"); got != 180*time.Second {
		t.Errorf("transport ResponseHeaderTimeout = %v, want 180s", got)
	}
	if got := responseHeaderTimeout(t, p, "seeing"); got != 600*time.Second {
		t.Errorf("vision's transport ResponseHeaderTimeout = %v, want 600s", got)
	}
	e := p.cfg.Endpoints["gb10"]
	if got := [3]any{*e.Retry.MaxAttempts, e.Retry.BaseDelay.Duration(), e.Retry.MaxDelay.Duration()}; got != [3]any{3, 500 * time.Millisecond, 30 * time.Second} {
		t.Errorf("retry = %v, want 3 attempts, 500ms base delay, 30s max delay", got)
	}
	v, tr, em, run := p.cfg.Vision, p.cfg.Transcription, p.cfg.Embeddings, p.cfg.Run
	for name, got := range map[string][2]int{
		"vision max_tokens":              {*v.MaxTokens, 2048},
		"vision max_pixels":              {*v.MaxPixels, 2580480},
		"vision concurrency":             {*v.Concurrency, 4},
		"transcription words_per_second": {*tr.WordsPerSecond, 8},
		"transcription max_tokens":       {*tr.MaxTokens, 0},
		"transcription concurrency":      {*tr.Concurrency, 4},
		"embeddings batch":               {*em.Batch, 16},
		"embeddings chunk_size":          {*em.ChunkSize, 480},
		"embeddings chunk_runes":         {*em.ChunkRunes, 8192},
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
	if got := string(v.Native); got != `{"temperature":1.0,"top_p":0.95,"top_k":64}` {
		t.Errorf("vision native = %s, want Gemma 4's card sampling", got)
	}
	if got := p.transcription.TranscriptionMode(); got != ai.TranscribeRoute {
		t.Errorf("transcription client's mode = %v, want route", got)
	}
	if tr.Native != nil {
		t.Errorf("transcription native = %s, want none in route mode", tr.Native)
	}
}

func TestConfig_ResponseHeaderTimeoutOverride(t *testing.T) {
	t.Setenv("DOCINFER_ENDPOINTS_GB10_RESPONSE_HEADER_TIMEOUT", "5s")

	p := probeConfig(t, embeddingsJSON(unusedURL))

	if got := responseHeaderTimeout(t, p, "gb10"); got != 5*time.Second {
		t.Errorf("transport ResponseHeaderTimeout = %v, want the override's 5s", got)
	}
}

func TestConfig_VisionResponseHeaderTimeout(t *testing.T) {
	visionJSON := func(gb10 string) string {
		return `{"endpoints":{"gb10":{"base_url":"` + unusedURL + `","format":"completions"` + gb10 + `}},
			"vision":{"endpoint":"gb10","model":"vision-model"},
			"embeddings":{"endpoint":"gb10","model":"embed-model","dimensions":768,"input_types":{},"tokenizer":"gb10"}}`
	}
	for name, tc := range map[string]struct {
		gb10 string
		env  string
		want time.Duration
	}{
		"shared with embeddings starts at vision's": {want: 600 * time.Second},
		"configured value wins":                     {gb10: `,"response_header_timeout":"90s"`, want: 90 * time.Second},
		"override wins":                             {env: "5s", want: 5 * time.Second},
	} {
		t.Run(name, func(t *testing.T) {
			if tc.env != "" {
				t.Setenv("DOCINFER_ENDPOINTS_GB10_RESPONSE_HEADER_TIMEOUT", tc.env)
			}

			p := probeConfig(t, visionJSON(tc.gb10))

			if got := responseHeaderTimeout(t, p, "gb10"); got != tc.want {
				t.Errorf("transport ResponseHeaderTimeout = %v, want %v", got, tc.want)
			}
		})
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
		"DOCINFER_TRANSCRIPTION_MAX_TOKENS":          "256",
		"DOCINFER_TRANSCRIPTION_NATIVE":              `{"temperature":0.1}`,
		"DOCINFER_VISION_SERVED_MODEL":               "org/vision",
		"DOCINFER_VISION_NATIVE":                     `{"temperature":0}`,
		"DOCINFER_VISION_MAX_PIXELS":                 "1000",
		"DOCINFER_EMBEDDINGS_BATCH":                  "32",
		"DOCINFER_EMBEDDINGS_CHUNK_RUNES":            "4096",
		"DOCINFER_EMBEDDINGS_INPUT_TYPES":            `{"query":{"prefix":"query: "}}`,
		"DOCINFER_EMBEDDINGS_TOKENIZER":              "gb10",
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
	if got := *p.cfg.Transcription.MaxTokens; got != 256 {
		t.Errorf("transcription max_tokens = %d, want the override's 256", got)
	}
	if got := string(p.cfg.Transcription.Native); got != `{"temperature":0.1}` {
		t.Errorf("transcription native = %s, want the override's", got)
	}
	if got := *p.cfg.Embeddings.Batch; got != 32 {
		t.Errorf("embeddings batch = %d, want 32", got)
	}
	if got := *p.cfg.Embeddings.ChunkRunes; got != 4096 {
		t.Errorf("embeddings chunk_runes = %d, want 4096", got)
	}
	if v := p.cfg.Vision; v == nil || v.ServedModel != "org/vision" || string(v.Native) != `{"temperature":0}` || *v.MaxPixels != 1000 {
		t.Errorf("vision = %+v, want the overrides' served_model, native, and max_pixels", v)
	}
	if got := p.embeddings.EmbeddingProfile().InputTypes[ai.InputQuery].Prefix; got != "query: " {
		t.Errorf("embeddings client's query prefix = %q, want the override's", got)
	}
	if got := *p.cfg.Run.Pending; got != 2 {
		t.Errorf("run pending = %d, want 2", got)
	}
}

func TestConfig_InputTypesOnTheEmbeddingsAndTokenizerClients(t *testing.T) {
	p := probeConfig(t, `{"endpoints":{
			"gateway":{"base_url":"`+unusedURL+`","format":"completions"},
			"root":{"base_url":"http://127.0.0.1:1","format":"completions"}},
		"embeddings":{"endpoint":"gateway","model":"e5","dimensions":1024,"tokenizer":"root",
			"input_types":{"document":{"prefix":"passage: "},"query":{"prefix":"query: "}}}}`)

	want := map[ai.InputType]string{ai.InputDocument: "passage: ", ai.InputQuery: "query: "}
	for name, c := range map[string]*ai.Client{"embeddings": p.embeddings, "tokenizer": p.tokenizer} {
		got := c.EmbeddingProfile().InputTypes
		if len(got) != len(want) {
			t.Errorf("%s client's input types = %v, want %v", name, got, want)
		}
		for typ, prefix := range want {
			if got[typ].Prefix != prefix {
				t.Errorf("%s client's %s prefix = %q, want %q", name, typ, got[typ].Prefix, prefix)
			}
		}
	}
}

func TestConfig_ChatModeIsTranscriptionsOwn(t *testing.T) {
	p := probeConfig(t, `{"endpoints":{"gb10":{"base_url":"`+unusedURL+`","format":"completions"}},
		"vision":{"endpoint":"gb10","model":"vision-model"},
		"transcription":{"endpoint":"gb10","model":"audio-model","mode":"chat"},
		"embeddings":{"endpoint":"gb10","model":"embed-model","dimensions":768,"input_types":{},"tokenizer":"gb10"}}`)

	if got := p.transcription.TranscriptionMode(); got != ai.TranscribeChat {
		t.Errorf("transcription client's mode = %v, want chat", got)
	}
	if got := p.vision.TranscriptionMode(); got != ai.TranscribeRoute {
		t.Errorf("vision client's mode = %v, want route", got)
	}
	if got := *p.cfg.Transcription.MaxTokens; got != 512 {
		t.Errorf("transcription max_tokens = %d, want chat mode's starting 512", got)
	}
	if got := string(p.cfg.Transcription.Native); got != `{"temperature":0}` {
		t.Errorf("transcription native = %s, want chat mode's starting {\"temperature\":0}", got)
	}
}

func TestConfig_RouteModeTakesAnEmptyNative(t *testing.T) {
	p := probeConfig(t, `{"endpoints":{"gb10":{"base_url":"`+unusedURL+`","format":"completions"}},
		"transcription":{"endpoint":"gb10","model":"audio-model","native":{}},
		"embeddings":{"endpoint":"gb10","model":"embed-model","dimensions":768,"input_types":{},"tokenizer":"gb10"}}`)

	if got := string(p.cfg.Transcription.Native); got != `{}` {
		t.Errorf("transcription native = %s, want the configured {}", got)
	}
}

func TestConfig_DirectoryFromTheEnvironment(t *testing.T) {
	e := aitest.Serve(t, aitest.Routes{Models: modelList(`{"id":"embed-model","object":"model"}`)})
	t.Setenv("DOCINFER_CONFIG", configDir(t, map[string]string{"config.json": embeddingsJSON(e.URL())}))
	a, stdout, stderr, _ := newApp(t)

	code := a.Run(context.Background(), runArgs(t))

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
	embeddingsWith := func(members string) string {
		return `"embeddings":{"endpoint":"gb10","model":"embed-model","dimensions":768` + members + `}`
	}
	embeddings := embeddingsWith(`,"input_types":{},"tokenizer":"gb10"`)
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
		{name: "no embeddings endpoint", config: withEndpoint(completions, `"embeddings":{"model":"m","dimensions":768,"input_types":{},"tokenizer":"gb10"}`), want: "embeddings: no endpoint"},
		{name: "unknown endpoint", config: withEndpoint(completions, `"embeddings":{"endpoint":"other","model":"m","dimensions":768,"input_types":{},"tokenizer":"gb10"}`), want: `embeddings: endpoint "other" is not one of endpoints`},
		{name: "no dimensions", config: withEndpoint(completions, `"embeddings":{"endpoint":"gb10","model":"m","input_types":{},"tokenizer":"gb10"}`), want: "embeddings: no dimensions"},
		{name: "no input_types", config: withEndpoint(completions, embeddingsWith(`,"tokenizer":"gb10"`)), want: "embeddings: no input_types"},
		{name: "null input_types", config: withEndpoint(completions, embeddingsWith(`,"input_types":null,"tokenizer":"gb10"`)), want: "embeddings: no input_types"},
		{name: "unknown input type", config: withEndpoint(completions, embeddingsWith(`,"input_types":{"passage":{"prefix":"passage: "}},"tokenizer":"gb10"`)), want: `unknown input type "passage"`},
		{name: "unknown convention member", config: withEndpoint(completions, embeddingsWith(`,"input_types":{"query":{"prefx":"query: "}},"tokenizer":"gb10"`)), want: "prefx"},
		{name: "input_types override not an object", config: withEndpoint(completions, embeddingsWith(`,"tokenizer":"gb10"`)), env: map[string]string{"DOCINFER_EMBEDDINGS_INPUT_TYPES": "[]"}, want: "DOCINFER_EMBEDDINGS_INPUT_TYPES"},
		{name: "no tokenizer", config: withEndpoint(completions, embeddingsWith(`,"input_types":{}`)), want: "embeddings: no tokenizer"},
		{name: "unknown tokenizer", config: withEndpoint(completions, embeddingsWith(`,"input_types":{},"tokenizer":"vllm"`)), want: `embeddings: tokenizer "vllm" is not one of endpoints`},
		{name: "native not an object", config: withEndpoint(completions, embeddings+`,"vision":{"endpoint":"gb10","model":"v","native":[1]}`), want: "vision: native [1] is not a JSON object with unique member names"},
		{name: "native with a repeated member", config: withEndpoint(completions, embeddings+`,"vision":{"endpoint":"gb10","model":"v","native":{"seed":1,"seed":2}}`), want: "vision: native "},
		{name: "native override not an object", config: withEndpoint(completions, embeddings+`,"vision":{"endpoint":"gb10","model":"v"}`), env: map[string]string{"DOCINFER_VISION_NATIVE": "0.2"}, want: "vision: native 0.2 is not a JSON object"},
		{name: "chunk_runes below 1", config: withEndpoint(completions, embeddingsWith(`,"input_types":{},"tokenizer":"gb10","chunk_runes":0`)), want: "embeddings: chunk_runes 0 is below 1"},
		{name: "chunk_runes within the prefix", config: withEndpoint(completions, embeddingsWith(`,"input_types":{"document":{"prefix":"passage: "}},"tokenizer":"gb10","chunk_runes":9`)), want: "embeddings: chunk_runes 9 leaves no character beside the document prefix's 9"},
		{name: "negative max_pixels", config: withEndpoint(completions, embeddings+`,"vision":{"endpoint":"gb10","model":"v","max_pixels":-1}`), want: "vision: max_pixels -1 is below zero"},
		{name: "negative max_pixels override", config: withEndpoint(completions, embeddings+`,"vision":{"endpoint":"gb10","model":"v"}`), env: map[string]string{"DOCINFER_VISION_MAX_PIXELS": "-5"}, want: "vision: max_pixels -5 is below zero"},
		{name: "count below 1", config: withEndpoint(completions, embeddings+`,"vision":{"endpoint":"gb10","model":"v","concurrency":0}`), want: "vision: concurrency 0 is below 1"},
		{name: "unknown format", config: withEndpoint(`{"base_url":"`+unusedURL+`","format":"grpc"}`, embeddings), want: `endpoints "gb10": format "grpc" is not completions, messages, or responses`},
		{name: "unknown profile member", config: withEndpoint(`{"base_url":"`+unusedURL+`","format":"completions","profile":{"completions":{"bogus":true}}}`, embeddings), want: `endpoints "gb10": profile: `},
		{name: "chat without completions", config: withEndpoint(`{"base_url":"`+unusedURL+`","format":"messages"}`, embeddings+`,"transcription":{"endpoint":"gb10","model":"a","mode":"chat"}`), want: `transcription: mode chat needs the completions format, and endpoint "gb10"'s is "messages"`},
		{name: "route with max_tokens", config: withEndpoint(completions, embeddings+`,"transcription":{"endpoint":"gb10","model":"a","max_tokens":512}`), want: "transcription: max_tokens 512 bounds a chat-mode reply, and mode is route, which has no field for it: set max_tokens to 0, or mode to chat"},
		{name: "route with a max_tokens override", config: withEndpoint(completions, embeddings+`,"transcription":{"endpoint":"gb10","model":"a"}`), env: map[string]string{"DOCINFER_TRANSCRIPTION_MAX_TOKENS": "64"}, want: "transcription: max_tokens 64 bounds a chat-mode reply, and mode is route"},
		{name: "route with native", config: withEndpoint(completions, embeddings+`,"transcription":{"endpoint":"gb10","model":"a","native":{"temperature":0}}`), want: `transcription: native {"temperature":0} is sent in a chat-mode request, and mode is route, which sends no JSON body: set native to {}, or mode to chat`},
		{name: "route with a native override", config: withEndpoint(completions, embeddings+`,"transcription":{"endpoint":"gb10","model":"a"}`), env: map[string]string{"DOCINFER_TRANSCRIPTION_NATIVE": `{"top_p":0.5}`}, want: `transcription: native {"top_p":0.5} is sent in a chat-mode request, and mode is route`},
		{name: "transcription native not an object", config: withEndpoint(completions, embeddings+`,"transcription":{"endpoint":"gb10","model":"a","mode":"chat","native":[1]}`), want: "transcription: native [1] is not a JSON object with unique member names"},
		{name: "negative max_tokens", config: withEndpoint(completions, embeddings+`,"transcription":{"endpoint":"gb10","model":"a","mode":"chat","max_tokens":-1}`), want: "transcription: max_tokens -1 is below zero"},
		{name: "unknown mode", config: withEndpoint(completions, embeddings+`,"transcription":{"endpoint":"gb10","model":"a","mode":"stream"}`), want: "stream"},
		{name: "unknown reasoning", config: withEndpoint(completions, embeddings+`,"vision":{"endpoint":"gb10","model":"v","reasoning":"extreme"}`), want: "extreme"},
		{name: "override not a count", config: withEndpoint(completions, embeddings), env: map[string]string{"DOCINFER_RUN_PENDING": "many"}, want: "DOCINFER_RUN_PENDING"},
		{name: "negative response header timeout", config: withEndpoint(completions, embeddings), env: map[string]string{"DOCINFER_ENDPOINTS_GB10_RESPONSE_HEADER_TIMEOUT": "-1s"}, want: `endpoints "gb10": response_header_timeout -1s is below zero`},
		{name: "endpoints sharing overrides", config: `{"endpoints":{"a-b":` + completions + `,"a_b":` + completions + `},"embeddings":{"endpoint":"a-b","model":"m","dimensions":768,"input_types":{},"tokenizer":"a-b"}}`, want: `endpoints "a-b" and "a_b" compose the same overrides, DOCINFER_ENDPOINTS_A_B_*`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("DOCINFER_CONFIG", "")
			for name, value := range tt.env {
				t.Setenv(name, value)
			}
			args := runArgs(t)
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

	code := a.Run(context.Background(), append(runArgs(t), "--config", dir))

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
