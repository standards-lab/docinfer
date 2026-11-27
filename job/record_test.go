package job_test

import (
	"context"
	"iter"
	"strings"
	"testing"

	"github.com/standards-lab/docinfer"
	"github.com/standards-lab/docinfer/job"
	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/route"
)

const (
	fileText  = "The plan for the quarter."
	officeRec = `{"core":{"title":"Quarterly Report","creator":"Ann Lee","created":"2026-03-15T09:30:00+05:30"},` +
		`"extended":{"Application":"Microsoft Office Word","Company":"Acme"}}`
	officeText = "title: Quarterly Report\ncreator: Ann Lee\ncreated: 2026-03-15T04:00:00Z\nCompany: Acme"
)

type officeStage struct{}

func (officeStage) Identity() dispatch.Identity {
	return dispatch.Identity{Name: "office", Version: "1", Config: "office"}
}

func (officeStage) Prepare(context.Context, dispatch.Part, string) iter.Seq2[dispatch.Item, error] {
	return func(yield func(dispatch.Item, error) bool) {
		if !yield(dispatch.OutputItem(dispatch.Output{
			Kind: dispatch.TextOutput, Name: "text", MediaType: "text/plain", Value: []byte(fileText),
		}), nil) {
			return
		}
		yield(dispatch.OutputItem(dispatch.Output{
			Kind: dispatch.RecordOutput, Name: "metadata", MediaType: dispatch.MediaOfficeMetadata, Value: []byte(officeRec),
		}), nil)
	}
}

func TestRunRendersMetadataRecord(t *testing.T) {
	d, err := dispatch.New(dispatch.Config{
		Stages:  dispatch.Stages{route.Text: officeStage{}},
		Index:   dispatch.NewMemoryIndex(0),
		Content: dispatch.NewMemoryContent(),
		Dir:     t.TempDir(),
		Puts:    2,
	})
	if err != nil {
		t.Fatal(err)
	}
	runner, err := job.New(job.Config{Dispatcher: d, Pipeline: newPipeline(t, serve(t)), CheckpointEvery: 1})
	if err != nil {
		t.Fatal(err)
	}
	r := collect(runner.Run(t.Context(), input(t, "plan.txt", "plan"), docinfer.Options{}), nil)
	done := r.done(t)

	outputs := map[string]dispatch.Output{}
	var input dispatch.Part
	for _, ev := range r.events() {
		switch {
		case ev.Output != nil:
			outputs[ev.Output.Name] = *ev.Output
		case own(ev):
			input = ev.Part
		}
	}
	if input.Depth != 0 || len(outputs) != 2 {
		t.Fatalf("input %+v and outputs %v, want a depth-0 part with its text and its record", input, outputs)
	}

	type got struct{ kind, output, sha, text string }
	var chunks []got
	for _, u := range r.updates {
		if u.Batch == nil {
			continue
		}
		for _, c := range u.Batch.Chunks {
			s := u.Batch.Sources[c.Source]
			g := got{kind: s.Kind, sha: s.SHA256, text: c.Text}
			if s.Output != nil {
				g.output = s.Output.SHA256
			}
			chunks = append(chunks, g)
		}
	}
	want := map[got]bool{
		{kind: "output", output: outputs["text"].SHA256, text: fileText}:                                                  true,
		{kind: "metadata", output: outputs["metadata"].SHA256, sha: sum(officeText + "\n"), text: officeText}:             true,
		{kind: "metadata", sha: sum(docinfer.MetadataText(input)), text: strings.TrimSpace(docinfer.MetadataText(input))}: true,
	}
	for _, c := range chunks {
		if !want[c] {
			t.Errorf("chunk %+v, want one of %v", c, want)
		}
	}
	if len(chunks) != len(want) {
		t.Errorf("%d chunks, want %d: the file's text, the record's text, and the input's metadata text", len(chunks), len(want))
	}
	if c := done.Counts; c.Texts != 1 || c.Metadata != 2 {
		t.Errorf("Done counts %+v, want the file's text and two metadata texts", c)
	}
}
