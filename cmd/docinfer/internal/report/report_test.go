package report_test

import (
	"errors"
	"flag"
	"html"
	"io/fs"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/docinfer/cmd/docinfer/internal/report"
)

var update = flag.Bool("update", false, "rewrite the golden report pages")

const (
	ended   = "019a6000-0000-7000-8000-0000000000a1"
	unended = "019a6000-0000-7000-8000-0000000000a2"
)

const (
	pdfSHA     = "29d1283686193dc1461a7deac4f53d9bc5402a28b95d854f69e94986756fd0a9"
	image1SHA  = "0cf457e24a479f02fd4d34540389f720f0807dcff92a7562108165b2637ea82f"
	blueSHA    = "842a63621e9be1013a23aff7314b4a2c2e716434b07c59ae3a07405f182283db"
	longPrefix = "A long description of a busy page, with a table, a chart, and a footnote — näive café. "
)

func fixture(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "out")
	if err := os.CopyFS(root, os.DirFS(filepath.Join("testdata", "outdir"))); err != nil {
		t.Fatal(err)
	}
	return root
}

func render(t *testing.T, root, id string) map[string]string {
	t.Helper()
	if err := report.Render(t.Context(), root, id); err != nil {
		t.Fatal(err)
	}
	return pages(t, filepath.Join(root, "runs", id, "report"))
}

func pages(t *testing.T, dir string) map[string]string {
	t.Helper()
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

func row(page, path string) string {
	for line := range strings.Lines(page) {
		if strings.HasPrefix(line, "<tr><td>") && strings.Contains(line, "<code>"+path+"</code>") {
			return line
		}
	}
	return ""
}

func TestRender_Golden(t *testing.T) {
	root := fixture(t)
	for _, id := range []string{ended, unended} {
		got := render(t, root, id)
		dir := filepath.Join("testdata", "golden", id)
		if *update {
			if err := os.RemoveAll(dir); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			for name, body := range got {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
					t.Fatal(err)
				}
			}
		}
		want := pages(t, dir)
		if g, w := slices.Sorted(maps.Keys(got)), slices.Sorted(maps.Keys(want)); !slices.Equal(g, w) {
			t.Errorf("run %s: pages %v, want %v", id, g, w)
		}
		for name, body := range want {
			if got[name] != body {
				t.Errorf("run %s: %s differs from its golden; -update rewrites it, and git diff shows how", id, name)
			}
		}
	}
}

func TestRender_Index(t *testing.T) {
	got := render(t, fixture(t), ended)
	names := slices.Sorted(maps.Keys(got))
	if len(names) != 4 || !slices.Contains(names, "index.html") || !slices.Contains(names, pdfSHA+".html") {
		t.Fatalf("pages %v, want index.html and a page per input content, three", names)
	}
	index := got["index.html"]
	for _, path := range []string{"/docs/report.pdf", "/docs/copy of report.pdf"} {
		if r := row(index, path); !strings.Contains(r, `href="`+pdfSHA+`.html"`) {
			t.Errorf("the index's row of %s doesn't link the PDF's page: %q", path, r)
		}
	}
	locked := row(index, "/docs/locked.pdf")
	if locked == "" || strings.Contains(locked, "href") || !strings.Contains(locked, "open /docs/locked.pdf: permission denied") {
		t.Errorf("the index's row of the input with no SHA-256 isn't there, unlinked, with its error: %q", locked)
	}
	if r := row(index, "/docs/report.pdf"); !strings.Contains(r, `<td class="n">4</td><td class="n">0</td><td class="n">6</td><td class="n">3</td>`) {
		t.Errorf("report.pdf's row counts %q, want 4 images, 0 transcripts, 6 chunks, 3 failures", r)
	}
	if !strings.Contains(index, "<dt>End</dt><dd class=\"mono\">2026-11-20T10:00:05.623456789Z <span class=\"muted\">(5.5s)</span></dd>") {
		t.Error("the index lacks the run's end in UTC and how long it took")
	}
}

func TestRender_Vision(t *testing.T) {
	pdf := render(t, fixture(t), ended)[pdfSHA+".html"]
	long := strings.Repeat(longPrefix, 30)
	if !strings.Contains(pdf, "<pre>"+long+"</pre>") {
		t.Error("the PDF's page doesn't show the long vision text whole")
	}
	if n := strings.Count(pdf, `alt="image `+image1SHA+`"`); n != 1 {
		t.Errorf("the image both inputs name shows %d times, want once", n)
	}
	for _, want := range []string{
		`<img src="../../../../docproc/content/` + image1SHA[:2] + "/" + image1SHA + `"`,
		`<a href="../../../../docproc/runs/019a5f3c-1d2e-7a10-8b00-0000000000d1/report/` + pdfSHA + `.html">`,
		`data-outcome="refused">refused</span> <span class="badge">held</span>`,
		`<p class="error">413: the image is too large &lt;b&gt;too big&lt;/b&gt;</p>`,
		`data-outcome="failed">failed</span></div>` + "\n" + `<p class="error">500: the model crashed</p>`,
	} {
		if !strings.Contains(pdf, want) {
			t.Errorf("the PDF's page lacks %q", want)
		}
	}
}

func TestRender_Damaged(t *testing.T) {
	pdf := render(t, fixture(t), ended)[pdfSHA+".html"]
	if !strings.Contains(pdf, "<code>"+blueSHA+`</code> <span class="badge" data-outcome="damaged">damaged</span>`) ||
		!strings.Contains(pdf, "<pre>a green circle</pre>") {
		t.Error("the PDF's page doesn't show the damaged text marked damaged")
	}
}

func TestRender_Missing(t *testing.T) {
	root := fixture(t)
	if err := os.Remove(filepath.Join(root, "texts", blueSHA[:2], blueSHA)); err != nil {
		t.Fatal(err)
	}
	pdf := render(t, root, ended)[pdfSHA+".html"]
	if !strings.Contains(pdf, "texts/ holds no text by this SHA-256.") || strings.Contains(pdf, "a green circle") {
		t.Error("the PDF's page doesn't say texts/ misses the text")
	}
}

func TestRender_Transcript(t *testing.T) {
	got := render(t, fixture(t), ended)
	var wav string
	for name, page := range got {
		if strings.Contains(page, "<code>/docs/talk.wav</code>") && name != "index.html" {
			wav = page
		}
	}
	for _, want := range []string{
		`data-outcome="incomplete">incomplete</span>`,
		`<a href="../../../../docproc/content/`,
		`1</a> <span class="error">not transcribed</span>`,
		`<dt>Not transcribed</dt><dd class="error">1</dd>`,
		"matched 5 of 6 words at word 12<br><span class=\"warning\">unmatched</span> at word 20",
		"<pre>hello there &lt;and&gt; welcome to the talk about inference</pre>",
		`<dt>Incomplete</dt><dd class="warning">window 1 failed</dd>`,
	} {
		if !strings.Contains(wav, want) {
			t.Errorf("the WAV's page lacks %q", want)
		}
	}
}

func TestRender_Sources(t *testing.T) {
	root := fixture(t)
	got := render(t, root, ended)
	pdf := got[pdfSHA+".html"]
	first := strings.Index(pdf, "chunk 0, span 0 to ")
	rejected := strings.Index(pdf, `chunk 1, span 100 to 130 <span class="badge" data-outcome="failed">rejected</span></div>`+"\n"+`<p class="error">413: input too long</p>`)
	last := strings.Index(pdf, "<pre>The end, � no newline after it.</pre>")
	if first < 0 || rejected < first || last < rejected {
		t.Error("the PDF's text output doesn't show its chunks in order, the rejected one with its error")
	}
	if !strings.Contains(pdf, "&lt;/pre&gt;&lt;script&gt;alert(1)&lt;/script&gt; &amp; more.") {
		t.Error("the PDF's page doesn't escape a chunk's text")
	}
	for name, page := range got {
		if strings.Contains(page, "AACAPw==") {
			t.Errorf("%s shows a vector", name)
		}
	}
	var zip string
	for _, page := range got {
		if strings.Contains(page, "<li><code>/docs/archive.zip</code></li>") {
			zip = page
		}
	}
	if !strings.Contains(zip, "<code>/inner.txt</code>, depth 1") || !strings.Contains(zip, "<pre>The inner file&#39;s text.</pre>") {
		t.Error("the archive's page lacks its inner part's source")
	}
}

func TestRender_Unended(t *testing.T) {
	got := render(t, fixture(t), unended)
	if len(got) != 2 {
		t.Errorf("the unended run's pages %v, want its index and c.txt's", slices.Sorted(maps.Keys(got)))
	}
	for name, page := range got {
		if !strings.Contains(page, "not ended") {
			t.Errorf("the unended run's %s doesn't say it has not ended", name)
		}
		if strings.Contains(page, "/docs/e.txt") {
			t.Errorf("the unended run's %s shows its torn last line", name)
		}
	}
	index := got["index.html"]
	if !strings.Contains(row(index, "/docs/c.txt"), "href=") {
		t.Error("c.txt, whose part line is recorded, isn't linked")
	}
	if r := row(index, "/docs/d.zip"); r == "" || strings.Contains(r, "href=") {
		t.Errorf("d.zip, whose part line isn't recorded, has no unlinked row: %q", r)
	}
	if strings.Contains(render(t, fixture(t), ended)["index.html"], "not ended") {
		t.Error("the ended run's index says it has not ended")
	}
}

func TestRender_Deterministic(t *testing.T) {
	root := fixture(t)
	first := render(t, root, ended)
	local := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = local })
	if err := os.MkdirAll(filepath.Join(filepath.Dir(root), "docproc", "content"), 0o755); err != nil {
		t.Fatal(err)
	}
	if again := render(t, root, ended); !maps.Equal(again, first) {
		t.Error("a second render, in UTC with docproc's directory present, differs from the first")
	}
}

func TestRender_Replaces(t *testing.T) {
	root := fixture(t)
	dir := filepath.Join(root, "runs", ended, "report")
	render(t, root, ended)
	if err := os.WriteFile(filepath.Join(dir, "stale.html"), []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(filepath.Dir(dir), ".report-killed"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, ok := render(t, root, ended)["stale.html"]; ok {
		t.Error("a render kept a page it didn't write")
	}
	entries, err := os.ReadDir(filepath.Dir(dir))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if want := []string{"chunks.jsonl", "inferences.jsonl", "manifest.json", "report"}; !slices.Equal(names, want) {
		t.Errorf("the run's directory holds %v, want %v", names, want)
	}
}

func TestRender_NoRun(t *testing.T) {
	root := fixture(t)
	const id = "019a6000-0000-7000-8000-0000000000ff"
	if err := report.Render(t.Context(), root, id); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Render of no run: %v, want fs.ErrNotExist", err)
	}
	if _, err := os.Stat(filepath.Join(root, "runs", id)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Render of no run wrote its directory: %v", err)
	}
}

var attrs = regexp.MustCompile(`(?:href|src)="([^"]*)"`)

func TestRender_Links(t *testing.T) {
	root := fixture(t)
	got := render(t, root, ended)
	dir := filepath.Join(root, "runs", ended, "report")
	docproc := filepath.Join(filepath.Dir(root), "docproc") + string(filepath.Separator)
	for name, page := range got {
		for _, m := range attrs.FindAllStringSubmatch(page, -1) {
			ref := html.UnescapeString(m[1])
			u, err := url.Parse(ref)
			if err != nil || u.IsAbs() || u.Host != "" || strings.HasPrefix(u.Path, "/") {
				t.Errorf("%s: link %q isn't relative", name, ref)
				continue
			}
			target := filepath.Join(dir, filepath.FromSlash(u.Path))
			if strings.HasPrefix(target, docproc) {
				continue
			}
			if info, err := os.Stat(target); err != nil || !info.Mode().IsRegular() {
				t.Errorf("%s: link %q is neither into docproc's directory nor to a page: %v", name, ref, err)
			}
		}
	}
}

func TestRender_SelfContained(t *testing.T) {
	for name, page := range render(t, fixture(t), ended) {
		for _, bad := range []string{"<script", "<link", "@import", "url(", `="http`, `="file:`} {
			if strings.Contains(strings.ToLower(page), bad) {
				t.Errorf("%s holds %q", name, bad)
			}
		}
		if !strings.Contains(page, "prefers-color-scheme: dark") {
			t.Errorf("%s doesn't follow the reader's scheme", name)
		}
	}
}
