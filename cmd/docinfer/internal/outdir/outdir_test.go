package outdir_test

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/standards-lab/docproc/dispatch"

	"github.com/standards-lab/docinfer"
	"github.com/standards-lab/docinfer/chunk"

	"github.com/standards-lab/docinfer/cmd/docinfer/internal/outdir"
)

func entries(t *testing.T, dir string) []string {
	t.Helper()
	es, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range es {
		names = append(names, e.Name())
	}
	return names
}

func TestOpen_CreatesNothing(t *testing.T) {
	root := t.TempDir()

	_, err := outdir.Open(root)

	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Open of an empty directory = %v, want an error matching fs.ErrNotExist", err)
	}
	if names := entries(t, root); len(names) != 0 {
		t.Errorf("Open made %q, want nothing", names)
	}
	missing := filepath.Join(root, "missing")
	if _, err := outdir.Open(missing); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Open of a missing directory = %v, want an error matching fs.ErrNotExist", err)
	}
	if _, err := os.Stat(missing); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Open made %s", missing)
	}
}

func TestCreate_WritesTheMarker(t *testing.T) {
	root := filepath.Join(t.TempDir(), "out")

	if _, err := outdir.Create(root); err != nil {
		t.Fatal(err)
	}

	b, err := os.ReadFile(filepath.Join(root, "outdir.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(b), "{\n  \"schema\": \"2\"\n}\n"; got != want {
		t.Errorf("outdir.json = %q, want %q", got, want)
	}
	for _, dir := range []string{"results", "texts"} {
		if info, err := os.Stat(filepath.Join(root, dir)); err != nil || !info.IsDir() {
			t.Errorf("%s/ = %v, %v, want a directory", dir, info, err)
		}
	}
	if _, err := outdir.Open(root); err != nil {
		t.Errorf("Open after Create = %v, want nil", err)
	}
}

func TestOpen_RefusesAnotherSchema(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "outdir.json"), []byte(`{"schema":"3"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	for name, open := range map[string]func(string) (*outdir.Dir, error){"Open": outdir.Open, "Create": outdir.Create} {
		_, err := open(root)
		if err == nil || !strings.Contains(err.Error(), `schema "3", want "2"`) {
			t.Errorf("%s = %v, want a refusal naming both schemas", name, err)
		}
	}
}

func TestCreate_RefusesSchemaOne(t *testing.T) {
	for _, dir := range []string{"results", "texts", "runs"} {
		root := t.TempDir()
		if err := os.Mkdir(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}

		_, err := outdir.Create(root)

		if err == nil || !strings.Contains(err.Error(), `schema "1", want "2"`) {
			t.Errorf("Create over %s/ = %v, want a refusal naming both schemas", dir, err)
		}
		if _, err := os.Stat(filepath.Join(root, "outdir.json")); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("Create over %s/ wrote outdir.json", dir)
		}
	}
}

func batch(part, text, vector string) docinfer.Batch {
	return docinfer.Batch{
		Parts:   []dispatch.Part{{ID: part, Name: part + ".txt"}},
		Sources: []docinfer.Source{{Kind: "metadata", SHA256: strings.Repeat("a", 64)}},
		Chunks: []docinfer.Chunk{{
			Digest: strings.Repeat("b", 64),
			Text:   text, Span: chunk.Span{End: len(text)},
			Vector: vector,
		}},
	}
}

func lines(t *testing.T, d *outdir.Dir, id string) []outdir.Line {
	t.Helper()
	var got []outdir.Line
	for l, err := range d.Lines(id) {
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, l)
	}
	return got
}

func inferences(t *testing.T, d *outdir.Dir, id string) []outdir.Inference {
	t.Helper()
	var got []outdir.Inference
	for i, err := range d.Inferences(id) {
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, i)
	}
	return got
}

func TestRun_AppendsPerBatch(t *testing.T) {
	root := t.TempDir()
	d, err := outdir.Create(root)
	if err != nil {
		t.Fatal(err)
	}
	r, err := d.Begin(outdir.Manifest{})
	if err != nil {
		t.Fatal(err)
	}

	if err := r.Batch(batch("p1", "one", "AACAPw=="), []string{"/in/a.pdf"}); err != nil {
		t.Fatal(err)
	}
	if got := lines(t, d, r.ID()); len(got) != 1 || got[0].Text != "one" || got[0].Part.ID != "p1" {
		t.Errorf("lines after one batch = %+v, want its line", got)
	}
	if err := r.Batch(batch("p2", "two", ""), []string{"/in/b.txt"}); err != nil {
		t.Fatal(err)
	}
	inf := docinfer.Inference{Capability: docinfer.CapabilityVision, Part: "p1", ContentSHA256: []string{strings.Repeat("c", 64)}, Outcome: docinfer.OutcomeFailed, Reason: "500"}
	if err := r.Inference(outdir.Inference{Input: "/in/a.pdf", Inference: inf}); err != nil {
		t.Fatal(err)
	}
	if got := lines(t, d, r.ID()); len(got) != 2 || got[1].Text != "two" || got[0].Input != "/in/a.pdf" || got[1].Input != "/in/b.txt" {
		t.Errorf("lines after two batches = %+v, want both, each naming its input", got)
	}
	if got := inferences(t, d, r.ID()); len(got) != 1 || got[0].Outcome != docinfer.OutcomeFailed || got[0].Reason != "500" || got[0].Input != "/in/a.pdf" {
		t.Errorf("inferences = %+v, want the failed image, naming its input", got)
	}
	if err := r.Batch(batch("p3", "three", ""), nil); !errors.Is(err, dispatch.ErrRequest) {
		t.Errorf("Batch with no input for its part = %v, want dispatch.ErrRequest", err)
	}
	if _, err := d.Latest(); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Latest before End = %v, want no run that ended", err)
	}

	if err := r.End(outdir.Manifest{Outputs: 2}); err != nil {
		t.Fatal(err)
	}
	m, err := d.Manifest(r.ID())
	if err != nil {
		t.Fatal(err)
	}
	if m.End.IsZero() || m.Schema != outdir.Version || m.Run != r.ID() || m.Outputs != 2 {
		t.Errorf("manifest = %+v, want its end, schema, run, and outputs", m)
	}
	if latest, err := d.Latest(); err != nil || latest != r.ID() {
		t.Errorf("Latest = %q, %v, want %q", latest, err, r.ID())
	}
	b, err := os.ReadFile(filepath.Join(root, "runs", r.ID(), "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"start": "`) || !strings.Contains(string(b), `Z",`) {
		t.Errorf("manifest = %s, want its times in UTC", b)
	}
}

func TestLines_DropAnUnendedRunsPartialLastLine(t *testing.T) {
	root := t.TempDir()
	d, err := outdir.Create(root)
	if err != nil {
		t.Fatal(err)
	}
	r, err := d.Begin(outdir.Manifest{})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Batch(batch("p1", "one", ""), []string{"/in/a.pdf"}); err != nil {
		t.Fatal(err)
	}
	if err := r.Inference(outdir.Inference{Input: "/in/a.pdf", Part: "p1", Outcome: docinfer.OutcomeDescribed}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"chunks.jsonl", "inferences.jsonl"} {
		f, err := os.OpenFile(filepath.Join(root, "runs", r.ID(), name), os.O_WRONLY|os.O_APPEND, 0)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.WriteString(`{"digest":"torn`); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	}

	if got := lines(t, d, r.ID()); len(got) != 1 || got[0].Text != "one" {
		t.Errorf("lines = %+v, want the whole line alone", got)
	}
	if got := inferences(t, d, r.ID()); len(got) != 1 || got[0].Part != "p1" {
		t.Errorf("inferences = %+v, want the whole line alone", got)
	}
}

func TestLines_LeaveOutVectors(t *testing.T) {
	root := t.TempDir()
	d, err := outdir.Create(root)
	if err != nil {
		t.Fatal(err)
	}
	r, err := d.Begin(outdir.Manifest{})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Batch(batch("p1", "one", "AACAPw=="), []string{"/in/a.pdf"}); err != nil {
		t.Fatal(err)
	}
	if err := r.End(outdir.Manifest{}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(root, "runs", r.ID(), "chunks.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"vector":"AACAPw=="`) {
		t.Fatalf("chunks.jsonl = %s, want the vector written", b)
	}

	got := lines(t, d, r.ID())

	if len(got) != 1 || got[0].Vector != "" || got[0].Digest != strings.Repeat("b", 64) || got[0].Source.Kind != "metadata" {
		t.Errorf("lines = %+v, want the line without its vector", got)
	}
}

func TestDir_RefusesAnIDThatIsNoRun(t *testing.T) {
	d, err := outdir.Create(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	for _, id := range []string{"nope", "019a5e2a-1b3c-7d4e-8f00-000000000001", "../outdir.json"} {
		if _, err := d.Manifest(id); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("Manifest(%q) = %v, want an error matching fs.ErrNotExist", id, err)
		}
		if _, err := d.ReportDir(id); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("ReportDir(%q) = %v, want an error matching fs.ErrNotExist", id, err)
		}
		for _, err := range d.Lines(id) {
			if !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("Lines(%q) = %v, want an error matching fs.ErrNotExist", id, err)
			}
		}
	}
}

func TestOpen_ReadsAlone(t *testing.T) {
	root := t.TempDir()
	if _, err := outdir.Create(root); err != nil {
		t.Fatal(err)
	}
	d, err := outdir.Open(root)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := d.Begin(outdir.Manifest{}); err == nil {
		t.Error("Begin on a Dir from Open = nil, want a refusal")
	}
	if err := d.Texts().Put(context.Background(), strings.Repeat("a", 64), strings.NewReader("text")); err == nil {
		t.Error("Texts().Put on a Dir from Open = nil, want a refusal")
	}
	if _, err := d.Texts().Open(context.Background(), strings.Repeat("a", 64)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Texts().Open of a missing text = %v, want an error matching fs.ErrNotExist", err)
	}
}
