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

**L13. issue: transcription is written against a reader, a path, and a client that never retries.**
`transcribe/transcribe.go:87-95`, `transcribe/doc.go:8-9`, `infer/transcription.go:25-27`. Against
go-ai v0.3.0 (`TranscriptionRequest.Open`, called per attempt) and layers "The cache" (`Key`).
**accepted**, library, as G2 amends it: `Open` is `dispatch.Content.Open(ctx, sha256)` (W3),
`FileName` is made from `Output.MediaType`, and the transcription mode is read from the client (G2),
not restated by the host.

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
a nested audio part's windows end at the count of docproc's audio plan record
(`MediaAudioPlan`), admitted as a `Run` input, or at its last window, as "The pipeline" states; the
first event of an index wins, since events repeat.

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
`apptest/apptest.go:197-345`. Against `tests-and-docs.md`. **accepted**, go-ai: go-ai v0.3.0
already has `internal/aitest`, its own fake endpoint, so the go-ai task exports that package as
`aitest` rather than writing a new fake: a fake OpenAI-compatible endpoint (generate, stream,
embed, transcribe, models; scriptable per request; holding requests until released, for bound
tests without sleeps), gaining what of that list it lacks.
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
module, `postgres`, and `cmd/docinfer`.

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

docinfer's API: layers' docinfer sections at 673ed3d, restated in go-ai's and docproc's released
names, with the findings above applied. A finding's id marks where it changes layers. Three go-ai
names are go-ai v0.4.0's, which the go-ai task adds (G1–G3): `(*protocol.Response).OutputText`,
`(*ai.Client).TranscriptionMode()`, and the package `aitest`, v0.3.0's `internal/aitest` exported
rather than a new fake. Every other go-ai name is v0.3.0's, and every docproc name docproc
v0.3.0's. layers' docinfer sections are superseded by this one.

### Modules and packages

docinfer (`github.com/standards-lab/docinfer`) infers over what docproc extracts: it describes
images, transcribes audio, chunks and embeds text, extracts by a caller's prompt and JSON schema,
holds results in a cache, and runs one job's dispatch and inference together. It is an adjacent
library that depends on docproc and on go-ai, an infrastructure library, so whatever may not depend
on go-ai's tier may not depend on docinfer either (`principles/repository-topology.md`, "Adjacent
libraries"): only the layers above go-ai's tier, such as Cerebro and go-web-service, use it. It
calls go-ai directly, with no adapter module between them.

```text
github.com/standards-lab/docinfer               the library; requires docproc and go-ai alone
github.com/standards-lab/docinfer/postgres      Cache over PostgreSQL through sqlate; requires the
                                                library, sqlate, and sqlate/postgres
github.com/standards-lab/docinfer/cmd/docinfer  the docinfer command; requires the library,
                                                docproc, go-ai, go-cli-sdk v0.4.0, and go-core
                                                v0.12.0 (C2)
```

The base module requires docproc v0.3.0 and go-ai v0.4.0 and no third module: the join of work in
flight is its own ("Join of work in flight"), and its tests drive go-ai's client against go-ai's
`aitest` (W2, G3).

```text
docinfer         the pipeline over go-ai's client: the cache and its key, the join of work in
                   flight, batches as made, the go-ai mapping, and the shapes of its results
docinfertest     the Cache conformance suite every implementation runs, as docproc's
                   dispatchtest (W1); imports the root and the standard library alone
chunk            the chunker: Splitter, Chunk, Span, and Version
join             a part's window transcripts joined at their seams
job              one job: dispatch, then inference, under the job's context; its updates
postgres         (module) Cache in PostgreSQL, one row per key, a batch's Get one query, and its
                   migrations, as docproc/postgres ships its own
cmd/docinfer     (module) main.go at the module's root, so go install installs the command
  internal/app       the composition root, started from go-cli-sdk-template template/v0.3.0 (C1, C2)
  internal/apptest   its fixtures: Recorder, Builds, Halt, and a fixture per node (C22)
  internal/run       the run command: docproc's rundir in, Pipeline.Run, outdir out (C3, C4, C5)
  internal/report    the report command and run --report's render (C16–C19)
  internal/outdir    docinfer's directory at schema "2", the one package run and report share
  integration        the built-binary tier, tagged, against a real endpoint a variable names (C21)
```

The inference package is the module's root, so a caller writes `docinfer.Pipeline` and
`docinfer.Batch`. `chunk` and `join` stand alone, and the root imports them. The imports run one
way: the root imports docproc's `dispatch` alone, and go-ai's `ai`, `protocol`, and `format`;
`job` imports `dispatch` and the root, which never imports `job`; `docinfertest` imports the root.
docinfer has no `internal/endpointtest` (W2). A committed `go.work` spans the base module,
`postgres`, and `cmd/docinfer`, kept in step with `mise.toml`'s `GO_MODULES` as docproc's is, so a
change to the base is seen by both before it is tagged; the check runs every module with
`GOWORK=off` (C3).

### The cache

The cache holds one model result per key: an image's, a window's, a chunk's, or an extraction's,
each bounded by its token cap, so each is a small row. Content lives apart from it: docproc's
`dispatch.Content` holds every output's content and, through `job`, every inference text, by
SHA-256; the cache holds results by `Key` alone, so content seen before is replayed from the cache,
not recomputed.

```go
package docinfer
type Cache interface {
    // Get returns keys' values in one call, values[i] keys[i]'s, nil for a miss; a held value
    // is never empty.
    Get(ctx context.Context, keys ...string) ([][]byte, error)
    Put(ctx context.Context, key string, value []byte) error // replaces a held value
}
// Key is "<capability>/" and the lowercase hex SHA-256 of Version, id's capability, model, and
// settings, and content, each length-prefixed (L18). content is the content's SHA-256: the
// image's, the window's, the source text's, or the chunk's text's.
func Key(id Identity, content string) string
func NewFileCache(dir string) (*FileCache, error) // <dir>/<capability>/<xx>/<hash>.json; "" refused
func NewMemoryCache() *MemoryCache
// Held is a held value's JSON, the bytes a Cache holds under a Key: one model result. A refusal
// and an incomplete reply are held; a failure never is.
type Held struct {
    Text       string `json:"text,omitzero"`       // vision, transcription, extraction: inline
    Vector     string `json:"vector,omitzero"`     // embeddings: as Chunk.Vector
    Incomplete string `json:"incomplete,omitzero"` // why the model stopped: max_output_tokens
    Refused    string `json:"refused,omitzero"`    // the refusal's text, or the refusing reply's
}

package docinfertest
// Cache runs the cache contract against a new, empty cache newCache makes for each case: a miss
// is nil, a Get of many keys answers each in order, a Put replaces, and values come back whole.
func Cache(t *testing.T, newCache func(t *testing.T) docinfer.Cache)
```

`Version` is in `Key`'s hash, so a new shape of `Held` or of a key's parts misses rather than
decoding wrong (L18). A held value that doesn't decode as `Held` is a miss: the model is asked
again, and its `Put` replaces the value. A held vector whose length isn't `Config.Dimensions` is no
miss but the same permanent error as a new one of that length (L11). `Cache` keeps `Get` and `Put`
alone: docinfer removes nothing, so a held refusal stays until its key changes, by `Version`, a
request version, or the model, or the host removes it from its own store. `FileCache` and
`MemoryCache` are safe for concurrent use and run `docinfertest.Cache`, as `postgres`'s cache does.
A golden key per capability pins `Key`'s form, failing with "change the version" (L27). The spike's
held results are not carried over, so the cache starts empty.

### The pipeline

The pipeline holds the batching, the cache, the join of work in flight, the go-ai calls and the
mapping of their failures, and the retry of a refused embedding batch one text at a time. It is
configured with a built go-ai client per capability (L3), and declares no `Describer`,
`Transcriber`, or `Embedder`: its tests drive the client against go-ai's `aitest`, which tests the
go-ai mapping too.

```go
package docinfer
type Config struct {
    Vision        *Vision        // nil skips images, counted
    Transcription *Transcription // nil skips windows, counted
    Extraction    *Extraction    // nil: a run that asks for extraction fails, permanent
    Embeddings    Embeddings     // required
    Chunker       chunk.Splitter // its Bound at least 1
    Dimensions    int            // at least 1: the embedding space's; a vector of another
                                 // length, made or held, fails the job, permanent (L11)
    Cache         Cache          // required
    Bounds        *Bounds        // required: the requests in flight per capability, shared
    Pending       int            // at least 1: a job's inputs admitted and not yet finished
    EmbedBatch    int            // at least 1: texts per Embed call
    BatchBytes    int            // at least 1: a batch's marshalled JSON's cap; no default (L24)
    Join          join.Request   // how a part's window transcripts join (L20)
}
// ErrRequest is what New, NewBounds, and Run's refusal of a bad value match.
var ErrRequest = errors.New("docinfer: invalid request")
// Bounds bounds the requests in flight per capability, across every Pipeline built with it.
type Bounds struct{ … }
// NewBounds refuses a bound below 1, matching ErrRequest (L24).
func NewBounds(vision, transcription, extraction, embeddings int) (*Bounds, error)
// New panics naming the field on a nil Cache, Bounds, or Embeddings.Client, or a set Vision,
// Transcription, or Extraction whose Client is nil: a wiring defect. It refuses, matching
// ErrRequest, a count or bound below 1 (Chunker.Bound, Dimensions, Pending, EmbedBatch,
// BatchBytes, Join.WordsPerSecond, a MaxTokens) and an empty Model, as dispatch.New does (L3,
// L22, L24).
func New(cfg Config) (*Pipeline, error)
// Profile is the resolved identity: the models, settings, dimensions, and chunker, known before
// any job runs.
func (p *Pipeline) Profile() Profile
// Run infers over one job's inputs, opening their content from content, and yields its
// inference texts, extraction progress, batches, and counts as made.
func (p *Pipeline) Run(ctx context.Context, content dispatch.Content, inputs iter.Seq2[Input, error], opts Options) iter.Seq2[Result, error]

type Options struct { // one run's
    Extract *Extract // a caller's prompt and schema; nil extracts nothing
    Final   bool     // the job's last delivery ("go-ai use")
}
type Extract struct {
    Name   string         `json:"name"`   // the schema's, 1 to 64 letters, digits, _ and -
    Prompt string         `json:"prompt"`
    Schema jsontext.Value `json:"schema"` // a JSON Schema object
}
type Input struct {
    // Event is an output's event: text or markdown chunked; image described; audio transcribed,
    // then joined per part; an audio plan record admitted for its window count (L19).
    // Its content is opened by its Output.SHA256 from Run's content, only on a cache miss or to
    // chunk its text (L2).
    Event    dispatch.Event
    Metadata string // or the input's metadata text, Event then the depth-0 part's own event
}
// MetadataText is an input's metadata text, from its depth-0 part: the one builder of it.
func MetadataText(p dispatch.Part) string
// MissingError is Run's error for an input whose content Run's content misses: Event is the
// input's, and Err is Content.Open's, which matches fs.ErrNotExist (W3). Permanent doesn't
// report it.
type MissingError struct { Event dispatch.Event; Err error }
func (e *MissingError) Error() string
func (e *MissingError) Unwrap() error
type Result struct { // one field is set
    Inference *Inference
    Progress  *Progress
    Batch     *Batch
    Done      *Counts // last
}
```

A replica holds one `Bounds` and builds one pipeline per profile over it, so it bounds each
capability as a whole whatever profiles its jobs use. A pipeline is built once and runs many jobs.
It joins work in flight by `Key`, as "Join of work in flight" decides: a result one job has asked a
model for, another waits for rather than asks again; a waiter whose leader ends without a result
asks the model itself; a cancelled waiter stops at once; a waiter holds no `Bounds` slot, and its
result counts as `Held`. Each go-ai call takes the job's context, and the client is safe for
concurrent use. One unexported runner over `Bounds` serves every capability; `Cache.Put` runs
outside any lock; `yield` is called from `Run`'s goroutine alone, and `Run` waits for its requests
in flight before it returns (L5).

`Run` reads each input by `dispatch`'s constants, never literals (L21):

- **Text and Markdown** (`dispatch.TextOutput`, `MarkdownOutput`) outputs are chunked, as sources
  of kind `output`, and extracted. A `Metadata` input is chunked as kind `metadata`.
- **An image** (`dispatch.ImageOutput`) is described from its content as
  `protocol.DataURL(Output.MediaType, data)`; an image output without a `MediaType` is the input's
  defect, a permanent error (L25). Its text is chunked as kind `vision`, and extracted.
- **A window** (`dispatch.AudioOutput`) is transcribed with
  `ai.TranscriptionRequest{Open, FileName, Language, Prompt}`, `Open` calling
  `content.Open(ctx, Output.SHA256)` per attempt, and `FileName` made from `Output.MediaType`,
  `.wav` for `audio/wav` and `.mp3` for `audio/mpeg`, any other a permanent error (L13, W3). Windows
  are grouped by `{Part.ID, Event.Stage}`, placed by `dispatch.MetaWindow`, the first event of an
  index winning, since events repeat (L19, L21). A group ends when its windows are all in: at the
  count an audio plan record (`dispatch.RecordOutput` of `dispatch.MediaAudioPlan`) gives, decoding
  its `windows` alone; at a last window, the one without `dispatch.MetaSeamAfter`, once every index
  before it is in; or when `Run`'s inputs end. A video plan record (`MediaVideoPlan`) holds no
  windows, so it ends none. The group is then joined, each seam's bound derived from its span,
  `MetaSeamAfter` of the earlier window or `MetaSeamBefore` of the later, at
  `Config.Join.WordsPerSecond` (L20), and the transcript is chunked as kind `transcript`, and
  extracted. A window refused, failed, incomplete, or never cut joins as empty and is listed in
  the transcript's `Failed`.
- **Any other output**, another record among them, is skipped.

Content is opened only on a miss, or to chunk an output's text: an output's `SHA256` is already its
key's content (L2). A content `content` misses is `*MissingError`.

Extraction runs the run's `Extract` over each source text but metadata, once each, whole: a
`protocol.Request` of `Instructions` the prompt, one user `protocol.Message` of the text as
`protocol.InputText`, `Text: protocol.TextParam{Format: protocol.JSONSchemaFormat{Name, Schema,
Strict: true}}`, its `MaxOutputTokens` and `Reasoning.Effort` `Extraction`'s, sent with
`GenerateStream` and folded by a `protocol.Accumulator` (L15). Through a Chat Completions client,
the format sends the schema as `response_format` `json_schema`. Each `protocol.OutputTextDelta` is
yielded as `Progress`, coalesced to at most one a second per source text, each carrying the text
since the last. A stream that ends in `ResponseCompleted` is outcome extracted, its text the folded
response's `OutputText`, the JSON the schema describes, which nothing decodes; one that ends in
`ResponseIncomplete` is outcome incomplete, its partial text kept and held; one with a refusal and
no text is refused, held. A stream that breaks part way holds nothing and takes the transport row
("go-ai use"): the progress already yielded stands, and the redelivery asks again. `Run` refuses an
`Extract` whose name, prompt, or schema is empty or invalid, matching `ErrRequest`, before it reads
an input.

The root's package doc states the consumer's contract (L6): a part's results may be yielded before
its own event, whatever it comes to, and a consumer drops a quarantined or failed part's
inferences and chunks by that part's own event. `job` holds no output back until its part's event,
which would cost the "as made" order. The doc also names the host as the owner of a request's
time ("go-ai use", L12).

`chunk` is the chunker, its `Splitter` the one spelling of a split (L23):

```go
package chunk
// Chunk's JSON is docinfer.Chunk's, which embeds it, versioned by docinfer.Version.
type Chunk struct {
    Text string `json:"text"`
    Span Span   `json:"span"`
}
type Span struct { // the text's [Start, End) bytes
    Start int `json:"start"`
    End   int `json:"end"`
}
const Version = "1" // of the splitting; a golden corpus of chunks is named for it (L27)
var ErrRequest = errors.New("chunk: invalid request")
type Splitter struct { Bound int } // at least 1
func (s Splitter) Settings() string // Version and Bound: Profile.Chunker's, keying nothing (L16)
func (s Splitter) Split(text string) ([]Chunk, error) // a Bound below 1 matches ErrRequest (L24)
```

`join` is processing's `audio` join, moved, with no build tag (L22):

```go
package join
var ErrRequest = errors.New("join: invalid request")
type Request struct {
    WordsPerSecond int // at least 1: a seam's bound is its span's seconds times this, rounded up,
                       // and at least 2, the fewest words a seam matches on; no default (L20)
}
// Join merges window transcripts, in plan order, a refused or missing window "", seams[i] the
// seconds window i shares with window i+1. A Request below 1 matches ErrRequest.
func Join(transcripts []string, seams []float64, req Request) (Transcript, error)
type Seam struct { // seams[i] is window i's with window i+1, versioned by docinfer.Version
    Matched  bool `json:"matched"`
    Matches  int  `json:"matches"`
    Overlap  int  `json:"overlap"`
    Position int  `json:"position"`
}
type Transcript struct { Words []string; Seams []Seam }
func (t Transcript) Text() string
```

`TestJoin`'s table and the settled-prefix tests move as they are (L22).

### Batches as made

`Pipeline.Run` streams per job, in the order things are made (L1):

- an `Inference` as each image's text, each part's joined transcript, or each extraction is made,
  or read from the cache, and an extraction's `Progress` as it streams;
- a `Batch` once its chunks' embeddings complete. A batch is yielded when the next chunk, with the
  part and source it adds, would take the batch's marshalled JSON past `Config.BatchBytes`, or
  when no embedded chunk is waiting, so it never waits to fill; a chunk alone past the cap is a
  batch of one;
- the job's `Counts` last.

A result is held as it completes, before anything derived from it is yielded (L1). `Run` pulls its
next input only while fewer than `Config.Pending` of the job's inputs are admitted and not yet
finished, so a slow model holds the job's dispatch back rather than letting it run ahead. Neither
`Pending` nor `BatchBytes` has a library default (L24): Cerebro and `cmd/docinfer` ship their own,
512 KiB the starting cap, which keeps a batch under NATS's default 1 MB `max_payload` (a
768-dimension vector is 4,096 B in base64). A batch carries no index: Cerebro identifies it, and
`cmd/docinfer` writes chunks, not batches.

### The shapes

```go
package docinfer
// Version is "1", grown by addition only: of the shapes below, Held, job's, chunk's, and join's.
// It includes dispatch.Version "1", whose shapes Input, Batch, and Source carry, so a break
// there is a break here. Key hashes it (L18).
const Version = "1"
type Capability string // CapabilityVision, CapabilityTranscription, CapabilityExtraction, and
                       // CapabilityEmbeddings: vision, transcription, extraction, embeddings
type Identity struct {
    Capability Capability `json:"capability"`
    Model      string     `json:"model"`
    Settings   string     `json:"settings"` // the JSON of the capability's settings, its first
                                            // member "request", the request's version (L17);
                                            // embeddings: "" (L16)
}
type Profile struct {
    Vision        *Identity `json:"vision,omitempty"`
    Transcription *Identity `json:"transcription,omitempty"`
    Extraction    *Identity `json:"extraction,omitempty"` // its settings less prompt and schema
    Embeddings    Identity  `json:"embeddings"`
    Dimensions    int       `json:"dimensions"` // Config.Dimensions, checked per vector
    Chunker       string    `json:"chunker"`    // the Splitter's Settings
}
type Usage struct {
    Input  int `json:"input"`
    Output int `json:"output"`
}
type Outcome string // OutcomeDescribed, OutcomeTranscribed, OutcomeExtracted, OutcomeIncomplete,
                    // OutcomeRefused, OutcomeFailed: described, transcribed, ..., failed
type Inference struct {
    Capability    Capability  `json:"capability"`           // vision, transcription, extraction
    Part          string      `json:"part"`                 // its part's ID
    ContentSHA256 []string    `json:"content_sha256"`       // the image's, the windows' in order
                                                            // ("" for one never cut), or the
                                                            // source text's
    Outcome       Outcome     `json:"outcome"`
    Reason        string      `json:"reason,omitzero"`      // an image's or extraction's: why
                                                            // incomplete, refused, or failed
    Failed        []int       `json:"failed,omitempty"`     // a transcript's windows not
                                                            // transcribed, by index
    Held          bool        `json:"held,omitzero"`        // read from the cache or a leader,
                                                            // not asked of a model
    TextSHA256    string      `json:"text_sha256,omitzero"` // the text, in docproc's Content
    Text          string      `json:"-"`                    // local: job puts it in Content
    Seams         []join.Seam `json:"seams,omitempty"`      // a transcript's, as join made them
    Usage         Usage       `json:"usage,omitzero"`
    TookMS        int64       `json:"took_ms"`
}
type Progress struct { // an extraction's text since its last progress
    ContentSHA256 string `json:"content_sha256"` // the source text's
    Delta         string `json:"delta"`
}
// Batch holds its chunks, and each part and source text they come from once, by index.
type Batch struct {
    Parts   []dispatch.Part `json:"parts"`   // provenance: each, its parent and depth, where found
    Sources []Source        `json:"sources"`
    Chunks  []Chunk         `json:"chunks"`
}
// Digest is the SHA-256 over its chunks' digests, in order: derived, so no field holds it.
func (b Batch) Digest() string
// Source is the text a chunk is cut from, and where it sits.
type Source struct {
    Kind       string             `json:"kind"`            // output, metadata, vision, transcript
    SHA256     string             `json:"sha256,omitzero"` // the text's; an output's is its
                                                            // Output.SHA256, held there alone
    Incomplete string             `json:"incomplete,omitzero"`
    Part       int                `json:"part"`            // an index into the batch's Parts
    Stage      *dispatch.Identity `json:"stage,omitempty"`
    Output     *dispatch.Output   `json:"output,omitempty"` // the output it is, or the image or
                                                             // first window its inference read
}
// Chunk embeds chunk.Chunk, whose fields encode inline, so its JSON is {digest, text, span,
// index, source, vector, error}.
type Chunk struct {
    Digest      string `json:"digest"` // SHA-256 of Text: the per-item digest, its embedding's
                                       // key content (L16)
    chunk.Chunk        // inline: text, span
    Index       int    `json:"index"`           // within its source text
    Source      int    `json:"source"`          // an index into the batch's Sources
    Vector      string `json:"vector,omitzero"` // standard base64 of little-endian float32s
    Error       string `json:"error,omitzero"`  // why the endpoint refused it, or it failed
}
// DecodeVector is the one reader of Chunk.Vector and Held.Vector.
func DecodeVector(s string) ([]float32, error)
// Counts is one job's, counting occurrences (L2).
type Counts struct {
    Texts         int                  `json:"texts"`    // text and Markdown outputs chunked
    Metadata      int                  `json:"metadata"` // metadata texts: one per job
    Embedded      int                  `json:"embedded"` // chunks with a vector
    Rejected      int                  `json:"rejected"` // chunks refused, or failed on the last
                                                          // delivery
    Held          int                  `json:"held"`     // of those, read from the cache
    Requests      int                  `json:"requests"` // embeddings requests sent
    Images        int                  `json:"images"`   // image outputs, vision run or not
    Vision        *VisionCounts        `json:"vision,omitempty"` // nil when skipped
    Windows       int                  `json:"windows"`
    Transcription *TranscriptionCounts `json:"transcription,omitempty"`
    Extraction    *ExtractionCounts    `json:"extraction,omitempty"` // nil when none was asked
    Tokens        Tokens               `json:"tokens"`   // this job's requests only
}
type VisionCounts struct { // each image under its outcome; Empty of the Incomplete
    Described, Incomplete, Empty, Refused, Failed, Held, Requests int
}
type TranscriptionCounts struct { // each window under its outcome
    Transcribed, Incomplete, Refused, Failed, Held, Requests, Transcripts int
    SeamsMatched, SeamsUnmatched                                          int
}
type ExtractionCounts struct { // each source text under its outcome
    Extracted, Incomplete, Refused, Failed, Held, Requests int
}
type Tokens struct { Vision, Transcription, Extraction *Usage; Embeddings Usage }
```

An embedding is keyed by `Key(Identity{"embeddings", model, ""}, chunk.Digest)`: model and the
chunk's SHA-256 alone, so a chunk within both bounds is never embedded twice; the chunker stays in
`Profile` (L16). Every other capability's settings are one encoding, the JSON of its settings in a
fixed member order, led by `request`, its request version, changed when the request's construction
changes (L17): vision's `request`, `prompt`, `max_tokens`, `effort`; transcription's `request`,
`mode` (`route` or `chat`, read from its client, G2), `prompt`, `language`; extraction's
`request`, `name`, `prompt_sha256`, `schema_sha256`, `max_tokens`, `effort`.

`Embedded` and `Rejected` add up to the chunks the job's batches hold. The JSON names are
snake_case (`seams_matched`). `Counts` counts occurrences, not distinct content: each image,
window, source text, and chunk as often as the job holds it, and the cache hits among them, so no
count needs a set of the job's digests (L2); the outputs and the parts by outcome are dispatch's
facts, in `job.Done`. Each `Usage` is less `Total`.

A transcript's outcome is transcribed when every window was; otherwise failed when a window
failed, refused when one was refused and none failed, and incomplete when the rest were incomplete
or never cut. A chunk is identified by its job, its part's `ID`, its source's kind and text
SHA-256, and its index: the same content in two places is two chunks, and a redelivery's repeat is
the same one. Per-item digests, provenance, and the profile are docinfer's, and warnings docproc's.

### One job, and its cancellation

```go
package job
type Config struct {
    Dispatcher      *dispatch.Dispatcher // required
    Pipeline        *docinfer.Pipeline   // required
    CheckpointEvery int                  // at least 1: messages between a store's checkpoints
}
// New panics naming a nil Dispatcher or Pipeline, and refuses a CheckpointEvery below 1,
// matching docinfer.ErrRequest.
func New(cfg Config) (*Runner, error)
// Run dispatches one job's input and infers over it, yielding its updates as made, in the order
// below, and done last.
func (r *Runner) Run(ctx context.Context, in dispatch.Input, opts docinfer.Options) iter.Seq2[Update, error]

// Update is one thing Run yields; one field is set. It is the Go side's one-of alone: each
// field's type is its own JSON shape, so Update has none.
type Update struct {
    Event      *dispatch.Event // a part's own, an output's, or a warning's
    Inference  *docinfer.Inference
    Progress   *docinfer.Progress
    Batch      *docinfer.Batch
    Checkpoint *Checkpoint
    Done       *Done
}
type Checkpoint struct { // a store's cursor, a low watermark
    After uint64 `json:"after"` // the last message, in read order, whose updates, and every
                                // earlier message's, are out, their batches included
}
type Done struct { // one attempt's
    Parts   map[dispatch.Outcome]int `json:"parts"`   // by outcome, every depth
    Outputs int                      `json:"outputs"` // by part ID, never by event
    Counts  docinfer.Counts          `json:"counts"`
}
```

A job's ID is its host's: `Run` takes the input alone, and Cerebro names the job in each event's
subject. `job` feeds `Run` from dispatch's events: each output event, the plan records among them
(L19), and the depth-0 part's own event with its `Metadata`, `docinfer.MetadataText`'s.

`job` has one content store, the dispatcher's (`Dispatcher.Content()`): the dispatcher stores each
output's content there before its event, the pipeline opens its inputs' content from it, and `Run`
puts each inference text into it before its inference's update. `job` keeps no output's file past
its event, and no quarantined part's file. A process that dies leaves its files in the dispatcher's
`Dir`, so a host empties its `Dir` when it starts; Cerebro's work queue redelivers the job.

A content the store misses reaches `job` as `*docinfer.MissingError`. `job` drops its part's entry,
`Dispatcher.Index().Drop` on `dispatch.Key{SHA256: e.Part.SHA256, Stage: e.Stage}`, `e` the error's
`Event`, and stops the attempt with the error, which is transient, so the work queue redelivers
the job, whose replay prepares the nearest part with a file again; its events may repeat, so `Done`
counts parts by ID, never events. `cmd/docinfer` has no index, and reports the miss as its run's
error.

Updates keep one order: an output's update precedes anything derived from it (its inference, the
batches holding its chunks), and a part's update follows its outputs', warnings', and children's.
Nothing else is promised. A part's results may precede its own event, which a consumer reads to
drop a quarantined or failed part's results (L6).

`job` tracks a store's progress by its low watermark alone: a checkpoint carries the last message,
in read order, whose updates, and every earlier message's, are out, never the highest
`Event.After`, at most every `CheckpointEvery` messages. A host resumes from the last checkpoint by
setting `dispatch.Input.After` on the redelivery. `job` threads no mail: a consumer threads the
record outputs with docproc's `email.Threader`, so no threader state joins the checkpoint.

A job is cancelled through `Run`'s context, which is the job's own. Its end stops the job's tool
runs, its index waits and claims, its join waits, and its go-ai calls, since each takes the
context. It removes the job's files and yields `ctx.Err()` alone, last. Other jobs on the same
`Runner` go on. Stopping the loop early does the same.

### go-ai use

The pipeline calls go-ai's client itself, one configuration per capability, each holding a client
its host built (L3). The go-ai mapping lives here, beside the calls it maps, and is stated here
alone.

```go
package docinfer
type Vision struct {
    Client    *ai.Client
    Model     string
    Prompt    string                   // the user message's InputText beside the image
    MaxTokens int                      // at least 1: MaxOutputTokens
    Reasoning protocol.ReasoningEffort // "" sends none
}
func (v Vision) Identity() Identity // {vision, Model, its settings}
type Transcription struct {
    Client           *ai.Client // its TranscriptionMode() keys the cache (G2)
    Model            string
    Prompt, Language string     // "" sends none
}
func (t Transcription) Identity() Identity // {transcription, Model, its settings, mode included}
type Extraction struct {
    Client    *ai.Client
    Model     string
    MaxTokens int // at least 1
    Reasoning protocol.ReasoningEffort
}
func (x Extraction) Identity(e Extract) Identity // {extraction, Model, e hashed, MaxTokens, …}
type Embeddings struct {
    Client *ai.Client
    Model  string
}
func (e Embeddings) Identity() Identity // {embeddings, Model, ""} (L16)
// Permanent reports whether an error Pipeline.Run or job.Runner.Run yields is one no
// redelivery changes: a row the table marks permanent, a mismatched input (dispatch.ErrSHA256),
// an input's defect (an image without a MediaType, a window of another media type), a vector
// whose length is not Config.Dimensions, a refused Extract (ErrRequest), or a run asking for
// extraction with no Config.Extraction.
func Permanent(err error) bool
```

| go-ai | docinfer |
| --- | --- |
| `protocol.Usage{InputTokens, OutputTokens}`, `ai.TranscriptionUsage{InputTokens, OutputTokens}` | `Usage{Input, Output}` |
| `ai.EmbeddingUsage{PromptTokens}` | `Usage{Input}` |
| a reply in `protocol.StatusCompleted`, its text `(*protocol.Response).OutputText` (G1) | outcome described or extracted; held |
| a reply in `protocol.StatusIncomplete`, `Response.IncompleteDetails.Reason` | outcome incomplete, its text kept; `Held.Incomplete`, `Inference.Reason`, and `Source.Incomplete` the reason; held |
| a completed reply with a `protocol.Refusal` part and no output text (L10) | outcome refused, `Held.Refused` and `Inference.Reason` the refusal's text; held |
| `*ai.IncompleteTranscriptError` (L9) | the window's outcome incomplete, joined as empty; held under its settings, `Held.Incomplete` its `Reason` |
| `*ai.APIError` 400, 413, 415, or 422 | refused: the inference's outcome refused, or the chunk's `Error`, its text the error's; recorded and held. An embedding batch so refused is re-sent a text at a time, and only then (L8) |
| `*ai.APIError` 401, 403, 404, 501, 505, or any other 4xx but 408 and 429 | permanent: the error, wrapped, stops the job, and `Permanent` reports it |
| `*format.UnsupportedError` (L9) | permanent, as the row above |
| `*ai.APIError` 408, 429, or any 5xx but 500, 501, and 505, once go-ai's retries (408, 429, 502, 503, 504) are spent | transient: the error, wrapped, stops the job, and a redelivery retries it; its `*ai.APIError`'s `RetryAfter` is the redelivery's delay (L7) |
| `*ai.APIError` 500; a 2xx reply in `protocol.StatusFailed`, a stream's `ResponseFailed` among them; a 2xx reply that doesn't decode, and any other error a call returns (L9) | the error, wrapped: it stops the job; on the last delivery (`Options.Final`), the item's outcome failed, or the chunk's `Error`, recorded, never held, and the job goes on |
| a transport failure: an `*url.Error` with no reply, the transport's `ResponseHeaderTimeout` among them, or a stream that breaks, matching `io.ErrUnexpectedEOF`, `Options.StreamIdleTimeout`'s included | transient: the error, wrapped, stops the job |
| a cancelled context | `ctx.Err()`, alone and last |

docinfer refuses only the statuses that judge the content (the refused row); a status that judges
the caller or the route, or a request the format can't send, fails the same way on every delivery,
so it ends the job at once (the permanent rows). A job the gateway turned away, or failed
transiently, stops, the work queue redelivers it after `RetryAfter`, and the cache replays what it
finished, since such a status is the gateway's or an upstream's, never the item's. Only the last
delivery records an item failed, so one item a model can't answer never holds the rest back. Each
row has its own test against `aitest` (L7).

A request's time is the host's (L12, C9): it bounds `Generate`, `Embed`, `Transcribe`, and the
`/models` listing with its transport's `ResponseHeaderTimeout`, and extraction's stream with
`ai.Options.StreamIdleTimeout`, which ends a stalled stream without cutting off a long one.
docinfer sets no deadline and no default of its own, and the root's doc says so.

Cerebro, per profile, builds a go-ai client over the gateway and sets `docinfer.Vision`,
`Transcription`, `Extraction`, and `Embeddings` into a `docinfer.Config`, over its replica's one
`Bounds`. `cmd/docinfer` runs no extraction: it builds `Vision`, `Transcription`, and `Embeddings`
from its configuration's nodes (C1).

docinfer and Cerebro call none of the following, so none is part of this surface: function tools
(`protocol.FunctionTool`), tool choice, `FunctionCall` and `FunctionCallOutput` items, the
reasoning round trip (`protocol.Reasoning` items, their `EncryptedContent`),
`ReasoningParam.Summary`, `TextParam.Verbosity`, `InputImage.Detail`, `protocol.InputAudio`
(docinfer transcribes through `Transcribe`, whose chat mode builds it), the `Native` tiers of
`Request`, `EmbeddingRequest`, and `TranscriptionRequest`, the refusal events (`RefusalDelta`,
`RefusalDone`, which the `Accumulator` folds into the `Refusal` part docinfer reads), and
`ai.TranscriptionInstruction`. `format/messages` and `format/responses` serve `cmd/docinfer`'s
endpoints that name them; Cerebro uses `format/completions` alone. `ai.Options.Query` and
`(*ai.Client).Models` (C5) are `cmd/docinfer`'s. go-ai's `tools/infer` and its harness-session
surface are outside both, and so outside docinfer.

### cmd/docinfer

`cmd/docinfer` runs the pipeline over a `cmd/docproc` run directory and writes its own. Its
composition root is a graph (C1): a node for the configuration, read from the directory `--config`
or `DOCINFER_CONFIG` names in the node's constructor; a node per capability's `*ai.Client`, built
as `ai.New(baseURL, httpClient, wireFormat, ai.Options{…})` and reporting its error named for its
endpoint (C7); and a node for `run.Config`, holding
`docinfer.Vision`, `Transcription`, and `Embeddings`; `run.Commands(n.RunConfig)` takes it. A
configuration error fails the Build, labelled with its node, before the docproc run is read.

The configuration ships the operator's starting values, which the library leaves none (L24):

```text
endpoints.<name>     base_url; format: completions, messages, or responses; profile, the format's
                       Options, decoded as tools/infer's formatOptions is (C6); query; retry
                       {max_attempts, base_delay, max_delay}, the command's starting policy (C6);
                       response_header_timeout, its starting value above vision's slowest observed
                       image, 38.9 s, and stream_idle_timeout (C9); token and headers in the
                       secrets file (C6)
vision               endpoint, model, prompt, max_tokens 2048, reasoning none (a
                       protocol.ReasoningEffort, refused by its UnmarshalText, C7), concurrency 4
transcription        endpoint, model, prompt, language, mode route or chat, set on this
                       capability's own client as ai.TranscribeRoute or ai.TranscribeChat, chat
                       for an endpoint with no transcription route and refused without the
                       completions format (C6),
                       words_per_second 8, concurrency 4
embeddings           endpoint, model, dimensions, batch 16, chunk_size 2000, concurrency 4
run                  pending, batch_bytes 524288
```

Each override is `DOCINFER_` and its setting's path, composed where it is read (C8). `validate`
keeps what is docinfer's: a capability's endpoint exists, and the counts are at least 1 (C7). Each
endpoint has one `http.Client` over a cloned default transport with its `ResponseHeaderTimeout`,
and each capability's client its endpoint's `Options` with `StreamIdleTimeout`, `Retry`, `Header`,
`Query`, `Token`, and, for transcription, `TranscriptionMode`, `ai.TranscribeRoute` for `route`
and `ai.TranscribeChat` for `chat`. A setting that changes a result is
in the manifest.

The commands, each with a footer stating that recorded refusals exit 0, what exits 1, and what the
last line counts (C20):

```text
docinfer run <docproc-dir> --out <dir> [--run <id>] [--report]
    infers over docproc's run, Dir.Latest's, the latest that ended, or --run's, validated as
    docproc's is (C4); --report renders the run's report after it, never after a cancellation,
    a failed render exiting 1 with the run intact (C19); a cancelled run exits 1 (C23)
docinfer report <dir> [--run <id>]
    renders a run's report, the latest that ended or --run's; Make prints the index's absolute
    path (C19)
```

Before it reads docproc's run, `run` checks each capability's model: `(*ai.Client).Models`, once per
capability's client, so it lists through the same query, token, headers, and retry the run calls
with, reading `status.value` from `ai.Model.Native`; an `*ai.APIError` is "no listing", any other
error "unreachable". The states, the table, and the 30 s bound stay (C5).

`run` opens docproc's directory with `rundir.Open` and feeds `Pipeline.Run` from `Dir.Record` and
`Dir.Content()`, each output line's event as an `Input` and each input's part line with
`MetadataText`'s text and the record's `File` lines after it (C4); its count line and progress lines
are in `output.go`, printed from `Result` values alone (L4).

The directory is schema "2":

```text
<dir>/
  outdir.json                         the root marker: {"schema": "2"} (C11)
  results/<capability>/<xx>/<hash>.json   the held results: a docinfer.FileCache
  texts/<xx>/<sha256>                 each inference text: a dispatch.FileContent, no .txt (C10)
  runs/<run>/manifest.json            the run's manifest, written as it starts and as it ends
  runs/<run>/chunks.jsonl             its chunk record: each batch's lines appended as yielded (C12)
  runs/<run>/inferences.jsonl         each docinfer.Inference appended as yielded (C15)
  runs/<run>/report/                  index.html and a <sha256>.html per input content (C16)
```

```go
package outdir
// Open opens the directory at root to read it, creating nothing; Create makes it, its marker,
// results/, and texts/ when they are absent. Each refuses another schema, naming both, so no "1"
// directory's results/ and texts/ are reused (C11).
func Open(root string) (*Dir, error)
func Create(root string) (*Dir, error)
func (d *Dir) Runs() ([]string, error)                   // its runs' ids, in order
func (d *Dir) Latest() (string, error)                   // the latest run that ended
func (d *Dir) Manifest(id string) (Manifest, error)
func (d *Dir) Lines(id string) iter.Seq2[Line, error]    // the chunk record, vectors left out (C12)
func (d *Dir) Inferences(id string) iter.Seq2[docinfer.Inference, error]
func (d *Dir) Cache() *docinfer.FileCache                // over results/
func (d *Dir) Texts() dispatch.Content                   // over texts/
func (d *Dir) ReportDir(id string) (string, error)       // runs/<run>/report, not made
func (d *Dir) Begin(m Manifest) (*Run, error)            // on a Dir from Create
func (r *Run) Inference(i docinfer.Inference) error      // one line, written through
func (r *Run) Batch(b docinfer.Batch) error              // its lines, written through
func (r *Run) End(m Manifest) error                      // the manifest, its end set

type Manifest struct {
    Schema       string          `json:"schema"`            // "2"
    Run          string          `json:"run"`               // a version 7 UUID
    Docinfer     Build           `json:"docinfer"`          // {command, library}, from the
                                                             // build info (C13)
    Start        schema.Time     `json:"start"`             // docproc's rundir/schema.TimeOf (C24)
    End          schema.Time     `json:"end,omitzero"`      // marks the record whole (C12)
    Docproc      Docproc         `json:"docproc"`
    Inputs       []Input         `json:"inputs"`            // the docproc run's, in order
    Capabilities []Capability    `json:"capabilities"`
    Skipped      []Skipped       `json:"skipped,omitempty"`
    Space        *Space          `json:"embedding_space,omitempty"`
    Counts       docinfer.Counts `json:"counts"`
    Outputs      int             `json:"outputs"`           // the docproc run's output lines read
    Error        string          `json:"error,omitzero"`
}
type Build struct { Command, Library string } // "command", "library"
type Docproc struct {
    Dir string `json:"dir"` // docproc's run directory, relative to this one, recorded once (C17)
    Run string `json:"run"`
}
type Input struct { // one input of docproc's run (the report's shape)
    Path   string `json:"path"`             // as docproc's record gives it
    SHA256 string `json:"sha256,omitzero"`  // its depth-0 part's; none for an error line
    Error  string `json:"error,omitzero"`
}
type Capability struct { // C14
    docinfer.Identity         // inline: capability, model, settings, the cache's key
    Endpoint    string `json:"endpoint"`
    BaseURL     string `json:"base_url"`
    Format      string `json:"format"`
    Concurrency int    `json:"concurrency"`
    Batch       int    `json:"batch,omitzero"`      // embeddings
    ChunkSize   int    `json:"chunk_size,omitzero"` // embeddings
}
type Skipped struct { Name, Reason string } // "name", "reason": not configured
type Space struct { … } // the measured embedding space and its norms, as the spike's (L11)
// Line is a batch's chunk with what its indexes name inline, since chunks.jsonl holds no batches.
type Line struct {
    Digest      string          `json:"digest"`
    chunk.Chunk                 // inline: text, span
    Index       int             `json:"index"`
    Vector      string          `json:"vector,omitzero"`
    Error       string          `json:"error,omitzero"`
    Part        dispatch.Part   `json:"part"`
    Source      docinfer.Source `json:"source"` // its part index 0: Part is inline
}
```

`outdir` owns its whole-file write, its run list, and its id check, each `Dir` method refusing an
id that isn't one of its runs (C10). A record is appended, never renamed, so a stopped run keeps
the lines it made, and a reader of a run whose manifest has no `end` drops a last line with no
newline, as `rundir`'s does (C12). The report hashes each text it reads from `texts/`, and renders
one whose SHA-256 isn't its name as damaged: `ReadText`'s digest check is kept, in the report
(C10, C23). The manifest's capabilities tie it to its held results by the same `Identity` (C14),
and its inputs list each input's SHA-256, since a chunk line's part is the part it was cut from,
not the input. A refused or failed image or window is found in `inferences.jsonl`, by its part's
ID and content SHA-256 (C15).

The report follows "The report's shape": per run, `report/index.html` and a `<sha256>.html` per
input content, the name docproc's page for the same content has, so the cross link is one relative
path; an input with no SHA-256 keeps its row, its error, and its metadata chunk on the index; each
vision text inline and whole with its outcome and an `img` at docproc's `rundir.ContentPath`; each
transcript with its windows, failed windows, and seams; each source once, as its chunks in order,
grouped by part ID, source kind and text SHA-256, and index (C18), with no vector. It renders from
docinfer's directory alone, reads no clock, and a test renders it with docproc's directory absent.
`report.Render(ctx, dir, id)` writes the pages; its golden pages over a committed directory hold a
long text, a damaged text, and an unended run (C23).

`cmd/docinfer`'s tests write the docproc run directories they read with `rundir.Create` and
`Begin`, and drive go-ai's `aitest`, its model list included (C22); each test package's
`main_test.go` sets Asia/Kolkata (C24). `integration` runs the built binary as a subprocess on
go-core's `processtest`, against the endpoint a variable names, skipped when it is unset; the check
vets and lints it without running it (C21).

### Names

Every exported name layers' docinfer sections write, by where it lives, against this API:
**carried** under its name (its shape changed where the reason says), **renamed**, or **dropped**.

| layers | verdict | reason |
| --- | --- | --- |
| module `github.com/standards-lab/docinfer` | carried | requires docproc and go-ai alone |
| module `github.com/standards-lab/docinfer/postgres` | carried | |
| module `github.com/standards-lab/docinfer/cmd/docinfer` | carried | go-cli-sdk v0.4.0, go-core v0.12.0 (C2) |
| package `docinfer` | carried | the root |
| package `chunk` | carried | |
| package `join` | carried | from processing's `audio`; no build tag (L22) |
| package `job` | carried | |
| package `internal/endpointtest` | dropped | go-ai's `aitest` (W2, G3) |
| `internal/visiontest`, `internal/audiotest` | dropped | stay in the spike, as layers has it |
| `apptest.Serve` | dropped | go-ai's `aitest` (G3, C22) |
| `Cache` | carried | |
| `Cache.Get`, `Cache.Put` | carried | `Cache` keeps them alone (L18) |
| `Key` | carried | `Version` joins its hash (L18) |
| `NewFileCache`, `FileCache` | carried | |
| `NewMemoryCache`, `MemoryCache` | carried | |
| `Held` | carried | a refusal and an incomplete reply held; versioned through `Key` (L18) |
| `Held.Text`, `Held.Vector`, `Held.Incomplete` | carried | |
| `Held.Refused` | carried | the refusal's text too (L10) |
| `Config` | carried | |
| `Config.Vision`, `Config.Transcription`, `Config.Extraction`, `Config.Embeddings` | carried | each holds a built `*ai.Client` (L3) |
| `Config.Chunker` | carried | `chunk.Splitter`, the one spelling (L23) |
| `Config.Dimensions` | carried | checked before `Put` and on `Get` (L11) |
| `Config.Cache`, `Config.Bounds` | carried | nil panics by name (L3) |
| `Config.Pending`, `Config.EmbedBatch` | carried | below 1 refused (L24) |
| `Config.BatchBytes` | carried | its 512 KiB default dropped (L24) |
| `Config.Join` | carried | `join.Request` of words per second (L20) |
| `Bounds` | carried | |
| `NewBounds` | carried | returns an error, refusing below 1 (L24) |
| `New` | carried | panics by name, refuses with `ErrRequest` (L3, L24) |
| `Pipeline` | carried | |
| `Pipeline.Profile` | carried | |
| `Pipeline.Run` | carried | |
| `Options`, `Options.Extract`, `Options.Final` | carried | |
| `Extract`, `Extract.Name`, `Extract.Prompt`, `Extract.Schema` | carried | refused up front, `ErrRequest` |
| `Input`, `Input.Metadata` | carried | |
| `Input.Event` | carried | admits a plan record for its window count (L19) |
| `MetadataText` | carried | |
| `MissingError`, `MissingError.Event`, `MissingError.Err`, `MissingError.Unwrap` | carried | from `dispatch.Content.Open` (W3) |
| `Result`, `Result.Inference`, `Result.Progress`, `Result.Batch`, `Result.Done` | carried | |
| `Version` | carried | in `Key`'s hash (L18) |
| `Capability` | carried | |
| `Identity`, `Identity.Capability`, `Identity.Model` | carried | |
| `Identity.Settings` | carried | one JSON encoding with a request version (L17); embeddings `""` (L16) |
| `Profile`, `Profile.Vision`, `Profile.Transcription`, `Profile.Embeddings`, `Profile.Dimensions`, `Profile.Chunker` | carried | |
| `Profile.Extraction` | carried | its settings less the prompt and schema |
| `Usage`, `Usage.Input`, `Usage.Output` | carried | |
| `Outcome` | carried | its values named as constants |
| `Inference`, `Inference.Capability`, `Inference.Part`, `Inference.Outcome`, `Inference.TextSHA256`, `Inference.Text`, `Inference.Seams`, `Inference.Usage`, `Inference.TookMS` | carried | |
| `Inference.ContentSHA256` | carried | `""` for a window never cut |
| `Inference.Failed` | carried | lists incomplete and uncut windows too (L9, L19) |
| `Inference.Held` | carried | a waiter's result too ("Join of work in flight") |
| `Progress`, `Progress.ContentSHA256`, `Progress.Delta` | carried | |
| `Batch`, `Batch.Parts`, `Batch.Sources`, `Batch.Chunks`, `Batch.Digest` | carried | |
| `Source`, `Source.Kind`, `Source.SHA256`, `Source.Incomplete`, `Source.Part`, `Source.Stage`, `Source.Output` | carried | |
| `Chunk`, `Chunk.Index`, `Chunk.Source`, `Chunk.Vector`, `Chunk.Error` | carried | |
| `Chunk.Digest` | carried | its embedding's key content (L16) |
| `Counts`, `Counts.Texts`, `Counts.Metadata`, `Counts.Embedded`, `Counts.Rejected`, `Counts.Held`, `Counts.Requests`, `Counts.Images`, `Counts.Vision`, `Counts.Windows`, `Counts.Transcription`, `Counts.Extraction`, `Counts.Tokens` | carried | by occurrence (L2) |
| `VisionCounts`, `VisionCounts.Described`, `VisionCounts.Incomplete`, `VisionCounts.Empty`, `VisionCounts.Refused`, `VisionCounts.Failed`, `VisionCounts.Held`, `VisionCounts.Requests` | carried | |
| `TranscriptionCounts`, `TranscriptionCounts.Transcribed`, `TranscriptionCounts.Refused`, `TranscriptionCounts.Failed`, `TranscriptionCounts.Held`, `TranscriptionCounts.Requests`, `TranscriptionCounts.Transcripts`, `TranscriptionCounts.SeamsMatched`, `TranscriptionCounts.SeamsUnmatched` | carried | `Incomplete` added beside them (L9) |
| `ExtractionCounts`, `ExtractionCounts.Extracted`, `ExtractionCounts.Incomplete`, `ExtractionCounts.Refused`, `ExtractionCounts.Failed`, `ExtractionCounts.Held`, `ExtractionCounts.Requests` | carried | |
| `Tokens`, `Tokens.Vision`, `Tokens.Transcription`, `Tokens.Extraction`, `Tokens.Embeddings` | carried | |
| `Vision` | carried | |
| `Vision.Client` | carried | `*ai.Client` |
| `Vision.Model`, `Vision.Prompt`, `Vision.MaxTokens`, `Vision.Reasoning` | carried | |
| `Vision.Identity` | carried | settings in one encoding (L17) |
| `Transcription`, `Transcription.Model`, `Transcription.Prompt`, `Transcription.Language` | carried | |
| `Transcription.Client` | carried | `*ai.Client`, its mode read by `TranscriptionMode()` (G2) |
| `Transcription.Identity` | carried | the mode in its settings (G2) |
| `Extraction`, `Extraction.Model`, `Extraction.MaxTokens`, `Extraction.Reasoning` | carried | |
| `Extraction.Client` | carried | `*ai.Client` |
| `Extraction.Identity` | carried | |
| `Embeddings`, `Embeddings.Client`, `Embeddings.Model` | carried | |
| `Embeddings.Identity` | carried | settings `""` (L16) |
| `Permanent` | carried | `*format.UnsupportedError` and input defects added (L9, L25) |
| `Describer`, `Transcriber`, `Embedder` | dropped | never declared, as layers has it |
| `infer.Pipeline` | dropped | a spelling layers rejects |
| `chunk.Chunk`, `chunk.Chunk.Text`, `chunk.Chunk.Span` | carried | |
| `chunk.Chunk`'s `Start`, `End` | renamed | `chunk.Chunk.Span` (L23) |
| `chunk.Span`, `chunk.Span.Start`, `chunk.Span.End` | carried | moved from `outdir.Span` (L23) |
| `chunk.Version` | carried | names the golden corpus (L27) |
| `chunk.Splitter`, `chunk.Splitter.Bound`, `chunk.Splitter.Settings` | carried | `Settings` feeds `Profile.Chunker` alone (L16) |
| `chunk.Splitter.Split` | carried | returns an error for a bound below 1 (L24) |
| `chunk.Split` | dropped | one spelling of the split (L23) |
| `join.Join` | carried | takes each seam's seconds (L20) |
| `join.JoinRequest` | renamed | `join.Request`, as layers renames it |
| `join.Request`'s seam-word setting and its default | renamed | `Request.WordsPerSecond`, no default (L20, L24) |
| `join.Seam` | carried | the one `Seam`, with JSON tags (L22) |
| `join.Transcript` | carried | |
| `job.Config`, `job.Config.Dispatcher`, `job.Config.Pipeline`, `job.Config.CheckpointEvery` | carried | |
| `job.New`, `job.Runner`, `job.Runner.Run` | carried | |
| `job.Update`, `job.Update.Event`, `job.Update.Inference`, `job.Update.Progress`, `job.Update.Batch`, `job.Update.Checkpoint`, `job.Update.Done` | carried | |
| `job.Checkpoint`, `job.Checkpoint.After` | carried | |
| `job.Done`, `job.Done.Parts`, `job.Done.Outputs`, `job.Done.Counts` | carried | |
| `Counts.Chunks`, `Counts.Lines`, `Counts.Outputs`, `VisionCounts.Images`, `VisionCounts.Texts`, `TranscriptionCounts.Distinct`, `Usage.Total` | dropped | as layers drops them (L2) |
| the manifest's `Attempt` | dropped | Cerebro's manifest, outside docinfer's API |
| `client.Client`, package `client` | renamed | `ai.Client`, package `ai` |
| `Config.Query` (go-ai's) | renamed | `ai.Options.Query` |
| `protocol.ReasoningEffort` | carried | seven efforts; C7 |
| `protocol.Usage`, `InputTokens`, `OutputTokens` | carried | |
| `TranscriptionUsage` | carried | `ai.TranscriptionUsage` |
| `EmbeddingUsage`, `PromptTokens` | carried | `ai.EmbeddingUsage` |
| `Response.IncompleteDetails`, `IncompleteDetails.Reason` | carried | `protocol.Response`'s |
| `APIError` | carried | `ai.APIError`, its `RetryAfter` read (L7) |
| `GenerateStream`, `Embed`, `Transcribe` | carried | methods of `*ai.Client` |
| `FunctionCall`, `FunctionCallOutput`, `InputAudio`, `ReasoningParam.Summary`, the `Native` tiers | carried | unused, as layers has it |
| `format/messages`, `format/responses`, `format/completions` | carried | `messages.New(messages.Options{})` and the rest |
| `tools/infer` | carried | outside docinfer |
| go-ai's harness-session surface | carried | outside docinfer |
| `dispatch.Content`, `Content.Open` | carried | the content's one reader (W3) |
| `dispatch.Event`, `Event.After`, `dispatch.Part`, `dispatch.Output`, `Output.SHA256`, `dispatch.Identity`, `dispatch.Outcome`, `dispatch.Version` | carried | docproc v0.3.0 |
| `dispatch.Dispatcher`, `Dispatcher.Content`, `Dispatcher.Index`, `Index.Drop`, `dispatch.Key`, `Key.SHA256`, `Part.SHA256`, `dispatch.Input`, `Input.After`, `dispatch.ErrSHA256` | carried | docproc v0.3.0 |
| `FileContent` | carried | `dispatch.FileContent`, now `texts/` too (C10) |
| `Content` (docproc's store, as layers' section names it) | carried | `dispatch.Content` |
| `tool.Run`, `email.Threader`, `Pool` | carried | docproc's, unchanged |
| `sqlate.Session` | carried | `postgres`'s, as docproc/postgres's |
| `fs.ErrNotExist`, `context.Context`, `iter.Seq2`, `jsontext.Value` | carried | the standard library's |
| `outdir.Key` | renamed | `docinfer.Key` |
| `outdir.Dir.Held`, `Hold` | renamed | `Cache.Get`, `Cache.Put` |
| `outdir.Span` | renamed | `chunk.Span` (L23) |
| `outdir.Seam` | renamed | `join.Seam` (L22) |
| `outdir.Source` | renamed | `docinfer.Source`, less `Ref` and `File` |
| `outdir.Line` | renamed | `docinfer.Chunk` in the library; `outdir.Line` stays the command's |
| `outdir.Part` | dropped | its outcome is the part's own event |
| `outdir.Counts` | renamed | `docinfer.Counts` |
| `outdir.Dir.WriteChunks` | dropped | the record is appended per batch (C12) |
| `Ref`, `File` (a source's) | dropped | its directories' paths |
| `outdir.Line`'s `Input`, `File`, `ContentType` | dropped | the input's file fields: its part holds what it has |
| `EncodeVector` | dropped | the pipeline encodes; `DecodeVector` the one reader |
| `Transcript` (the command's) | dropped | a transcription `Inference` |
| `Failure` | dropped | `inferences.jsonl` holds each refused or failed image and window by part ID and content SHA-256 (C15) |
| `Docproc` | carried | its `dir` relative, recorded once (C17) |
| `Capability` (the command's) | carried | holds `docinfer.Identity` and typed bounds (C14) |
| `Skipped` | carried | |
| `Space` | carried | the measured norms (L11) |
| `Line` (the command's) | carried | its part and source inline; grouped by C18's key |
| `Recorder` | carried | `internal/apptest`'s own (C22) |
| `results/` | carried | a `FileCache` |
| `texts/` | carried | a `dispatch.FileContent`, no `.txt` (C10) |
| `chunks.jsonl` | carried | appended per batch (C12) |
| the manifest | carried | `manifest.json`, gaining `docinfer`, `inputs`, and the relative `docproc.dir` (C13, C17) |
| `cmd/docinfer` (the spike's `main.go`) | carried | at the module's root (C3) |
| `app`, `apptest` | carried | `internal/app`, `internal/apptest`, from template/v0.3.0 (C2, C3) |
| `config` | dropped | the node constructors (C1) |
| `infer` | renamed | `internal/run` (C3) |
| `commands.go` | carried | `run`'s |
| `progress.go` | renamed | `run`'s `output.go` (C3, L4) |
| `execute.go`, `sources.go`, `vision.go`, `transcription.go` | dropped | their rules ported to the root, not their flow (L1) |
| `models` | dropped | folded into `run`, over `(*ai.Client).Models` (C5) |
| `source` | dropped | `rundir` (C4) |
| `outdir` | carried | `internal/outdir` (C3) |
| `report` | carried | `internal/report` (C3) |

Names this API adds: `ErrRequest`, `CapabilityVision`, `CapabilityTranscription`,
`CapabilityExtraction`, `CapabilityEmbeddings`, the six `Outcome` constants, `Inference.Reason`,
`TranscriptionCounts.Incomplete`, `MissingError.Error`, `DecodeVector`, `docinfertest.Cache`,
`chunk.ErrRequest`, `join.ErrRequest`, `join.Request.WordsPerSecond`, and
`join.Transcript.Text`; the command's `outdir.json`, `inferences.jsonl`, `report/`, `Manifest`'s
`Docinfer`, `Inputs`, and `Outputs`, `Build`, `Input`, and `integration`.

## Releases

Each tag, the module it versions, and the task of v1.ai.inference that releases it, in the order
they are tagged. The acceptance task releases nothing unless it finds a fix, which takes the next
patch of the module it fixes.

| Tag | Module | Task | Requires |
| --- | --- | --- | --- |
| go-ai `v0.4.0` | go-ai's base module, in go-ai's repository | go-ai | G1–G3's additions (G4) |
| `v0.1.0` | the base module | library | docproc v0.3.0, go-ai v0.4.0 |
| `v0.2.0` | the base module | job | docproc v0.3.0, go-ai v0.4.0 |
| `postgres/v0.1.0` | `postgres` | postgres | the base module, sqlate, sqlate/postgres |
| `cmd/docinfer/v0.1.0` | `cmd/docinfer` | docinfer | the base module, docproc v0.3.0, go-ai v0.4.0, go-cli-sdk v0.4.0, go-core v0.12.0 |

- A module is tagged only after every module it requires: go-ai v0.4.0 before the base module's
  v0.1.0, and the base module's tag before `postgres`'s or `cmd/docinfer`'s that requires it.
- No tagged module carries a replace directive; the committed `go.work` serves development.
- Each module's first CHANGELOG entry links the spike commit it is promoted from: spike-ai-pipeline
  6a631f1 for the base module and `cmd/docinfer`, spike-ai-pipeline-processing v0.9.0 for `join`.
  `postgres` and `docinfertest` have no spike source, and their entries say "new".
