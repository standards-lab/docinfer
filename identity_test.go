package docinfer_test

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/standards-lab/docinfer"
	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/protocol"
)

func TestIdentity(t *testing.T) {
	route := newClient(t, "http://localhost/v1", ai.TranscribeRoute)
	chat := newClient(t, "http://localhost/v1", ai.TranscribeChat)
	schema := jsontext.Value(`{"type":"object"}`)
	cases := []struct {
		name string
		got  docinfer.Identity
		want docinfer.Identity
	}{
		{
			"vision",
			docinfer.Vision{Client: route, Model: "v", Prompt: "Describe <this> & that.", MaxTokens: 7, Reasoning: protocol.ReasoningEffortHigh}.Identity(),
			docinfer.Identity{
				Capability: docinfer.CapabilityVision, Model: "v",
				Settings: `{"request":"1","prompt":"Describe <this> & that.","max_tokens":7,"effort":"high"}`,
			},
		},
		{
			"vision with no effort",
			docinfer.Vision{Client: route, Model: "v", MaxTokens: 7}.Identity(),
			docinfer.Identity{
				Capability: docinfer.CapabilityVision, Model: "v",
				Settings: `{"request":"1","prompt":"","max_tokens":7,"effort":""}`,
			},
		},
		{
			"transcription by route",
			docinfer.Transcription{Client: route, Model: "w", Prompt: "Names: Ada.", Language: "en"}.Identity(),
			docinfer.Identity{
				Capability: docinfer.CapabilityTranscription, Model: "w",
				Settings: `{"request":"1","mode":"route","prompt":"Names: Ada.","language":"en"}`,
			},
		},
		{
			"transcription by chat",
			docinfer.Transcription{Client: chat, Model: "w"}.Identity(),
			docinfer.Identity{
				Capability: docinfer.CapabilityTranscription, Model: "w",
				Settings: `{"request":"1","mode":"chat","prompt":"","language":""}`,
			},
		},
		{
			"extraction",
			docinfer.Extraction{Client: route, Model: "x", MaxTokens: 9, Reasoning: protocol.ReasoningEffortMedium}.Identity(
				docinfer.Extract{Name: "invoice", Prompt: "Extract the invoice.", Schema: schema}),
			docinfer.Identity{
				Capability: docinfer.CapabilityExtraction, Model: "x",
				Settings: `{"request":"1","name":"invoice","prompt_sha256":"` + sha("Extract the invoice.") +
					`","schema_sha256":"` + sha(string(schema)) + `","max_tokens":9,"effort":"medium"}`,
			},
		},
		{
			"embeddings",
			docinfer.Embeddings{Client: route, Model: "e"}.Identity(),
			docinfer.Identity{Capability: docinfer.CapabilityEmbeddings, Model: "e", Settings: ""},
		},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s: Identity\n %+v, want\n %+v", tc.name, tc.got, tc.want)
		}
	}
}

func TestGoldenKeysAreIdentities(t *testing.T) {
	route := newClient(t, "http://localhost/v1", ai.TranscribeRoute)
	ids := map[docinfer.Capability]docinfer.Identity{
		docinfer.CapabilityVision: docinfer.Vision{
			Client: route, Model: "gpt-4.1-mini", Prompt: "Describe the image.", MaxTokens: 1024,
			Reasoning: protocol.ReasoningEffortLow,
		}.Identity(),
		docinfer.CapabilityTranscription: docinfer.Transcription{Client: route, Model: "whisper-1", Language: "en"}.Identity(),
		docinfer.CapabilityExtraction: docinfer.Extraction{
			Client: route, Model: "gpt-4.1", MaxTokens: 4096, Reasoning: protocol.ReasoningEffortMedium,
		}.Identity(docinfer.Extract{Name: "invoice", Prompt: "prompt", Schema: jsontext.Value("schema")}),
		docinfer.CapabilityEmbeddings: docinfer.Embeddings{Client: route, Model: "text-embedding-3-small"}.Identity(),
	}
	for _, g := range goldenKeys {
		if id := ids[g.Identity.Capability]; id != g.Identity {
			t.Errorf("golden %s identity\n %+v, the capability's\n %+v", g.Identity.Capability, g.Identity, id)
		}
	}
}

func TestProfile(t *testing.T) {
	cfg := validConfig(t)
	p, err := docinfer.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	want := docinfer.Profile{
		Vision: &docinfer.Identity{
			Capability: docinfer.CapabilityVision, Model: "vision-model",
			Settings: `{"request":"1","prompt":"Describe the image.","max_tokens":2,"effort":"low"}`,
		},
		Transcription: &docinfer.Identity{
			Capability: docinfer.CapabilityTranscription, Model: "transcription-model",
			Settings: `{"request":"1","mode":"route","prompt":"","language":"en"}`,
		},
		Extraction: &docinfer.Identity{
			Capability: docinfer.CapabilityExtraction, Model: "extraction-model",
			Settings: `{"request":"1","max_tokens":2,"effort":"medium"}`,
		},
		Embeddings: docinfer.Identity{Capability: docinfer.CapabilityEmbeddings, Model: "embedding-model"},
		Dimensions: 2,
		Chunker:    `{"version":"1","bound":2}`,
	}
	cfg.Vision.Prompt, cfg.Transcription.Language, cfg.Extraction.MaxTokens = "changed", "de", 3
	first := p.Profile()
	first.Vision.Model, first.Transcription.Model, first.Extraction.Model = "x", "x", "x"
	got := p.Profile()
	equalProfile(t, got, want)

	cfg = validConfig(t)
	cfg.Vision, cfg.Transcription, cfg.Extraction = nil, nil, nil
	p, err = docinfer.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	want.Vision, want.Transcription, want.Extraction = nil, nil, nil
	equalProfile(t, p.Profile(), want)
}

func equalProfile(t *testing.T, got, want docinfer.Profile) {
	t.Helper()
	for _, c := range []struct {
		name      string
		got, want *docinfer.Identity
	}{
		{"Vision", got.Vision, want.Vision},
		{"Transcription", got.Transcription, want.Transcription},
		{"Extraction", got.Extraction, want.Extraction},
		{"Embeddings", &got.Embeddings, &want.Embeddings},
	} {
		switch {
		case (c.got == nil) != (c.want == nil):
			t.Errorf("Profile.%s: %v, want %v", c.name, c.got, c.want)
		case c.got != nil && *c.got != *c.want:
			t.Errorf("Profile.%s:\n %+v, want\n %+v", c.name, *c.got, *c.want)
		}
	}
	if got.Dimensions != want.Dimensions || got.Chunker != want.Chunker {
		t.Errorf("Profile's Dimensions and Chunker: %d, %s; want %d, %s", got.Dimensions, got.Chunker, want.Dimensions, want.Chunker)
	}
}
