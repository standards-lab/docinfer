//go:build integration

package integration_test

import (
	"strings"
	"testing"

	"github.com/standards-lab/docinfer"
	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/go-ai"
)

// imagePart is the part shapes.png is an image output of.
var imagePart = dispatch.Part{ID: "/shapes.png", Name: "shapes.png", Size: 5384, SHA256: strings.Repeat("a", 64),
	Parent: "/", Depth: 1, Where: "shapes.png"}

// TestVision runs shapes.png through Run as an image output, on the vision role's endpoint, and
// wants it described: one inference of outcome described with non-empty text, asked of the
// model, and its text chunked as kind vision and embedded at the configured dimensions. A second
// run over the same content is held whole, from the FileCache, asking no model.
func TestVision(t *testing.T) {
	client, model := endpoint(t, vision, ai.Options{})
	emb, dims := embeddings(t)
	p := newPipeline(t, emb, dims, func(c *docinfer.Config) {
		c.Vision = &docinfer.Vision{Client: client, Model: model, Prompt: "Describe this image in one sentence.", MaxTokens: visionTokens}
	})
	content := dispatch.NewMemoryContent()
	in := docinfer.Input{Event: dispatch.Event{
		Part:  imagePart,
		Stage: stage,
		Output: &dispatch.Output{Kind: dispatch.ImageOutput, Name: "image", MediaType: "image/png",
			SHA256: put(t, content, readTestdata(t, "shapes.png"))},
	}}

	first := run(t, p, content, docinfer.Options{}, in)

	inf := first.inference(t, docinfer.CapabilityVision)
	if inf.Outcome != docinfer.OutcomeDescribed || strings.TrimSpace(inf.Text) == "" || inf.Held {
		t.Fatalf("the image's inference = %+v, want described with text, asked of the model", inf)
	}
	t.Logf("%s describes shapes.png: %q", model, inf.Text)
	checkChunks(t, first, dims, "vision")
	if v := first.counts.Vision; v == nil || v.Described != 1 || v.Requests != 1 {
		t.Errorf("Counts.Vision = %+v, want one described of one request", v)
	}

	checkHeld(t, first, run(t, p, content, docinfer.Options{}, in))
}
