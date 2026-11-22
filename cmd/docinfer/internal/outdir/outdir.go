package outdir

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"iter"
	"os"
	"path/filepath"
	"slices"
	"uuid"

	"github.com/standards-lab/docproc/dispatch"

	"github.com/standards-lab/docinfer"
)

// Adapted from docproc (ddb1c07): rundir's open, create, schema check, run ids, runs, latest, manifest and record readers, read-only content, and whole-file write, over docinfer's marker, records, and texts.

// Version is the directory's schema: its layout, its marker, its manifest, and its records.
const Version = "2"

const (
	markerFile     = "outdir.json"
	resultsDir     = "results"
	textsDir       = "texts"
	runsDir        = "runs"
	manifestFile   = "manifest.json"
	chunksFile     = "chunks.jsonl"
	inferencesFile = "inferences.jsonl"
	reportDir      = "report"
)

// Dir is docinfer's directory, opened to read by [Open] or to write by [Create]. Its reader's
// methods are safe for concurrent use; its writer's are not, and one process writes a run.
type Dir struct {
	root  string
	write bool
	cache *docinfer.FileCache
	texts dispatch.Content
}

type marker struct {
	Schema string `json:"schema"`
}

// Open opens the directory at root to read it, creating nothing: it fails with an error matching
// fs.ErrNotExist when root holds no outdir.json, and refuses another schema, naming both. Its
// [Dir.Texts] reads alone, refusing a put, and its [Dir.Begin] refuses.
func Open(root string) (*Dir, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("outdir: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("outdir: %s is no directory: %w", root, fs.ErrNotExist)
	}
	found, err := checkSchema(root)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("outdir: %s is no docinfer directory, holding no %s: %w", root, markerFile, fs.ErrNotExist)
	}
	d := &Dir{root: root, texts: readTexts{}}
	if isDir(filepath.Join(root, resultsDir)) {
		if d.cache, err = docinfer.NewFileCache(filepath.Join(root, resultsDir)); err != nil {
			return nil, fmt.Errorf("outdir: %w", err)
		}
	}
	if isDir(filepath.Join(root, textsDir)) {
		c, err := dispatch.NewFileContent(filepath.Join(root, textsDir), 0)
		if err != nil {
			return nil, fmt.Errorf("outdir: %w", err)
		}
		d.texts = readTexts{c}
	}
	return d, nil
}

// Create opens the directory at root to write it, making the directory, its outdir.json,
// results/, and texts/ when they are absent; over an existing directory it refuses another
// schema, or results/, texts/, or runs/ with no outdir.json, as [Open] does. Its texts never
// rewrite a text they hold.
func Create(root string) (*Dir, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("outdir: %w", err)
	}
	found, err := checkSchema(root)
	if err != nil {
		return nil, err
	}
	if !found {
		err := writeFile(filepath.Join(root, markerFile), func(w io.Writer) error {
			return encodeIndented(w, marker{Schema: Version})
		})
		if err != nil {
			return nil, fmt.Errorf("outdir: %w", err)
		}
	}
	cache, err := docinfer.NewFileCache(filepath.Join(root, resultsDir))
	if err != nil {
		return nil, fmt.Errorf("outdir: %w", err)
	}
	texts, err := dispatch.NewFileContent(filepath.Join(root, textsDir), 0)
	if err != nil {
		return nil, fmt.Errorf("outdir: %w", err)
	}
	return &Dir{root: root, write: true, cache: cache, texts: texts}, nil
}

func checkSchema(root string) (bool, error) {
	path := filepath.Join(root, markerFile)
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		for _, dir := range []string{resultsDir, textsDir, runsDir} {
			if _, err := os.Stat(filepath.Join(root, dir)); err == nil {
				return false, fmt.Errorf("outdir: %s holds %s/ but no %s, as schema \"1\" wrote it: schema \"1\", want %q",
					root, dir, markerFile, Version)
			}
		}
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("outdir: %w", err)
	}
	var m marker
	if err := json.Unmarshal(b, &m); err != nil {
		return false, fmt.Errorf("outdir: %s: %w", path, err)
	}
	if m.Schema != Version {
		return false, fmt.Errorf("outdir: %s is schema %q, want %q", root, m.Schema, Version)
	}
	return true, nil
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func validID(id string) bool {
	u, err := uuid.Parse(id)
	return err == nil && u.String() == id && u[6]>>4 == 7
}

func runPath(id, name string) string { return filepath.Join(runsDir, id, name) }

func (d *Dir) checkRun(id string) error {
	if validID(id) {
		info, err := os.Stat(filepath.Join(d.root, runPath(id, manifestFile)))
		if err == nil && info.Mode().IsRegular() {
			return nil
		}
	}
	return fmt.Errorf("outdir: %q is no run of %s: %w", id, d.root, fs.ErrNotExist)
}

// Runs returns the ids of d's runs, in the order they began, which is the ids' own. A directory
// under runs/ that isn't a run, its name no valid id or its manifest absent, is left out.
func (d *Dir) Runs() ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(d.root, runsDir))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("outdir: %w", err)
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() && d.checkRun(e.Name()) == nil {
			ids = append(ids, e.Name())
		}
	}
	slices.Sort(ids)
	return ids, nil
}

// Latest returns the id of d's latest run that ended, its manifest holding its end; a run still
// running, or killed, is passed over. A directory with no ended run is an error matching
// fs.ErrNotExist.
func (d *Dir) Latest() (string, error) {
	ids, err := d.Runs()
	if err != nil {
		return "", err
	}
	for _, id := range slices.Backward(ids) {
		m, err := d.Manifest(id)
		if err != nil {
			return "", err
		}
		if !m.End.IsZero() {
			return id, nil
		}
	}
	return "", fmt.Errorf("outdir: %s holds no run that ended: %w", d.root, fs.ErrNotExist)
}

// Manifest reads the manifest of d's run id, refusing an id that isn't a run of d.
func (d *Dir) Manifest(id string) (Manifest, error) {
	if err := d.checkRun(id); err != nil {
		return Manifest{}, err
	}
	path := filepath.Join(d.root, runPath(id, manifestFile))
	b, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("outdir: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return Manifest{}, fmt.Errorf("outdir: %s: %w", path, err)
	}
	return m, nil
}

type skipped struct{}

func (*skipped) UnmarshalJSON([]byte) error { return nil }

type lineWithoutVector struct {
	Line
	Vector skipped `json:"vector"`
}

// Lines yields the lines of d's run id's chunk record, in the order they were written, each
// without its Vector, which it never decodes, refusing an id that isn't a run of d with an error
// yielded alone. It ends on the first line that doesn't decode, yielding its error, which names
// the line. For an unended run it drops a last line with no newline.
func (d *Dir) Lines(id string) iter.Seq2[Line, error] {
	return func(yield func(Line, error) bool) {
		records(d, id, chunksFile, func(l lineWithoutVector, err error) bool { return yield(l.Line, err) })
	}
}

// Inferences yields the lines of d's run id's inferences record, in the order they were written,
// as [Dir.Lines] yields the chunk record's. A line's Text is empty: the text is in [Dir.Texts],
// under its TextSHA256.
func (d *Dir) Inferences(id string) iter.Seq2[Inference, error] {
	return func(yield func(Inference, error) bool) {
		records(d, id, inferencesFile, yield)
	}
}

func records[T any](d *Dir, id, name string, yield func(T, error) bool) {
	var zero T
	m, err := d.Manifest(id)
	if err != nil {
		yield(zero, err)
		return
	}
	ended := !m.End.IsZero()
	path := filepath.Join(d.root, runPath(id, name))
	f, err := os.Open(path)
	if !ended && errors.Is(err, fs.ErrNotExist) {
		return
	}
	if err != nil {
		yield(zero, fmt.Errorf("outdir: %w", err))
		return
	}
	defer func() { _ = f.Close() }()
	r := bufio.NewReader(f)
	for n := 1; ; n++ {
		line, err := r.ReadBytes('\n')
		last := errors.Is(err, io.EOF)
		switch {
		case err != nil && !last:
			yield(zero, fmt.Errorf("outdir: %w", err))
			return
		case last && (len(line) == 0 || !ended):
			return
		}
		var v T
		if err := json.Unmarshal(line, &v); err != nil {
			yield(zero, fmt.Errorf("outdir: %s: line %d: %w", path, n, err))
			return
		}
		if !yield(v, nil) || last {
			return
		}
	}
}

// Cache returns the held results, a docinfer.FileCache over results/; nil on a Dir from [Open]
// whose results/ is absent.
func (d *Dir) Cache() *docinfer.FileCache { return d.cache }

// Texts returns the inferences' texts, a dispatch.FileContent over texts/, each text under its
// SHA-256 at dispatch.ContentFile's layout, with no extension. On a Dir from [Open] it reads
// alone, refusing a put, and misses every text when texts/ is absent.
func (d *Dir) Texts() dispatch.Content { return d.texts }

// ReportDir returns the report directory of d's run id, runs/<run>/report, relative to the
// directory, with slashes, refusing an id that isn't a run of d with an error matching
// fs.ErrNotExist. It names the directory alone: it neither makes it nor looks for it.
func (d *Dir) ReportDir(id string) (string, error) {
	if err := d.checkRun(id); err != nil {
		return "", err
	}
	return runsDir + "/" + id + "/" + reportDir, nil
}

type readTexts struct {
	c *dispatch.FileContent
}

func (r readTexts) Put(_ context.Context, sha256 string, _ io.Reader) error {
	return fmt.Errorf("outdir: put text %s: the directory is opened to read", sha256)
}

func (r readTexts) Open(ctx context.Context, sha256 string) (io.ReadCloser, error) {
	if r.c != nil {
		return r.c.Open(ctx, sha256)
	}
	if dispatch.ContentFile(sha256) == "" {
		return nil, fmt.Errorf("outdir: text %q isn't a SHA-256 as lowercase hex: %w", sha256, dispatch.ErrRequest)
	}
	return nil, fmt.Errorf("outdir: text %s: %w", sha256, fs.ErrNotExist)
}

func encodeIndented(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

func writeFile(dst string, write func(io.Writer) error) (err error) {
	dir := filepath.Dir(dst)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = os.Remove(tmp.Name())
		}
	}()
	if err := write(tmp); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write %s: %w", dst, err)
	}
	if err := errors.Join(tmp.Sync(), tmp.Close()); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), dst)
}
