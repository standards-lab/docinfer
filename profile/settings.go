package profile

import (
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"

	"github.com/standards-lab/go-ai/protocol"

	"github.com/standards-lab/docinfer"
)

// Settings is a profile's settings, each key the one cmd/docinfer's configuration names. Vision,
// Transcription, and Extraction are optional, a capability left unset skipped; Embeddings is
// required. Pending is a job's inputs admitted and not yet finished, BatchBytes a batch's
// marshalled JSON's cap in bytes, and WordsPerSecond the most words a second of speech holds,
// which joins window transcripts. Nothing has a default.
type Settings struct {
	Vision         *Vision        `json:"vision,omitzero"`
	Transcription  *Transcription `json:"transcription,omitzero"`
	Extraction     *Extraction    `json:"extraction,omitzero"`
	Embeddings     Embeddings     `json:"embeddings"`
	Pending        *int           `json:"pending"`
	BatchBytes     *int           `json:"batch_bytes"`
	WordsPerSecond *int           `json:"words_per_second"`
}

// Capability is what every capability names: endpoint, the endpoint its host builds the
// capability's client against, which errors name; model, the model its requests name; and
// served_model, the model the deployment serves under model, for an endpoint whose model listing
// gives no root.
type Capability struct {
	Endpoint    string `json:"endpoint"`
	Model       string `json:"model"`
	ServedModel string `json:"served_model,omitzero"`
}

// Vision is the vision capability's settings, docinfer.Vision's: prompt, max_tokens, at least 1,
// reasoning, empty sending none, native, empty or {} sending none, max_pixels, 0 sending every
// image as it is, and layer_words, 0 sending every page image.
type Vision struct {
	Capability
	Prompt     string                   `json:"prompt"`
	MaxTokens  *int                     `json:"max_tokens"`
	Reasoning  protocol.ReasoningEffort `json:"reasoning,omitzero"`
	Native     jsontext.Value           `json:"native,omitzero"`
	MaxPixels  *int                     `json:"max_pixels"`
	LayerWords *int                     `json:"layer_words"`
}

// Transcription is the transcription capability's settings, docinfer.Transcription's: prompt and
// language, empty sending none, max_tokens, 0 sending none, and native, empty or {} sending none.
// Its mode is its client's TranscriptionMode; in route mode [New] refuses a max_tokens above 0 or
// a native with members.
type Transcription struct {
	Capability
	Prompt    string         `json:"prompt,omitzero"`
	Language  string         `json:"language,omitzero"`
	MaxTokens *int           `json:"max_tokens"`
	Native    jsontext.Value `json:"native,omitzero"`
}

// Extraction is the extraction capability's settings, docinfer.Extraction's: max_tokens, at least
// 1, reasoning, empty sending none, native, empty or {} sending none, and window_runes, at least
// 1, the characters of a document or timeline extraction's window.
type Extraction struct {
	Capability
	MaxTokens   *int                     `json:"max_tokens"`
	Reasoning   protocol.ReasoningEffort `json:"reasoning,omitzero"`
	Native      jsontext.Value           `json:"native,omitzero"`
	WindowRunes *int                     `json:"window_runes"`
}

// Embeddings is the embeddings capability's settings and its chunker's: dimensions, the embedding
// space's; tokenizer, the endpoint whose tokenize route counts a chunk's tokens; batch, the texts
// one request carries; chunk_size, the most tokens a chunk holds, its document prefix included;
// and chunk_runes, the most characters a chunk holds, its document prefix included. Each count is
// at least 1. Its input-type conventions are its client's EmbeddingProfile.
type Embeddings struct {
	Capability
	Dimensions *int   `json:"dimensions"`
	Tokenizer  string `json:"tokenizer"`
	Batch      *int   `json:"batch"`
	ChunkSize  *int   `json:"chunk_size"`
	ChunkRunes *int   `json:"chunk_runes"`
}

// Decode returns the Settings data holds, a JSON object, refusing a member Settings doesn't name
// and settings [Settings.Validate] refuses.
func Decode(data []byte) (Settings, error) {
	var s Settings
	if err := jsonv2.Unmarshal(data, &s, jsonv2.RejectUnknownMembers(true)); err != nil {
		return Settings{}, fmt.Errorf("profile: %w: %w", docinfer.ErrRequest, err)
	}
	if err := s.Validate(); err != nil {
		return Settings{}, err
	}
	return s, nil
}

// Validate refuses, matching docinfer.ErrRequest and naming each by its key, every required value
// s leaves unset and every value out of range: each set capability's endpoint and model; vision's
// prompt, max_tokens, max_pixels, and layer_words; transcription's max_tokens; extraction's
// max_tokens and window_runes; the embeddings' dimensions, tokenizer, batch, chunk_size, and
// chunk_runes; and pending, batch_bytes, and words_per_second. A count is at least 1, but
// max_pixels, layer_words, and transcription's max_tokens, which are at least 0.
func (s Settings) Validate() error {
	var errs []error
	if v := s.Vision; v != nil {
		errs = append(errs, v.validate("vision"),
			atLeast("vision", "max_tokens", v.MaxTokens, 1),
			atLeast("vision", "max_pixels", v.MaxPixels, 0),
			atLeast("vision", "layer_words", v.LayerWords, 0))
		if v.Prompt == "" {
			errs = append(errs, refuse("vision: no prompt"))
		}
	}
	if t := s.Transcription; t != nil {
		errs = append(errs, t.validate("transcription"),
			atLeast("transcription", "max_tokens", t.MaxTokens, 0))
	}
	if x := s.Extraction; x != nil {
		errs = append(errs, x.validate("extraction"),
			atLeast("extraction", "max_tokens", x.MaxTokens, 1),
			atLeast("extraction", "window_runes", x.WindowRunes, 1))
	}
	e := s.Embeddings
	errs = append(errs, e.validate("embeddings"),
		atLeast("embeddings", "dimensions", e.Dimensions, 1),
		atLeast("embeddings", "batch", e.Batch, 1),
		atLeast("embeddings", "chunk_size", e.ChunkSize, 1),
		atLeast("embeddings", "chunk_runes", e.ChunkRunes, 1))
	if e.Tokenizer == "" {
		errs = append(errs, refuse("embeddings: no tokenizer"))
	}
	errs = append(errs,
		atLeast("", "pending", s.Pending, 1),
		atLeast("", "batch_bytes", s.BatchBytes, 1),
		atLeast("", "words_per_second", s.WordsPerSecond, 1))
	return errors.Join(errs...)
}

func (c Capability) validate(name string) error {
	var errs []error
	if c.Endpoint == "" {
		errs = append(errs, refuse("%s: no endpoint", name))
	}
	if c.Model == "" {
		errs = append(errs, refuse("%s: no model", name))
	}
	return errors.Join(errs...)
}

func atLeast(section, key string, v *int, least int) error {
	prefix := ""
	if section != "" {
		prefix = section + ": "
	}
	switch {
	case v == nil:
		return refuse("%sno %s", prefix, key)
	case *v < least:
		return refuse("%s%s %d is below %d", prefix, key, *v, least)
	}
	return nil
}

func refuse(format string, args ...any) error {
	return fmt.Errorf("profile: %w: %s", docinfer.ErrRequest, fmt.Sprintf(format, args...))
}
