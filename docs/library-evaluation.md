# Library evaluation

docinfer's library was built from a review of two spikes: spike-ai-pipeline at 6a631f1
(`internal/docinfer`), which this page calls the spike, and spike-ai-pipeline-processing
v0.9.0's audio join. Each package's documentation states what the library does; this page keeps
the reasoning behind the review's findings that has no other home, and the alternatives it
rejected. [No library defaults](no-defaults.md) and the
[join of work in flight](join-of-work-in-flight.md) hold their own findings.

## What was ported

The spike ran whole-record phases over in-memory maps, so none of its control flow ports; its
rules do. An image or window is asked once per content and settings; a rejected embedding batch
is retried a text at a time, only on a refused status; a refused window joins as empty; an
incomplete reply keeps its text; and a result is held as it completes, before anything derived
from it is yielded.

## Composition

- **Clients are the host's.** The spike built its own go-ai clients mid-run from configuration.
  Under the composition-root principle, `Config` holds a built `*ai.Client` per capability, and
  `New` panics by name on a nil required field and refuses a bad value, as docproc's
  `dispatch.New` does.
- **No progress in the library.** The spike threaded its terminal's progress through the
  inference. A tool sits beside the library, so nothing of it enters the root package: a command
  prints from `Result` values, and the spike's embed result's `Last` flag, which existed only for
  the progress line, went.
- **One runner.** The spike had three copies of one bounded fan-out, each serializing storage
  under its lock. One unexported runner over `Bounds` serves every capability, and `Cache.Put`
  runs outside any lock.
- **Counts by occurrence.** The spike kept per-run dedupe maps and hashed content itself. The
  cache and the join give the same guarantee across jobs, and `dispatch.Output.SHA256` is
  already filled, so content is opened only on a miss, and `Counts` counts occurrences, so no
  count needs a set of the job's digests.
- **Content through a store.** The spike read content from a path, which a content store
  doesn't have. Content is read through `dispatch.Content.Open(ctx, sha256)`, and a miss is
  `fs.ErrNotExist`, mapped to `MissingError`.

## The go-ai mapping

The spike held every `*APIError` as a rejection, held any 4xx, re-sent a rejected embedding batch a
text at a time on any status, and had no row for failures that aren't an `*APIError`. The failure
table in [`doc.go`](../doc.go) replaces it, one test per row against go-ai's `aitest`. The spike
held a model's refusal as an empty description and checked a vector's length after holding it; a
refusal is now its own outcome, and a vector is checked against `Config.Dimensions` before `Put` and
again on `Get`.

## The cache and its key

- **Embeddings by model and text alone.** The spike keyed embeddings by the chunk bound and the
  chunker's version, which change nothing about a vector. An embedding is keyed by model and the
  chunk's SHA-256, and the chunker stays in `Profile`.
- **One settings encoding.** The spike had three, and nothing versioned how a request is built.
  Every capability's settings are one JSON encoding led by the request's version.
- **`Version` in the key.** A held value that didn't decode failed every spike run. With
  `Version` hashed into `Key`, a new shape misses, and a held value that doesn't decode is a miss
  the next `Put` replaces, so `Cache` keeps `Get` and `Put` alone.
- **Golden formats.** Nothing pinned the formats the spike's cache depended on. A golden key per
  capability and a golden corpus of chunks named for `chunk.Version` fail with "change the
  version".

## Transcription and the join

The spike read a part's window count from a Meta key docproc never sets, so nothing told a streaming
pipeline the windows were all in; a group now ends at its audio plan record's count, at its last
window, or at the end of the inputs. Windows are grouped by the input's `Origin`, `Part.ID`, and the event's stage, and
kinds and Meta are read by `dispatch`'s constants, not literals. The spike's `mediaType` fallbacks
were dead against docproc, which gives every image output a `MediaType`, so an image output without
one is the input's defect.

## Chunking

`chunk` has one spelling of the split, `Splitter`, and `Span` moved into it from the spike's
output directory, since a type is introduced only when it is needed.

## Tests

The spike had tests that couldn't fail. `Bounds`' test holds requests at the fake endpoint until the
bound is reached and asserts it is reached and never passed, with no sleep. The cache's behavior was
tested per implementation, through one; `docinfertest` is the one cache suite every implementation
passes, exported in the base module as docproc's `dispatchtest` is.

## Modules

The base module requires docproc and go-ai alone and calls go-ai directly, with no adapter module
between them. It is an adjacent library above go-ai's tier: whatever may not depend on go-ai's tier
may not depend on docinfer either. The imports run one way: `chunk` and `join` import the standard
library alone; the root imports them, docproc's `dispatch`, and go-ai's `ai`, `protocol`, and
`format`; `job` imports `dispatch` and the root, which never imports `job`; and `docinfertest`
imports the root. A committed `go.work` spans the modules, kept in step with `mise.toml`'s
`GO_MODULES`, so a change to the base is seen by every module before it is tagged; the check runs
each module with `GOWORK=off`.

## The go-ai surface docinfer leaves alone

docinfer calls none of these, so none is part of its surface: function tools, tool choice,
function calls and their outputs, the reasoning round trip and its encrypted content,
`ReasoningParam.Summary`, `TextParam.Verbosity`, `InputImage.Detail`, `protocol.InputAudio`
(docinfer transcribes through `Transcribe`, whose chat mode builds it), the `Native` tiers of
`Request`, `EmbeddingRequest`, and `TranscriptionRequest`, the refusal events (the `Accumulator`
folds them into the `Refusal` part docinfer reads), and `ai.TranscriptionInstruction`.

## Rejected

- **A host-restated transcription mode.** A `docinfer.Transcription` field for the mode can
  disagree with the client and silently mis-key the cache, so `Transcription.Identity` reads the
  mode from the client, through go-ai's `(*Client).TranscriptionMode()`.
- **docinfer's own fake endpoint.** go-ai exported no fake endpoint, and an internal
  `endpointtest` in docinfer was rejected: the library, the command, go-ai's `tools/infer`, and
  every host need the same fake, so go-ai's public `aitest` serves them.
- **Holding a part's outputs until its event.** It would cost the order as made, so a consumer
  drops a quarantined or failed part's results by that part's own event instead.
