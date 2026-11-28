package outdir

import (
	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/rundir/schema"

	"github.com/standards-lab/docinfer"
	"github.com/standards-lab/docinfer/chunk"
)

// Manifest is a run's manifest, runs/<run>/manifest.json, one JSON object, written as the run
// starts and again as it ends:
//
//   - "schema": the directory's schema, [Version], so a manifest read alone names it
//   - "run": the run's id, a version 7 UUID, which sorts by when the run started
//   - "docinfer": the [Build] that wrote it
//   - "start": when the run started, a schema.Time, in UTC
//   - "end": when it ended, a schema.Time, in UTC; absent while it runs, or when it never ended,
//     and present once its records are whole
//   - "docproc": the docproc run it read, [Docproc]
//   - "inputs": the docproc run's inputs, [Input], in the order their record closed each
//   - "capabilities": each capability the run ran, [Capability], vision, transcription,
//     extraction, then embeddings
//   - "skipped": each capability it didn't run, [Skipped]; absent when none
//   - "embedding_space": the embedding space of the vectors it made or read, [Space]; absent
//     until it ends
//   - "counts": the pipeline's docinfer.Counts; zero for a run that stopped
//   - "outputs": the docproc run's output lines read
//   - "error": the error that stopped the run; absent when none did
type Manifest struct {
	Schema       string          `json:"schema"`
	Run          string          `json:"run"`
	Docinfer     Build           `json:"docinfer"`
	Start        schema.Time     `json:"start"`
	End          schema.Time     `json:"end,omitzero"`
	Docproc      Docproc         `json:"docproc"`
	Inputs       []Input         `json:"inputs"`
	Capabilities []Capability    `json:"capabilities"`
	Skipped      []Skipped       `json:"skipped,omitempty"`
	Space        *Space          `json:"embedding_space,omitempty"`
	Counts       docinfer.Counts `json:"counts"`
	Outputs      int             `json:"outputs"`
	Error        string          `json:"error,omitzero"`
}

// Build is what wrote a run, from the command's build info: "command", the docinfer command's
// module version, and "library", the docinfer module's it was built with, each "(devel)" when
// the build info names none.
type Build struct {
	Command string `json:"command"`
	Library string `json:"library"`
}

// Docproc is the docproc run a run read: "dir", docproc's run directory relative to this one,
// with slashes, recorded once, so a moved pair of directories keeps it, and "run", the run's id.
type Docproc struct {
	Dir string `json:"dir"`
	Run string `json:"run"`
}

// Input is one input of the docproc run: "path", as docproc's record gives it, and "sha256", its
// depth-0 part's, or "error", why its dispatch stopped before its part's line.
type Input struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256,omitzero"`
	Error  string `json:"error,omitzero"`
}

// Capability is one capability a run ran: its docinfer.Identity, inline, the key its held results
// are under, whose "model" is the model the deployment serves; "request_model", the model its
// requests named, which the deployment serves as "model"; and the endpoint it sent to, by name,
// base URL, and format, with its bounds: "concurrency", its requests in flight, and for
// embeddings "batch", the texts per request, and "chunk_size", the tokens per chunk; and for vision
// "layer_words", docinfer.Profile's VisionLayerWords, kept out of the identity: a page image skips
// vision when its page's text layer holds at least that many words, omitted when it is 0 and none
// skips.
type Capability struct {
	docinfer.Identity
	RequestModel string `json:"request_model"`
	Endpoint     string `json:"endpoint"`
	BaseURL      string `json:"base_url"`
	Format       string `json:"format"`
	Concurrency  int    `json:"concurrency"`
	Batch        int    `json:"batch,omitzero"`
	ChunkSize    int    `json:"chunk_size,omitzero"`
	LayerWords   int    `json:"layer_words,omitzero"`
}

// Skipped is a capability a run didn't run: "name", the capability, and "reason", why.
type Skipped struct {
	Name   docinfer.Capability `json:"name"`
	Reason string              `json:"reason"`
}

// UnitTolerance is how far a vector's Euclidean norm may be from 1 for [Space].Unit to count it
// unit length: |norm - 1| <= UnitTolerance.
const UnitTolerance = 1e-3

// Space is the embedding space of a run's vectors, as the endpoint returned them, never
// normalized: the endpoint and model that made them, their dimensions, how many distinct vectors
// were measured, whether every one is unit length within Tolerance, the least and greatest norm,
// and the chunker's settings, chunk.Splitter's Settings, the texts were cut with. Unit is false
// when no vector was measured.
type Space struct {
	Endpoint   string  `json:"endpoint"`
	Model      string  `json:"model"`
	Dimensions int     `json:"dimensions"`
	Vectors    int     `json:"vectors"`
	Unit       bool    `json:"unit_length"`
	Tolerance  float64 `json:"tolerance"`
	MinNorm    float64 `json:"min_norm"`
	MaxNorm    float64 `json:"max_norm"`
	Chunker    string  `json:"chunker"`
}

// Line is one line of a run's chunk record, runs/<run>/chunks.jsonl: a batch's chunk with the
// part and source its indexes name inline, since the record holds no batches, and the input its
// part was found in. Its JSON is {input, digest, text, span, index, vector, error, part, source},
// its Source's "part" always 0, since Part is inline. The same chunk in two places is two lines.
type Line struct {
	// Input is the path of the input the chunk's part was found in, as docproc's record gives it,
	// an [Input]'s Path.
	Input  string `json:"input"`
	Digest string `json:"digest"`
	chunk.Chunk
	Index int `json:"index"`
	// Vector is the chunk's embedding, standard base64 of little-endian float32s; absent when
	// the chunk was refused or failed, and left out by [Dir.Lines].
	Vector string          `json:"vector,omitzero"`
	Error  string          `json:"error,omitzero"`
	Part   dispatch.Part   `json:"part"`
	Source docinfer.Source `json:"source"`
}

// Inference is one line of a run's inferences record, runs/<run>/inferences.jsonl: a
// docinfer.Inference, inline, and the input its part was found in. Its JSON is the Inference's
// with "input" first; its Text is never written, being in [Dir.Texts] under its TextSHA256.
type Inference struct {
	// Input is the path of the input the inference's part was found in, as docproc's record
	// gives it, an [Input]'s Path.
	Input string `json:"input"`
	docinfer.Inference
}

func lines(b docinfer.Batch, inputs []string) []Line {
	out := make([]Line, len(b.Chunks))
	for i, c := range b.Chunks {
		src := b.Sources[c.Source]
		part := b.Parts[src.Part]
		input := inputs[src.Part]
		src.Part = 0
		out[i] = Line{Input: input, Digest: c.Digest, Chunk: c.Chunk, Index: c.Index, Vector: c.Vector, Error: c.Error, Part: part, Source: src}
	}
	return out
}
