package docinfer

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/json/jsontext"
	"fmt"
	"strings"

	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/protocol"
)

const (
	visionRequest        = "2"
	transcriptionRequest = "2"
	extractionRequest    = "2"
	embeddingRequest     = "1"
)

// Vision is how a [Pipeline] describes images: a client its host built, and the request's
// settings.
type Vision struct {
	Client *ai.Client
	Model  string // the model the request names
	// Served is the model the deployment serves under Model, which keys results; the host resolves
	// it.
	Served    string
	Prompt    string                   // the user message's InputText beside the image
	MaxTokens int                      // at least 1: the request's MaxOutputTokens
	Reasoning protocol.ReasoningEffort // "" sends none
	// Native is the native tier, sent as every request's protocol.Request.Native: a JSON object
	// whose members the endpoint defines, such as temperature. Empty sends none; [New] refuses
	// one that isn't a JSON object with unique member names.
	Native jsontext.Value
}

type visionSettings struct {
	Request   string                   `json:"request"`
	Prompt    string                   `json:"prompt"`
	MaxTokens int                      `json:"max_tokens"`
	Effort    protocol.ReasoningEffort `json:"effort"`
	Native    jsontext.Value           `json:"native,omitempty"`
}

// Identity returns vision's identity: its capability, Served, and settings, the JSON
// {"request","prompt","max_tokens","effort","native"} in that member order, "native" there only
// when Native is set, canonical: its whitespace stripped, its strings in their shortest form, and
// each object's members sorted by name, its numbers as given. Native must be one [New] accepts.
func (v Vision) Identity() Identity {
	return Identity{
		Capability: CapabilityVision,
		Model:      v.Served,
		Settings:   encodeSettings(visionSettings{visionRequest, v.Prompt, v.MaxTokens, v.Reasoning, canonical(v.Native)}),
	}
}

// Transcription is how a [Pipeline] transcribes audio windows: a client its host built, whose
// TranscriptionMode keys the cache, and the request's settings.
type Transcription struct {
	Client *ai.Client
	Model  string // the model the request names
	// Served is the model the deployment serves under Model, which keys results; the host resolves
	// it.
	Served           string
	Prompt, Language string // "" sends none
	// MaxTokens is the request's MaxOutputTokens, which bounds the reply in chat mode; 0 sends
	// none, and go-ai refuses any other in route mode, where the route's max_tokens goes through
	// its Native.
	MaxTokens int
}

type transcriptionSettings struct {
	Request   string               `json:"request"`
	Mode      ai.TranscriptionMode `json:"mode"`
	Prompt    string               `json:"prompt"`
	Language  string               `json:"language"`
	MaxTokens int                  `json:"max_tokens"`
}

// Identity returns transcription's identity: its capability, Served, and settings, the JSON
// {"request","mode","prompt","language","max_tokens"} in that member order. The mode is the Client's
// TranscriptionMode, "route" or "chat", read from the client so the key can't disagree with how
// the client transcribes; Client must be set.
func (t Transcription) Identity() Identity {
	return Identity{
		Capability: CapabilityTranscription,
		Model:      t.Served,
		Settings: encodeSettings(transcriptionSettings{
			transcriptionRequest, t.Client.TranscriptionMode(), t.Prompt, t.Language, t.MaxTokens,
		}),
	}
}

// Extraction is how a [Pipeline] extracts by a run's [Extract]: a client its host built, and the
// request's settings. The prompt and schema are each run's own.
type Extraction struct {
	Client *ai.Client
	Model  string // the model the request names
	// Served is the model the deployment serves under Model, which keys results; the host resolves
	// it.
	Served    string
	MaxTokens int                      // at least 1: the request's MaxOutputTokens
	Reasoning protocol.ReasoningEffort // "" sends none
	// Native is the native tier, sent as every request's protocol.Request.Native: a JSON object
	// whose members the endpoint defines, such as temperature. Empty sends none; [New] refuses
	// one that isn't a JSON object with unique member names.
	Native jsontext.Value
}

// Extract is a caller's extraction: a prompt and the JSON schema its reply follows.
type Extract struct {
	Name   string         `json:"name"`   // the schema's, 1 to 64 letters, digits, _ and -
	Prompt string         `json:"prompt"` // the request's instructions
	Schema jsontext.Value `json:"schema"` // a JSON Schema object
}

type extractionSettings struct {
	Request      string                   `json:"request"`
	Name         string                   `json:"name"`
	PromptSHA256 string                   `json:"prompt_sha256"`
	SchemaSHA256 string                   `json:"schema_sha256"`
	MaxTokens    int                      `json:"max_tokens"`
	Effort       protocol.ReasoningEffort `json:"effort"`
	Native       jsontext.Value           `json:"native,omitempty"`
}

type extractionProfile struct {
	Request   string                   `json:"request"`
	MaxTokens int                      `json:"max_tokens"`
	Effort    protocol.ReasoningEffort `json:"effort"`
	Native    jsontext.Value           `json:"native,omitempty"`
}

// Identity returns extraction's identity for e: its capability, Served, and settings, the JSON
// {"request","name","prompt_sha256","schema_sha256","max_tokens","effort","native"} in that member
// order, each SHA-256 lowercase hex of the prompt's bytes or the schema's as given, and "native"
// there only when Native is set, canonical as in [Vision.Identity]. An extraction's identity is
// per Extract, so a changed prompt or schema misses. Native must be one [New] accepts.
func (x Extraction) Identity(e Extract) Identity {
	return Identity{
		Capability: CapabilityExtraction,
		Model:      x.Served,
		Settings: encodeSettings(extractionSettings{
			extractionRequest, e.Name, sha256Hex([]byte(e.Prompt)), sha256Hex(e.Schema), x.MaxTokens, x.Reasoning,
			canonical(x.Native),
		}),
	}
}

func (x Extraction) profile() Identity {
	return Identity{
		Capability: CapabilityExtraction,
		Model:      x.Served,
		Settings:   encodeSettings(extractionProfile{extractionRequest, x.MaxTokens, x.Reasoning, canonical(x.Native)}),
	}
}

// Embeddings is how a [Pipeline] embeds chunks: a client its host built, and its models. The host
// declares the model's input-type conventions on the client, in ai.Options.Embedding; docinfer
// sends every chunk as ai.InputDocument, so the client applies the document convention, the
// identity keys on it, and the [Profile] carries the query convention a consumer applies to its
// search queries.
type Embeddings struct {
	Client *ai.Client
	Model  string // the model the request names
	// Served is the model the deployment serves under Model, which keys results; the host resolves
	// it.
	Served string
}

type embeddingSettings struct {
	Request   string         `json:"request"`
	InputType ai.InputType   `json:"input_type"`
	Prefix    string         `json:"prefix"`
	Native    jsontext.Value `json:"native,omitempty"`
}

// Identity returns the embeddings' identity: its capability, Served, and settings, read from the
// document convention the Client's EmbeddingProfile declares, so the key can't disagree with what
// the client sends. With none declared, a chunk goes as it is and the settings are "", since a
// vector depends on the model and the chunk's text alone. With one, they are the JSON
// {"request","input_type","prefix","native"} in that member order, "input_type" "document",
// "native" there only when the convention's Native is set, canonical as in [Vision.Identity]. The
// query convention isn't keyed, since docinfer embeds no queries. Client must be set.
func (e Embeddings) Identity() Identity {
	id := Identity{Capability: CapabilityEmbeddings, Model: e.Served}
	if conv, ok := e.Client.EmbeddingProfile().InputTypes[ai.InputDocument]; ok {
		id.Settings = encodeSettings(embeddingSettings{embeddingRequest, ai.InputDocument, conv.Prefix, canonical(conv.Native)})
	}
	return id
}

func (e Embeddings) query() *ai.InputConvention {
	conv, ok := e.Client.EmbeddingProfile().InputTypes[ai.InputQuery]
	if !ok {
		return nil
	}
	return &conv
}

func encodeSettings(s any) string {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		panic("docinfer: encode settings: " + err.Error())
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func canonical(v jsontext.Value) jsontext.Value {
	if len(v) == 0 {
		return nil
	}
	c := v.Clone()
	if err := c.Canonicalize(jsontext.CanonicalizeRawInts(false), jsontext.CanonicalizeRawFloats(false)); err != nil {
		return v
	}
	return c
}

func probeNative(field string, v jsontext.Value) error {
	probe := &protocol.Request{
		Model: "probe",
		Input: []protocol.Item{protocol.Message{
			Role: protocol.RoleUser, Content: []protocol.ContentPart{protocol.InputText{Text: "probe"}},
		}},
		Native: v,
	}
	if err := probe.Validate(); err != nil {
		return fmt.Errorf("docinfer: New: %w: %s: %w", ErrRequest, field, err)
	}
	return nil
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
