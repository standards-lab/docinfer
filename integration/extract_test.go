//go:build integration

package integration_test

import (
	"encoding/json/v2"
	"maps"
	"slices"
	"testing"

	"github.com/standards-lab/docinfer"
	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/go-ai"
)

// capital is the scenario's Extract: a strict schema of two required string members, and no
// other.
var capital = docinfer.Extract{
	Name:   "capital",
	Prompt: "Extract the country the text names and its capital city.",
	Schema: []byte(`{
		"type": "object",
		"properties": {
			"capital": {"type": "string"},
			"country": {"type": "string"}
		},
		"required": ["capital", "country"],
		"additionalProperties": false
	}`),
}

// TestExtraction runs a text output through Run with capital's Extract on the agent role's
// endpoint, and wants one extraction of outcome extracted, asked of the model, whose text is
// JSON valid against the schema: an object of exactly its two members, each a string. Its text is
// chunked and embedded at the configured dimensions too. A second run over the same content is
// held whole, from the FileCache, asking no model and yielding no progress.
func TestExtraction(t *testing.T) {
	client, model := endpoint(t, agent, ai.Options{})
	emb, dims := embeddings(t)
	p := newPipeline(t, emb, dims, func(c *docinfer.Config) {
		c.Extraction = &docinfer.Extraction{Client: client, Model: model, MaxTokens: extractionTokens}
	})
	content := dispatch.NewMemoryContent()
	in := textInput(t, content, dispatch.TextOutput,
		"France is a country in Western Europe. Its capital, Paris, sits on the Seine.")
	opts := docinfer.Options{Extract: &capital}

	first := run(t, p, content, opts, in)

	inf := first.inference(t, docinfer.CapabilityExtraction)
	if inf.Outcome != docinfer.OutcomeExtracted || inf.Held {
		t.Fatalf("the extraction = %+v, want extracted, asked of the model", inf)
	}
	t.Logf("%s extracts %s", model, inf.Text)
	var got map[string]any
	if err := json.Unmarshal([]byte(inf.Text), &got); err != nil {
		t.Fatalf("the extraction's text %q isn't a JSON object: %v", inf.Text, err)
	}
	if keys := slices.Sorted(maps.Keys(got)); !slices.Equal(keys, []string{"capital", "country"}) {
		t.Errorf("the extraction's members are %q, want exactly capital and country", keys)
	}
	for k, v := range got {
		if _, ok := v.(string); !ok {
			t.Errorf("the extraction's %s is %#v, want a string", k, v)
		}
	}
	checkChunks(t, first, dims, "output")
	if x := first.counts.Extraction; x == nil || x.Extracted != 1 || x.Requests != 1 {
		t.Errorf("Counts.Extraction = %+v, want one extracted of one request", x)
	}

	checkHeld(t, first, run(t, p, content, opts, in))
}
