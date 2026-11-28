//go:build integration

package integration_test

import (
	"encoding/json/v2"
	"maps"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

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
		c.Extraction = &docinfer.Extraction{Client: client, Model: model, Served: served, MaxTokens: extractionTokens, WindowRunes: extractionWindow}
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

var capitals = docinfer.Extract{
	Name:   "capitals",
	Prompt: "Extract each country the text names and its capital city.",
	Schema: []byte(`{
		"type": "object",
		"properties": {
			"capitals": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": {
						"capital": {"type": "string"},
						"country": {"type": "string"}
					},
					"required": ["capital", "country"],
					"additionalProperties": false
				}
			}
		},
		"required": ["capitals"],
		"additionalProperties": false
	}`),
}

const documentWindowRunes = 200

type capitalOf struct {
	Capital string `json:"capital"`
	Country string `json:"country"`
}

func TestDocumentExtraction(t *testing.T) {
	client, model, served := endpoint(t, agent, ai.Options{})
	emb, dims := embeddings(t)
	p := newPipeline(t, emb, dims, func(c *docinfer.Config) {
		c.Extraction = &docinfer.Extraction{Client: client, Model: model, Served: served, MaxTokens: extractionTokens,
			WindowRunes: documentWindowRunes}
	})
	content := dispatch.NewMemoryContent()
	paragraphs := []string{
		"France is a country in Western Europe, bordered by Belgium, Germany, Switzerland, Italy, and Spain. " +
			"Its capital, Paris, sits on the Seine.",
		"Japan is an island country in East Asia, a chain of islands in the Pacific Ocean off the coast of the " +
			"mainland. Its capital is Tokyo.",
		"Kenya is a country in East Africa, on the Indian Ocean, bordered by Ethiopia, Somalia, Tanzania, and " +
			"Uganda. Its capital is Nairobi.",
	}
	var inputs []docinfer.Input
	for _, para := range paragraphs {
		inputs = append(inputs, textInput(t, content, dispatch.TextOutput, para))
	}
	if n := utf8.RuneCountInString(strings.Join(paragraphs, "\n\n")); n <= 2*documentWindowRunes {
		t.Fatalf("the document text is %d characters, want more than two windows of %d", n, documentWindowRunes)
	}
	opts := docinfer.Options{Document: &capitals}

	first := run(t, p, content, opts, inputs...)

	inf := first.inference(t, docinfer.CapabilityExtraction)
	if inf.Scope != docinfer.ScopeDocument || inf.Outcome != docinfer.OutcomeExtracted || len(inf.Failed) != 0 || inf.Held {
		t.Fatalf("the document's extraction = %+v, want of scope document, extracted, no window failed, asked of the model", inf)
	}
	t.Logf("%s extracts the document as %s", model, inf.Text)
	var got struct {
		Capitals []capitalOf `json:"capitals"`
	}
	if err := json.Unmarshal([]byte(inf.Text), &got); err != nil {
		t.Fatalf("the document's extraction %q doesn't follow the schema: %v", inf.Text, err)
	}
	for _, capital := range []string{"Paris", "Tokyo", "Nairobi"} {
		if !slices.ContainsFunc(got.Capitals, func(c capitalOf) bool { return strings.Contains(c.Capital, capital) }) {
			t.Errorf("the combined extraction %s has no %s: a window's value was lost", inf.Text, capital)
		}
	}
	checkChunks(t, first, dims, "output")
	if x := first.counts.Extraction; x == nil || x.Extracted != 1 || x.Requests < len(paragraphs)+1 {
		t.Errorf("Counts.Extraction = %+v, want one extracted of a request per window and at least one combine", x)
	}

	checkHeld(t, first, run(t, p, content, opts, inputs...))
}

var videoPart = dispatch.Part{ID: "/talk.mp4", Name: "talk.mp4", Size: 8192, SHA256: strings.Repeat("e", 64),
	Parent: "/", Depth: 1, Where: "talk.mp4"}

var videoStage = dispatch.Identity{Name: "video", Version: "1", Config: "v0"}

var sightings = docinfer.Extract{
	Name:   "sightings",
	Prompt: "Extract the access code the video's speaker says and the shapes the video shows.",
	Schema: []byte(`{
		"type": "object",
		"properties": {
			"code": {"type": "string"},
			"shapes": {"type": "array", "items": {"type": "string"}}
		},
		"required": ["code", "shapes"],
		"additionalProperties": false
	}`),
}

func TestTimelineExtraction(t *testing.T) {
	client, model, served := endpoint(t, agent, ai.Options{})
	emb, dims := embeddings(t)
	cache, err := docinfer.NewFileCache(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	vis := docinfer.Vision{Client: client, Model: model, Served: served, MaxTokens: visionTokens}
	tr := docinfer.Transcription{Client: client, Model: model, Served: served}
	p := newPipeline(t, emb, dims, func(c *docinfer.Config) {
		c.Cache = cache
		c.Vision, c.Transcription = &vis, &tr
		c.Extraction = &docinfer.Extraction{Client: client, Model: model, Served: served, MaxTokens: extractionTokens,
			WindowRunes: extractionWindow}
	})
	content := dispatch.NewMemoryContent()
	frame := put(t, content, readTestdata(t, "shapes.png"))
	window := put(t, content, readTestdata(t, "phrase.wav"))
	hold(t, cache, docinfer.Key(vis.Identity(), frame), "A red circle, a blue square, and a green triangle on white.")
	hold(t, cache, docinfer.Key(tr.Identity(), window), "The access code is seven four two nine.")
	inputs := []docinfer.Input{
		{Event: dispatch.Event{Part: videoPart, Stage: videoStage, Output: &dispatch.Output{
			Kind: dispatch.ImageOutput, Name: "frame", MediaType: "image/png", SHA256: frame,
			Meta: map[string]string{dispatch.MetaFrame: "0", dispatch.MetaReason: "scene", dispatch.MetaTime: "1.5"},
		}}},
		{Event: dispatch.Event{Part: videoPart, Stage: videoStage, Output: &dispatch.Output{
			Kind: dispatch.AudioOutput, Name: "window", MediaType: "audio/wav", SHA256: window,
			Meta: map[string]string{dispatch.MetaWindow: "0", dispatch.MetaStart: "0", dispatch.MetaEnd: "6.05"},
		}}},
		{Event: dispatch.Event{Part: videoPart, Stage: videoStage, Outcome: dispatch.Prepared, Outputs: 2}},
	}
	opts := docinfer.Options{Timeline: &sightings}

	first := run(t, p, content, opts, inputs...)

	var timeline []docinfer.Inference
	for _, inf := range first.inferences {
		if inf.Scope == docinfer.ScopeTimeline {
			timeline = append(timeline, inf)
		}
	}
	if len(timeline) != 1 {
		t.Fatalf("Run yielded inferences %+v, want one of scope timeline", first.inferences)
	}
	inf := timeline[0]
	if inf.Part != videoPart.ID || inf.Outcome != docinfer.OutcomeExtracted || inf.Held {
		t.Fatalf("the timeline's extraction = %+v, want of %s, extracted, asked of the model", inf, videoPart.ID)
	}
	t.Logf("%s extracts the timeline as %s", model, inf.Text)
	var got struct {
		Code   string   `json:"code"`
		Shapes []string `json:"shapes"`
	}
	if err := json.Unmarshal([]byte(inf.Text), &got); err != nil {
		t.Fatalf("the timeline's extraction %q doesn't follow the schema: %v", inf.Text, err)
	}
	if code := strings.Map(digitsOnly, got.Code); code != "7429" && !strings.Contains(strings.ToLower(got.Code), "seven four two nine") {
		t.Errorf("the timeline's code is %q, want what the transcript says, 7429", got.Code)
	}
	if len(got.Shapes) == 0 {
		t.Errorf("the timeline's extraction %s names no shape the frame shows", inf.Text)
	}
	if x := first.counts.Extraction; x == nil || x.Extracted != 1 || x.Requests != 1 {
		t.Errorf("Counts.Extraction = %+v, want one extracted of one request", x)
	}

	checkHeld(t, first, run(t, p, content, opts, inputs...))
}

func hold(t *testing.T, cache docinfer.Cache, key, text string) {
	t.Helper()
	b, err := json.Marshal(docinfer.Held{Text: text})
	if err != nil {
		t.Fatal(err)
	}
	if err := cache.Put(t.Context(), key, b); err != nil {
		t.Fatal(err)
	}
}

func digitsOnly(r rune) rune {
	if r >= '0' && r <= '9' {
		return r
	}
	return -1
}
