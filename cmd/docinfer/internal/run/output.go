package run

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/standards-lab/docproc/dispatch"

	"github.com/standards-lab/docinfer"

	"github.com/standards-lab/docinfer/cmd/docinfer/internal/outdir"
)

// Adapted from spike-ai-pipeline (6a631f1): the table's error is joined with its flush's.
func writeModels(w io.Writer, check modelCheck) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	var err error
	row := func(cells ...string) {
		if err == nil {
			_, err = io.WriteString(tw, strings.Join(cells, "\t")+"\n")
		}
	}
	row("CAPABILITY", "ENDPOINT", "MODEL", "STATE")
	for _, r := range check.rows {
		row(string(r.capability), r.endpoint, r.model, r.state)
	}
	if err := errors.Join(err, tw.Flush()); err != nil {
		return err
	}
	for _, warning := range check.warnings {
		if _, err := fmt.Fprintf(w, "warning: %s\n", warning); err != nil {
			return err
		}
	}
	return nil
}

type inputTally struct {
	images, windows, chunks, failures int
	pending                           int
	closed                            bool
}

type progress struct {
	w        io.Writer
	f        *feed
	echoed   int
	tallies  map[string]*inputTally
	parts    map[string]string
	contents map[string]string
}

func newProgress(w io.Writer, f *feed) *progress {
	return &progress{w: w, f: f, tallies: map[string]*inputTally{}, parts: map[string]string{}, contents: map[string]string{}}
}

func (p *progress) tally(path string) *inputTally {
	t := p.tallies[path]
	if t == nil {
		t = &inputTally{}
		p.tallies[path] = t
	}
	return t
}

func (p *progress) echo(in docinfer.Input) {
	fd := p.f.at(p.echoed)
	p.echoed++
	t := p.tally(fd.path)
	t.pending++
	t.closed = t.closed || fd.last
	if part := in.Event.Part; part.ID != "" {
		p.parts[partKey(part)] = fd.path
	}
	if o := in.Event.Output; o != nil {
		p.contents[o.SHA256] = fd.path
		switch o.Kind {
		case dispatch.ImageOutput:
			t.images++
		case dispatch.AudioOutput:
			t.windows++
		}
	}
}

func (p *progress) inference(inf docinfer.Inference) {
	path, ok := "", false
	for _, sha := range inf.ContentSHA256 {
		if path, ok = p.contents[sha]; ok {
			break
		}
	}
	if !ok {
		return
	}
	t := p.tally(path)
	switch {
	case inf.Capability == docinfer.CapabilityTranscription:
		t.failures += len(inf.Failed)
	case inf.Outcome == docinfer.OutcomeRefused, inf.Outcome == docinfer.OutcomeFailed:
		t.failures++
	}
}

func (p *progress) batch(b docinfer.Batch) {
	for _, c := range b.Chunks {
		path, ok := p.parts[partKey(b.Parts[b.Sources[c.Source].Part])]
		if !ok {
			continue
		}
		t := p.tally(path)
		t.chunks++
		if c.Error != "" {
			t.failures++
		}
	}
}

func partKey(p dispatch.Part) string { return p.ID + "\x00" + p.SHA256 }

func (p *progress) settle(pos int) error {
	path := p.f.at(pos).path
	t := p.tally(path)
	t.pending--
	if !t.closed || t.pending > 0 {
		return nil
	}
	delete(p.tallies, path)
	_, err := fmt.Fprintf(p.w, "%s  images %d  windows %d  chunks %d  failures %d\n",
		path, t.images, t.windows, t.chunks, t.failures)
	return err
}

func writeBegin(w io.Writer, id, docproc string) error {
	_, err := fmt.Fprintf(w, "docinfer run %s: docproc run %s\n", id, docproc)
	return err
}

func writeEnd(w io.Writer, id string, m outdir.Manifest) error {
	c := m.Counts
	var lines []string
	failures := c.Rejected
	if v := c.Vision; v != nil {
		failures += v.Refused + v.Failed
		lines = append(lines, fmt.Sprintf("vision: images %d: described %d, incomplete %d (empty %d), refused %d, failed %d, held %d; requests %d",
			c.Images, v.Described, v.Incomplete, v.Empty, v.Refused, v.Failed, v.Held, v.Requests))
	} else {
		lines = append(lines, fmt.Sprintf("vision: skipped, %s; images %d", notConfigured, c.Images))
	}
	if t := c.Transcription; t != nil {
		failures += t.Incomplete + t.Refused + t.Failed
		lines = append(lines, fmt.Sprintf("transcription: windows %d: transcribed %d, incomplete %d, refused %d, failed %d, held %d; requests %d; "+
			"transcripts %d, seams matched %d, unmatched %d",
			c.Windows, t.Transcribed, t.Incomplete, t.Refused, t.Failed, t.Held, t.Requests, t.Transcripts, t.SeamsMatched, t.SeamsUnmatched))
	} else {
		lines = append(lines, fmt.Sprintf("transcription: skipped, %s; windows %d", notConfigured, c.Windows))
	}
	lines = append(lines,
		fmt.Sprintf("embeddings: texts %d, metadata %d: embedded %d, rejected %d, held %d; requests %d",
			c.Texts, c.Metadata, c.Embedded, c.Rejected, c.Held, c.Requests),
		fmt.Sprintf("docinfer run %s: inputs %d, outputs %d, chunks %d, failures %d",
			id, len(m.Inputs), m.Outputs, c.Embedded+c.Rejected, failures))
	_, err := io.WriteString(w, strings.Join(lines, "\n")+"\n")
	return err
}
