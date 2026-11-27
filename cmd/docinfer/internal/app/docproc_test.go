package app_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/rundir"
	"github.com/standards-lab/docproc/rundir/schema"
)

type docprocRun struct {
	t   *testing.T
	dir *rundir.Dir
	run *rundir.Run
}

func newDocprocRun(t *testing.T, root string) *docprocRun {
	t.Helper()
	return newDocprocRunID(t, root, "")
}

func newDocprocRunID(t *testing.T, root, id string) *docprocRun {
	t.Helper()
	d, err := rundir.Create(root)
	if err != nil {
		t.Fatal(err)
	}
	r, err := d.Begin(schema.Manifest{Run: id})
	if err != nil {
		t.Fatal(err)
	}
	return &docprocRun{t: t, dir: d, run: r}
}

func sha(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func inputPart(name string, content []byte) dispatch.Part {
	return dispatch.Part{ID: "/", Name: name, Size: int64(len(content)), SHA256: sha(content)}
}

func (d *docprocRun) write(e schema.Event) {
	d.t.Helper()
	if err := d.run.Write(e); err != nil {
		d.t.Fatal(err)
	}
}

func (d *docprocRun) output(path string, part dispatch.Part, kind dispatch.OutputKind, mediaType string, content []byte) string {
	d.t.Helper()
	s := sha(content)
	if err := d.dir.Content().Put(context.Background(), s, bytes.NewReader(content)); err != nil {
		d.t.Fatal(err)
	}
	d.write(schema.Event{Kind: schema.OutputEvent, Input: path,
		Part:   part,
		Stage:  dispatch.Identity{Name: "stage"},
		Output: &dispatch.Output{Kind: kind, MediaType: mediaType, SHA256: s}})
	return s
}

func (d *docprocRun) pageOutput(path string, part dispatch.Part, kind dispatch.OutputKind, mediaType, page string, content []byte) {
	d.t.Helper()
	s := sha(content)
	if err := d.dir.Content().Put(context.Background(), s, bytes.NewReader(content)); err != nil {
		d.t.Fatal(err)
	}
	d.write(schema.Event{Kind: schema.OutputEvent, Input: path,
		Part:   part,
		Stage:  dispatch.Identity{Name: "document"},
		Output: &dispatch.Output{Kind: kind, MediaType: mediaType, SHA256: s, Meta: map[string]string{dispatch.MetaPage: page}}})
}

func (d *docprocRun) part(path string, part dispatch.Part, outputs int) {
	d.t.Helper()
	d.write(schema.Event{Kind: schema.PartEvent, Input: path,
		Part:    part,
		Stage:   dispatch.Identity{Name: "stage"},
		Outcome: dispatch.Prepared,
		Outputs: outputs})
}

func (d *docprocRun) failed(path, message string) {
	d.t.Helper()
	d.write(schema.Event{Kind: schema.ErrorEvent, Input: path, Error: message})
}

func (d *docprocRun) end() string {
	d.t.Helper()
	if err := d.run.End(nil); err != nil {
		d.t.Fatal(err)
	}
	return d.run.ID()
}
