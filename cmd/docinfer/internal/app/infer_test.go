package app_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/standards-lab/go-cli-sdk"
	"github.com/standards-lab/go-core/process"

	"github.com/standards-lab/go-ai/aitest"

	"github.com/standards-lab/docproc/dispatch"

	"github.com/standards-lab/docinfer"

	"github.com/standards-lab/docinfer/cmd/docinfer/internal/app"
	"github.com/standards-lab/docinfer/cmd/docinfer/internal/apptest"
	"github.com/standards-lab/docinfer/cmd/docinfer/internal/outdir"
)

const (
	firstRun  = "019a5e2a-1b3c-7d4e-8f00-000000000001"
	secondRun = "019a5e2a-1b3c-7d4e-8f00-000000000002"
)

func inferConfig(baseURL string, vision bool) app.Config {
	cfg := everyCapability(baseURL, "completions")
	cfg.Transcription = nil
	if !vision {
		cfg.Vision = nil
	}
	cfg.Endpoints["gb10"].Retry.MaxAttempts = new(1)
	cfg.Embeddings.Dimensions = new(4)
	return cfg
}

func unitVectors(_ context.Context, req aitest.EmbeddingRequest) aitest.Reply {
	reply := aitest.EmbeddingsReply{PromptTokens: len(req.Input), TotalTokens: len(req.Input)}
	for range req.Input {
		reply.Vectors = append(reply.Vectors, []float32{1, 0, 0, 0})
	}
	return reply
}

func wordTokens(_ context.Context, req aitest.TokenizeRequest) aitest.Reply {
	return aitest.TokenizeReply{Count: len(strings.Fields(req.Prompt)) + 2}
}

var listed = modelList(
	`{"id":"vision-model","object":"model","status":{"value":"loaded"}}`,
	`{"id":"embed-model","object":"model","status":{"value":"loaded"}}`,
)

func inferApp(t *testing.T, cfg app.Config, stdout io.Writer) (*app.App, *bytes.Buffer) {
	t.Helper()
	stderr := new(bytes.Buffer)
	a := app.New(cli.Streams{Stdin: strings.NewReader(""), Stdout: stdout, Stderr: stderr})
	apptest.Config(a, cfg)
	return a, stderr
}

func openOut(t *testing.T, root string) (*outdir.Dir, outdir.Manifest) {
	t.Helper()
	d, err := outdir.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	runs, err := d.Runs()
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) == 0 {
		t.Fatal("the output directory holds no run")
	}
	m, err := d.Manifest(runs[len(runs)-1])
	if err != nil {
		t.Fatal(err)
	}
	return d, m
}

func collect[T any](t *testing.T, seq func(func(T, error) bool)) []T {
	t.Helper()
	var got []T
	for v, err := range seq {
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, v)
	}
	return got
}

func TestRun_EndsWithAFailedImageRecorded(t *testing.T) {
	e := aitest.Serve(t, aitest.Routes{
		Models:     listed,
		Embeddings: unitVectors,
		Tokenize:   wordTokens,
		ChatCompletions: func(_ context.Context, req aitest.ChatRequest) aitest.Reply {
			for _, m := range req.Messages {
				for _, img := range m.Images {
					if string(img.Data) == "image-b" {
						return aitest.ErrorReply{Status: http.StatusInternalServerError, Message: "the model crashed"}
					}
				}
			}
			return aitest.ChatReply{Text: "a red square"}
		},
	})
	root := t.TempDir()
	docproc, out := filepath.Join(root, "docproc"), filepath.Join(root, "out")
	dr := newDocprocRun(t, docproc)
	part := inputPart("a.pdf", []byte("pdf-bytes"))
	dr.output("/in/a.pdf", part, dispatch.TextOutput, "text/plain", []byte("hello world"))
	dr.output("/in/a.pdf", part, dispatch.ImageOutput, "image/png", []byte("image-a"))
	failedImage := dr.output("/in/a.pdf", part, dispatch.ImageOutput, "image/png", []byte("image-b"))
	dr.part("/in/a.pdf", part, 3)
	dr.failed("/in/b.bin", "open /in/b.bin: permission denied")
	docprocID := dr.end()
	stdout := new(bytes.Buffer)
	a, stderr := inferApp(t, inferConfig(e.URL(), true), stdout)

	code := a.Run(context.Background(), []string{"run", docproc, "--out", out})

	if code != process.ExitOK {
		t.Fatalf("code = %d, want %d; stderr = %q", code, process.ExitOK, stderr)
	}
	d, m := openOut(t, out)
	if !strings.Contains(stdout.String(), "docinfer run "+m.Run+": docproc run "+docprocID+"\n") {
		t.Errorf("stdout = %q, want the run's first line", stdout)
	}
	if !strings.Contains(stdout.String(), "/in/a.pdf  images 2  windows 0  chunks 3  failures 1\n") {
		t.Errorf("stdout = %q, want a.pdf's progress line", stdout)
	}
	if !strings.Contains(stdout.String(), "vision: images 2: described 1, incomplete 0 (empty 0), refused 0, failed 1, held 0; requests 2\n") {
		t.Errorf("stdout = %q, want vision's counts", stdout)
	}
	if want := "docinfer run " + m.Run + ": inputs 2, outputs 3, chunks 3, failures 1\n"; !strings.HasSuffix(stdout.String(), want) {
		t.Errorf("stdout = %q, want it to end with %q", stdout, want)
	}

	if m.End.IsZero() || m.Error != "" {
		t.Errorf("manifest end %v, error %q, want an end and no error", m.End, m.Error)
	}
	if filepath.Join(out, filepath.FromSlash(m.Docproc.Dir)) != docproc || m.Docproc.Run != docprocID {
		t.Errorf("manifest docproc = %+v, want docproc's directory relative to out and its run", m.Docproc)
	}
	wantInputs := []outdir.Input{{Path: "/in/a.pdf", SHA256: part.SHA256}, {Path: "/in/b.bin", Error: "open /in/b.bin: permission denied"}}
	if !slices.Equal(m.Inputs, wantInputs) || m.Outputs != 3 {
		t.Errorf("manifest inputs %+v, outputs %d, want %+v and 3", m.Inputs, m.Outputs, wantInputs)
	}
	version := new(bytes.Buffer)
	if code := app.New(cli.Streams{Stdout: version, Stderr: io.Discard}).Run(context.Background(), []string{"version"}); code != process.ExitOK {
		t.Fatalf("version's code = %d, want %d", code, process.ExitOK)
	}
	if got := "command " + m.Docinfer.Command + "\nlibrary " + m.Docinfer.Library + "\n"; got != version.String() {
		t.Errorf("manifest docinfer = %+v, want the versions docinfer version prints, %q", m.Docinfer, version)
	}
	var names []docinfer.Capability
	for _, c := range m.Capabilities {
		names = append(names, c.Capability)
		if c.Endpoint != "gb10" || c.BaseURL != e.URL() || c.Format != "completions" || c.Concurrency != 4 {
			t.Errorf("capability %s = %+v, want its endpoint and bounds", c.Capability, c)
		}
	}
	if !slices.Equal(names, []docinfer.Capability{docinfer.CapabilityVision, docinfer.CapabilityEmbeddings}) {
		t.Errorf("manifest capabilities = %q, want vision and embeddings", names)
	}
	if len(m.Skipped) != 1 || m.Skipped[0].Name != docinfer.CapabilityTranscription {
		t.Errorf("manifest skipped = %+v, want transcription", m.Skipped)
	}
	if m.Counts.Vision == nil || m.Counts.Vision.Failed != 1 || m.Counts.Embedded != 3 {
		t.Errorf("manifest counts = %+v, want the failed image and three chunks", m.Counts)
	}
	if s := m.Space; s == nil || s.Vectors != 3 || !s.Unit || s.Dimensions != 4 {
		t.Errorf("manifest embedding space = %+v, want three unit vectors of 4 dimensions", s)
	}

	infs := collect(t, d.Inferences(m.Run))
	var failed, described *outdir.Inference
	for i := range infs {
		switch infs[i].Outcome {
		case docinfer.OutcomeFailed:
			failed = &infs[i]
		case docinfer.OutcomeDescribed:
			described = &infs[i]
		}
	}
	if failed == nil || failed.Input != "/in/a.pdf" || failed.Part != "/" || !slices.Equal(failed.ContentSHA256, []string{failedImage}) || failed.Reason == "" {
		t.Errorf("inferences = %+v, want the failed image by its input, part ID, and content SHA-256, with its reason", infs)
	}
	if described == nil {
		t.Fatalf("inferences = %+v, want the described image", infs)
	}
	rc, err := d.Texts().Open(context.Background(), described.TextSHA256)
	if err != nil {
		t.Fatal(err)
	}
	text, err := io.ReadAll(rc)
	if err := errors.Join(err, rc.Close()); err != nil {
		t.Fatal(err)
	}
	if string(text) != "a red square" {
		t.Errorf("texts/ holds %q, want the description", text)
	}
	lines := collect(t, d.Lines(m.Run))
	if len(lines) != 3 {
		t.Errorf("chunk lines = %d, want 3", len(lines))
	}
	for _, l := range lines {
		if l.Input != "/in/a.pdf" {
			t.Errorf("chunk line %s of part %s names input %q, want /in/a.pdf", l.Source.Kind, l.Part.ID, l.Input)
		}
	}

	stdout.Reset()
	a, stderr = inferApp(t, inferConfig(e.URL(), true), stdout)
	if code := a.Run(context.Background(), []string{"run", docproc, "--out", out}); code != process.ExitOK {
		t.Fatalf("second run's code = %d, want %d; stderr = %q", code, process.ExitOK, stderr)
	}
	if _, m := openOut(t, out); m.Counts.Vision == nil || m.Counts.Vision.Held != 1 || m.Counts.Vision.Failed != 1 {
		t.Errorf("second run's vision counts = %+v, want the description held and the failure asked again", m.Counts.Vision)
	}
}

type watchedOutput struct {
	mu    sync.Mutex
	buf   bytes.Buffer
	want  string
	found bool
	seen  chan struct{}
}

func watch(want string) *watchedOutput {
	return &watchedOutput{want: want, seen: make(chan struct{})}
}

func (w *watchedOutput) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n, err := w.buf.Write(p)
	if !w.found && strings.Contains(w.buf.String(), w.want) {
		w.found = true
		close(w.seen)
	}
	return n, err
}

func (w *watchedOutput) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

const waitLimit = 10 * time.Second

func TestRun_CancelledKeepsWhatItMade(t *testing.T) {
	var gate aitest.Gate
	e := aitest.Serve(t, aitest.Routes{
		Models:   listed,
		Tokenize: wordTokens,
		Embeddings: func(ctx context.Context, req aitest.EmbeddingRequest) aitest.Reply {
			if slices.ContainsFunc(req.Input, func(s string) bool { return strings.Contains(s, "park-me") }) {
				if err := gate.Wait(ctx); err != nil {
					return aitest.ErrorReply{Status: http.StatusServiceUnavailable, Message: "cancelled"}
				}
			}
			return unitVectors(ctx, req)
		},
	})
	root := t.TempDir()
	docproc, out := filepath.Join(root, "docproc"), filepath.Join(root, "out")
	dr := newDocprocRun(t, docproc)
	a1 := inputPart("a.txt", []byte("alpha"))
	dr.output("/in/a.txt", a1, dispatch.TextOutput, "text/plain", []byte("alpha text"))
	dr.part("/in/a.txt", a1, 1)
	b1 := inputPart("b.txt", []byte("beta"))
	dr.output("/in/b.txt", b1, dispatch.TextOutput, "text/plain", []byte("park-me"))
	dr.part("/in/b.txt", b1, 1)
	dr.end()
	stdout := watch("/in/a.txt  images 0  windows 0  chunks 2  failures 0\n")
	a, stderr := inferApp(t, inferConfig(e.URL(), false), stdout)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan int, 1)

	go func() { done <- a.Run(ctx, []string{"run", docproc, "--out", out}) }()
	gate.Await(t, 1)
	select {
	case <-stdout.seen:
	case <-time.After(waitLimit):
		t.Fatalf("a.txt's progress line never came; stdout = %q", stdout)
	}
	cancel()
	var code int
	select {
	case code = <-done:
	case <-time.After(waitLimit):
		t.Fatal("the cancelled run never returned")
	}

	if code != process.ExitFailure {
		t.Errorf("code = %d, want %d", code, process.ExitFailure)
	}
	if !strings.Contains(stderr.String(), "context canceled") {
		t.Errorf("stderr = %q, want the cancellation", stderr)
	}
	if strings.Contains(stdout.String(), ", failures ") {
		t.Errorf("stdout = %q, want no last line", stdout)
	}
	d, m := openOut(t, out)
	if m.End.IsZero() || !strings.Contains(m.Error, "context canceled") {
		t.Errorf("manifest end %v, error %q, want its end and the cancellation", m.End, m.Error)
	}
	lines := collect(t, d.Lines(m.Run))
	var texts []string
	for _, l := range lines {
		texts = append(texts, l.Text)
	}
	if !slices.Contains(texts, "alpha text") || slices.Contains(texts, "park-me") {
		t.Errorf("chunk lines = %q, want a.txt's and not b.txt's", texts)
	}
}

func TestRun_RunNamesNoDocprocRun(t *testing.T) {
	e := aitest.Serve(t, aitest.Routes{Models: listed})
	root := t.TempDir()
	docproc, out := filepath.Join(root, "docproc"), filepath.Join(root, "out")
	newDocprocRun(t, docproc).end()

	for _, id := range []string{"nope", secondRun} {
		a, stderr := inferApp(t, inferConfig(e.URL(), false), io.Discard)

		code := a.Run(context.Background(), []string{"run", docproc, "--out", out, "--run", id})

		if code != process.ExitFailure {
			t.Errorf("--run %s: code = %d, want %d", id, code, process.ExitFailure)
		}
		if want := `docinfer run: rundir: "` + id + `" is no run of `; !strings.HasPrefix(stderr.String(), want) {
			t.Errorf("--run %s: stderr = %q, want it to start with %q", id, stderr, want)
		}
		if _, err := outdir.Open(out); err == nil {
			t.Errorf("--run %s: the output directory was made, want a run that never began", id)
		}
	}
}

func TestRun_ReadsTheLatestRunThatEnded(t *testing.T) {
	e := aitest.Serve(t, aitest.Routes{Models: listed, Embeddings: unitVectors, Tokenize: wordTokens})
	root := t.TempDir()
	docproc, out := filepath.Join(root, "docproc"), filepath.Join(root, "out")
	ended := newDocprocRunID(t, docproc, firstRun)
	a1 := inputPart("a.txt", []byte("alpha"))
	ended.part("/in/a.txt", a1, 0)
	ended.end()
	unended := newDocprocRunID(t, docproc, secondRun)
	b1 := inputPart("b.txt", []byte("beta"))
	unended.part("/in/b.txt", b1, 0)
	a, stderr := inferApp(t, inferConfig(e.URL(), false), io.Discard)

	code := a.Run(context.Background(), []string{"run", docproc, "--out", out})

	if code != process.ExitOK {
		t.Fatalf("code = %d, want %d; stderr = %q", code, process.ExitOK, stderr)
	}
	if _, m := openOut(t, out); m.Docproc.Run != firstRun || len(m.Inputs) != 1 || m.Inputs[0].Path != "/in/a.txt" {
		t.Errorf("manifest docproc %+v, inputs %+v, want the run that ended", m.Docproc, m.Inputs)
	}

	a, stderr = inferApp(t, inferConfig(e.URL(), false), io.Discard)
	code = a.Run(context.Background(), []string{"run", docproc, "--out", out, "--run", secondRun})

	if code != process.ExitFailure {
		t.Errorf("--run of the unended run: code = %d, want %d", code, process.ExitFailure)
	}
	if want := "docinfer run: docproc run " + secondRun + ": rundir: run has not ended"; !strings.HasPrefix(stderr.String(), want) {
		t.Errorf("stderr = %q, want it to start with %q", stderr, want)
	}
}
