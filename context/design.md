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

The command's sources: `app`, `apptest`, `config`, `models`, `source`, and `report`; `outdir` less
the library's part; `infer/commands.go` and `infer/progress.go`, the run command's own files; their
tests; and the spike root's `cmd/docinfer/main.go`, `go.mod`, and `docs/` (`docinfer-schema.md`,
`findings/docinfer.md`), cited from that root. C1–C24 are the evaluation's; what layers already
settles for `cmd/docinfer` (schema "2"'s shapes, `Transcript` going, `Failure` by part ID and
SHA-256, `FileCache` under `results/`) is not repeated. Every finding is accepted, for the docinfer
task.

#### Composition

**C1. issue: the configuration and the model clients are not graph nodes.**
`infer/commands.go:45`, `config/config.go:163`, `config/config.go:484-491`, `infer/vision.go:119`,
`infer/transcription.go:121`, `infer/execute.go:293`, `app/domain.go:14`. Against
`principles/composition-root.md` and `cli-applications.md`, with go-ai's `tools/infer` and
docproc's `cmd/docproc` as precedent. **accepted**, docinfer: nodes for the configuration (the
directory `--config` or `DOCINFER_CONFIG` names, read in the node's constructor), for each
capability's `*ai.Client`, and for a `run.Config` of `docinfer.Vision`, `Transcription`, and
`Embeddings`; `run.Commands(n.RunConfig)`. A configuration error fails the Build, labelled with
its node, before the docproc run is read.

**C2. issue: `App.Run` is go-cli-sdk v0.2.0's functional-option call.** `app/app.go:45`,
`apptest/apptest.go:31`, `go.mod`. Against `construction-and-execution.md`, `cli-applications.md`,
and `principles/rolling-currency.md`. **accepted**, docinfer: `internal/app` and
`internal/apptest` start from go-cli-sdk-template `template/v0.3.0`, as `cmd/docproc` did, on
go-cli-sdk v0.4.0's `cli.Program` and go-core v0.12.0's `Graph.Observe(func(graph.Ref))`; the
spike's files are not ported.

**C3. issue: the layout and packages are the spike root's, not a nested command module's.**
`cmd/docinfer/main.go:1`, `infer/progress.go:1`, `models/models.go:181`, `report/report.go:1`.
Against docproc's `STANDARDS.md` (the nested command's exception), `domain-files.md`, and
`topology-and-naming.md`. **accepted**, docinfer: `cmd/docinfer/main.go`,
`internal/{app, apptest, run, report}`, `outdir`, the one package `run` and `report` share, and
`integration`; `infer` becomes `run`, `models` and `source` fold into it (C4, C5), and `config`
becomes the node constructors (C1). Writers live in `output.go`; `report` splits as docproc's does,
into `commands.go`, `model.go`, `render.go`, and `output.go`. A committed `go.work` spans the base
module and `cmd/docinfer`.

#### Configuration and go-ai

**C5. issue: `models` lists `/models` with its own HTTP request.** `models/models.go:42`,
`models/models.go:67`, `models/models.go:121-177`. Against `principles/resolution.md` and
`principles/minimal-footprint.md`. **accepted**, docinfer: the check calls `(*ai.Client).Models`
once per capability's client, so it lists through the same query, token, headers, and retry the
run calls with, and reads `status.value` from `Model.Native`; an `*ai.APIError` is "no listing",
any other error "unreachable". The states, the table, and the 30 s bound stay.

**C6. issue: the configuration has no place for four of go-ai v0.3.0's options.**
`config/config.go:83-88`, `config/config.go:121-125`, `config/config.go:484-503`. Against go-ai
v0.3.0's `Options` and `baseline-standards.md` (the command ships the starting values).
**accepted**, docinfer: the transcription mode (`route`, `chat`) is set on the transcription
capability's own client in the configuration, the load refusing chat without the completions
format, and docinfer reads it from that client through `(*Client).TranscriptionMode()` (G2); an
endpoint gains `retry`, with the command's starting policy, `headers`, in the secrets file, and an
optional format profile decoded as `tools/infer`'s `formatOptions` is. Each is a manifest setting
where it changes a result.

**C7. simplification: `config` re-validates what go-ai validates, and its copy has drifted.**
`config/config.go:58-61`, `config/config.go:418-420`, `config/config.go:454-469`. Against
`principles/resolution.md`. **accepted**, docinfer: the vision effort is a
`protocol.ReasoningEffort`, refused by its own `UnmarshalText`, so `minimal` and `max` are
accepted; the client node (C1) reports `ai.New`'s error named for its endpoint; `validate` keeps
what is docinfer's (a capability's endpoint exists, the counts are at least 1).

**C8. simplification: `config.Env` and its five types are read by one test.**
`config/config.go:71`, `config/config.go:127-157`, `config/config.go:286-291`,
`config/config_test.go:60`. Against `baseline-standards.md`. **accepted**, docinfer: the six types
go, each override name is composed where it is read, and the test asserts the override's effect.

**C9. issue: no inference request has a deadline.** `app/infrastructure.go:28-33`,
`models/models.go:39`. Against `baseline-standards.md`, `timeouts.md`, and `cmd/docproc`'s
`DOCPROC_UNIT_TIMEOUT` and `DOCPROC_STREAM_IDLE`. **accepted**, docinfer: each endpoint's transport
ships a `ResponseHeaderTimeout`, its starting value sized by vision's slowest observed image, 38.9
s under mixed load (data-intelligence, line 239), and its client an `Options.StreamIdleTimeout` for
extraction's stream; the library adds no deadline (L12). Rejected: a deadline the command sets
around each call; the host bounds a request at the transport, where L12 puts it.

#### The run directories

**C4. simplification: `source` restates what docproc's `rundir` now does.**
`source/source.go:17`, `source/source.go:36-91`, `infer/commands.go:39`, `report/commands.go:30`.
Against docproc v0.3.0 (`rundir.Open`, `Dir.Latest`, `Dir.Record`) and
`principles/validation-first.md`. **accepted**, docinfer: no `source` package; `run` opens
`rundir`, feeds `Pipeline.Run` from `Dir.Record` and `Dir.Content()`, and keeps its count line in
`output.go`. The latest run is `Dir.Latest`'s, the latest that ended, and `--run` names any other,
validated as docproc's is. Rejected: the spike's greatest id, which can name a run still running.

**C10. issue: `outdir` writes through spike `rundir` helpers docproc doesn't export.**
`outdir/outdir.go:108`, `outdir/outdir.go:125`, `outdir/outdir.go:151`, `outdir/outdir.go:162`,
`report/report.go:51`, `report/report.go:70`. Against docproc v0.3.0's `rundir` exports.
**accepted**, docinfer: `outdir` owns its whole-file write, its run list, and its id check, each
`Dir` method refusing an id that isn't one of its runs; `texts/` is a `dispatch.FileContent`, laid
out `<xx>/<sha256>` with no `.txt` as the schema "2" page states, replacing `WriteText`,
`ReadText`, and `textPath`, with `ReadText`'s digest check kept or dropped on purpose.

**C11. issue: the directory has no schema of its own, and no reader checks one.**
`outdir/outdir.go:40-66`, `outdir/outdir.go:202-213`, `report/report.go:55`,
`docs/docinfer-schema.md:10`. Against `principles/validation-first.md`, with `rundir.json` as
precedent. **accepted**, docinfer: a root marker file; `Open` reads and creates nothing, `Create`
makes the directory, in place of `OpenExisting` and `Open`; any schema but "2" is refused, naming
both, so no "1" directory's `results/` and `texts/` are reused.

**C12. issue: the chunk record is held in memory and written once, at the end.**
`infer/execute.go:95-121`, `outdir/outdir.go:161-199`, `report/report.go:59-63`,
`docs/findings/docinfer.md:136`. Against layers "Batches as made" and docproc's record, appended
line by line. **accepted**, docinfer: each batch's lines are appended as the pipeline yields them
and the manifest's end marks the record whole, so a stopped run keeps the lines it made; the read
side is an iterator, and the report reads lines without their vectors.

**C13. issue: the manifest records no build.** `outdir/outdir.go:243-273`,
`docs/findings/docinfer.md:37`. Against docproc's manifest `Build`. **accepted**, docinfer: the
schema "2" manifest gains `docinfer`, the command's and the library's versions from the build
info, read as `version.go` reads the main module's.

**C14. simplification: a capability's manifest settings are an untyped map mixing identity and
bounds.** `outdir/outdir.go:293-299`, `outdir/outdir.go:538-543`, `infer/execute.go:74-89`,
`app/vision_test.go:114`. Against layers "The cache" (`Identity`). **accepted**, docinfer: a
capability holds the library's `Identity`, so a reader ties a manifest to its held results by the
same key, with the endpoint and its bounds as typed fields beside it; `Part`'s `kind` and `route`
go.

**C15. issue: the directory records no per-run vision result, so the report shows only what has a
chunk line.** `report/doc.go:21-23`, `report/report.go:200-242`, `docs/findings/docinfer.md:151`.
Against data-intelligence (the full vision texts) and the report's purpose. **accepted**,
docinfer: each run's inferences, vision's and transcription's, are a per-run file appended per
batch beside the chunk record (C12), and the report lists every image and window with its outcome,
text or none. Rejected: the inferences in the manifest, which stays small.

**C24. issue: times are converted as schema "2" no longer allows, and no test runs under a non-UTC
zone.** `infer/execute.go:68`, `infer/execute.go:113`, `report/report.go:164`. Against
`utc-times.md` and docproc v0.3.0's `schema.Time`. **accepted**, docinfer: `schema.TimeOf` at the
two stamps, `Time()` in the report, and a `main_test.go` per test package setting Asia/Kolkata.

#### The report

**C16. issue: one page a run, texts cut at 1,000 runes, no chunk text.**
`report/report.go:22-31`, `report/report.go:296-309`, `report/report.html.tmpl:132`,
`report/report.html.tmpl:175-177`, `report/doc.go:19-20`, `docs/findings/docinfer.md:72`.
Against data-intelligence, lines 42-43. **accepted**, docinfer: pages per input content, every
text whole, as "The report's shape" settles.

**C17. issue: the page's links depend on where docproc's run directory is.**
`report/report.go:65-90`, `report/doc.go:25-31`, `app/report_test.go:118-126`. Against the
package doc's own "the same directory renders the same bytes". **accepted**, docinfer: the run
records the relative path to docproc's directory once, as "The report's shape" settles, and the
file-URL branch, unreachable on Unix, goes.

**C18. issue: the report groups a source's lines by a line of index 0.**
`report/report.go:200-227`. Against layers "docinfer's shapes" (a chunk's key). **accepted**,
docinfer: lines are grouped by their part's ID, source kind and text SHA-256, and index, which a
schema "2" line carries.

**C19. issue: the run command always renders, and the report command differs from docproc's.**
`infer/execute.go:119`, `infer/execute.go:125`, `report/report.go:45-53`,
`report/commands.go:36-41`. Against `cmd/docproc`'s `run --report` and `report`. **accepted**,
docinfer: `docinfer run --report`, never after a cancellation, a failed render exit 1 with the run
intact; `docinfer report <dir> [--run <id>]` takes the latest run that ended (C4);
`report.Render(ctx, dir, id)`, and a `Make` that prints the index's absolute path.

**C20. issue: neither command's help states its exit status.** `infer/commands.go:25-29`,
`report/commands.go:21-25`, `app/report_test.go:188`. Against `cli-applications.md` and
`cmd/docproc`'s `exitStatus` footer. **accepted**, docinfer: a footer on each, stating that
recorded refusals exit 0, what exits 1, and what the last line counts.

#### Tests

**C21. issue: the integration test runs in process, inside the composition root's package.**
`app/integration_test.go:1-34`. Against `tests-and-docs.md` (the built binary as a subprocess, on
go-core's `processtest`), with go-ai's `tools/infer/integration` as precedent. **accepted**,
docinfer: `cmd/docinfer/integration`, tagged, against a real endpoint a variable names, skipped
when unset; the check vets and lints it without running it.

**C22. issue: the fixtures depend on what the nested module can't have.**
`apptest/apptest.go:24`, `apptest/apptest.go:47-81`, `apptest/apptest.go:210-223`,
`app/testdata/README.md:3-7`. Against layers "The nested modules" and `tests-and-docs.md`.
**accepted**, docinfer: `apptest` holds its own `Recorder`, `Builds`, `Halt`, and a fixture per
node C1 adds; the fake endpoint, model list included, is go-ai's `aitest` (G3), not an
`endpointtest` of docinfer's; a test helper writes each docproc run directory with `rundir.Create`
and `Begin`, in place of the hand-written schema "1" one.

**C23. issue: tests that can't fail, pin the wrong thing, or reach no branch.**
`report/report.go:80-84`, `report/report.go:182-184`, `report/report.go:296-309`,
`app/report_test.go:263`, `app/vision_test.go:114`, `app/embed_test.go:442-450`,
`app/vision_test.go:541-550`, `app/transcription_test.go:451-459`. Against `tests-and-docs.md` (a
stall is a finding, never a sleep). **accepted**, docinfer: the report's golden pages over a
committed directory, as docproc's `report` has them, with a long text, a damaged text, and an
unended run; `errors.Is` through the message the command owns, not the operating system's; exit 1
for a cancelled run, not any but 0; a wait that runs out fails the test.

### The report's shape

The spike renders one page a run, `runs/<run id>/report.html`, its inputs keyed and sorted by
absolute path, so the same content under two paths shows twice. A vision text or transcript is cut
at 1,000 runes, then linked under `texts/`; a text or Markdown output is a link into docproc's run
directory; a chunk is counted, never shown; an image is an `img` and a window a link into docproc's
directory, by a path computed at render time from the manifest's absolute `docproc.dir` (C17).
docproc's report is `runs/<run>/report/index.html` and a `<sha256>.html` per distinct content, the
index listing every input linked to its content's page and each page holding the content's part
tree with every output inline and whole; it reads its own directory alone, links relatively, and
reads no clock. Cerebro asks for docinfer's pages "linked to docproc's by the input's SHA-256 and
holding the full vision texts, transcripts, and chunks" (data-intelligence, lines 38-45).

**Decision.** Per run, one page per input content: `runs/<run>/report/index.html` and a
`<sha256>.html` per content, the file name docproc's page for the same content has, so the cross
link to it is one relative path. Schema "2" records each input's SHA-256 in an input list, since a
chunk line's part is the part it was cut from, not the input; an input with no SHA-256 keeps its
row, its error, and its metadata chunk on the index. Everything is inline and whole: each vision
text with its outcome, each transcript with its windows, failed windows, and seams, and each source
once, as its chunks in order (index, span, a rejected chunk's error, no vector), since the chunks
don't overlap and cover the text but for trimmed white space (`chunk/doc.go:12-15`). Images and
windows stay docproc's and are never copied: each vision text has an `img` beside it at docproc's
`rundir.ContentPath`, and each content page links to docproc's page for it. The run records the
relative path to docproc's directory once, so the rendered bytes don't depend on when the report
is rendered, and a moved directory costs links, never text. The report renders from docinfer's own
directory alone (`texts/`, the record, the manifest, and the inferences file, C15), and a test
renders it with docproc's directory absent.

**Rejected: one page a run.** It is the spike's, and the reason for its cut; the same content
under two paths shows twice, and nothing links to docproc's page by SHA-256.

**Rejected: a page per input path.** The same content under two paths would show twice, and the
page's name would not be docproc's.

**Rejected: keeping the 1,000-rune cut.** The median vision text is 1,419 characters
(`docs/findings/docinfer.md:72`), so the cut hides most of what the page is for, and per-content
pages remove its reason.

**Rejected: link-only images and windows.** It loses the image beside its text, which is the page's
use.

## API

Written by a later slice.

## Releases

Written by a later slice.
