# Changelog

All notable changes to `github.com/standards-lab/docinfer`, docinfer's Go base module, are
documented here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and
the module adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html). Each release's
section is headed by the version its `vX.Y.Z` tag carries, and is the release's notes. Its exported
API is its contract: a change to it is a change here. This changelog covers the base module only;
each nested module keeps its own.

## [Unreleased]

### Added

- `Served` on `Vision`, `Transcription`, `Extraction`, and `Embeddings`: the model the deployment
  serves under `Model`, which the host resolves. Requests still send `Model`.
- `Native` on `Vision` and `Extraction`: go-ai's native tier, a JSON object of members the
  endpoint defines, such as temperature, sent on every request as `protocol.Request.Native`. Empty
  sends none. It enters the capability's identity settings, and so its `Key` and
  `Pipeline.Profile`, as a `"native"` member after `"effort"`, canonical: whitespace stripped,
  strings in their shortest form, and each object's members sorted by name, numbers as given. An
  identity with no `Native` keeps its settings and key, so `Version` stays "1". `New` refuses a
  `Native` that isn't a JSON object with unique member names, matching `ErrRequest`.
- `EmbeddingQuery` on `Profile`, JSON `embedding_query`: the query convention, an
  `ai.InputConvention`, the embeddings' client declares in `ai.Options.Embedding`, which a consumer
  applies to its search queries so they land in the space the chunks were embedded in. It is
  omitted when the client declares none; the other members are unchanged.

### Changed

- Each capability's `Identity`, and so each cache `Key` and `Pipeline.Profile`, names the served
  model, `Served`, not `Model`: another model served under the same name misses, and the same
  model served under another name hits. A caller sets `Served` on every capability it configures,
  and `New` refuses an empty one, matching `ErrRequest`. `Version` stays "1": the shapes and the
  key's format are unchanged, and a value held under a request's name misses under the served
  model's rather than decoding wrong.
- Every embeddings request is sent as `ai.InputDocument`, so the client applies the document
  convention its `ai.Options.Embedding` declares; with none declared, chunks go as they are, as
  before. `Embeddings.Identity`, and so each embedding's `Key` and `Pipeline.Profile`, keys on that
  convention, read from the client: its settings become `{"request","input_type","prefix",
  "native"}`, `"native"` there only when the convention's native is set, canonical as `Native`'s.
  A client with no document convention keeps the settings `""` and its keys, so `Version` stays
  "1". The query convention is never keyed: changing it re-embeds nothing. `Embeddings.Identity`
  now needs its `Client` set.
- Breaking: `chunk.Splitter` measures in tokens, by the host's count. `Bound` is in tokens as its
  new `Count`, a `chunk.Counter`, measures a text, the tokens its embedding would take, special
  tokens and any document prefix included, such as a closure over the embeddings' client's
  `Tokenize` with `ai.InputDocument`; its new `Model` names the served model whose tokenizer
  counts. Every chunk measures within the bound, cut by structure as before with no overlap,
  and a stretch with no cut is cut hard by measuring. `Split` takes a context, which docinfer
  passes the job's, and returns the count's error wrapped; a character that alone measures over the
  bound is refused with `chunk.ErrRequest`. `chunk.Version` is "2", its golden corpus
  `chunks-v2.json`, and `Settings`, which `Pipeline.Profile` reports as `Chunker`, is
  `{"version","bound","model"}`. No cache key moves: the chunker keys nothing. `New` refuses a nil
  `Chunker.Count` and an empty `Chunker.Model`, matching `ErrRequest`. A failure of the count
  stops the job, nothing held: `Permanent` reports a permanent status and a refused one, which no
  chunk can hold, and `chunk.ErrRequest`.
- The module requires the go-ai release with embedding input types and `Tokenize`.
- The module requires docproc v0.4.2, a patch that moves the default LibreOffice and FFmpeg
  images, with its exported API unchanged.

## [v0.2.0] - 2026-11-20

Adds `job`, which runs one job end to end over docproc's dispatcher and the pipeline, and the two
results of `Pipeline.Run` it is built on. The module requires docproc v0.4.0 and go-ai v0.4.0
alone. `job` is new: the spike has no job to copy.

### Added

- `job`: `Config` (`Dispatcher`, `Pipeline`, and `CheckpointEvery`, with no default), `New`, and
  `Runner.Run`, which dispatches a job's input and infers over what it prepares, yielding each
  `Update` in one order: an output's event before its inference and batches, and a part's own
  event after its outputs', warnings', and children's. Each inference's text is put into the
  dispatcher's content store before its update. A store's input yields a `Checkpoint` at its low
  watermark, every `CheckpointEvery` settled messages, and a dispatch resumed with its `After`
  yields only the messages after it. `Done` ends the attempt, its parts counted once per part ID
  by each part's last own event. A content miss drops its part's index entry, so a redelivery
  prepares the part again. Any error or a cancellation stops the job, its error yielded alone and
  last, a loop stopped early stops it too, and other jobs on the `Runner` go on.
  `Checkpoint`'s and `Done`'s JSON is under `Version` "1".
- `Result.Input`, each input echoed as `Pipeline.Run` admits it, in pull order, before anything
  derived from it. An input that needs no work, such as a warning or a part's own event, is
  passed through: echoed, then settled, with nothing counted.
- `Result.Settled`, an input's position in pull order, yielded once everything derived from that
  input is out. A window and its audio plan record settle once their group's transcript and everything derived from it are out.

### Changed

- `Permanent` reports `dispatch.ErrRequest`, a request docproc refuses, as permanent.
- The module requires docproc v0.4.0.

## [v0.1.0] - 2026-11-19

The first release: the inference library above go-ai's tier. It describes images, transcribes
audio windows, extracts by a caller's prompt and JSON schema, and chunks and embeds text, over the
parts docproc's dispatcher prepares, yielding each result as it is made and holding every model
result in a cache. The module requires docproc v0.3.0 and go-ai v0.4.0 alone.

`chunk` is copied from spike-ai-pipeline's `internal/docinfer/chunk` at 6a631f1, and `join` from
spike-ai-pipeline-processing v0.9.0's (4402f7c) `audio/join.go`, each with only its module and
import paths changed, then built to the design. The spike's `infer`, `vision`, `transcribe`, and
`embed` packages, and its `outdir`'s `Key`, `Held`, `Hold`, `Span`, and `Seam`, are not copied:
they are rewritten to the design against go-ai v0.4.0. `docinfertest`, the claim table, `Bounds`,
extraction, and the integration tier are new.

### Added

- `Pipeline`, built by `New` from a `Config`: a go-ai client and settings per capability
  (`Vision`, `Transcription`, `Extraction`, and `Embeddings`), the chunker, the `Cache`, the
  `Bounds`, the inputs admitted at once (`Pending`), the texts per embedding call (`EmbedBatch`),
  a batch's byte cap (`BatchBytes`), the embedding space's `Dimensions`, and the transcript join's
  `join.Request`. Nothing has a default: `New` panics, naming the field, on a missing client,
  cache, or bounds, and refuses a value below 1 or an empty model, matching `ErrRequest`.
  `Pipeline.Profile` reports each capability's `Identity`, the dimensions, and the chunker's
  settings.
- `Pipeline.Run`, one job over its `Input`s, a dispatch event's output or the input's metadata
  text (`MetadataText`), with `Options`: an `Extract`, a caller's prompt and schema, and `Final`,
  the job's last delivery. It yields each `Result` as it is made: each `Inference`, each
  extraction's `Progress`, each `Batch` of embedded chunks once its embeddings complete, and the
  job's `Counts` last. Every result is held before anything derived from it is yielded. Images
  are described, audio windows transcribed, grouped, and joined, text and Markdown outputs and
  the metadata chunked, and every chunk embedded, its vector read by `DecodeVector`.
- The go-ai failure table: a refused status is the item's outcome, held; a permanent or transient
  failure stops the job, `Permanent` telling them apart; a failed call stops it too, but on the
  last delivery its item is recorded failed, never held. A content miss is a `MissingError`.
- `Bounds`, from `NewBounds`, the requests in flight per capability across every pipeline built
  on it. The pipeline joins work in flight across its jobs by key, so each key is asked of a model
  once, single keys and embedding batches alike.
- `Cache`, its `Get` of many keys in one call and its `Put`, with `FileCache` (`NewFileCache`) and
  `MemoryCache` (`NewMemoryCache`). `Key` is a result's key, hashing `Version`, its `Identity`,
  and its content's SHA-256; `Held` is a held value's JSON. A golden key per capability pins the
  form.
- `docinfertest.Cache`, the cache contract every implementation runs, the module's two and a
  provider's.
- `chunk.Splitter`, which cuts a text along its headings, paragraphs, and sentences, then hard,
  into chunks of at most its `Bound` of runes, each with its byte `Span`; `chunk.Version`, which a
  golden corpus pins.
- `join.Join`, which merges an audio part's window transcripts at their seams, each seam's search
  bounded by its seconds at a `Request`'s `WordsPerSecond`, into a `Transcript`.
- The integration tier, `integration`, behind the `integration` build tag, which `mise run
  integration` runs against the live endpoints go-ai's `AI_<ROLE>_*` variables name.
- The check, `mise run check`, run by CI; `mise run currency` and `mise run upgrade`; and the
  release workflow, which makes a GitHub release from `CHANGELOG.md` for a `v*` tag and from
  `<module>/CHANGELOG.md` for a `<module>/v*` tag.

[Unreleased]: https://github.com/standards-lab/docinfer/compare/v0.2.0...HEAD
[v0.2.0]: https://github.com/standards-lab/docinfer/compare/v0.1.0...v0.2.0
[v0.1.0]: https://github.com/standards-lab/docinfer/releases/tag/v0.1.0
