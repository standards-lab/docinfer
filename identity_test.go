package docinfer_test

import (
	"encoding/json/jsontext"
	"slices"
	"testing"

	"github.com/standards-lab/docinfer"
	"github.com/standards-lab/docinfer/chunk"
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
			docinfer.Vision{Client: route, Model: "alias", Served: "v", Prompt: "Describe <this> & that.", MaxTokens: 7, Reasoning: protocol.ReasoningEffortHigh}.Identity(),
			docinfer.Identity{
				Capability: docinfer.CapabilityVision, Model: "v",
				Settings: `{"request":"2","prompt":"Describe <this> & that.","max_tokens":7,"effort":"high"}`,
			},
		},
		{
			"vision with no effort",
			docinfer.Vision{Client: route, Model: "alias", Served: "v", MaxTokens: 7}.Identity(),
			docinfer.Identity{
				Capability: docinfer.CapabilityVision, Model: "v",
				Settings: `{"request":"2","prompt":"","max_tokens":7,"effort":""}`,
			},
		},
		{
			"transcription by route",
			docinfer.Transcription{Client: route, Model: "alias", Served: "w", Prompt: "Names: Ada.", Language: "en"}.Identity(),
			docinfer.Identity{
				Capability: docinfer.CapabilityTranscription, Model: "w",
				Settings: `{"request":"2","mode":"route","prompt":"Names: Ada.","language":"en","max_tokens":0}`,
			},
		},
		{
			"transcription by chat",
			docinfer.Transcription{Client: chat, Model: "alias", Served: "w"}.Identity(),
			docinfer.Identity{
				Capability: docinfer.CapabilityTranscription, Model: "w",
				Settings: `{"request":"2","mode":"chat","prompt":"","language":"","max_tokens":0}`,
			},
		},
		{
			"transcription by chat with a cap",
			docinfer.Transcription{Client: chat, Model: "alias", Served: "w", MaxTokens: 512}.Identity(),
			docinfer.Identity{
				Capability: docinfer.CapabilityTranscription, Model: "w",
				Settings: `{"request":"2","mode":"chat","prompt":"","language":"","max_tokens":512}`,
			},
		},
		{
			"extraction",
			docinfer.Extraction{Client: route, Model: "alias", Served: "x", MaxTokens: 9, Reasoning: protocol.ReasoningEffortMedium}.Identity(
				docinfer.Extract{Name: "invoice", Prompt: "Extract the invoice.", Schema: schema}),
			docinfer.Identity{
				Capability: docinfer.CapabilityExtraction, Model: "x",
				Settings: `{"request":"2","name":"invoice","prompt_sha256":"` + sha("Extract the invoice.") +
					`","schema_sha256":"` + sha(string(schema)) + `","max_tokens":9,"effort":"medium"}`,
			},
		},
		{
			"embeddings",
			docinfer.Embeddings{Client: route, Model: "alias", Served: "e"}.Identity(),
			docinfer.Identity{Capability: docinfer.CapabilityEmbeddings, Model: "e", Settings: ""},
		},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s: Identity\n %+v, want\n %+v", tc.name, tc.got, tc.want)
		}
	}
}

func TestIdentityNamesTheServedModel(t *testing.T) {
	route := newClient(t, "http://localhost/v1", ai.TranscribeRoute)
	extract := docinfer.Extract{Name: "invoice", Prompt: "prompt", Schema: jsontext.Value(`{"type":"object"}`)}
	identities := map[docinfer.Capability]func(model, served string) docinfer.Identity{
		docinfer.CapabilityVision: func(model, served string) docinfer.Identity {
			return docinfer.Vision{Client: route, Model: model, Served: served, MaxTokens: 1}.Identity()
		},
		docinfer.CapabilityTranscription: func(model, served string) docinfer.Identity {
			return docinfer.Transcription{Client: route, Model: model, Served: served}.Identity()
		},
		docinfer.CapabilityExtraction: func(model, served string) docinfer.Identity {
			return docinfer.Extraction{Client: route, Model: model, Served: served, MaxTokens: 1}.Identity(extract)
		},
		docinfer.CapabilityEmbeddings: func(model, served string) docinfer.Identity {
			return docinfer.Embeddings{Client: route, Model: model, Served: served}.Identity()
		},
	}
	content := sha("content")
	for capability, identity := range identities {
		key := docinfer.Key(identity("alias", "org/model-1"), content)
		if docinfer.Key(identity("alias", "org/model-2"), content) == key {
			t.Errorf("%s: another model served under the same alias keys the same", capability)
		}
		if docinfer.Key(identity("another-alias", "org/model-1"), content) != key {
			t.Errorf("%s: the same model served under another alias keys differently", capability)
		}
	}
}

func TestGoldenKeysAreIdentities(t *testing.T) {
	route := newClient(t, "http://localhost/v1", ai.TranscribeRoute)
	vision := docinfer.Vision{
		Client: route, Model: "vision", Served: "gpt-4.1-mini", Prompt: "Describe the image.", MaxTokens: 1024,
		Reasoning: protocol.ReasoningEffortLow,
	}
	extraction := docinfer.Extraction{
		Client: route, Model: "agent", Served: "gpt-4.1", MaxTokens: 4096, Reasoning: protocol.ReasoningEffortMedium,
	}
	extract := docinfer.Extract{Name: "invoice", Prompt: "prompt", Schema: jsontext.Value("schema")}
	sampledVision, sampledExtraction := vision, extraction
	sampledVision.Native = jsontext.Value(`{"top_p": 0.95, "top_k": 64, "temperature": 0.2}`)
	sampledExtraction.Native = jsontext.Value(`{"temperature": 0}`)
	ids := []docinfer.Identity{
		vision.Identity(),
		docinfer.Transcription{Client: route, Model: "audio", Served: "whisper-1", Language: "en"}.Identity(),
		docinfer.Transcription{
			Client: newClient(t, "http://localhost/v1", ai.TranscribeChat), Model: "audio", Served: "google/gemma-4-E4B-it",
			MaxTokens: 512,
		}.Identity(),
		extraction.Identity(extract),
		docinfer.Embeddings{Client: route, Model: "embedding", Served: "text-embedding-3-small"}.Identity(),
		sampledVision.Identity(),
		sampledExtraction.Identity(extract),
		docinfer.Embeddings{
			Client: profiledClient(t, "http://localhost/v1", conventions(&ai.InputConvention{Prefix: "passage: "}, queryPrefix)),
			Model:  "embedding", Served: "intfloat/multilingual-e5-large",
		}.Identity(),
		chunkIdentity(chunk.Splitter{Bound: 480, Runes: 8183, Model: "intfloat/multilingual-e5-large"}),
	}
	for i, id := range ids {
		if !slices.ContainsFunc(goldenKeys, func(g goldenKey) bool { return g.Identity == id }) {
			t.Errorf("identity %d, %+v, has no golden key", i, id)
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
			Capability: docinfer.CapabilityVision, Model: "org/vision-1",
			Settings: `{"request":"2","prompt":"Describe the image.","max_tokens":2,"effort":"low"}`,
		},
		Transcription: &docinfer.Identity{
			Capability: docinfer.CapabilityTranscription, Model: "org/transcription-1",
			Settings: `{"request":"2","mode":"route","prompt":"","language":"en","max_tokens":0}`,
		},
		Extraction: &docinfer.Identity{
			Capability: docinfer.CapabilityExtraction, Model: "org/extraction-1",
			Settings: `{"request":"2","max_tokens":2,"effort":"medium"}`,
		},
		Embeddings: docinfer.Identity{Capability: docinfer.CapabilityEmbeddings, Model: "org/embedding-1"},
		Dimensions: 2,
		Chunker:    `{"version":"2","bound":2,"runes":2,"model":"org/embedding-1"}`,
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
