package app_test

import (
	"context"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/standards-lab/go-core/process"
)

const (
	reportEnded   = "019a6000-0000-7000-8000-0000000000a1"
	reportUnended = "019a6000-0000-7000-8000-0000000000a2"
)

func reportFixture(t *testing.T) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "out")
	if err := os.CopyFS(out, os.DirFS(filepath.Join("..", "report", "testdata", "outdir"))); err != nil {
		t.Fatal(err)
	}
	return out
}

func reportPages(t *testing.T, out, id string) map[string]string {
	t.Helper()
	dir := filepath.Join(out, "runs", id, "report")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		got[e.Name()] = string(b)
	}
	return got
}

func indexPath(t *testing.T, out, id string) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join(out, "runs", id, "report", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestReport_LatestThatEnded(t *testing.T) {
	out := reportFixture(t)
	a, stdout, stderr, built := newApp(t)

	code := a.Run(context.Background(), []string{"report", out})

	if code != process.ExitOK {
		t.Fatalf("code = %d, want %d; stderr = %q", code, process.ExitOK, stderr)
	}
	if want := indexPath(t, out, reportEnded) + "\n"; stdout.String() != want {
		t.Errorf("stdout = %q, want the ended run's index path, %q", stdout, want)
	}
	if _, err := os.Stat(filepath.Join(out, "runs", reportUnended, "report")); err == nil {
		t.Error("the report rendered the unended run too")
	}
	if log := built.Log(); len(log) != 0 {
		t.Errorf("nodes built = %q, want none", log)
	}
}

func TestReport_Run(t *testing.T) {
	out := reportFixture(t)
	a, stdout, stderr, _ := newApp(t)

	code := a.Run(context.Background(), []string{"report", out, "--run", reportUnended})

	if code != process.ExitOK {
		t.Fatalf("code = %d, want %d; stderr = %q", code, process.ExitOK, stderr)
	}
	if want := indexPath(t, out, reportUnended) + "\n"; stdout.String() != want {
		t.Errorf("stdout = %q, want the unended run's index path, %q", stdout, want)
	}
	if !strings.Contains(reportPages(t, out, reportUnended)["index.html"], "not ended") {
		t.Error("the unended run's index isn't marked not ended")
	}
}

func TestReport_RendersNothingExits1(t *testing.T) {
	for _, tc := range []struct {
		name    string
		prepare func(t *testing.T, out string)
		args    []string
		want    string
	}{
		{
			name: "unknown run",
			args: []string{"--run", "019a6000-0000-7000-8000-0000000000ff"},
			want: "019a6000-0000-7000-8000-0000000000ff",
		},
		{
			name: "no run that ended",
			prepare: func(t *testing.T, out string) {
				if err := os.RemoveAll(filepath.Join(out, "runs", reportEnded)); err != nil {
					t.Fatal(err)
				}
			},
			want: "--run renders a run that hasn't ended",
		},
		{
			name: "another schema",
			prepare: func(t *testing.T, out string) {
				if err := os.WriteFile(filepath.Join(out, "outdir.json"), []byte(`{"schema": "1"}`), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			args: []string{"--run", reportEnded},
			want: `schema "1", want "2"`,
		},
		{
			name: "no directory",
			prepare: func(t *testing.T, out string) {
				if err := os.RemoveAll(out); err != nil {
					t.Fatal(err)
				}
			},
			want: "outdir: ",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := reportFixture(t)
			if tc.prepare != nil {
				tc.prepare(t, out)
			}
			a, stdout, stderr, _ := newApp(t)

			code := a.Run(context.Background(), append([]string{"report", out}, tc.args...))

			if code != process.ExitFailure {
				t.Errorf("code = %d, want %d", code, process.ExitFailure)
			}
			if !strings.Contains(stderr.String(), tc.want) {
				t.Errorf("stderr = %q, want it to name %q", stderr, tc.want)
			}
			if stdout.Len() != 0 {
				t.Errorf("stdout = %q, want no index path", stdout)
			}
			for _, id := range []string{reportEnded, reportUnended} {
				if _, err := os.Stat(filepath.Join(out, "runs", id, "report")); err == nil {
					t.Errorf("run %s's report was rendered", id)
				}
			}
		})
	}
}

func TestReport_UsageErrorExits2(t *testing.T) {
	for _, args := range [][]string{{"report"}, {"report", "a", "b"}, {"report", "a", "--run="}} {
		a, _, _, _ := newApp(t)
		if code := a.Run(context.Background(), args); code != process.ExitUsage {
			t.Errorf("%q: code = %d, want %d", args, code, process.ExitUsage)
		}
	}
}

func TestReport_HelpStatesItsExitStatus(t *testing.T) {
	a, stdout, stderr, _ := newApp(t)

	a.Run(context.Background(), []string{"report", "--help"})

	if help := stdout.String() + stderr.String(); !strings.Contains(help, "Exit status:") || !strings.Contains(help, "1  nothing was rendered") {
		t.Errorf("help = %q, want its exit status", help)
	}
}

func TestReport_InputWithNoSHA256(t *testing.T) {
	out := reportFixture(t)
	a, _, stderr, _ := newApp(t)
	if code := a.Run(context.Background(), []string{"report", out}); code != process.ExitOK {
		t.Fatalf("code = %d, want %d; stderr = %q", code, process.ExitOK, stderr)
	}

	var row string
	for line := range strings.Lines(reportPages(t, out, reportEnded)["index.html"]) {
		if strings.Contains(line, "<code>/docs/locked.pdf</code>") {
			row = line
		}
	}
	if row == "" || strings.Contains(row, "href") || !strings.Contains(row, `<span class="error">open /docs/locked.pdf: permission denied</span>`) {
		t.Errorf("locked.pdf's row = %q, want it unlinked, with its error", row)
	}
}

func TestReport_SameBytesWithDocprocsDirectoryAbsent(t *testing.T) {
	out := reportFixture(t)
	docproc := filepath.Join(filepath.Dir(out), "docproc")
	if err := os.MkdirAll(filepath.Join(docproc, "content"), 0o755); err != nil {
		t.Fatal(err)
	}
	a, _, stderr, _ := newApp(t)
	if code := a.Run(context.Background(), []string{"report", out}); code != process.ExitOK {
		t.Fatalf("code = %d, want %d; stderr = %q", code, process.ExitOK, stderr)
	}
	present := reportPages(t, out, reportEnded)
	if err := os.RemoveAll(docproc); err != nil {
		t.Fatal(err)
	}

	a, _, stderr, _ = newApp(t)
	if code := a.Run(context.Background(), []string{"report", out}); code != process.ExitOK {
		t.Fatalf("code with docproc's directory absent = %d, want %d; stderr = %q", code, process.ExitOK, stderr)
	}

	if absent := reportPages(t, out, reportEnded); !maps.Equal(absent, present) {
		t.Error("the pages rendered with docproc's directory absent differ from those rendered with it present")
	}
}
