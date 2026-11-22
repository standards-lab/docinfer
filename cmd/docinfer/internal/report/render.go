package report

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/standards-lab/docproc/rundir/schema"

	"github.com/standards-lab/docinfer/cmd/docinfer/internal/outdir"
)

// IndexPage is the file name of a report's index page; each content's page is its SHA-256 and
// ".html".
const IndexPage = "index.html"

const tempPattern = ".report-*"

//go:embed pages.html.tmpl
var sources embed.FS

var pages = template.Must(template.New("").Funcs(funcs).ParseFS(sources, "pages.html.tmpl"))

var funcs = template.FuncMap{
	"time": func(t schema.Time) string { return t.String() },
	"took": func(span schema.Span) string {
		return span.End.Time().Sub(span.Start.Time()).Round(time.Millisecond).String()
	},
}

// Render renders the report of the run id in docinfer's directory dir into the run's report
// directory, outdir.Dir.ReportDir(id) under dir: index.html, the index of every input, and one
// page per input content, <sha256>.html. It replaces earlier pages whole: it writes every page
// into a new directory beside the report directory, then moves the earlier report directory
// aside, moves the new one into its place, and removes the earlier one, first removing the
// working directories a killed render left beside it.
//
// A render reads the run's manifest, its records, and the texts in dir, and no clock and nothing
// of docproc's run directory: the same directory renders the same bytes, in any local zone. An
// unended run renders what its records hold, marked not ended. A dir that isn't docinfer's
// directory, or is of another schema, is refused as outdir.Open refuses it, and an id that isn't a
// run of dir with an error matching fs.ErrNotExist.
func Render(ctx context.Context, dir, id string) error {
	d, err := outdir.Open(dir)
	if err != nil {
		return err
	}
	rel, err := d.ReportDir(id)
	if err != nil {
		return err
	}
	v, err := build(ctx, d, id)
	if err != nil {
		return err
	}
	files, err := render(ctx, v)
	if err != nil {
		return err
	}
	return replace(filepath.Join(dir, filepath.FromSlash(rel)), files)
}

type page struct {
	name string
	body []byte
}

type contentView struct {
	Run     *model
	Content *content
}

// Adapted from docproc (6f955e3): over docinfer's model.
func render(ctx context.Context, v *model) ([]page, error) {
	var out []page
	exec := func(name, tmpl string, data any) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		var buf bytes.Buffer
		if err := pages.ExecuteTemplate(&buf, tmpl, data); err != nil {
			return fmt.Errorf("report: %s: %w", name, err)
		}
		out = append(out, page{name: name, body: buf.Bytes()})
		return nil
	}
	if err := exec(IndexPage, "index", v); err != nil {
		return nil, err
	}
	for _, c := range v.Contents {
		if err := exec(c.SHA256+".html", "content", contentView{Run: v, Content: c}); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// Adapted from docproc (6f955e3): unchanged.
func replace(dst string, files []page) (err error) {
	parent := filepath.Dir(dst)
	stale, err := filepath.Glob(filepath.Join(parent, tempPattern))
	if err != nil {
		return fmt.Errorf("report: %w", err)
	}
	for _, s := range stale {
		if err := os.RemoveAll(s); err != nil {
			return fmt.Errorf("report: %w", err)
		}
	}
	tmp, err := os.MkdirTemp(parent, tempPattern)
	if err != nil {
		return fmt.Errorf("report: %w", err)
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(tmp)
		}
	}()
	if err := os.Chmod(tmp, 0o755); err != nil {
		return fmt.Errorf("report: %w", err)
	}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(tmp, f.name), f.body, 0o644); err != nil {
			return fmt.Errorf("report: %w", err)
		}
	}
	old, err := os.MkdirTemp(parent, tempPattern)
	if err != nil {
		return fmt.Errorf("report: %w", err)
	}
	if err := os.Rename(dst, filepath.Join(old, filepath.Base(dst))); err != nil && !errors.Is(err, fs.ErrNotExist) {
		_ = os.Remove(old)
		return fmt.Errorf("report: %w", err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		return fmt.Errorf("report: %w", err)
	}
	if err := os.RemoveAll(old); err != nil {
		return fmt.Errorf("report: %w", err)
	}
	return nil
}
