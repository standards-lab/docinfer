package docinfer

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/json/jsontext"
	"strings"

	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/protocol"
)

const (
	visionRequest        = "1"
	transcriptionRequest = "1"
	extractionRequest    = "1"
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
}

type visionSettings struct {
	Request   string                   `json:"request"`
	Prompt    string                   `json:"prompt"`
	MaxTokens int                      `json:"max_tokens"`
	Effort    protocol.ReasoningEffort `json:"effort"`
}

// Identity returns vision's identity: its capability, Served, and settings, the JSON
// {"request","prompt","max_tokens","effort"} in that member order.
func (v Vision) Identity() Identity {
	return Identity{
		Capability: CapabilityVision,
		Model:      v.Served,
		Settings:   encodeSettings(visionSettings{visionRequest, v.Prompt, v.MaxTokens, v.Reasoning}),
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
}

type transcriptionSettings struct {
	Request  string               `json:"request"`
	Mode     ai.TranscriptionMode `json:"mode"`
	Prompt   string               `json:"prompt"`
	Language string               `json:"language"`
}

// Identity returns transcription's identity: its capability, Served, and settings, the JSON
// {"request","mode","prompt","language"} in that member order. The mode is the Client's
// TranscriptionMode, "route" or "chat", read from the client so the key can't disagree with how
// the client transcribes; Client must be set.
func (t Transcription) Identity() Identity {
	return Identity{
		Capability: CapabilityTranscription,
		Model:      t.Served,
		Settings: encodeSettings(transcriptionSettings{
			transcriptionRequest, t.Client.TranscriptionMode(), t.Prompt, t.Language,
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
}

type extractionProfile struct {
	Request   string                   `json:"request"`
	MaxTokens int                      `json:"max_tokens"`
	Effort    protocol.ReasoningEffort `json:"effort"`
}

// Identity returns extraction's identity for e: its capability, Served, and settings, the JSON
// {"request","name","prompt_sha256","schema_sha256","max_tokens","effort"} in that member order,
// each SHA-256 lowercase hex of the prompt's bytes or the schema's as given. An extraction's
// identity is per Extract, so a changed prompt or schema misses.
func (x Extraction) Identity(e Extract) Identity {
	return Identity{
		Capability: CapabilityExtraction,
		Model:      x.Served,
		Settings: encodeSettings(extractionSettings{
			extractionRequest, e.Name, sha256Hex([]byte(e.Prompt)), sha256Hex(e.Schema), x.MaxTokens, x.Reasoning,
		}),
	}
}

func (x Extraction) profile() Identity {
	return Identity{
		Capability: CapabilityExtraction,
		Model:      x.Served,
		Settings:   encodeSettings(extractionProfile{extractionRequest, x.MaxTokens, x.Reasoning}),
	}
}

// Embeddings is how a [Pipeline] embeds chunks: a client its host built, and its models.
type Embeddings struct {
	Client *ai.Client
	Model  string // the model the request names
	// Served is the model the deployment serves under Model, which keys results; the host resolves
	// it.
	Served string
}

// Identity returns the embeddings' identity: its capability, Served, and settings "", since a
// vector depends on the model and the chunk's text alone.
func (e Embeddings) Identity() Identity {
	return Identity{Capability: CapabilityEmbeddings, Model: e.Served}
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

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
