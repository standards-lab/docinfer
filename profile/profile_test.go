package profile_test

import (
	"encoding/json/jsontext"
	"errors"
	"strings"
	"testing"

	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/aitest"

	"github.com/standards-lab/docinfer"
	"github.com/standards-lab/docinfer/profile"
)

const passage = "passage: "

type built struct {
	settings profile.Settings
	clients  profile.Clients
	models   []profile.Model
	bounds   *docinfer.Bounds
}

func resolved(t *testing.T) built {
	t.Helper()
	e := aitest.Serve(t, aitest.Routes{Models: listing(
		`{"id":"vision","root":"org/vision-1"}`,
		`{"id":"whisper"}`,
		`{"id":"agent","root":"org/agent-1"}`,
		`{"id":"e5","root":"intfloat/multilingual-e5-large"}`,
	)})
	plain := newClient(t, e, ai.Options{})
	profiled := newClient(t, e, ai.Options{Embedding: ai.EmbeddingProfile{InputTypes: map[ai.InputType]ai.InputConvention{
		ai.InputDocument: {Prefix: passage},
		ai.InputQuery:    {Prefix: "query: "},
	}}})
	s := decode(t, settingsJSON)
	s.Transcription.ServedModel = "org/whisper-1"
	clients := profile.Clients{Vision: plain, Transcription: plain, Extraction: plain, Embeddings: profiled, Tokenizer: profiled}
	models, err := profile.Resolve(t.Context(), s, clients)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	bounds, err := docinfer.NewBounds(1, 1, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	return built{settings: s, clients: clients, models: models, bounds: bounds}
}

func (b built) config() profile.Config {
	return profile.Config{Settings: b.settings, Clients: b.clients, Models: b.models, Bounds: b.bounds, Cache: docinfer.NewMemoryCache()}
}

func TestNewReportsTheResolvedModels(t *testing.T) {
	b := resolved(t)
	p, err := profile.New(b.config())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	pr := p.Profile()
	if pr.Vision == nil || pr.Vision.Model != "org/vision-1" || pr.VisionLayerWords != 100 {
		t.Errorf("vision %+v, layer words %d, want org/vision-1 and 100", pr.Vision, pr.VisionLayerWords)
	}
	if pr.Transcription == nil || pr.Transcription.Model != "org/whisper-1" {
		t.Errorf("transcription %+v, want org/whisper-1, its served_model", pr.Transcription)
	}
	if pr.Extraction == nil || pr.Extraction.Model != "org/agent-1" || !strings.Contains(pr.Extraction.Settings, `"max_tokens":4096`) {
		t.Errorf("extraction %+v, want org/agent-1 and max_tokens 4096", pr.Extraction)
	}
	if pr.Embeddings.Model != "intfloat/multilingual-e5-large" || pr.Dimensions != 1024 {
		t.Errorf("embeddings %+v, dimensions %d, want the root and 1024", pr.Embeddings, pr.Dimensions)
	}
	if pr.EmbeddingQuery == nil || pr.EmbeddingQuery.Prefix != "query: " {
		t.Errorf("embedding query %+v, want the client's query convention", pr.EmbeddingQuery)
	}
	if want := `"bound":480,"runes":8183,"model":"intfloat/multilingual-e5-large"`; !strings.Contains(pr.Chunker, want) {
		t.Errorf("chunker %s, want %s: chunk_runes less the document prefix, and the embeddings' served model", pr.Chunker, want)
	}
}

func TestNewSendsNoNativeForAnEmptyObject(t *testing.T) {
	b := resolved(t)
	withEmpty, err := profile.New(b.config())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	b.settings.Transcription.Native = nil
	withNone, err := profile.New(b.config())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got, want := withEmpty.Profile().Transcription.Settings, withNone.Profile().Transcription.Settings; got != want {
		t.Errorf("transcription settings with native {}: %s, want those with none, %s", got, want)
	}
}

func TestNewRefuses(t *testing.T) {
	cases := []struct {
		name string
		edit func(*built)
		want string
	}{
		{"settings Validate refuses", func(b *built) { b.settings.Pending = nil }, "no pending"},
		{"no served model resolved", func(b *built) { b.models = b.models[:len(b.models)-1] }, "embeddings: no served model resolved"},
		{"an unresolved capability", func(b *built) { b.models[0].Served = "" }, "vision: no served model resolved"},
		{"chunk_runes within the document prefix", func(b *built) { *b.settings.Embeddings.ChunkRunes = len(passage) },
			"embeddings: chunk_runes 9 leaves no character beside the document prefix's 9"},
		{"max_tokens in route mode", func(b *built) { *b.settings.Transcription.MaxTokens = 512 }, "transcription: max_tokens 512"},
		{"native in route mode", func(b *built) { b.settings.Transcription.Native = jsontext.Value(`{"temperature":0}`) },
			`transcription: native {"temperature":0}`},
		{"a native docinfer refuses", func(b *built) { b.settings.Vision.Native = jsontext.Value(`[1]`) }, "Vision.Native"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := resolved(t)
			tc.edit(&b)
			if _, err := profile.New(b.config()); !errors.Is(err, docinfer.ErrRequest) || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("New: %v, want ErrRequest naming %q", err, tc.want)
			}
		})
	}
}

func TestNewTakesChatModesSettings(t *testing.T) {
	b := resolved(t)
	e := aitest.Serve(t, aitest.Routes{})
	b.clients.Transcription = newClient(t, e, ai.Options{TranscriptionMode: ai.TranscribeChat})
	*b.settings.Transcription.MaxTokens = 512
	b.settings.Transcription.Native = jsontext.Value(`{"temperature":0}`)
	p, err := profile.New(b.config())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if s := p.Profile().Transcription.Settings; !strings.Contains(s, `"mode":"chat"`) || !strings.Contains(s, `"max_tokens":512`) {
		t.Errorf("transcription settings %s, want chat mode and max_tokens 512", s)
	}
}

func TestNewPanicsNamingTheField(t *testing.T) {
	cases := []struct {
		field string
		unset func(*profile.Config)
	}{
		{"Bounds", func(c *profile.Config) { c.Bounds = nil }},
		{"Cache", func(c *profile.Config) { c.Cache = nil }},
		{"Clients.Vision", func(c *profile.Config) { c.Clients.Vision = nil }},
		{"Clients.Transcription", func(c *profile.Config) { c.Clients.Transcription = nil }},
		{"Clients.Extraction", func(c *profile.Config) { c.Clients.Extraction = nil }},
		{"Clients.Embeddings", func(c *profile.Config) { c.Clients.Embeddings = nil }},
		{"Clients.Tokenizer", func(c *profile.Config) { c.Clients.Tokenizer = nil }},
	}
	for _, tc := range cases {
		t.Run(tc.field, func(t *testing.T) {
			cfg := resolved(t).config()
			tc.unset(&cfg)
			defer func() {
				if r := recover(); r == nil || !strings.Contains(r.(string), tc.field) {
					t.Errorf("New with a nil %s: panic %v, want one naming it", tc.field, r)
				}
			}()
			_, _ = profile.New(cfg)
		})
	}
}

func TestNewTakesNoClientForAnUnsetCapability(t *testing.T) {
	b := resolved(t)
	b.settings.Vision, b.settings.Transcription, b.settings.Extraction = nil, nil, nil
	b.clients.Vision, b.clients.Transcription, b.clients.Extraction = nil, nil, nil
	p, err := profile.New(b.config())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if pr := p.Profile(); pr.Vision != nil || pr.Transcription != nil || pr.Extraction != nil {
		t.Errorf("profile %+v, want no vision, transcription, or extraction", pr)
	}
}
