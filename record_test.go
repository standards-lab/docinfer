package docinfer_test

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/standards-lab/docinfer"
	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/go-ai/aitest"
)

var (
	docxPart  = dispatch.Part{ID: "/", Name: "plan.docx", Size: 4096, SHA256: strings.Repeat("c", 64)}
	pdfPart   = dispatch.Part{ID: "/", Name: "plan.pdf", Size: 8192, SHA256: strings.Repeat("d", 64)}
	pdfStage  = dispatch.Identity{Name: "document", Version: "1", Config: "c1"}
	officeRec = `{"core":{"title":"Quarterly Report","creator":"Ann Lee","created":"2026-03-15T09:30:00+05:30",` +
		`"lastModifiedBy":"Bo Chan"},` +
		`"extended":{"Application":"Microsoft Office Word","Pages":3,"Company":"Acme","Manager":"Cy Diaz"},` +
		`"custom":{"Project":"bunrei","Reviewed":true,"Ratio":0.25},"converted_from":"doc"}`
	officeText = "title: Quarterly Report\n" +
		"creator: Ann Lee\n" +
		"created: 2026-03-15T04:00:00Z\n" +
		"lastModifiedBy: Bo Chan\n" +
		"Project: bunrei\n" +
		"Reviewed: true\n" +
		"Ratio: 0.25\n" +
		"Company: Acme\n" +
		"Manager: Cy Diaz\n" +
		"converted from: doc\n"
	pdfRec = `{"version":"1.7","info":{"title":"Annual Plan","author":"Ann Lee; Bo Chan","subject":"Budget",` +
		`"keywords":"plan, budget","creator":"Writer","producer":"LibreOffice 7.6",` +
		`"creation_date":"2026-03-15T04:00:00Z","mod_date":"2026-03-16T05:30:00Z","custom":{"Department":"Finance"}},` +
		`"xmp":{"title":"Annual Plan","creators":["Ann Lee","Bo Chan"],"description":"The year's plan",` +
		`"subjects":["plan","budget","2026"],"languages":["en"],"create_date":"2026-03-15T04:00:00Z",` +
		`"creator_tool":"Writer","producer":"LibreOffice 7.6","document_id":"uuid:1","packet":"<x:xmpmeta>packet</x:xmpmeta>"}}`
	pdfText = "title: Annual Plan\n" +
		"author: Ann Lee; Bo Chan\n" +
		"subject: Budget\n" +
		"keywords: plan, budget\n" +
		"creation date: 2026-03-15T04:00:00Z\n" +
		"modification date: 2026-03-16T05:30:00Z\n" +
		"Department: Finance\n" +
		"description: The year's plan\n" +
		"subjects: plan, budget, 2026\n"
)

func recordInput(t *testing.T, content *dispatch.MemoryContent, part dispatch.Part, stage dispatch.Identity, mediaType, record string) docinfer.Input {
	t.Helper()
	in := outputInput(t, content, part, dispatch.RecordOutput, record)
	in.Event.Stage = stage
	in.Event.Output.Name = "metadata"
	in.Event.Output.MediaType = mediaType
	return in
}

func TestRunRendersMetadataRecords(t *testing.T) {
	e := aitest.Serve(t, aitest.Routes{Embeddings: embedAll})
	cfg := extractConfig(t, e, nil)
	p := newPipeline(t, cfg)
	content := dispatch.NewMemoryContent()
	inputs := []docinfer.Input{
		recordInput(t, content, docxPart, textStage, dispatch.MediaOfficeMetadata, officeRec),
		recordInput(t, content, pdfPart, pdfStage, dispatch.MediaPDFMetadata, pdfRec),
		recordInput(t, content, childPart, textStage, dispatch.MediaForm, `{"fields":[]}`),
	}

	o := collect(t, p.Run(t.Context(), content, seq(inputs...), extracting(false)))
	if o.err != nil {
		t.Fatalf("Run: %v", o.err)
	}
	got := o.chunks()
	var all int
	for _, w := range []struct {
		text  string
		in    docinfer.Input
		stage dispatch.Identity
	}{{officeText, inputs[0], textStage}, {pdfText, inputs[1], pdfStage}} {
		pieces, err := cfg.Chunker.Split(t.Context(), w.text)
		if err != nil {
			t.Fatal(err)
		}
		var texts []string
		for _, g := range got {
			if g.source.Output == nil || g.source.Output.SHA256 != w.in.Event.Output.SHA256 {
				continue
			}
			texts = append(texts, g.chunk.Text)
			if g.source.Kind != "metadata" || g.source.SHA256 != sha(w.text) || !reflect.DeepEqual(*g.source.Output, *w.in.Event.Output) ||
				g.source.Stage == nil || *g.source.Stage != w.stage || g.part.ID != w.in.Event.Part.ID || g.part.Name != w.in.Event.Part.Name {
				t.Errorf("chunk %q: source %+v of part %+v, want kind metadata, the rendered text's SHA-256, the record output, and its part",
					g.chunk.Text, g.source, g.part)
			}
		}
		var want []string
		for _, c := range pieces {
			want = append(want, c.Text)
		}
		slices.Sort(texts)
		slices.Sort(want)
		if !slices.Equal(texts, want) {
			t.Errorf("record %s: chunks %q, want those of %q", w.in.Event.Output.MediaType, texts, w.text)
		}
		all += len(want)
	}
	if len(got) != all {
		t.Errorf("Run yielded %d chunks, want the records' %d", len(got), all)
	}
	for _, b := range o.batches {
		checkBatch(t, b)
	}
	if n := chatRequests(e); n != 0 {
		t.Errorf("%d extraction requests, want none for metadata records", n)
	}
	if o.counts == nil || o.counts.Metadata != 2 || o.counts.Texts != 0 || o.counts.Embedded != all ||
		o.counts.Extraction == nil || *o.counts.Extraction != (docinfer.ExtractionCounts{}) {
		t.Errorf("counts %+v, want two metadata texts, their chunks embedded, and nothing extracted", o.counts)
	}
	if len(o.settled) != len(inputs) {
		t.Errorf("settled %v, want every input", o.settled)
	}
}

func TestRunMetadataRecordWithNoLines(t *testing.T) {
	e := aitest.Serve(t, aitest.Routes{Embeddings: embedAll})
	content := dispatch.NewMemoryContent()
	inputs := []docinfer.Input{
		recordInput(t, content, docxPart, textStage, dispatch.MediaOfficeMetadata,
			`{"extended":{"Application":"Microsoft Office Word","Pages":3},"custom":{}}`),
		recordInput(t, content, pdfPart, pdfStage, dispatch.MediaPDFMetadata,
			`{"version":"1.4","info":{"creator":"Writer","producer":"LibreOffice"},"xmp":{"creator_tool":"Writer","packet":"<x/>"}}`),
	}
	o := collect(t, newPipeline(t, runConfig(t, e, nil)).Run(t.Context(), content, seq(inputs...), docinfer.Options{}))
	if o.err != nil {
		t.Fatalf("Run: %v", o.err)
	}
	if len(o.batches) != 0 || len(e.Requests()) != 0 {
		t.Errorf("%d batches and %d requests, want none", len(o.batches), len(e.Requests()))
	}
	if !slices.Equal(slices.Sorted(slices.Values(o.settled)), []int{0, 1}) {
		t.Errorf("settled %v, want both records", o.settled)
	}
	checkCounts(t, o, docinfer.Counts{})
}

func TestRunMetadataRecordUndecodable(t *testing.T) {
	e := aitest.Serve(t, aitest.Routes{Embeddings: embedAll})
	for _, mediaType := range []string{dispatch.MediaOfficeMetadata, dispatch.MediaPDFMetadata} {
		t.Run(mediaType, func(t *testing.T) {
			content := dispatch.NewMemoryContent()
			in := recordInput(t, content, childPart, textStage, mediaType, `{"core":`)
			o := collect(t, newPipeline(t, runConfig(t, e, nil)).Run(t.Context(), content, seq(in), docinfer.Options{Final: true}))
			if o.err == nil || !docinfer.Permanent(o.err) {
				t.Fatalf("Run: %v, want a permanent error", o.err)
			}
			if msg := o.err.Error(); !strings.Contains(msg, in.Event.Output.SHA256) || !strings.Contains(msg, childPart.ID) ||
				!strings.Contains(msg, "unexpected end of JSON input") {
				t.Errorf("Run: %v, want the record's SHA-256, its part, and the cause", o.err)
			}
		})
	}
	if n := len(e.Requests()); n != 0 {
		t.Errorf("%d requests, want none", n)
	}
}

func TestRunMetadataRecordMissing(t *testing.T) {
	e := aitest.Serve(t, aitest.Routes{Embeddings: embedAll})
	in := recordInput(t, nil, docxPart, textStage, dispatch.MediaOfficeMetadata, officeRec)
	o := collect(t, newPipeline(t, runConfig(t, e, nil)).Run(t.Context(), dispatch.NewMemoryContent(), seq(in), docinfer.Options{}))
	if m, ok := errors.AsType[*docinfer.MissingError](o.err); !ok || docinfer.Permanent(o.err) || m.Event.Output.SHA256 != in.Event.Output.SHA256 {
		t.Errorf("Run: %v, want a *MissingError of the record, not permanent", o.err)
	}
}
