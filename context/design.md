# design

docinfer's design, reviewed from spike-ai-pipeline at 6a631f1 (`internal/docinfer` and
`cmd/docinfer`) with spike-ai-pipeline-processing v0.9.0's audio join: what the spike is measured
against, what docinfer keeps, changes, and drops from it, the API the library, job, postgres,
docinfer, and acceptance tasks of v1.ai.inference build to, and the task that releases each tag.
It takes over docproc's `context/layers.md` docinfer sections, cited at docproc commit 673ed3d:
"docinfer's API" with its cache, pipeline, batches as made, shapes, and one job; "docinfer's go-ai
use"; "docinfer's packages"; and docinfer's part of "The nested modules". The judgement calls it
settles are docinfer's `STANDARDS.md`.

## Evaluation

Fable 5.2's evaluations of the spike at 6a631f1, one for the library's sources and one for the
command's, and the session's own review against the workspace, one finding per issue or
simplification, each settled in a review round with the architect. Each names the spike's file
and lines, what it is measured against, and a verdict: **accepted**, naming the task that builds
it (go-ai, library, job, postgres, docinfer, or acceptance), **rejected** with its reason, or
**deferred** to the backlog. Where a finding changes a name, the API section states the name.

Citations are relative to `internal/docinfer/` at 6a631f1 unless they start with `audio/`
(spike-ai-pipeline-processing at v0.9.0), `go-ai:`, or `docproc:` (each at v0.3.0). The measures
are cited in short. A bare page name, such as `timeouts.md`, is a Go Elemental principle under
the architecture repository's `standards/go-elemental/principles/`; `principles/<page>` is an
architecture principle; a quoted name is the page's section. "layers" is docproc's
`context/layers.md` at 673ed3d. "data-intelligence" is standards-lab's
`context/data-intelligence.md`, which states what Cerebro asks of v1.ai.inference.

### The library

The library's sources: `infer/`, `vision/`, `transcribe/`, `embed/`, `chunk/`, and `outdir`'s
`Key`, `Held`/`Hold`, `Span`, and `Seam`, with their tests, and processing's `audio/join.go`.
L1–L27 are the evaluation's; W1–W3 and G1–G4 are the session's. Every finding is accepted.

#### Orchestration

**L1. issue: the run is whole-record phases over in-memory maps, so none of its control flow
ports.** `infer/execute.go:97-112`, `infer/execute.go:232-375`, `infer/sources.go:95-171`. Against
layers "Batches as made" and "One job, and its cancellation", and data-intelligence (results in
batches as made, cancellation per job). **accepted**, library: port the rules, not the flow. An
image or window is asked once per content and settings; a rejected embedding batch is retried a
text at a time (as L8 narrows it); a refused window joins as empty; an incomplete reply keeps its
text; a result is held as it completes, before anything derived from it is yielded.

**L2. simplification: the per-run dedupe maps and the hashing go.** `infer/vision.go:77-112`,
`infer/transcription.go:83-114`, `infer/transcription.go:285-296`, `infer/execute.go:237-272`.
Against layers "The pipeline" (`Input`) and "docinfer's packages". **accepted**, library: the
cache and the join of work in flight give the same guarantee across jobs, and
`dispatch.Output.SHA256` is already filled, so content is opened only on a miss; no set of
digests per job, and `Counts` by occurrence.

**L3. issue: inference builds its own clients, mid-run, from configuration.**
`infer/execute.go:31-36`, `infer/execute.go:293`, `infer/vision.go:119`,
`infer/transcription.go:121`. Against `principles/composition-root.md`,
`principles/validation-first.md`, and `principles/tool-beside-library.md`. **accepted**, library:
`Config` holds a built `*ai.Client` per capability; `docinfer.New` panics by name on a nil required
field and refuses a bad value, as docproc's `dispatch.New` does.

**L4. issue: the terminal's progress is threaded through the inference.**
`infer/progress.go:1-183`, `infer/vision.go:130`, `infer/transcription.go:135`,
`infer/execute.go:281-290`, `infer/execute.go:321-340`. Against
`principles/tool-beside-library.md`. **accepted**, docinfer: nothing of `progress.go` enters the
root package; `cmd/docinfer` prints from `Result` values (`Inference.Outcome`, `Held`, `TookMS`,
`Batch`), and the embed result's `Last` flag, which exists only for the progress line, goes.

**L5. simplification: three copies of one bounded fan-out, each serializing storage under its
lock.** `vision/vision.go:58-96`, `transcribe/transcribe.go:44-82`, `embed/embed.go:44-71`,
`infer/vision.go:126-153`. Against layers "docinfer's go-ai use" (concurrency moves into `Bounds`).
**accepted**, library: one unexported runner over `Bounds`; `Cache.Put` outside any lock; `Run`'s
`yield` called from `Run`'s goroutine alone, and `Run` waits for its requests in flight before it
returns.

**L6. issue: outputs are sent to a model whatever their part came to, and streaming makes that
unconditional.** `infer/sources.go:88-94`. Against layers "One job, and its cancellation" (an
output's update precedes anything derived from it). **accepted**, library: the root's doc states
the contract: a consumer drops a quarantined or failed part's results by that part's own event.
`job` does not hold a part's outputs until its event, which would cost the "as made" order.

#### The go-ai mapping

**L7. issue: every `*APIError` is a rejection, and any 4xx is held.** `infer/execute.go:209-211`,
`vision/vision.go:118-119`, `transcribe/transcribe.go:99-100`, `embed/embed.go:104-109`; asserted
by `app/vision_test.go:263-306` and `app/transcription_test.go:249-296`. Against layers
"docinfer's go-ai use" (the failure table). **accepted**, library: the table, one test per row;
the two tests are rewritten, not ported. go-ai v0.3.0 also retries 502 and 504, and the
redelivery row carries `APIError.RetryAfter`.

**L8. issue: a rejected embedding batch is re-sent a text at a time on any status.**
`embed/embed.go:104-115`; the only test rejects with a 400 (`app/embed_test.go:378`). Against
layers' failure table. **accepted**, library: a batch is split only on a refused status (400,
413, 415, 422); any other takes its row.

**L9. issue: failures that aren't an `*APIError` have no row.** `vision/vision.go:124-131`,
`transcribe/transcribe.go:101-102`. Against layers "docinfer's go-ai use" (`Permanent`).
**accepted**, library: the rows are added. `*format.UnsupportedError` is permanent;
`*ai.IncompleteTranscriptError` is the window's outcome, incomplete and held under its settings; a
2xx reply with `protocol.StatusFailed` and a reply that can't be decoded take the 500 row.

**L10. issue: a model's refusal is held as an empty description.** `vision/vision.go:137-155`.
Against layers "docinfer's shapes" (`Outcome` refused). **accepted**, library: a completed reply
with a `protocol.Refusal` part and no output text is outcome refused, held with `Held.Refused` the
refusal's text, and extraction reads it the same way.

**L11. issue: the vector's length is checked after it is held.** `infer/execute.go:317`,
`infer/execute.go:417-429`. Against layers "The pipeline" (`Config.Dimensions`). **accepted**,
library: checked against `Config.Dimensions` before `Put`, and again on `Get`, where a held vector
of another length is the same permanent error; `measure`'s norms are `cmd/docinfer`'s report.

**L12. issue: no bound on a request, and a stalled one now pins shared state.**
`vision/vision.go:114`, `transcribe/transcribe.go:93`, `embed/embed.go:89`,
`app/infrastructure.go:31-32`. Against `timeouts.md` and go-ai's contract (`go-ai:client.go:96`).
**accepted**, library: the root's doc names the host as the owner, through the transport's
`ResponseHeaderTimeout` for `Generate`, `Embed`, and `Transcribe` and `Options.StreamIdleTimeout`
for extraction's stream; docinfer adds no default.

**L13. issue: transcription is written against a reader, a path, and a client that never
retries.** `transcribe/transcribe.go:87-95`, `transcribe/doc.go:8-9`, `infer/transcription.go:25-27`.
Against go-ai v0.3.0 (`TranscriptionRequest.Open`, called per attempt) and layers "The cache"
(`Key`). **accepted**, library, as G2 amends it: `Open` is `dispatch.Content.Open(ctx, sha256)`
(W3), `FileName` is made from `Output.MediaType`, and the transcription mode is read from the
client (G2), not restated by the host.

**L14. simplification: `text` restates a helper go-ai keeps unexported.**
`vision/vision.go:135-155`, `go-ai:transcribe.go:399`. Against `principles/resolution.md`.
**accepted**, go-ai: resolved at its root by G1; docinfer keeps no helper.

**L15. issue: extraction has no spike code and no spike evidence.** Nearest code:
`vision/vision.go:99-133`. Against data-intelligence (extraction by a caller's prompt and schema).
**accepted**, library and acceptance: the design names each behavior (`GenerateStream` folded by
an `Accumulator`, `JSONSchemaFormat` through Chat Completions, progress coalesced to one a second,
a stream that breaks part way, whether a partial extraction is held) and its test against go-ai's
`aitest` (G3), and the acceptance run exercises extraction against the live gateway.

#### The cache and its key

**L16. simplification: embeddings are keyed by the chunk bound and chunker version, which change
nothing about a vector.** `infer/execute.go:235`, `infer/execute.go:260`;
`app/embed_test.go:313-335`. Against data-intelligence "Caching". **accepted**, library: an
embedding is keyed by model and the chunk's SHA-256 alone, settings `""`; the chunker stays in
`Profile`.

**L17. simplification: three settings encodings, and nothing versions how a request is built.**
`infer/vision.go:27-29`, `infer/transcription.go:25-27`, `infer/execute.go:235`,
`vision/vision.go:104-112`. Against layers "The cache". **accepted**, library: one encoding for
every capability, and a per-capability request version in the settings, changed when the
request's construction changes.

**L18. issue: a held value that doesn't decode fails every run, and `Held` can't say what version
it is.** `outdir/outdir.go:90-102`. Against layers "The cache" (a miss costs one model call).
**accepted**, library: `Version` joins `Key`'s hash, so a new shape misses; a held value that
doesn't decode is a miss the next `Put` replaces; `Cache` keeps `Get` and `Put`.

#### Transcription and the join

**L19. issue: a part's window count is read from a Meta key docproc v0.3.0 never sets, and
nothing tells a streaming pipeline the windows are all in.** `infer/sources.go:123`,
`infer/sources.go:176-187`, `infer/transcription.go:193-206`. Against docproc v0.3.0
(`docproc:dispatch/shapes.go:80-82`) and layers "The pipeline" (`Input`). **accepted**, library:
the end of a nested audio part's windows comes from docproc's plan record output
(`MediaAudioPlan`, `MediaVideoPlan`), admitted as a `Run` input for its count; the first event of
an index wins, since events repeat.

**L20. issue: the seam bound is fixed at 16 words on the claim that the windows say nothing.**
`infer/transcription.go:254-256`, `audio/join.go:12-15`. Against layers "The pipeline"
(`join.Request`). **accepted**, library: the bound is derived per seam from `MetaSeamBefore` and
`MetaSeamAfter` at `Config.Join`'s words per second, with no default.

**L21. simplification: `partKey`, `windowGroup`, and the outcomes pass are `Part.ID` and the
stage's `Identity`.** `infer/sources.go:56-86`, `infer/sources.go:98-113`. Against docproc v0.3.0
(`dispatch.Part.ID`) and layers "docinfer's packages". **accepted**, library: windows are grouped
by `{Part.ID, Event.Stage}`, and kinds and Meta are read by `dispatch`'s constants, not literals.

**L22. simplification: the join moves with three things it doesn't need.** `audio/join.go:1`,
`audio/join.go:93-96`, `infer/transcription.go:269-270` with `outdir/outdir.go:433-443`. Against
`principles/validation-first.md` and layers "The pipeline" (`join`). **accepted**, library: no
build tag; `join.Request` validated at `docinfer.New`, `join` with its own `ErrRequest`; one
`join.Seam` with the JSON tags. `TestJoin`'s table and the settled-prefix tests move as they are.

#### Chunking and shapes

**L23. simplification: `Splitter` beside `Split`, and `Span` beside `Start`/`End`.**
`chunk/chunk.go:14-18`, `chunk/chunk.go:31-36`, `infer/execute.go:362`. Against
`baseline-standards.md` (a type is introduced only when it is needed). **accepted**, library: one
spelling of the split; `Span` moves to `chunk` and removes the copy.

**L24. issue: invalid bounds are coerced, and the design adds library defaults.**
`vision/vision.go:66`, `transcribe/transcribe.go:52`, `embed/embed.go:48-50`, `chunk/chunk.go:32`,
`audio/join.go:93`. Against `principles/validation-first.md` and `baseline-standards.md`.
**accepted**, library: no library defaults, as docproc: `New` and `NewBounds` refuse a value below
1 with `ErrRequest`, and `cmd/docinfer` ships the operator's starting values. layers' 512 KiB
`BatchBytes` default is dropped.

**L25. simplification: `mediaType`'s fallbacks are dead against docproc v0.3.0.**
`infer/vision.go:217-231`. Against docproc v0.3.0 (every image output carries `MediaType`).
**accepted**, library: `Output.MediaType` is used; an image output without one is the input's
defect, refused.

#### Tests

**L26. issue: tests that can't fail.** `app/embed_test.go:371`, `app/vision_test.go:525`,
`app/transcription_test.go:435`, `chunk/chunk_test.go:164-166`, `audio/join_test.go:161-178`.
Against `tests-and-docs.md` (a test proves a behavior only if it can fail). **accepted**, library:
`Bounds`' test holds requests at the fake endpoint until the bound is reached and asserts it is
reached and never passed, with no sleep; the two determinism tests are dropped.

**L27. issue: nothing pins the formats the cache depends on.** `outdir/outdir_test.go:88-105`,
`chunk/chunk.go:9-11`. Against `tests-and-docs.md` and layers "The cache". **accepted**, library:
a golden key per capability, and a golden corpus of chunks named for `Version`, each failing with
"change the version".

#### The workspace

**W1. simplification: the cache's behavior is tested per implementation, through one.**
`outdir/outdir_test.go:88-131`. Against `tests-and-docs.md` and docproc's `dispatch/dispatchtest`.
**accepted**, library: the one cache suite every implementation passes follows docproc's
`dispatchtest`: an exported `docinfertest` package in the base module.

**W2. issue: go-ai v0.3.0 exports no fake endpoint.** `apptest/apptest.go:197-345`. Against
`tests-and-docs.md` and go-ai v0.3.0. **accepted**, go-ai: resolved by G3, so docinfer has no
`endpointtest`.

**W3. issue: content is read from a path, which a content store doesn't have.**
`vision/vision.go:100`, `transcribe/transcribe.go:87`. Against docproc v0.3.0
(`dispatch.Content`) and layers "The pipeline". **accepted**, library: content is read through
`dispatch.Content.Open(ctx, sha256)`; a miss is `fs.ErrNotExist`, which `dispatchtest` asserts,
mapped to `MissingError`.

**G1. simplification: go-ai keeps a response's output text unexported, so every caller restates
it.** `vision/vision.go:135-155`, `go-ai:transcribe.go:399`. Against `principles/resolution.md`.
**accepted**, go-ai: go-ai exports `(*protocol.Response).OutputText`; its `transcribe.go:399`,
`tools/infer/internal/app/domain.go:515`, and `docs/quick-start.md:182` switch to it, and docinfer
keeps no helper.

**G2. issue: the client's transcription mode changes the transcript but can't be read from the
client.** `infer/transcription.go:25-27`. Against go-ai v0.3.0 (`Options.TranscriptionMode`) and
layers "The cache" (`Key`). **accepted**, go-ai: go-ai exposes `(*Client).TranscriptionMode()`,
and docinfer's `Identity` reads it from the client. Rejected: a host-restated
`docinfer.Transcription` field, which can disagree with the client and silently mis-key the cache.

**G3. issue: every consumer of go-ai would write its own fake endpoint.**
`apptest/apptest.go:197-345`. Against `tests-and-docs.md`. **accepted**, go-ai: go-ai exports
`aitest`, a fake OpenAI-compatible endpoint (generate, stream, embed, transcribe, models;
scriptable per request; holding requests until released, for bound tests without sleeps).
Rejected: docinfer's own internal `endpointtest`; the library, `cmd/docinfer`, `tools/infer`, and
Cerebro all need the same fake.

**G4. issue: G1–G3 change go-ai, and no task of the goal holds go-ai.** No spike code: the goal's
task list. Against `principles/independent-releases.md`. **accepted**, go-ai: a new task, go-ai,
runs between design and library and releases go-ai v0.4.0 (G1–G3), tagged before docinfer's
v0.1.0. Rejected: go-ai slices inside library, already the goal's largest task.

### Join of work in flight

The spike has no join: one process runs one run, and per-run maps by content SHA-256 remove
duplicates before sending (`infer/vision.go:77-112`, `infer/transcription.go:83-114`,
`infer/execute.go:237-272`). layers' waiter rule asks that a waiter get the leader's result, that a
waiter whose leader ends without a result ask the model itself, and that a cancelled waiter stop
without waiting out the leader's request.

**Decision.** docinfer's own claim table, in the library task: a mutex, a map of calls, and a done
channel per call. `claim(keys)` returns which keys are the caller's and which are waited for; the
wait selects on the caller's context; a call that ends without a result wakes its waiters to
claim again. It is one mechanism for single keys and batches. Release is deferred, so a panicking
leader doesn't strand waiters, and the key is deleted before they wake. Its unit tests run under
`-race`: leader cancelled, leader's transient failure, waiter cancelled, leader panics, a batch
partly in flight. The base module requires docproc and go-ai alone.

**Rejected: `golang.org/x/sync/singleflight`.** It meets the waiter rule only through `DoChan`, a
leader flag the closure sets, the waiter's own `ctx.Err()` check, and a retry loop around it; and
`Embed` answers a batch of keys, which singleflight's one key, one function can't claim without
blocking, so embeddings would need a second mechanism.

**Either way.** The leader `Put`s before it releases, so a waiter's result equals a cache read and
counts as `Held`, its tokens the leader's job's alone. A waiter holds no `Bounds` slot. A refusal
is a result and is shared, and a permanent error is final and is shared.

### The command

Written by a later slice.

## API

Written by a later slice.

## Releases

Written by a later slice.
