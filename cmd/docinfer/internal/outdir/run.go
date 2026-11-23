package outdir

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"time"
	"uuid"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/rundir/schema"

	"github.com/standards-lab/docinfer"
)

// Run is one run over docinfer's directory, begun by [Dir.Begin]: its manifest and its records,
// under runs/<run>/. It is not safe for concurrent use.
type Run struct {
	dir        string
	manifest   Manifest
	chunks     *os.File
	inferences *os.File
}

// Begin begins a run over d from m, on a Dir from [Create]. The run's id is m's Run, which must be
// a version 7 UUID in its lowercase form naming no run d holds, or a new one when m's is empty;
// its start is m's Start, or now when that is zero. Begin sets the manifest's schema, clears any
// end and error, makes the run's directory, writes its manifest, and opens its chunk record and
// its inferences record.
func (d *Dir) Begin(m Manifest) (*Run, error) {
	if !d.write {
		return nil, fmt.Errorf("outdir: begin: %s is opened to read", d.root)
	}
	if m.Run == "" {
		m.Run = uuid.NewV7().String()
	} else if !validID(m.Run) {
		return nil, fmt.Errorf("outdir: run id %q isn't a version 7 UUID in its lowercase form: %w", m.Run, dispatch.ErrRequest)
	}
	if m.Start.IsZero() {
		m.Start = schema.TimeOf(time.Now())
	}
	m.Schema, m.End, m.Error = Version, schema.Time{}, ""
	dir := filepath.Join(d.root, runsDir, m.Run)
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return nil, fmt.Errorf("outdir: %w", err)
	}
	if err := os.Mkdir(dir, 0o755); err != nil {
		return nil, fmt.Errorf("outdir: begin: %w", err)
	}
	r := &Run{dir: dir, manifest: m}
	if err := r.writeManifest(); err != nil {
		return nil, err
	}
	var err error
	if r.chunks, err = openRecord(dir, chunksFile); err != nil {
		return nil, err
	}
	if r.inferences, err = openRecord(dir, inferencesFile); err != nil {
		return nil, errors.Join(err, r.chunks.Close())
	}
	return r, nil
}

func openRecord(dir, name string) (*os.File, error) {
	f, err := os.OpenFile(filepath.Join(dir, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("outdir: %w", err)
	}
	return f, nil
}

// ID returns the run's id.
func (r *Run) ID() string { return r.manifest.Run }

// Inference appends i to the run's inferences record as one line, written through to the file in
// one write, with no fsync. Its Text isn't in the line; the caller puts it in [Dir.Texts].
func (r *Run) Inference(i Inference) error {
	var buf bytes.Buffer
	if err := encodeLine(&buf, i); err != nil {
		return fmt.Errorf("outdir: inferences %s: %w", r.manifest.Run, err)
	}
	if _, err := r.inferences.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("outdir: inferences %s: %w", r.manifest.Run, err)
	}
	return nil
}

// Batch appends b's chunks to the run's chunk record, a [Line] each, in order, written through
// to the file in one write, with no fsync. inputs holds the input path of each of b's Parts, by
// the same index; Batch refuses inputs of another length.
func (r *Run) Batch(b docinfer.Batch, inputs []string) error {
	if len(inputs) != len(b.Parts) {
		return fmt.Errorf("outdir: chunks %s: %d inputs for %d parts: %w", r.manifest.Run, len(inputs), len(b.Parts), dispatch.ErrRequest)
	}
	var buf bytes.Buffer
	for _, l := range lines(b, inputs) {
		if err := encodeLine(&buf, l); err != nil {
			return fmt.Errorf("outdir: chunks %s: %w", r.manifest.Run, err)
		}
	}
	if _, err := r.chunks.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("outdir: chunks %s: %w", r.manifest.Run, err)
	}
	return nil
}

// End syncs and closes the run's records, then writes m as its manifest, its end now: its schema,
// run, and start stay the ones [Dir.Begin] wrote. Call it once.
func (r *Run) End(m Manifest) error {
	var errs []error
	for _, f := range []*os.File{r.chunks, r.inferences} {
		if err := errors.Join(f.Sync(), f.Close()); err != nil {
			errs = append(errs, fmt.Errorf("outdir: record %s: %w", r.manifest.Run, err))
		}
	}
	m.Schema, m.Run, m.Start = r.manifest.Schema, r.manifest.Run, r.manifest.Start
	m.End = schema.TimeOf(time.Now())
	r.manifest = m
	return errors.Join(append(errs, r.writeManifest())...)
}

func (r *Run) writeManifest() error {
	err := writeFile(filepath.Join(r.dir, manifestFile), func(w io.Writer) error {
		return encodeIndented(w, r.manifest)
	})
	if err != nil {
		return fmt.Errorf("outdir: manifest %s: %w", path.Join(runsDir, r.manifest.Run), err)
	}
	return nil
}

func encodeLine(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}
