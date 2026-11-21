//go:build integration

package integration_test

import (
	"strings"
	"testing"

	"github.com/standards-lab/docinfer"
	"github.com/standards-lab/docproc/dispatch"
)

var inputPart = dispatch.Part{ID: "/", Name: "report.docx", Size: 2048, SHA256: strings.Repeat("c", 64)}

func textInput(t *testing.T, content *dispatch.MemoryContent, kind dispatch.OutputKind, text string) docinfer.Input {
	t.Helper()
	return docinfer.Input{Event: dispatch.Event{
		Part:   inputPart,
		Stage:  stage,
		Output: &dispatch.Output{Kind: kind, Name: string(kind), MediaType: "text/plain", SHA256: put(t, content, []byte(text))},
	}}
}

func TestEmbeddings(t *testing.T) {
	emb, dims := embeddings(t)
	p := newPipeline(t, emb, dims, nil)
	content := dispatch.NewMemoryContent()
	inputs := []docinfer.Input{
		textInput(t, content, dispatch.TextOutput, "The cat sat on the warm windowsill all afternoon.\n\n"+
			"Quarterly revenue rose eight percent on strong cloud sales."),
		textInput(t, content, dispatch.MarkdownOutput, "# Findings\n\nA cat dozed in the sunny window for hours."),
		{Event: dispatch.Event{Part: inputPart, Outcome: dispatch.Prepared}, Metadata: docinfer.MetadataText(inputPart)},
	}

	first := run(t, p, content, docinfer.Options{}, inputs...)

	checkChunks(t, first, dims, "output", "metadata")
	if c := first.counts; c.Texts != 2 || c.Metadata != 1 || c.Requests < 1 {
		t.Errorf("Counts = %+v, want 2 texts and 1 metadata embedded by at least one request", c)
	}

	checkHeld(t, first, run(t, p, content, docinfer.Options{}, inputs...))
}
