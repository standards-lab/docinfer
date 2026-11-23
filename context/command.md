# cmd/docinfer's evaluation

v1.ai.inference's docinfer task built cmd/docinfer to these findings;
[command-api.md](command-api.md) states its API, [command-report.md](command-report.md) its report
and C16–C20.

## Evaluation

One finding per issue or simplification in the spike's command. Each names the spike's file and
lines, what it is measured against, and a verdict.
Where a finding changes a name, the API states the name.

Citations are relative to spike-ai-pipeline's `internal/docinfer/` at 6a631f1. A bare page name,
such as `timeouts.md`, is a Go Elemental principle under the architecture repository's
`standards/go-elemental/principles/`; `principles/<page>` is an architecture principle; a quoted
name is the page's section. "layers" is docproc's `context/layers.md` at 673ed3d.
"data-intelligence" is standards-lab's `docs/data-intelligence.md`, which states what Cerebro
asks of v1.ai.inference.

## The command

The command's sources: `app`, `apptest`, `config`, `models`, `source`, and `report`; `outdir` less
the library's part; `infer/commands.go` and `infer/progress.go`, the run command's own files; their
tests; and the spike root's `cmd/docinfer/main.go`, `go.mod`, and `docs/` (`docinfer-schema.md`,
`findings/docinfer.md`), cited from that root. C1–C24 are the evaluation's; what layers already
settles for `cmd/docinfer` (schema "2"'s shapes, `Transcript` going, `Failure` by part ID and
SHA-256, `FileCache` under `results/`) is not repeated. Every finding is accepted, for the docinfer
task.

### Composition

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
`internal/apptest` start from go-cli-sdk-template's latest `template/v*` release, as `cmd/docproc`
did, on go-cli-sdk's `cli.Program` and go-core's `Graph.Observe(func(graph.Ref))`; the spike's
files are not ported.

**C3. issue: the layout and packages are the spike root's, not a nested command module's.**
`cmd/docinfer/main.go:1`, `infer/progress.go:1`, `models/models.go:181`, `report/report.go:1`.
Against docproc's `STANDARDS.md` (the nested command's exception), `domain-files.md`, and
`topology-and-naming.md`. **accepted**, docinfer: `cmd/docinfer/main.go`,
`internal/{app, apptest, run, report}`, `outdir`, the one package `run` and `report` share, and
`integration`; `infer` becomes `run`, `models` and `source` fold into it (C4, C5), and `config`
becomes the node constructors (C1). Writers live in `output.go`; `report` splits as docproc's does,
into `commands.go`, `model.go`, `render.go`, and `output.go`. A committed `go.work` spans the base
module, `postgres`, and `cmd/docinfer`.

### Configuration and go-ai

**C5. issue: `models` lists `/models` with its own HTTP request.** `models/models.go:42`,
`models/models.go:67`, `models/models.go:121-177`. Against `principles/resolution.md` and
`principles/minimal-footprint.md`. **accepted**, docinfer: the check calls `(*ai.Client).Models`
once per capability's client, so it lists through the same query, token, headers, and retry the
run calls with, and reads `status.value` from `Model.Native`; an `*ai.APIError` is "no listing",
any other error "unreachable". The states, the table, and the 30 s bound stay.

**C6. issue: the configuration has no place for four of go-ai's options.**
`config/config.go:83-88`, `config/config.go:121-125`, `config/config.go:484-503`. Against go-ai's
`Options` and `baseline-standards.md` (the command ships the starting values).
**accepted**, docinfer: the transcription mode (`route`, `chat`) is set on the transcription
capability's own client in the configuration, the load refusing chat without the completions format,
and docinfer reads it from that client through `(*Client).TranscriptionMode()`
(`docs/library-evaluation.md`, Rejected); an endpoint gains `retry`, with the command's starting
policy, `headers`, in the secrets file, and an optional format profile decoded as `tools/infer`'s
`formatOptions` is. Each is a manifest setting where it changes a result.

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
ships a `ResponseHeaderTimeout` starting at 180 s, above vision's slowest observed image, 38.9 s
under mixed load (data-intelligence, "Gateway load"), and its client a retry starting at 3
attempts, 500 ms, 30 s; the library adds no deadline (`docs/no-defaults.md`, A request's
time). With no extraction, the command sets no stream timeout.
Rejected: a deadline the command sets around each call; the host bounds a request at the transport,
where `docs/no-defaults.md` puts it.

### The run directories

**C4. simplification: `source` restates what docproc's `rundir` now does.**
`source/source.go:17`, `source/source.go:36-91`, `infer/commands.go:39`, `report/commands.go:30`.
Against docproc's `rundir` (`rundir.Open`, `Dir.Latest`, `Dir.Record`) and
`principles/validation-first.md`. **accepted**, docinfer: no `source` package; `run` opens
`rundir`, feeds `Pipeline.Run` from `Dir.Record` and `Dir.Content()`, and keeps its count line in
`output.go`. The latest run is `Dir.Latest`'s, the latest that ended, and `--run` names any other,
validated as docproc's is. Rejected: the spike's greatest id, which can name a run still running.

**C10. issue: `outdir` writes through spike `rundir` helpers docproc doesn't export.**
`outdir/outdir.go:108`, `outdir/outdir.go:125`, `outdir/outdir.go:151`, `outdir/outdir.go:162`,
`report/report.go:51`, `report/report.go:70`. Against docproc's `rundir` exports.
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
`utc-times.md` and docproc's `schema.Time`. **accepted**, docinfer: `schema.TimeOf` at the
two stamps, `Time()` in the report, and a `main_test.go` per test package setting Asia/Kolkata.

### Tests

**C21. issue: the integration test runs in process, inside the composition root's package.**
`app/integration_test.go:1-34`. Against `tests-and-docs.md` (the built binary as a subprocess, on
go-core's `processtest`), with go-ai's `tools/infer/integration` as precedent. **accepted**,
docinfer: `cmd/docinfer/integration`, tagged, runs `run --report` and `report` against the
`AI_<ROLE>_*` endpoints or skips; the check vets and lints it without running it. Extraction
meets the live gateway in `integration/extract_test.go`.

**C22. issue: the fixtures depend on what the nested module can't have.** `apptest/apptest.go:24`,
`apptest/apptest.go:47-81`, `apptest/apptest.go:210-223`, `app/testdata/README.md:3-7`. Against
layers "The nested modules" and `tests-and-docs.md`. **accepted**, docinfer: `apptest` holds its own
`Recorder`, `Builds`, `Halt`, and a fixture per node C1 adds; the fake endpoint, model list
included, is go-ai's `aitest` (`docs/library-evaluation.md`, Rejected), not an `endpointtest` of
docinfer's; a test helper writes each docproc run directory with `rundir.Create` and `Begin`, in
place of the hand-written schema "1" one.

**C23. issue: tests that can't fail, pin the wrong thing, or reach no branch.**
`report/report.go:80-84`, `report/report.go:182-184`, `report/report.go:296-309`,
`app/report_test.go:263`, `app/vision_test.go:114`, `app/embed_test.go:442-450`,
`app/vision_test.go:541-550`, `app/transcription_test.go:451-459`. Against `tests-and-docs.md` (a
stall is a finding, never a sleep). **accepted**, docinfer: the report's golden pages over a
committed directory, as docproc's `report` has them, with a long text, a damaged text, and an
unended run; `errors.Is` through the message the command owns, not the operating system's; exit 1
for a cancelled run, not any but 0; a wait that runs out fails the test.

