// Package docinfer infers over what docproc extracts: it describes images, transcribes audio,
// chunks and embeds text, extracts by a caller's prompt and JSON schema, and holds each model
// result in a [Cache].
//
// The cache holds one result per [Key], a [Held] as JSON. A key hashes [Version], the result's
// [Identity], and its content's SHA-256, so a new shape of a held value, a new model, or new
// settings misses rather than decoding wrong. A held value that doesn't decode as Held is a miss:
// the model is asked again, and its Put replaces the value. docinfer never removes a value.
// [FileCache] and [MemoryCache] are the module's caches, and package docinfertest holds the
// contract every cache passes.
//
// A [Pipeline] is built by [New] from a [Config]: a go-ai client and settings per capability
// ([Vision], [Transcription], [Extraction], and [Embeddings]), the chunker, the cache, and a
// [Bounds] from [NewBounds], which caps each capability's requests in flight across every
// pipeline built on it. Nothing has a default. Each capability's [Identity] holds its settings as
// one JSON encoding led by its request's version, and [Pipeline.Profile] reports them.
//
// The package exports:
//
//   - [Config] and [New], which builds a [Pipeline]; [Pipeline.Profile], its resolved identity
//   - [Bounds] and [NewBounds], the requests in flight per capability
//   - [Vision], [Transcription], [Extraction], and [Embeddings], each capability's client and
//     settings, and each one's Identity; [Extract], a caller's prompt and schema
//   - [Cache], its Get and Put, and its two implementations, [FileCache] from [NewFileCache] and
//     [MemoryCache] from [NewMemoryCache]
//   - [Key], a result's cache key, and [Held], a held value's JSON
//   - [Version], the version of the shapes, which Key hashes
//   - [Capability] and its four constants, [CapabilityVision], [CapabilityTranscription],
//     [CapabilityExtraction], and [CapabilityEmbeddings]
//   - [Identity], a result's capability, model, and settings, and [Profile], a pipeline's
//   - [Inference], one text a model made, its [Outcome] among the six Outcome constants, and its
//     [Usage]; [Progress], an extraction's text as it streams
//   - [Batch], its parts, [Source]s, and [Chunk]s, and its derived [Batch.Digest]; [DecodeVector],
//     the one reader of a chunk's or a held value's vector
//   - [Counts], one job's, with [VisionCounts], [TranscriptionCounts], [ExtractionCounts], and
//     [Tokens]
//   - [ErrRequest], the cause of a refused request
package docinfer
