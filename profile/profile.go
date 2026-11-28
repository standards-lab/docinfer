package profile

import (
	"context"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"unicode/utf8"

	"github.com/standards-lab/go-ai"

	"github.com/standards-lab/docinfer"
	"github.com/standards-lab/docinfer/chunk"
	"github.com/standards-lab/docinfer/join"
)

// Config is what [New] builds a pipeline from. Nothing in it has a default.
type Config struct {
	Settings Settings
	Clients  Clients
	// Models is [Resolve]'s, of Settings and Clients: each set capability's served model.
	Models []Model
	Bounds *docinfer.Bounds // required: the requests in flight per capability, shared
	Cache  docinfer.Cache   // required
}

// New returns the docinfer.Pipeline of cfg's settings, over its clients, Bounds, and Cache, each
// capability's Served the one cfg.Models resolved for it. The chunker counts a text's tokens
// through Clients.Tokenizer's Tokenize, as the embeddings' model and ai.InputDocument, and its
// Runes is chunk_runes less the characters of the document prefix Clients.Embeddings declares.
// It panics, naming the field, on a nil Bounds, Cache, Clients.Embeddings, or Clients.Tokenizer,
// or a nil client of a capability the settings set. It refuses, matching docinfer.ErrRequest,
// settings [Settings.Validate] refuses, a set capability cfg.Models resolves no served model for,
// a chunk_runes that leaves no character beside the document prefix, and, when
// Clients.Transcription transcribes in route mode, which sends no JSON body, a transcription
// max_tokens above 0 or a native with members; and whatever docinfer.New refuses. A native of {}
// sends none.
func New(cfg Config) (*docinfer.Pipeline, error) {
	if cfg.Bounds == nil {
		panic("profile: New: nil Bounds")
	}
	if cfg.Cache == nil {
		panic("profile: New: nil Cache")
	}
	s, c := cfg.Settings, cfg.Clients
	required := []struct {
		field   string
		set     bool
		missing bool
	}{
		{"Clients.Vision", s.Vision != nil, c.Vision == nil},
		{"Clients.Transcription", s.Transcription != nil, c.Transcription == nil},
		{"Clients.Extraction", s.Extraction != nil, c.Extraction == nil},
		{"Clients.Embeddings", true, c.Embeddings == nil},
		{"Clients.Tokenizer", true, c.Tokenizer == nil},
	}
	for _, r := range required {
		if r.set && r.missing {
			panic("profile: New: nil " + r.field)
		}
	}
	if err := s.Validate(); err != nil {
		return nil, err
	}

	served := map[docinfer.Capability]string{}
	for _, m := range cfg.Models {
		served[m.Capability] = m.Served
	}
	resolved := func(name docinfer.Capability) (string, error) {
		if served[name] == "" {
			return "", refuse("%s: no served model resolved", name)
		}
		return served[name], nil
	}

	e := s.Embeddings
	embeddingsServed, err := resolved(docinfer.CapabilityEmbeddings)
	if err != nil {
		return nil, err
	}
	prefix := utf8.RuneCountInString(c.Embeddings.EmbeddingProfile().InputTypes[ai.InputDocument].Prefix)
	if *e.ChunkRunes <= prefix {
		return nil, refuse("embeddings: chunk_runes %d leaves no character beside the document prefix's %d", *e.ChunkRunes, prefix)
	}
	pc := docinfer.Config{
		Embeddings: docinfer.Embeddings{Client: c.Embeddings, Model: e.Model, Served: embeddingsServed},
		Chunker: chunk.Splitter{
			Bound: *e.ChunkSize,
			Runes: *e.ChunkRunes - prefix,
			Count: counter(c.Tokenizer, e.Model),
			Model: embeddingsServed,
		},
		Dimensions: *e.Dimensions,
		Cache:      cfg.Cache,
		Bounds:     cfg.Bounds,
		Pending:    *s.Pending,
		EmbedBatch: *e.Batch,
		BatchBytes: *s.BatchBytes,
		Join:       join.Request{WordsPerSecond: *s.WordsPerSecond},
	}
	if v := s.Vision; v != nil {
		vs, err := resolved(docinfer.CapabilityVision)
		if err != nil {
			return nil, err
		}
		pc.Vision = &docinfer.Vision{
			Client:     c.Vision,
			Model:      v.Model,
			Served:     vs,
			Prompt:     v.Prompt,
			MaxTokens:  *v.MaxTokens,
			Reasoning:  v.Reasoning,
			MaxPixels:  *v.MaxPixels,
			LayerWords: *v.LayerWords,
			Native:     native(v.Native),
		}
	}
	if t := s.Transcription; t != nil {
		ts, err := resolved(docinfer.CapabilityTranscription)
		if err != nil {
			return nil, err
		}
		if c.Transcription.TranscriptionMode() == ai.TranscribeRoute {
			if n := *t.MaxTokens; n > 0 {
				return nil, refuse("transcription: max_tokens %d bounds a chat-mode reply, and the client's mode is route, which has no field for it", n)
			}
			if n := native(t.Native); len(n) > 0 {
				return nil, refuse("transcription: native %s is sent in a chat-mode request, and the client's mode is route, which sends no JSON body", n)
			}
		}
		pc.Transcription = &docinfer.Transcription{
			Client:    c.Transcription,
			Model:     t.Model,
			Served:    ts,
			Prompt:    t.Prompt,
			Language:  t.Language,
			MaxTokens: *t.MaxTokens,
			Native:    native(t.Native),
		}
	}
	if x := s.Extraction; x != nil {
		xs, err := resolved(docinfer.CapabilityExtraction)
		if err != nil {
			return nil, err
		}
		pc.Extraction = &docinfer.Extraction{
			Client:      c.Extraction,
			Model:       x.Model,
			Served:      xs,
			MaxTokens:   *x.MaxTokens,
			Reasoning:   x.Reasoning,
			Native:      native(x.Native),
			WindowRunes: *x.WindowRunes,
		}
	}
	return docinfer.New(pc)
}

func native(v jsontext.Value) jsontext.Value {
	var members map[string]jsontext.Value
	if jsonv2.Unmarshal(v, &members) == nil && len(members) == 0 {
		return nil
	}
	return v
}

func counter(client *ai.Client, model string) chunk.Counter {
	return func(ctx context.Context, text string) (int, error) {
		tokens, err := client.Tokenize(ctx, &ai.TokenizeRequest{Model: model, Text: text, InputType: ai.InputDocument})
		if err != nil {
			return 0, err
		}
		return tokens.Count, nil
	}
}
