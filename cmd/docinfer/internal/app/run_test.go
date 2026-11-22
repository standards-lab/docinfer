package app_test

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"net"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/go-cli-sdk"
	"github.com/standards-lab/go-core/config"
	"github.com/standards-lab/go-core/process"

	"github.com/standards-lab/go-ai/aitest"

	"github.com/standards-lab/docinfer/cmd/docinfer/internal/app"
	"github.com/standards-lab/docinfer/cmd/docinfer/internal/apptest"
)

func everyCapability(baseURL, format string) app.Config {
	return app.Config{
		Endpoints:     map[string]*app.Endpoint{"gb10": {BaseURL: baseURL, Format: format}},
		Vision:        &app.Vision{Endpoint: "gb10", Model: "vision-model"},
		Transcription: &app.Transcription{Endpoint: "gb10", Model: "audio-model"},
		Embeddings:    app.Embeddings{Capability: app.Capability{Endpoint: "gb10", Model: "embed-model"}, Dimensions: new(768)},
	}
}

func embeddingsOnly(baseURL string) app.Config {
	cfg := everyCapability(baseURL, "completions")
	cfg.Vision, cfg.Transcription = nil, nil
	return cfg
}

func modelList(entries ...string) func(context.Context, aitest.ModelsRequest) aitest.Reply {
	return func(context.Context, aitest.ModelsRequest) aitest.Reply {
		var reply aitest.ModelsReply
		for _, e := range entries {
			reply.Entries = append(reply.Entries, jsontext.Value(e))
		}
		return reply
	}
}

var runArgs = []string{"run", "docproc-dir", "--out", "out"}

func runWith(t *testing.T, cfg app.Config) (code int, stdout, stderr *bytes.Buffer, built *apptest.Recorder) {
	t.Helper()
	a, stdout, stderr, built := newApp(t)
	apptest.Config(a, cfg)
	return a.Run(context.Background(), runArgs), stdout, stderr, built
}

func modelRequests(e *aitest.Endpoint) []aitest.Request {
	var models []aitest.Request
	for _, r := range e.Requests() {
		if r.Path == "/v1/models" {
			models = append(models, r)
		}
	}
	return models
}

func TestRun_ModelTable(t *testing.T) {
	e := aitest.Serve(t, aitest.Routes{Models: modelList(
		`{"id":"vision-model","object":"model","status":{"value":"loaded"}}`,
		`{"id":"audio-model","object":"model","status":{"value":"unloaded"}}`,
		`{"id":"embed-model","object":"model","status":"ready"}`,
	)})

	code, stdout, stderr, built := runWith(t, everyCapability(e.URL(), "completions"))

	if code != process.ExitOK {
		t.Errorf("code = %d, want %d; stderr = %q", code, process.ExitOK, stderr)
	}
	want := "CAPABILITY     ENDPOINT  MODEL         STATE\n" +
		"vision         gb10      vision-model  loaded\n" +
		"transcription  gb10      audio-model   unloaded\n" +
		"embeddings     gb10      embed-model   listed\n" +
		`warning: transcription: endpoint "gb10" lists model "audio-model" as unloaded` + "\n"
	if stdout.String() != want {
		t.Errorf("stdout =\n%s\nwant\n%s", stdout, want)
	}
	if n := len(modelRequests(e)); n != 3 {
		t.Errorf("models requests = %d, want 3, one per capability's client", n)
	}
	log := built.Log()
	slices.Sort(log)
	wantBuilt := []string{"config", "embeddings client", "http clients", "lifecycle config", "run config", "transcription client", "vision client"}
	if !slices.Equal(log, wantBuilt) {
		t.Errorf("nodes built = %q, want %q", log, wantBuilt)
	}
}

func TestRun_UnsetCapabilitiesAreSkipped(t *testing.T) {
	e := aitest.Serve(t, aitest.Routes{Models: modelList(`{"id":"embed-model","object":"model","status":{"value":"loaded"}}`)})

	code, stdout, stderr, _ := runWith(t, embeddingsOnly(e.URL()))

	if code != process.ExitOK {
		t.Errorf("code = %d, want %d; stderr = %q", code, process.ExitOK, stderr)
	}
	want := "CAPABILITY     ENDPOINT  MODEL        STATE\n" +
		"vision         -         -            skipped\n" +
		"transcription  -         -            skipped\n" +
		"embeddings     gb10      embed-model  loaded\n"
	if stdout.String() != want {
		t.Errorf("stdout =\n%s\nwant\n%s", stdout, want)
	}
	if n := len(modelRequests(e)); n != 1 {
		t.Errorf("models requests = %d, want 1", n)
	}
}

func TestRun_UnlistedModelStopsTheRun(t *testing.T) {
	e := aitest.Serve(t, aitest.Routes{Models: modelList(`{"id":"other-model","object":"model"}`)})

	code, stdout, stderr, _ := runWith(t, embeddingsOnly(e.URL()))

	if code != process.ExitFailure {
		t.Errorf("code = %d, want %d", code, process.ExitFailure)
	}
	if !strings.Contains(stdout.String(), "embeddings     gb10      embed-model  unlisted\n") {
		t.Errorf("stdout = %q, want the embeddings row unlisted", stdout)
	}
	if want := `docinfer run: embeddings: endpoint "gb10" doesn't list model "embed-model"`; !strings.HasPrefix(stderr.String(), want) {
		t.Errorf("stderr = %q, want it to start with %q", stderr, want)
	}
}

func TestRun_NoListingIsUnchecked(t *testing.T) {
	e := aitest.Serve(t, aitest.Routes{})

	code, stdout, stderr, _ := runWith(t, everyCapability(e.URL(), "completions"))

	if code != process.ExitOK {
		t.Errorf("code = %d, want %d; stderr = %q", code, process.ExitOK, stderr)
	}
	for _, row := range []string{
		"vision         gb10      vision-model  unchecked\n",
		"transcription  gb10      audio-model   unchecked\n",
		"embeddings     gb10      embed-model   unchecked\n",
	} {
		if !strings.Contains(stdout.String(), row) {
			t.Errorf("stdout = %q, want the row %q", stdout, row)
		}
	}
	if want := `warning: embeddings: endpoint "gb10" gives no model listing (`; !strings.Contains(stdout.String(), want) {
		t.Errorf("stdout = %q, want the warning %q", stdout, want)
	}
}

func TestRun_UnreachableEndpointStopsTheRun(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	cfg := embeddingsOnly("http://" + addr + "/v1")
	cfg.Endpoints["gb10"].Retry.MaxAttempts = new(1)

	code, stdout, stderr, _ := runWith(t, cfg)

	if code != process.ExitFailure {
		t.Errorf("code = %d, want %d", code, process.ExitFailure)
	}
	if !strings.Contains(stdout.String(), "embeddings     gb10      embed-model  unreachable\n") {
		t.Errorf("stdout = %q, want the embeddings row unreachable", stdout)
	}
	if want := `docinfer run: embeddings: endpoint "gb10" is unreachable: `; !strings.HasPrefix(stderr.String(), want) {
		t.Errorf("stderr = %q, want it to start with %q", stderr, want)
	}
}

func TestRun_ModelsRetryStartsAtThreeAttempts(t *testing.T) {
	e := aitest.Serve(t, aitest.Routes{Models: func(context.Context, aitest.ModelsRequest) aitest.Reply {
		return aitest.ErrorReply{Status: http.StatusServiceUnavailable, Message: "loading"}
	}})
	cfg := embeddingsOnly(e.URL())
	cfg.Endpoints["gb10"].Retry = app.Retry{BaseDelay: new(config.Duration(time.Millisecond)), MaxDelay: new(config.Duration(time.Millisecond))}

	code, stdout, stderr, _ := runWith(t, cfg)

	if code != process.ExitOK {
		t.Errorf("code = %d, want %d; stderr = %q", code, process.ExitOK, stderr)
	}
	if !strings.Contains(stdout.String(), "embed-model  unchecked\n") {
		t.Errorf("stdout = %q, want the embeddings row unchecked", stdout)
	}
	if n := len(modelRequests(e)); n != 3 {
		t.Errorf("models requests = %d, want 3, the starting retry policy's attempts", n)
	}
}

func TestRun_ModelsListThroughTheEndpointsSettings(t *testing.T) {
	e := aitest.Serve(t, aitest.Routes{Models: modelList(`{"id":"embed-model","object":"model"}`)})
	dir := configDir(t, map[string]string{
		"config.json": `{"endpoints":{"gb10":{"base_url":"` + e.URL() + `","format":"completions","query":{"autoload":"false"}}},
			"embeddings":{"endpoint":"gb10","model":"embed-model","dimensions":768}}`,
		"secrets.json": `{"endpoints":{"gb10":{"token":"secret-token","headers":{"X-Gateway-Key":"gateway-key"}}}}`,
	})
	a, _, stderr, _ := newApp(t)

	code := a.Run(context.Background(), append(slices.Clone(runArgs), "--config", dir))

	if code != process.ExitOK {
		t.Fatalf("code = %d, want %d; stderr = %q", code, process.ExitOK, stderr)
	}
	reqs := modelRequests(e)
	if len(reqs) != 1 {
		t.Fatalf("models requests = %d, want 1", len(reqs))
	}
	r := reqs[0]
	if got := r.Query.Get("autoload"); got != "false" {
		t.Errorf("query autoload = %q, want %q", got, "false")
	}
	if got := r.Header.Get("Authorization"); got != "Bearer secret-token" {
		t.Errorf("Authorization = %q, want the secrets file's token", got)
	}
	if got := r.Header.Get("X-Gateway-Key"); got != "gateway-key" {
		t.Errorf("X-Gateway-Key = %q, want the secrets file's header", got)
	}
}

func TestRun_UsageErrorBuildsNothing(t *testing.T) {
	a, _, stderr, built := newApp(t)

	code := a.Run(context.Background(), []string{"run", "docproc-dir"})

	if code != process.ExitUsage {
		t.Errorf("code = %d, want %d", code, process.ExitUsage)
	}
	if !strings.Contains(stderr.String(), "out") {
		t.Errorf("stderr = %q, want it to name --out", stderr)
	}
	if log := built.Log(); len(log) != 0 {
		t.Errorf("nodes built = %q, want none", log)
	}
}

func probeNodes(a *app.App, read func(inv *cli.Invocation)) {
	n := a.Nodes()
	a.Root().Add((&cli.Command{
		Name:    "probe",
		Summary: "Read the configuration and client nodes",
		Run: func(_ context.Context, inv *cli.Invocation) error {
			read(inv)
			return nil
		},
	}).Use(n.Config, n.HTTPClients, n.VisionClient, n.TranscriptionClient, n.EmbeddingsClient))
}
