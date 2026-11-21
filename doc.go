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
// yielded. A go-ai failure maps by one table, the failure table below: a refused status is the
// item's outcome, held; a permanent or transient failure stops the job, [Permanent] telling them
// apart for the host; and a failed call stops it too, but on the job's last delivery
// ([Options].Final) its items are recorded failed, never held, and the job goes on.
//
// A held refusal stays until its key changes, by [Version], a request's version, or the model, or
// until the host removes it from its own store. A chunk is identified by its job, its part's ID,
// its source's kind and text SHA-256, and its index: the same content in two places is two chunks,
// and a redelivery's repeat is the same one. A transcript that isn't transcribed carries its
// outcome in its chunks' [Source].Incomplete. A [Batch] carries no index, since its host
// identifies it. [Counts] counts occurrences, and its Embedded and Rejected add up to the chunks
// the job's batches hold.
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
//
// # The failure table
//
// Each go-ai result maps to docinfer by one row of this table, and each row has its own test
// against go-ai's aitest:
//
//   - protocol.Usage{InputTokens, OutputTokens} and ai.TranscriptionUsage{InputTokens,
//     OutputTokens} are a [Usage]{Input, Output}, and ai.EmbeddingUsage{PromptTokens} a
//     Usage{Input}.
//   - A reply in protocol.StatusCompleted, its text (*protocol.Response).OutputText, is outcome
//     described or extracted, held.
//   - A reply in protocol.StatusIncomplete is outcome incomplete, its text kept, held:
//     Response.IncompleteDetails.Reason is its [Held].Incomplete, [Inference].Reason, and
//     [Source].Incomplete.
//   - A completed reply with a protocol.Refusal part and no output text is outcome refused, held,
//     the refusal's text its Held.Refused and Inference.Reason.
//   - An *ai.IncompleteTranscriptError is the window's outcome incomplete, joined as empty, held
//     under its settings, its Reason the Held.Incomplete.
//   - Refused: an *ai.APIError of 400, 413, 415, or 422 is the inference's outcome refused, or the
//     chunk's Error, its text the error's, recorded and held. An embedding batch so refused is
//     re-sent a text at a time, and only then.
//   - Permanent: an *ai.APIError of 401, 403, 404, 501, 505, or any other 4xx but 408 and 429,
//     and a *format.UnsupportedError, stop the job with the error, wrapped, and [Permanent]
//     reports it.
//   - Transient: an *ai.APIError of 408, 429, or any 5xx but 500, 501, and 505, once go-ai's own
//     retries (408, 429, 502, 503, 504) are spent, stops the job with the error, wrapped, and a
//     redelivery retries it, the APIError's RetryAfter the redelivery's delay. A transport
//     failure does the same: an *url.Error with no reply, the transport's ResponseHeaderTimeout
//     among them, or a stream that breaks, matching io.ErrUnexpectedEOF, go-ai's
//     Options.StreamIdleTimeout included.
//   - Failed: an *ai.APIError of 500; a 2xx reply in protocol.StatusFailed, a stream's
//     ResponseFailed among them; a 2xx reply that doesn't decode; and any other error a call
//     returns, a 3xx go-ai didn't follow among them, stop the job with the error, wrapped. A
//     reply that doesn't decode takes this row even when its syntax error matches
//     io.ErrUnexpectedEOF: the reply arrived whole, and decoded wrong. On the last delivery,
//     [Options].Final, the item's outcome is failed instead, or the chunk's Error, recorded and
//     never held, and the job goes on.
//   - A cancelled context is ctx.Err(), yielded alone and last.
//
// docinfer refuses only the statuses that judge the content. A status that judges the caller or
// the route, or a request the format can't send, fails the same way on every delivery, so it ends
// the job at once. A job the gateway turned away, or failed transiently, stops; the work queue
// redelivers it after RetryAfter, and the cache replays what it finished, since such a status is
// the gateway's or an upstream's, never the item's. Only the last delivery records an item failed,
// so one item a model can't answer never holds the rest back.
package docinfer
