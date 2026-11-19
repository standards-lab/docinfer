//go:build integration

package integration_test

import (
	"strings"
	"testing"

	"github.com/standards-lab/docinfer"
	"github.com/standards-lab/docproc/dispatch"
)

// inputPart is the part the text scenarios' outputs come from, and whose metadata is a job's.
var inputPart = dispatch.Part{ID: "/", Name: "report.docx", Size: 2048, SHA256: strings.Repeat("c", 64)}

// textInput returns an input of an output of kind on inputPart, its text put in content.
func textInput(t *testing.T, content *dispatch.MemoryContent, kind dispatch.OutputKind, text string) docinfer.Input {
	t.Helper()
	return docinfer.Input{Event: dispatch.Event{
		Part:   inputPart,
		Stage:  stage,
		Output: &dispatch.Output{Kind: kind, Name: string(kind), MediaType: "text/plain", SHA256: put(t, content, []byte(text))},
	}}
}

// TestEmbeddings runs a text output, a Markdown output, and the input's metadata through Run on
// the embedding role's endpoint, and wants each chunked, as kind output or metadata, and every
// chunk embedded at the configured dimensions, none rejected. A second run over the same content
// is held whole, from the FileCache, asking no model and counting no token.
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
