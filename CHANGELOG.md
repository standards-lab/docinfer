# Changelog

All notable changes to `github.com/standards-lab/docinfer`, docinfer's Go base module, are
documented here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and
the module adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html). Each release's
section is headed by the version its `vX.Y.Z` tag carries, and is the release's notes. Its exported
API is its contract: a change to it is a change here. This changelog covers the base module only;
each nested module keeps its own.

## [Unreleased]

## [v0.4.0] - 2026-11-27

This release renders docproc's metadata records as metadata texts: the office, PDF, media, and
picture metadata records, and the mail message and OneNote page records, are each chunked and
embedded as a text of its own. It requires docproc v0.10.0 and go-ai v0.5.0.

### Added

- `Pipeline.Run` takes docproc's office, PDF, media, and picture metadata records, record outputs
  of `dispatch.MediaOfficeMetadata`, `dispatch.MediaPDFMetadata`, `dispatch.MediaMediaMetadata`,
  and `dispatch.MediaPictureMetadata`, and its mail message and OneNote page records, record
  outputs of `dispatch.MediaMail` whose `Meta` holds `dispatch.MetaSubject` and of
  `dispatch.MediaOneNote` whose `Meta` holds `dispatch.MetaTitle`, as metadata texts. Each is
  opened by its SHA-256 and decoded as `metadata.Office`, `metadata.PDF`, `metadata.Media`,
  `metadata.Picture`, `email.Message`, or `onenote.Page`. It is rendered as a "label: value" line
  per field, a date in RFC 3339 in UTC, and chunked as a source of kind `metadata` whose `Output`
  is the record output and whose `SHA256` is the rendered text's. It is never extracted, and
  `Counts.Metadata` counts it.
- A media record renders its duration and its container's and streams' tags but the tags naming
  the encoder, container brand, handler, vendor, bit rate, byte or frame counts, or the writing
  tool; a picture record its capture time, make, model, and GPS position. Neither
  renders codecs, rates, sizes, orientation, format names, or the EXIF dump.
- A message renders its sender, its To, CC, and BCC recipients, each its name and its SMTP
  address, else its address, in angle brackets, its submit time, else its delivery time, as
  "sent", and its subject; a page its title, author, and creation and modification times. Neither
  renders a body, transport headers, threading ids, attachments, or media.
- A mail store's or folder's record, a notebook's, section group's, or section's, and a
  conversation's pass through. A record that renders no line is no source; one whose content
  doesn't decode stops the job, `Permanent`.

### Changed

- `Counts.Metadata` counts every metadata text in the job, the input's and each metadata
  record's, no longer one per job.
- A `Source` of kind `metadata` may carry `Output`: nil for an input's metadata text, the record
  output for a metadata record's, whose `SHA256` is the rendered text's.
- The module requires docproc v0.10.0, the release whose records carry the metadata docinfer
  renders.

## [v0.3.0] - 2026-11-25

This release meets the live rows: embeddings carry e5's document convention and chunk by the
row's token count within a character bound, every identity names the served model, vision and
extraction take native sampling, transcription takes a reply bound and native sampling, oversized
images are downscaled for the request, and looping replies are trimmed. It requires go-ai v0.5.0
and docproc v0.9.0.

### Added

- `Served` on `Vision`, `Transcription`, `Extraction`, and `Embeddings`: the model the deployment
  serves under `Model`, which the host resolves. Requests still send `Model`.
- `Native` on `Vision` and `Extraction`: go-ai's native tier, a JSON object of members the
  endpoint defines, such as temperature, sent on every request as `protocol.Request.Native`. Empty
  sends none. It enters the capability's identity settings, and so its `Key` and
  `Pipeline.Profile`, as a last `"native"` member, omitted when empty, canonical: whitespace
  stripped, strings in their shortest form, and each object's members sorted by name, numbers as
  given. `New` refuses a `Native` that isn't a JSON object with unique member names, matching
  `ErrRequest`.
- `EmbeddingQuery` on `Profile`, JSON `embedding_query`: the query convention, an
  `ai.InputConvention`, the embeddings' client declares in `ai.Options.Embedding`, which a consumer
  applies to its search queries so they land in the space the chunks were embedded in. It is
  omitted when the client declares none; the other members are unchanged.
- Each text's chunk spans are held in the `Cache`, so a rerun over the same cache rebuilds its
  chunks with no count, and so no tokenize request. `CapabilityChunk`, `"chunk"`, keys them: its
  identity's `Model` is `Chunker.Model`, its `Settings` `Chunker.Settings()`, and the key's
  content the text's SHA-256, so a changed bound, tokenizer model, or `chunk.Version` misses.
  `Held` gains `Spans`, JSON `"spans"`, each chunk's `[start, end)` bytes of the text in order,
  `[]` for a text of no chunks and omitted otherwise. Held spans that don't fit the text are a
  miss, recounted and replaced, and a count's failure holds nothing. `Version` stays "1": `Held`
  grows by addition, every held value keeps its key, and a release before this one never asks for a
  chunk key. `docinfertest.Cache` checks that a cache holds a spans value and an empty one
  byte for byte, and the same content under each capability is five keys.
- `Origin` on `Input`: the input, as the host names it, that the event's part was found in, so a
  job of several inputs keeps parts of one ID apart, each input's own part `/`. `Pipeline.Run`
  echoes it on each `Inference` and `Source` as `Origin`, a local field that doesn't marshal, so a
  host names each result's input from the result itself. A job of one input may leave it `""`.
- `Unplaced` on `TranscriptionCounts`, JSON `unplaced`: the windows `Run` places in no transcript,
  a repeat of an index already placed, past its group's bound, or after its group closed, and a
  window placed before its plan whose index is at or past the plan's count, moved from its
  outcome, and from `Held`, when the group is joined, its request and tokens kept. Each
  window is counted once, under its outcome or as `Unplaced`, so `Transcribed`, `Incomplete`,
  `Refused`, `Failed`, and `Unplaced` add up to `Counts.Windows`.
- `MaxTokens` on `Transcription`: the request's `ai.TranscriptionRequest.MaxOutputTokens`, which
  bounds a chat-mode reply. 0 sends none; docinfer ships no default. go-ai refuses any other in
  route mode, where the route's max_tokens goes through its Native. `New` refuses a negative one,
  matching `ErrRequest`. A reply cut off at the bound is the window's outcome incomplete, held,
  its reason `max_output_tokens`, as any incomplete transcript is, its partial text kept.
- `Native` on `Transcription`: go-ai's native tier, a JSON object of members the endpoint defines,
  such as temperature, sent on every request as `ai.TranscriptionRequest.ChatNative`, which chat
  mode sends as the chat request's `protocol.Request.Native`. Empty sends none; docinfer ships no
  default. go-ai refuses any other in route mode. It enters transcription's identity settings as a
  `"native"` member after `"max_tokens"`, omitted when empty and canonical as `Vision`'s is. `New`
  refuses one that isn't a JSON object with unique member names, matching `ErrRequest`.
- `MaxPixels` on `Vision`: an area bound on the image sent. A PNG or JPEG whose width × height
  exceeds it is downscaled before the request, its aspect ratio kept, to the largest whole size
  within it, by an area average over the standard library's `image` packages, and re-encoded in
  its own format, JPEG at quality 95. The downscaled copy carries the source's orientation and
  color metadata byte for byte, its fields not rewritten: a JPEG's Exif APP1 segments, then its
  ICC_PROFILE APP2 chunks in order, right after SOI, and a PNG's `iCCP`, `sRGB`, `gAMA`, `cHRM`,
  and `eXIf` chunks, in order, right after IHDR; no other segment or chunk is carried. An ICC
  profile whose color space isn't the copy's, GRAY for a grayscale copy and RGB otherwise, or that
  can't be read, is left out, and a JPEG whose segments don't parse is sent without metadata;
  neither fails the request. Its header is read first, so an image within the bound is
  never decoded and is sent byte for byte. An image of another format, GIF among them, or one the
  standard library can't decode, is sent as it is, and the endpoint's refusal classifies it as
  before. 0 sends every image as it is; docinfer ships no default. `New` refuses a negative one,
  matching `ErrRequest`. It enters vision's identity settings as `"max_pixels"` after `"effort"`,
  always present, within vision's request version "2".

### Changed

- Transcription's request version is "3" and its identity settings gain `"max_tokens"` after
  `"language"`, and `"native"` after that when `Native` is set:
  `{"request","mode","prompt","language","max_tokens","native"}`. Its keys and
  `Pipeline.Profile` change, so every transcript held under "1" or "2" misses; `Version` stays
  "1", since `Held`'s shape is unchanged.
- A window's incomplete transcript, an `*ai.IncompleteTranscriptError`, is held with the error's
  `Text`, the reply's unfinished text, and joins as it, in place of `""`; one with no text still
  joins as `""`. A text cut off at `max_output_tokens` that ends in a loop is held trimmed after
  the loop's first copy, its `Held.Incomplete` `"repetition"`, by vision's rule. The window's
  outcome stays incomplete, counted in `TranscriptionCounts.Incomplete` and listed in the
  transcript's `Failed`, and the transcript's `Source.Incomplete` stays its outcome. A replay from
  the cache joins the same text. Transcription's request version "3" keeps a transcript held
  empty under "2" from replaying.
- A window's incomplete transcript counts the tokens its request took, the
  `*ai.IncompleteTranscriptError`'s `Usage`, as a transcribed window's do: in the transcript's
  `Inference.Usage` and in `Counts.Tokens.Transcription`, in place of none. A replay from the cache
  counts none, as a transcribed window's replay does. Keys and request versions are unchanged.
- Breaking: `Held` is no longer comparable with `==`, since its `Spans` is a slice.
- A vision or extraction reply cut off at `protocol.ReasonMaxOutputTokens` whose text ends in a
  loop, a line, a sentence, or a short cycle repeated to the bound, is held trimmed after the
  loop's first copy, and its `Held.Incomplete`, `Inference.Reason`, and `Source.Incomplete` are
  `"repetition"`. The loop is the shortest period, 1 to 1000 runes, whose run at the text's end
  holds at least 2 copies and 200 runes. A reply whose tail doesn't loop, and every reply that
  finishes or stops for another reason, is held as before. An extraction's `Progress` still
  streams the whole text. Vision's and extraction's request versions are "2", so their identity
  settings, keys, and `Pipeline.Profile` change and every result held under "1", an untrimmed
  loop among them, misses; `Version` stays "1", since `Held`'s shape is unchanged.
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
  `{"version","bound","model"}`. No held result's key moves. `New` refuses a nil
  `Chunker.Count` and an empty `Chunker.Model`, matching `ErrRequest`. A failure of the count
  stops the job, nothing held: `Permanent` reports a permanent status and a refused one, which no
  chunk can hold, and `chunk.ErrRequest`.
- Breaking: `chunk.Splitter` gains `Runes`, the most characters, Unicode code points, a chunk
  holds, beside `Bound`'s tokens, for a server that refuses an input by its length before
  tokenizing it, as vLLM refuses one over 16 characters a token of `max_model_len`. Every chunk is
  within both; a stretch over `Runes` is cut by structure, then hard, with no count of its own,
  and a chunk's `Text` is still the text's own bytes, its white space uncollapsed. `Runes` has no
  default: `Split` refuses one below 1 with `chunk.ErrRequest`, and `New` refuses a
  `Chunker.Runes` below 1, matching `ErrRequest`. `Settings` is `{"version","bound","runes",
  "model"}`, so the chunker's identity, `Pipeline.Profile`'s `Chunker`, changes and spans held
  under the former settings miss and are recut. `chunk.Version` stays "2": a `Runes` no chunk
  reaches cuts every text as before, which `chunks-v2.json`'s earlier cases, unchanged, pin; the
  corpus gains each case's `runes` and cases the rune bound cuts, a text of long white-space runs
  among them.
- The module requires go-ai v0.5.0, the release with embedding input types and `Tokenize`.
- The module requires docproc v0.9.0, docproc's release before the capstone; docinfer's use of
  its API is unchanged, and it reads run directories docproc v0.4 wrote as before.

### Fixed

- A job of several inputs, such as a docproc run fed whole, grouped every input's windows of one
  part ID and stage as one, each input's own part being `/`: once the first input's group closed,
  every later input's windows and audio plan were dropped uncounted, and two inputs' windows could
  join into one transcript. Windows are grouped by the input's `Origin`, the part's ID, and the
  stage, so each input's part is joined from its own windows alone, and a window placed in no
  group is counted `Unplaced`, never dropped silently.
- A window placed before its group's plan record whose index was at or past the plan's count was
  transcribed and counted `Transcribed`, but left out of the transcript. It is still left out,
  and counted `Unplaced` instead.

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

[Unreleased]: https://github.com/standards-lab/docinfer/compare/v0.4.0...HEAD
[v0.4.0]: https://github.com/standards-lab/docinfer/compare/v0.3.0...v0.4.0
[v0.3.0]: https://github.com/standards-lab/docinfer/compare/v0.2.0...v0.3.0
[v0.2.0]: https://github.com/standards-lab/docinfer/compare/v0.1.0...v0.2.0
[v0.1.0]: https://github.com/standards-lab/docinfer/releases/tag/v0.1.0
