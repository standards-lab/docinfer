package app_test

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/go-core/process"

	"github.com/standards-lab/go-ai/aitest"

	"github.com/standards-lab/docproc/dispatch"
)

func textRun(t *testing.T, docproc string, texts ...string) {
	t.Helper()
	dr := newDocprocRun(t, docproc)
	for i, text := range texts {
		path := "/in/" + string(rune('a'+i)) + ".txt"
		part := inputPart(filepath.Base(path), []byte(text+" input"))
		dr.output(path, part, dispatch.TextOutput, "text/plain", []byte(text))
		dr.part(path, part, 1)
	}
	dr.end()
}

func TestRun_ReportAfterAnEndedRun(t *testing.T) {
	e := aitest.Serve(t, aitest.Routes{Models: listed, Embeddings: unitVectors})
	root := t.TempDir()
	docproc, out := filepath.Join(root, "docproc"), filepath.Join(root, "out")
	textRun(t, docproc, "alpha text")
	stdout := new(bytes.Buffer)
	a, stderr := inferApp(t, inferConfig(e.URL(), false), stdout)

	code := a.Run(context.Background(), []string{"run", docproc, "--out", out, "--report"})

	if code != process.ExitOK {
		t.Fatalf("code = %d, want %d; stderr = %q", code, process.ExitOK, stderr)
	}
	_, m := openOut(t, out)
	index := indexPath(t, out, m.Run)
	want := "docinfer run " + m.Run + ": inputs 1, outputs 1, chunks 2, failures 0\n" + index + "\n"
	if !strings.HasSuffix(stdout.String(), want) {
		t.Errorf("stdout = %q, want it to end with the last line and the index's path, %q", stdout, want)
	}
	if _, err := os.Stat(index); err != nil {
		t.Errorf("the index page: %v", err)
	}
}

func TestRun_CancelledRendersNoReport(t *testing.T) {
	var gate aitest.Gate
	e := aitest.Serve(t, aitest.Routes{
		Models: listed,
		Embeddings: func(ctx context.Context, req aitest.EmbeddingRequest) aitest.Reply {
			if err := gate.Wait(ctx); err != nil {
				return aitest.ErrorReply{Status: http.StatusServiceUnavailable, Message: "cancelled"}
			}
			return unitVectors(ctx, req)
		},
	})
	root := t.TempDir()
	docproc, out := filepath.Join(root, "docproc"), filepath.Join(root, "out")
	textRun(t, docproc, "alpha text")
	a, stderr := inferApp(t, inferConfig(e.URL(), false), new(bytes.Buffer))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan int, 1)

	go func() { done <- a.Run(ctx, []string{"run", docproc, "--out", out, "--report"}) }()
	gate.Await(t, 1)
	cancel()
	code := wait(t, done)

	if code != process.ExitFailure {
		t.Errorf("code = %d, want %d", code, process.ExitFailure)
	}
	if !strings.Contains(stderr.String(), "context canceled") || strings.Contains(stderr.String(), "the run's report") {
		t.Errorf("stderr = %q, want the cancellation and no report", stderr)
	}
	_, m := openOut(t, out)
	if _, err := os.Stat(filepath.Join(out, "runs", m.Run, "report")); !os.IsNotExist(err) {
		t.Errorf("the run's report directory: %v, want none", err)
	}
}

var beginLine = regexp.MustCompile(`docinfer run (\S+): docproc run `)

func TestRun_FailedReportKeepsTheRun(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes in a directory whatever its mode")
	}
	var gate aitest.Gate
	e := aitest.Serve(t, aitest.Routes{
		Models: listed,
		Embeddings: func(ctx context.Context, req aitest.EmbeddingRequest) aitest.Reply {
			if err := gate.Wait(ctx); err != nil {
				return aitest.ErrorReply{Status: http.StatusServiceUnavailable, Message: "cancelled"}
			}
			return unitVectors(ctx, req)
		},
	})
	root := t.TempDir()
	docproc, out := filepath.Join(root, "docproc"), filepath.Join(root, "out")
	textRun(t, docproc, "alpha text")
	stdout := watch("docinfer run ")
	a, stderr := inferApp(t, inferConfig(e.URL(), false), stdout)
	done := make(chan int, 1)

	go func() { done <- a.Run(context.Background(), []string{"run", docproc, "--out", out, "--report"}) }()
	gate.Await(t, 1)
	match := beginLine.FindStringSubmatch(stdout.String())
	if match == nil {
		gate.Open()
		wait(t, done)
		t.Fatalf("stdout = %q, want the run's first line while its embeddings are parked", stdout)
	}
	id := match[1]
	reportDir := filepath.Join(out, "runs", id, "report")
	if err := os.Mkdir(reportDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(reportDir, 0o755) })
	gate.Open()
	code := wait(t, done)

	if code != process.ExitFailure {
		t.Errorf("code = %d, want %d", code, process.ExitFailure)
	}
	again := "docinfer report " + out + " --run " + id + " renders its pages again"
	if !strings.Contains(stderr.String(), "the run's report: ") || !strings.Contains(stderr.String(), again) {
		t.Errorf("stderr = %q, want the report's error and %q", stderr, again)
	}
	if want := "docinfer run " + id + ": inputs 1, outputs 1, chunks 2, failures 0\n"; !strings.HasSuffix(stdout.String(), want) {
		t.Errorf("stdout = %q, want it to end with the last line, no index path after it", stdout)
	}
	_, m := openOut(t, out)
	if m.Run != id || m.End.IsZero() || m.Error != "" {
		t.Errorf("manifest run %s, end %v, error %q, want run %s ended with no error", m.Run, m.End, m.Error, id)
	}
	records := func() map[string]string {
		got := map[string]string{}
		for _, name := range []string{"manifest.json", "chunks.jsonl", "inferences.jsonl"} {
			b, err := os.ReadFile(filepath.Join(out, "runs", id, name))
			if err != nil {
				t.Fatal(err)
			}
			got[name] = string(b)
		}
		return got
	}
	before := records()
	if !strings.Contains(before["chunks.jsonl"], "alpha text") {
		t.Errorf("chunks.jsonl = %q, want the run's chunks", before["chunks.jsonl"])
	}

	if err := os.Chmod(reportDir, 0o755); err != nil {
		t.Fatal(err)
	}
	r, rout, rerr, _ := newApp(t)
	if code := r.Run(context.Background(), []string{"report", out, "--run", id}); code != process.ExitOK {
		t.Fatalf("report's code = %d, want %d; stderr = %q", code, process.ExitOK, rerr)
	}
	if want := indexPath(t, out, id) + "\n"; rout.String() != want {
		t.Errorf("report's stdout = %q, want %q", rout, want)
	}
	for name, b := range records() {
		if b != before[name] {
			t.Errorf("%s changed when the report rendered", name)
		}
	}
}

func wait(t *testing.T, done <-chan int) int {
	t.Helper()
	select {
	case code := <-done:
		return code
	case <-time.After(waitLimit):
		t.Fatal("the run never returned")
		return 0
	}
}
