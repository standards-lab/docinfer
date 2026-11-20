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
// [Pipeline.Run] runs one job: it reads the job's [Input]s, as many admitted at once as
// Config.Pending allows, and yields each [Result] as it is made, its batches of embedded chunks
// as their embeddings complete and its [Counts] last. It echoes each input as [Result.Input] as
// it admits it, in pull order, before anything derived from it, passing through an input that
// needs no work, and yields [Result.Settled], the input's position in pull order, once
// everything derived from it is out. Every result is held before anything derived from it is
// yielded. A go-ai failure maps by one table: a refused status is the item's outcome, held; a
// permanent or transient failure stops the job, [Permanent] telling them apart for the host; and
// a failed call stops it too, but on the job's last delivery ([Options].Final) its items are
// recorded failed, never held, and the job goes on.
//
// The consumer's contract: Run yields a part's results as they are made, which may be before the
// part's own event, whatever that event comes to, since holding a part's outputs until its event
// would cost the order as made. A consumer drops a quarantined or failed part's inferences and
// chunks by that part's own event.
//
// A request's time is the host's: it bounds each request with its client's transport's
// ResponseHeaderTimeout, and extraction's stream with go-ai's Options.StreamIdleTimeout, which
// ends a stalled stream without cutting off a long one. docinfer adds no deadline and no default
// of its own; Run's context is the job's, and its cancellation ends the job.
//
// The package exports:
//
//   - [Config] and [New], which builds a [Pipeline]; [Pipeline.Profile], its resolved identity
//   - [Pipeline.Run], one job's run, over [Input]s, with [Options], yielding [Result]s, among
//     them [Result.Input], each input's echo, and [Result.Settled], each input's settlement;
//     [MetadataText], the one builder of an input's metadata text; [MissingError], a content
//     miss, with its [MissingError.Error] and [MissingError.Unwrap]; and [Permanent], which tells
//     an error no redelivery changes
//   - [Bounds] and [NewBounds], the requests in flight per capability
//   - [Vision], [Transcription], [Extraction], and [Embeddings], each capability's client and
//     settings, and each one's Identity, [Vision.Identity], [Transcription.Identity],
//     [Extraction.Identity], and [Embeddings.Identity]; [Extract], a caller's prompt and schema
//   - [Cache], its Get and Put, and its two implementations, [FileCache] from [NewFileCache] and
//     [MemoryCache] from [NewMemoryCache], each with its [FileCache.Get] and [FileCache.Put], and
//     [MemoryCache.Get] and [MemoryCache.Put]
//   - [Key], a result's cache key, and [Held], a held value's JSON
//   - [Version], the version of the shapes, which Key hashes
//   - [Capability] and its four constants, [CapabilityVision], [CapabilityTranscription],
//     [CapabilityExtraction], and [CapabilityEmbeddings]
//   - [Identity], a result's capability, model, and settings, and [Profile], a pipeline's
//   - [Inference], one text a model made, its [Outcome], and its [Usage]; the six Outcome
//     constants, [OutcomeDescribed], [OutcomeTranscribed], [OutcomeExtracted],
//     [OutcomeIncomplete], [OutcomeRefused], and [OutcomeFailed]; [Progress], an extraction's text
//     as it streams
//   - [Batch], its parts, [Source]s, and [Chunk]s, and its derived [Batch.Digest]; [DecodeVector],
//     the one reader of a chunk's or a held value's vector
//   - [Counts], one job's, with [VisionCounts], [TranscriptionCounts], [ExtractionCounts], and
//     [Tokens]
//   - [ErrRequest], the cause of a refused request
package docinfer
