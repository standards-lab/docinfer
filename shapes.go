package docinfer

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"

	"github.com/standards-lab/docinfer/chunk"
	"github.com/standards-lab/docinfer/join"
	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/go-ai"
)

// Version is the version of docinfer's shapes, grown by addition only: of the shapes below,
// [Held], job's, chunk's, and join's. It includes dispatch.Version "1", whose shapes [Input],
// [Batch], and [Source] carry, so a break there is a break here. [Key] hashes it, so a new shape
// misses every value held under the old rather than decoding wrong.
const Version = "1"

// Capability names one of the four things docinfer asks a model for, or chunking, whose spans it
// holds by the host's count, and is the first segment of each of its [Key]s.
type Capability string

// The capabilities.
const (
	CapabilityVision        Capability = "vision"
	CapabilityTranscription Capability = "transcription"
	CapabilityExtraction    Capability = "extraction"
	CapabilityEmbeddings    Capability = "embeddings"
	CapabilityChunk         Capability = "chunk"
)

// Identity is what a capability's results are keyed by besides their content: the capability, the
// model, and the settings the request is built with.
type Identity struct {
	Capability Capability `json:"capability"`
	// Model is the model the deployment serves, the capability's Served, not the one its request
	// names, so a model swapped in under the same name misses.
	Model string `json:"model"`
	// Settings is the JSON of the capability's settings, its first member "request", the request's
	// version, changed when the request's construction changes; the embeddings' is "" when the
	// client declares no document convention, since a vector then depends on the model and the
	// chunk's text alone. Chunking's is the chunk.Splitter's Settings, led by its "version", and
	// its Model the Splitter's, the served model whose tokenizer counts.
	Settings string `json:"settings"`
}

// Profile is a pipeline's resolved identity: the served models, settings, dimensions, and chunker,
// known before any job runs.
type Profile struct {
	Vision *Identity `json:"vision,omitempty"`
	// VisionLayerWords is vision's LayerWords, kept out of its identity: a page image skips
	// vision when the page's text layer holds at least this many words; 0, or no vision, skips
	// none.
	VisionLayerWords int       `json:"vision_layer_words,omitempty"`
	Transcription    *Identity `json:"transcription,omitempty"`
	Extraction       *Identity `json:"extraction,omitempty"` // its settings less prompt and schema
	Embeddings       Identity  `json:"embeddings"`
	// EmbeddingQuery is the query convention the embeddings' client declares, which a consumer
	// applies to its search queries so they land in the space the chunks were embedded in; nil
	// when the client declares none, and the queries go as they are.
	EmbeddingQuery *ai.InputConvention `json:"embedding_query,omitempty"`
	Dimensions     int                 `json:"dimensions"` // the embedding space's, checked per vector
	Chunker        string              `json:"chunker"`    // the chunk.Splitter's Settings
}

// Usage is the tokens a model's request took, less their total.
type Usage struct {
	Input  int `json:"input"`
	Output int `json:"output"`
}

// Outcome is what an [Inference] came to.
type Outcome string

// The outcomes: one per capability's success, then the three every capability shares.
const (
	OutcomeDescribed   Outcome = "described"
	OutcomeTranscribed Outcome = "transcribed"
	OutcomeExtracted   Outcome = "extracted"
	OutcomeIncomplete  Outcome = "incomplete"
	OutcomeRefused     Outcome = "refused"
	OutcomeFailed      Outcome = "failed"
)

// Scope is an extraction's span when it is wider than one source text.
type Scope string

// The scopes an [Inference] carries.
const (
	ScopeDocument Scope = "document" // over the job's whole text
	ScopeTimeline Scope = "timeline" // over a video's frames and transcript
)

// Inference is one text a model made, or the cache held: an image's description, a part's joined
// transcript, or an extraction: a source text's, or, by its Scope, the job's or a video's.
type Inference struct {
	Capability Capability `json:"capability"` // vision, transcription, or extraction
	// Scope is a document or timeline extraction's; "" for one of a single part or source text.
	Scope  Scope  `json:"scope,omitempty"`
	Part   string `json:"part"` // its part's ID
	Origin string `json:"-"`    // local: its input's [Input].Origin
	// ContentSHA256 is the image's, the windows' in order, "" for one never cut, or the source
	// text's.
	ContentSHA256 []string `json:"content_sha256"`
	Outcome       Outcome  `json:"outcome"`
	// Reason is an image's or extraction's: why it is incomplete, refused, or failed.
	Reason string `json:"reason,omitzero"`
	// Failed is a transcript's windows not transcribed, by index: refused, failed, incomplete, or
	// never cut.
	Failed []int `json:"failed,omitempty"`
	// Held is true when the text was read from the cache or a leader's request, not asked of a
	// model.
	Held       bool        `json:"held,omitzero"`
	TextSHA256 string      `json:"text_sha256,omitzero"` // the text's, in docproc's Content
	Text       string      `json:"-"`                    // local: job puts it in Content
	Seams      []join.Seam `json:"seams,omitempty"`      // a transcript's, as join made them
	Usage      Usage       `json:"usage,omitzero"`
	TookMS     int64       `json:"took_ms"`
}

// Progress is an extraction's text since its last progress.
type Progress struct {
	ContentSHA256 string `json:"content_sha256"` // the extraction call's input text's
	Delta         string `json:"delta"`
}

// Batch holds its chunks, and each part and source text they come from once, by index.
type Batch struct {
	Parts   []dispatch.Part `json:"parts"` // provenance: each, its parent and depth, where found
	Sources []Source        `json:"sources"`
	Chunks  []Chunk         `json:"chunks"`
}

// Digest is the lowercase hex SHA-256 over its chunks' Digests, in order, each length-prefixed as
// [Key]'s parts are: derived, so no field holds it.
func (b Batch) Digest() string {
	parts := make([]string, len(b.Chunks))
	for i, c := range b.Chunks {
		parts[i] = c.Digest
	}
	return hashParts(parts...)
}

// Source is the text a chunk is cut from, and where it sits.
type Source struct {
	// Kind is output, metadata, vision, or transcript: metadata for an input's metadata text, its
	// Output nil, and for a metadata record's text, its Output the record output.
	Kind   string `json:"kind"`
	Origin string `json:"-"` // local: its input's [Input].Origin
	// SHA256 is the text's, a metadata record's its rendered text's; an output's is its
	// Output.SHA256, held there alone.
	SHA256     string             `json:"sha256,omitzero"`
	Incomplete string             `json:"incomplete,omitzero"` // why its inference is incomplete
	Part       int                `json:"part"`                // an index into the batch's Parts
	Stage      *dispatch.Identity `json:"stage,omitempty"`
	// Output is the output it is, or the image or first window its inference read.
	Output *dispatch.Output `json:"output,omitempty"`
}

// Chunk is one chunk of a source text and its embedding. It embeds chunk.Chunk, whose fields
// encode inline, so its JSON is {digest, text, span, index, source, vector, error}.
type Chunk struct {
	// Digest is the SHA-256 of Text: the per-item digest, and its embedding's key content.
	Digest      string `json:"digest"`
	chunk.Chunk        // inline: text, span
	Index       int    `json:"index"`           // within its source text
	Source      int    `json:"source"`          // an index into the batch's Sources
	Vector      string `json:"vector,omitzero"` // standard base64 of little-endian float32s
	Error       string `json:"error,omitzero"`  // why the endpoint refused it, or it failed
}

// DecodeVector is the one reader of [Chunk].Vector and [Held].Vector: standard base64, padded, of
// each float32's IEEE 754 bits, little-endian, in order. An empty string, a string that isn't
// standard base64, and bytes that aren't a whole number of float32s are errors.
func DecodeVector(s string) ([]float32, error) {
	if s == "" {
		return nil, fmt.Errorf("docinfer: decode vector: empty")
	}
	b, err := base64.StdEncoding.Strict().DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("docinfer: decode vector: %w", err)
	}
	if len(b)%4 != 0 {
		return nil, fmt.Errorf("docinfer: decode vector: %d bytes isn't a whole number of float32s", len(b))
	}
	v := make([]float32, len(b)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[4*i:]))
	}
	return v, nil
}

// Counts is one job's, counting occurrences: each image, window, source text, and chunk as often
// as the job holds it, and the cache hits among them.
type Counts struct {
	Texts    int `json:"texts"`    // text and Markdown outputs chunked
	Metadata int `json:"metadata"` // metadata texts: the input's and each metadata record's
	Embedded int `json:"embedded"` // chunks with a vector
	Rejected int `json:"rejected"` // chunks refused, or failed on the last delivery
	Held     int `json:"held"`     // of those, read from the cache or another call's request
	Requests int `json:"requests"` // embeddings requests sent
	Images   int `json:"images"`   // image outputs, vision run or not
	// Vision is nil when vision is skipped.
	Vision        *VisionCounts        `json:"vision,omitempty"`
	Windows       int                  `json:"windows"`
	Transcription *TranscriptionCounts `json:"transcription,omitempty"` // nil when skipped
	Extraction    *ExtractionCounts    `json:"extraction,omitempty"`    // nil when none was asked
	Tokens        Tokens               `json:"tokens"`                  // this job's requests only
}

// VisionCounts counts each image under its outcome, or as Skipped; Empty counts those of the
// Incomplete with no text.
type VisionCounts struct {
	Described  int `json:"described"`
	Incomplete int `json:"incomplete"`
	Empty      int `json:"empty"`
	Refused    int `json:"refused"`
	Failed     int `json:"failed"`
	Held       int `json:"held"`
	Requests   int `json:"requests"`
	// Skipped counts the page images sent no request, their page's text layer holding at least
	// Vision.LayerWords words.
	Skipped int `json:"skipped"`
}

// TranscriptionCounts counts each window under its outcome, or as Unplaced, the transcripts
// joined, and their seams by whether they matched. An unplaced window is one [Pipeline.Run] places
// in no transcript, its group's index already placed, past its group's bound, or its group already
// closed: it is never transcribed. A window placed before its group's bound was known, and past it
// once the group is joined, such as an index at or past a later plan's count, is moved to Unplaced
// from its outcome, and from Held, when the group is joined, its Requests and tokens kept, since
// its text is in no transcript. So Transcribed, Incomplete, Refused, Failed, and Unplaced add up to
// Counts.Windows, and Held counts those of the first four read from the cache or another call.
type TranscriptionCounts struct {
	Transcribed    int `json:"transcribed"`
	Incomplete     int `json:"incomplete"`
	Refused        int `json:"refused"`
	Failed         int `json:"failed"`
	Unplaced       int `json:"unplaced"`
	Held           int `json:"held"`
	Requests       int `json:"requests"`
	Transcripts    int `json:"transcripts"`
	SeamsMatched   int `json:"seams_matched"`
	SeamsUnmatched int `json:"seams_unmatched"`
}

// ExtractionCounts counts each source text under its outcome.
type ExtractionCounts struct {
	Extracted  int `json:"extracted"`
	Incomplete int `json:"incomplete"`
	Refused    int `json:"refused"`
	Failed     int `json:"failed"`
	Held       int `json:"held"`
	Requests   int `json:"requests"`
}

// Tokens is a job's token usage per capability, each nil when that capability is skipped or not
// asked for.
type Tokens struct {
	Vision        *Usage `json:"vision,omitempty"`
	Transcription *Usage `json:"transcription,omitempty"`
	Extraction    *Usage `json:"extraction,omitempty"`
	Embeddings    Usage  `json:"embeddings"`
}

func hashParts(parts ...string) string {
	h := sha256.New()
	var n [8]byte
	for _, p := range parts {
		binary.BigEndian.PutUint64(n[:], uint64(len(p)))
		h.Write(n[:])
		h.Write([]byte(p))
	}
	return hex.EncodeToString(h.Sum(nil))
}
