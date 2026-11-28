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

func TestExtraction(t *testing.T) {
	client, model, served := endpoint(t, agent, ai.Options{})
	emb, dims := embeddings(t)
	p := newPipeline(t, emb, dims, func(c *docinfer.Config) {
		c.Extraction = &docinfer.Extraction{Client: client, Model: model, Served: served, MaxTokens: extractionTokens, WindowRunes: 4000}
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
