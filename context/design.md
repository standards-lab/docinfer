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
(spike-ai-pipeline-processing at v0.9.0), `go-ai:` (at v0.3.0), or `docproc:` (at v0.4.0). The
measures are cited in short. A bare page name, such as `timeouts.md`, is a Go Elemental principle
under the architecture repository's `standards/go-elemental/principles/`; `principles/<page>` is
an architecture principle; a quoted name is the page's section. "layers" is docproc's
`context/layers.md` at 673ed3d. "data-intelligence" is standards-lab's
`context/data-intelligence.md`, which states what Cerebro asks of v1.ai.inference.

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
`(*ai.Client).TranscriptionMode()`, and the package `aitest`, a fake endpoint that answers each
route by a test's function over the decoded request and its arrival number. Its typed replies are
in Chat Completions' shapes only, a raw reply answers any route, an error reply carries
Retry-After, a route with no function answers 404, and a `Gate` and stream holds park requests and
streams. `internal/aitest` stays go-ai's format-test kit. Every other go-ai name is v0.3.0's, and
every docproc name docproc v0.4.0's. layers' docinfer sections are superseded by this one.

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
                                                library, sqlate, sqlate/postgres, and pgx's
                                                database/sql driver
github.com/standards-lab/docinfer/cmd/docinfer  the docinfer command; requires the library,
                                                docproc, go-ai, go-cli-sdk v0.4.0, and go-core
                                                v0.12.0 (C2)
```

The base module requires docproc v0.4.0 and go-ai v0.4.0 and no third module: the join of work in
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
postgres         (module) Cache in PostgreSQL, one row per key in docinfer_cache, a batch's Get
                   one query, its migrations, Verify, and Statements, as docproc/postgres ships
                   its own
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
`GOWORK=off` (C3). The base module's `integration/`, the AI live tier, runs under
`mise run test:integration`, which no CI job runs; `mise run integration` runs
`integration:postgres`, `postgres`'s tier against the compose PostgreSQL, which CI runs.

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
| `Permanent` | carried | `*format.UnsupportedError`, input defects, and `dispatch.ErrRequest` added (L9, L25) |
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
| `dispatch.Event`, `Event.After`, `dispatch.Part`, `dispatch.Output`, `Output.SHA256`, `dispatch.Identity`, `dispatch.Outcome`, `dispatch.Version` | carried | docproc v0.4.0 |
| `dispatch.Dispatcher`, `Dispatcher.Content`, `Dispatcher.Index`, `Index.Drop`, `dispatch.Key`, `Key.SHA256`, `Part.SHA256`, `dispatch.Input`, `Input.After`, `dispatch.ErrSHA256` | carried | docproc v0.4.0 |
| `FileContent` | carried | `dispatch.FileContent`, now `texts/` too (C10) |
| `Content` (docproc's store, as layers' section names it) | carried | `dispatch.Content` |
| `tool.Run`, `email.Threader`, `Pool` | carried | docproc's, unchanged |
| `sqlate.Session` | carried | `postgres.Verify`'s, as docproc/postgres's; `postgres.New` takes a `*sqlate.DB` |
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
`TranscriptionCounts.Incomplete`, `MissingError.Error`, `Result.Input`, `Result.Settled`,
`DecodeVector`, `docinfertest.Cache`, `chunk.ErrRequest`, `join.ErrRequest`,
`join.Request.WordsPerSecond`, and `join.Transcript.Text`; the command's `outdir.json`,
`inferences.jsonl`, `report/`, `Manifest`'s `Docinfer`, `Inputs`, and `Outputs`, `Build`, `Input`,
and `integration`. `postgres`, new with no spike source, adds `postgres.Cache`, `postgres.New`,
`postgres.Migrations`, `postgres.Source`, `postgres.Table`, `postgres.Verify`, and
`postgres.Statements`.

## Releases

Each tag, the module it versions, and the task of v1.ai.inference that releases it, in the order
they are tagged. The acceptance task releases nothing unless it finds a fix, which takes the next
patch of the module it fixes.

| Tag | Module | Task | Requires |
| --- | --- | --- | --- |
| go-ai `v0.4.0` | go-ai's base module, in go-ai's repository | go-ai | G1–G3's additions (G4) |
| `v0.1.0` | the base module | library | docproc v0.3.0, go-ai v0.4.0 |
| `v0.2.0` | the base module | job | docproc v0.4.0, go-ai v0.4.0 |
| `postgres/v0.1.0` | `postgres` | postgres | docinfer v0.2.0, sqlate v0.7.0, sqlate/postgres v0.8.0, pgx v5.13.0 |
| `cmd/docinfer/v0.1.0` | `cmd/docinfer` | docinfer | the base module, docproc v0.4.0, go-ai v0.4.0, go-cli-sdk v0.4.0, go-core v0.12.0 |

- A module is tagged only after every module it requires: go-ai v0.4.0 before the base module's
  v0.1.0, and the base module's tag before `postgres`'s or `cmd/docinfer`'s that requires it.
- No tagged module carries a replace directive; the committed `go.work` serves development.
- Each module's first CHANGELOG entry links the spike commit it is promoted from: spike-ai-pipeline
  6a631f1 for the base module and `cmd/docinfer`, spike-ai-pipeline-processing v0.9.0 for `join`.
  `postgres` and `docinfertest` have no spike source, and their entries say "new".
