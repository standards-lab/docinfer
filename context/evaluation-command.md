# Evaluation: the spike's docinfer command

Scope: spike-ai-pipeline at `6a631f1`, read through git: `cmd/docinfer/main.go`;
`internal/docinfer/{app, apptest, config, models, source, report}` whole; `outdir` less `Key`,
`Hold`, `Span`, and `Seam`; `infer/commands.go` and `infer/progress.go`, the run command's own
files; the tests under `app/`, `config/`, and `outdir/`; and `docs/docinfer-schema.md` and
`docs/findings/docinfer.md` for intent. Checked against the architecture principles, Go
Elemental's principles, docproc's `context/layers.md` (its docinfer sections and "The nested
modules"), go-ai at `v0.3.0`, standards-lab's `context/data-intelligence.md` (lines 38-45), and
the workspace's precedent: docproc's `cmd/docproc` (`cmd/docproc/v0.1.0`, base `v0.3.0`),
go-ai's `tools/infer`, go-cli-sdk `v0.4.0`, and go-cli-sdk-template `template/v0.3.0`. Paths
below are the spike's at `6a631f1` unless a repository is named. Nothing was built or run; every
finding is from reading.

What layers.md already settles for `cmd/docinfer` (the shapes its schema "2" takes from the
library, `Transcript` going, `Failure` by part ID and SHA-256, `FileCache` under `results/`) is
not repeated as a finding.

## Findings

### C1 · issue · Configuration and the model clients are not graph nodes

- `internal/docinfer/infer/commands.go:45`, `internal/docinfer/config/config.go:163`,
  `config.go:484-491`, `infer/vision.go:119`, `infer/transcription.go:121`,
  `infer/execute.go:293`, `app/domain.go:14`.
- The run command loads the configuration itself inside `Run`, and `config.Endpoint.Client`
  constructs each client, called from three places in `infer`. The graph holds the lifecycle
  configuration and an `*http.Client` only, and `defineDomain` is empty. So a Build reads none
  of docinfer's configuration, a fixture can replace no client, and a dependency is constructed
  outside the composition root.
- Against `composition-root.md` ("Nothing else in the application constructs a dependency"; "A
  CLI's configuration is a node of its graph") and `cli-applications.md` (`infrastructure.go`
  defines the configuration and the infrastructure services as nodes, and is the only file that
  names a provider). Precedent: go-ai `tools/infer/internal/app/infrastructure.go` defines its
  `*ai.Client`s, format, and retry policy as nodes read from root flags and variables at Build;
  docproc's `cmd/docproc` defines `Bounds` and a `run.Config` node.
- Resolution: nodes for the configuration (the directory `--config` names, read in the node's
  constructor from the flag's variable, as `tools/infer`'s `newSetting` reads a root flag, or
  from a `DOCINFER_CONFIG` variable), for each capability's `*ai.Client`, and for a `run.Config`
  holding `docinfer.Vision`, `Transcription`, and `Embeddings`; `run.Commands(n.RunConfig)`. A
  configuration error then fails the Build, labelled with its node, before the docproc run is
  read.

### C2 · issue · `App.Run` is go-cli-sdk v0.2.0's functional-option call

- `internal/docinfer/app/app.go:45`; `go.mod` pins go-cli-sdk v0.2.0 and go-core v0.10.0.
- `cli.Run(ctx, root, args, streams, cli.WithGraph(...))` no longer exists; v0.4.0 is
  `cli.Program{Root, Graph, LifecycleConfig}.Run(ctx, args, streams)`. `apptest.Builds` passes
  `r.Record`, a `func(string)`, to `Graph.Observe` (`apptest/apptest.go:31`), which go-core
  v0.12.0 calls with a `graph.Ref`.
- Against `construction-and-execution.md` (no functional options; a struct with a method),
  `cli-applications.md`, and `rolling-currency.md`.
- Resolution: start `internal/app` and `internal/apptest` from go-cli-sdk-template
  `template/v0.3.0`, as layers.md has `cmd/docproc` do, rather than port the spike's files.

### C3 · issue · The layout and the package set are the spike root's, not a nested command module's

- `cmd/docinfer/main.go:1`; `internal/docinfer/{app, apptest, config, infer, models, outdir,
  source, report}`; `infer/progress.go:1`, `models/models.go:181`, `report/report.go:1`.
- `main.go` sits under `cmd/docinfer/` of the spike root with eight packages under
  `internal/docinfer/`. The package named `infer` holds the command named `run`. Writers sit
  outside `output.go`: `progress.go`, `execute.go`'s `summary`, `models.Report.Write`; and
  `report.go` holds model, rendering, and file write together.
- Against docproc's `STANDARDS.md` (the nested command's exception: `main.go` at the module's
  root, `internal/app`, `internal/apptest`, and its command packages only, `integration` at the
  root), `domain-files.md` (`commands.go`, `output.go`), and `topology-and-naming.md`.
- Resolution: `cmd/docinfer/main.go`, `internal/{app, apptest, run, report}`, and `integration`.
  `infer` becomes `run`. `models` and `source` fold into `run` (C4, C5); `config` becomes node
  constructors (C1) or one internal package they call; `outdir` stays, the one package `run` and
  `report` share. `report` splits as docproc's does: `commands.go`, `model.go`, `render.go`,
  `output.go`. A committed `go.work` over the base module and `cmd/docinfer`.

### C4 · simplification · `source` restates what docproc's `rundir` now does

- `internal/docinfer/source/source.go:17`, `:36-66`, `:69-91`; `infer/commands.go:39`;
  `report/commands.go:30`.
- `Open` checks the id, picks the latest run, reads the manifest, checks its schema, and reads
  the whole record into two slices. docproc v0.3.0's `rundir.Open` refuses another schema,
  `Dir.Latest` names the latest run, every `Dir` method refuses an id that isn't a run, and
  `Dir.Record` is an iterator. `Open` re-checks the id the command's `Validate` already checked
  (`source.go:45`), which `validation-first.md` has an inner scope trust. `Run.Path` joins a
  `ref`, which schema "2" replaces with the content store by SHA-256.
- Behavior change to settle: the spike's latest is the greatest id, an unended run among them;
  `Dir.Latest` names only a run that ended.
- Resolution: no `source` package. `run` opens `rundir`, feeds `Pipeline.Run` from
  `Dir.Record` and `Dir.Content()`, and keeps the one-line output count in `output.go`. `--run`
  is validated as docproc's is: `inv.Changed("run") && id == ""`.

### C5 · issue · `models` lists `/models` with its own HTTP request

- `internal/docinfer/models/models.go:121-170`, `:173-177`, `:42`.
- The package builds the URL, query, and bearer header, reads and bounds the body, decodes the
  OpenAI list, and redacts the query from its messages. go-ai v0.3.0 has `Client.Models`, which
  sends through the client's own query, token, headers, and retry policy, and `Model.Native`,
  the entry as the endpoint sent it, which carries llama.cpp's router's `status`. A token or
  header the client sends and this request doesn't would list differently from what the run
  then calls. `Check(ctx, hc, cfg)` takes the whole configuration to do it (`:67`).
- Against `resolution.md` (interface at the resolution the purpose requires; go-ai is the
  layer) and `minimal-footprint.md`.
- Resolution: the check takes each capability's `*ai.Client` and model, calls `Models` once per
  client, and reads `status.value` from `Native`. An `*ai.APIError` is "no listing", any other
  error "unreachable". The states, the table, and the 30 s bound stay.

### C6 · issue · The configuration has no place for four of go-ai v0.3.0's options

- `internal/docinfer/config/config.go:83-88`, `:121-125`, `:484-503`.
- An endpoint is a base URL, a format name, a bearer token, and a query. Missing:
  - `Options.TranscriptionMode`: an endpoint whose audio model has no transcription route needs
    `TranscribeChat`, and it is a client option, so the transcription capability needs a client
    of its own (as `tools/infer`'s `Transcriber` node is), and chat mode needs the completions
    format, which the load can check.
  - `Options.Retry`: the zero value retries nothing, and layers.md's failure table is written
    over "retries spent". go-ai ships no policy, so the command ships the starting one.
  - `Options.Header`: a gateway's consumer-key header, or Messages' `x-api-key`; `token` is a
    bearer token only.
  - Each format's `Options` (v0.2.0's deployment profile, such as `refused_efforts`): every
    format is built at its spec.
- Against go-ai v0.3.0's exported surface and `baseline-standards.md` (the library ships no
  policy; the command ships the operator's starting values, as `cmd/docproc` does).
- Resolution: `transcription.mode` (`route`, `chat`), an endpoint's `retry` and `headers`
  (headers in the secrets file), and an optional per-endpoint format profile decoded as
  `tools/infer`'s `formatOptions` is; each a manifest setting where it changes a result.

### C7 · simplification · `config` re-validates what go-ai validates, and its copy has drifted

- `internal/docinfer/config/config.go:58-61`, `:418-420`, `:454-469`.
- `efforts` re-lists the protocol's reasoning efforts and lacks v0.2.0's `minimal` and `max`,
  so a valid effort is refused. `Endpoint.validate` re-implements `New`'s base URL checks "as
  the client's New checks it".
- Resolution: the vision effort is a `protocol.ReasoningEffort` field, decoded by its own
  `UnmarshalText`, which refuses an unknown one. `ai.New` does no I/O, so the client node (C1)
  builds each client and reports `New`'s error, named for its endpoint; `validate` keeps only
  what is docinfer's (a capability's endpoint exists, the counts are at least 1).

### C8 · simplification · `config.Env` and its five types are read by one test

- `internal/docinfer/config/config.go:71`, `:127-157`, `:286-291`; its one reader is
  `config/config_test.go:60`.
- Thirty lines of exported types record the override names `Finalize` composed, and nothing in
  the command reads them.
- Resolution: drop `Env`, `EndpointEnv`, `CapabilityEnv`, `EmbeddingsEnv`, `VisionEnv`, and
  `TranscriptionEnv`; compose each name where it is read. The test asserts the override's effect
  instead.

### C9 · issue · No inference request has a deadline

- `internal/docinfer/app/infrastructure.go:28-33`; the one bound is `models/models.go:39`.
- The HTTP client sets no timeout "since a request's is its context's", and no vision,
  transcription, or embeddings request is given one: its context is the signal context. An
  endpoint that accepts a request and never answers holds the run until the operator interrupts
  it. go-ai's `StreamIdleTimeout` bounds streams only, which the command does not use.
- Against `baseline-standards.md` and docproc's precedent: `cmd/docproc` ships
  `DOCPROC_UNIT_TIMEOUT` and `DOCPROC_STREAM_IDLE` as its starting bounds.
- Resolution: a per-capability request timeout in the configuration, with a starting value, and
  the design note says where it is applied (the library's `Bounds`, or a deadline the command
  sets around each call). The findings' slowest observed case sizes it: vision at 38.9 s an
  image under mixed load (data-intelligence.md:239).

### C10 · issue · `outdir` is written through spike `rundir` helpers docproc doesn't export

- `internal/docinfer/outdir/outdir.go:108`, `:125`, `:151`, `:162`; `report/report.go:51`,
  `:70`.
- `rundir.WriteFile`, `WriteRunFile`, package-level `Latest`, and `ValidID` are the spike's
  internal docproc package's. docproc v0.3.0's `rundir` exports `Open`, `Create`, and `Dir`'s
  and `Run`'s methods only, and they are for docproc's directory.
- Resolution: `outdir` owns its whole-file write, its run list, and its id check, each `Dir`
  method refusing an id that isn't one of its runs, as `rundir` does. `texts/` can be a
  `dispatch.FileContent` (docproc's base module, already required), replacing `WriteText`,
  `ReadText`, and `textPath`; its layout is `<xx>/<sha256>` with no `.txt`, which the schema "2"
  page states, and `ReadText`'s digest check is kept or dropped on purpose.

### C11 · issue · The directory has no schema of its own, and no reader checks one

- `internal/docinfer/outdir/outdir.go:40-66`, `:202-213`; `report/report.go:55`;
  `docs/docinfer-schema.md:10`.
- The schema is a field of each run's manifest. `ReadManifest` and the report accept any value,
  though the contract says a reader checks it first. `results/` and `texts/` are shared by every
  run, so a schema "2" run over a schema "1" directory would read held results of the old shape.
- Against `validation-first.md`. Precedent: `rundir.json` at the root, `rundir.Open` refusing
  another schema and naming both, `Create` making the directory.
- Resolution: a root marker file, `Open` (read, creates nothing) and `Create` in place of
  `OpenExisting` and `Open`, and a refusal of any schema but "2", so no "1" directory is
  reused.

### C12 · issue · The chunk record is held in memory and written once, at the end

- `internal/docinfer/infer/execute.go:95-111`; `outdir/outdir.go:161-172`, `:182-199`;
  `report/report.go:59-63`.
- Every line, vector included, is collected and written whole when the run ends; a run that
  stops writes none (`execute.go:114-121`). The findings' record is 197 MB a run
  (`docs/findings/docinfer.md:136`). `ReadChunks` then decodes all of it, vectors too, for a
  report that shows no vector.
- Against layers.md, "Batches as made", which `cmd/docinfer` consumes through `Pipeline.Run`,
  and docproc's record, appended line by line (`rundir.Run.Write`).
- Resolution: the run appends each batch's lines as the pipeline yields it, and the manifest's
  end marks the record whole, so a stopped run keeps the lines it made, as docproc's does. The
  read side is an iterator, and the report reads lines without their vectors.

### C13 · issue · The manifest records no build

- `internal/docinfer/outdir/outdir.go:243-273`; `docs/findings/docinfer.md:37`.
- A run's manifest names its models and settings but not the docinfer that wrote it; the
  finding had to state the two commits and a binary's SHA-256 by hand, and the re-run's lines
  differ from run 1's because of a build change the directory can't show.
- Precedent: docproc's manifest holds `docproc`, a `Build` of the command's module version and
  the library's, from the build info.
- Resolution: the schema "2" manifest gains `docinfer`, the command's and the library's
  versions, read as `version.go` reads the main module's.

### C14 · simplification · The manifest's capability settings are an untyped map, mixing identity and bounds

- `internal/docinfer/outdir/outdir.go:293-299`, `:538-543`; `infer/execute.go:74`, `:82`,
  `:89`; `app/vision_test.go:114`.
- `Capability.Settings` is `map[string]any` holding the prompt and cap, which key the held
  results, beside `concurrency` and `batch`, which don't. A test pins `fmt.Sprint` of the map.
  `outdir.Part` repeats its content type's `Kind` and `Route` "where a reader found them
  before the part was carried whole".
- Resolution: in schema "2" a capability holds the library's `Identity` (capability, model,
  settings string), so a reader ties a manifest to the held results by the same key, with the
  endpoint and its bounds as typed fields beside it; `Part`'s `kind` and `route` go.

### C15 · issue · The report shows only what has a chunk line

- `internal/docinfer/report/doc.go:21-23`; `report/report.go:200-242`.
- An image whose vision text is empty, an image vision skipped, and a text that cut to no chunk
  are counted and not shown. The page is built from chunk lines because the directory records no
  per-run vision result: transcripts and refusals are in the manifest, vision's texts are found
  only through the lines they were cut to. The stopped first run's 454 empty images of 2,086
  (`docs/findings/docinfer.md:151`) would be on no page.
- Against data-intelligence.md:42-43 (the full vision texts) and the report's purpose, which is
  to show what a model did with each image.
- Resolution: schema "2" records each run's inferences, vision's beside transcription's
  (layers.md makes the manifest's transcripts `Inference`s already), in the manifest or a file
  of their own, and the report lists every image and window with its outcome, text or none.

### C16 · issue · One page a run, texts cut at 1,000 runes, no chunk text

- `internal/docinfer/report/report.go:22-31`, `:296-309`; `report/report.html.tmpl:132`,
  `:175-177`; `report/doc.go:19-20`.
- The cut exists because every input is on one page, each `<details>` open. It cuts most of
  what it is for: the median vision text is 1,419 characters and the 95th percentile 4,290
  (`docs/findings/docinfer.md:72`). A text or Markdown output is a link, and no chunk's text is
  on the page.
- Against data-intelligence.md:42-43: pages linked to docproc's by the input's SHA-256, holding
  the full vision texts, transcripts, and chunks. See "Report shape".

### C17 · issue · The page's links depend on where the docproc run directory is

- `internal/docinfer/report/report.go:65-67`, `:76-90`; `report/doc.go:25-31`;
  `app/report_test.go:118-126`.
- The report reads docinfer's directory alone, but every image, window, and text output is a
  relative link computed from the manifest's absolute `docproc.dir` at render time. Moving
  docinfer's directory alone breaks them all, with nothing on the page saying so. The package
  doc's "the same directory renders the same bytes" is false once the directory moves relative
  to docproc's. The test asserts each link resolves on disk, which holds only in place. The
  file-URL branch (`:82-84`) can't be reached on Unix, where both paths are absolute.
- Resolution: see "Report shape", (c). Drop the file-URL branch.

### C18 · issue · The report groups a source's lines by a line of index 0

- `internal/docinfer/report/report.go:200-227`.
- A new source begins at each line whose `Index` is 0, and a transcript is matched to its
  manifest entry by input and file, in order. The grouping rests on the record's order alone:
  nothing on a line says which source it belongs to but its position after a zero.
- Against layers.md, "docinfer's shapes": a chunk is identified by its part's ID, its source's
  kind and text SHA-256, and its index.
- Resolution: group by that key, which schema "2"'s line carries inline.

### C19 · issue · The run command always renders, and the report command differs from docproc's

- `internal/docinfer/infer/execute.go:119`, `:125`; `report/report.go:45`, `:50-53`;
  `report/commands.go:36-41`.
- `run` writes the report every time, after a cancellation too. `docproc run` renders only with
  `--report`, never after a cancellation, and a failed render is exit 1 with the run intact.
  `docinfer report` renders the greatest run id, a run still running among them, where docproc's
  takes the latest that ended and `--run` for any other. It prints a path relative to the
  directory, where docproc prints the index's absolute path. `Write` takes no context, so a
  render over thousands of inputs can't be interrupted.
- Resolution: `docinfer run --report` and `docinfer report <dir> [--run <id>]` with docproc's
  rules, `report.Render(ctx, dir, id)` and a `Make` that prints the index's absolute path.

### C20 · issue · Neither command's help states its exit status

- `internal/docinfer/infer/commands.go:25-29`; `report/commands.go:21-25`.
- A run whose images were refused, windows failed, or chunks were rejected exits 0
  (`app/report_test.go:188`), and nothing the operator reads says so.
- Against `cli-applications.md`'s exit codes, and docproc's precedent: an `exitStatus` `Footer`
  on `run` and `report`.
- Resolution: a footer on each, stating that recorded refusals exit 0, what exits 1, and what
  the last line counts.

### C21 · issue · The integration test runs in process, inside the composition root's package

- `internal/docinfer/app/integration_test.go:1-34`.
- A `-tags integration` test in `app_test` calls `App.Run` against the endpoint
  `DOCINFER_CONFIG` names.
- Against `tests-and-docs.md`: the integration tier is a root-level `integration` package that
  runs the built binary as a subprocess, through its arguments, its two streams, and its exit
  code, on go-core's `processtest`; go-ai's `tools/infer/integration` is the nearest example.
- Resolution: `cmd/docinfer/integration`, tagged, against a real endpoint named by variable,
  skipped when unset; the check vets and lints it without running it.

### C22 · issue · The fixtures depend on what the nested module can't have

- `internal/docinfer/apptest/apptest.go:24`, `:47-81`, `:210-223`;
  `app/testdata/README.md:3-7`.
- `apptest` imports the spike's docproc `apptest` for `Recorder`, which is internal to
  `cmd/docproc`. Its fake endpoint is the one layers.md moves to the library's
  `internal/endpointtest`. The tests read a hand-written schema "1" run directory with made-up
  digests.
- Against layers.md ("The nested modules": `cmd/docinfer`'s tests write the run directories
  they read with `rundir`; `apptest` takes `Recorder` as its own) and `tests-and-docs.md` (a
  fixture replaces a node or observes a build, and wraps no production constructor).
- Resolution: `apptest` holds `Recorder`, `Builds`, `Halt`, and a fixture per node C1 adds
  (the configuration, the clients). The fake endpoint is `docinfer/internal/endpointtest`, which
  the nested module may import since Go's internal rule is by path, and it must serve the model
  list, which the library's own tests never call. A test helper writes each run directory with
  `rundir.Create` and `Begin`.

### C23 · issue · Tests that can't fail, or pin the wrong thing, and branches no test reaches

- No test reaches the 1,000-rune cut, a stored text that fails its digest, a run that never
  ended, or the file-URL link (`report/report.go:296-309`, `:182-184`, `:80-84`;
  `report.html.tmpl:61`, `:172-177`): the fixture's texts are all short.
- `app/report_test.go:263` asserts the operating system's "no such file or directory" text.
- `app/vision_test.go:114` asserts `fmt.Sprint` of a map (C14).
- `app/embed_test.go:450`, `vision_test.go:550`, `transcription_test.go:459` accept any exit
  code but 0 for a cancelled run, where the contract is 1.
- `app/embed_test.go:442`, `vision_test.go:541`, `transcription_test.go:451`: the fake waits
  for the request's cancellation or 10 s, then goes on either way, so a run that never cancels
  its request passes after a sleep. `tests-and-docs.md`: a stall is a finding, never a sleep.
- Resolution: the report's golden pages over a committed directory, as docproc's `report` has
  them, with a long text, a damaged text, and an unended run in it; assert `errors.Is` through
  the message the command owns; assert exit 1; fail the test when the wait runs out.

### C24 · issue · Times: a conversion schema "2" removes, and no test under a non-UTC zone

- `internal/docinfer/infer/execute.go:68`, `:113`; `report/report.go:164`; no `TestMain` under
  `internal/docinfer/`.
- `schema.Time(time.Now())` keeps the local zone and the monotonic reading and converts only
  when written. docproc v0.3.0's `schema.Time` is a struct made by `TimeOf`, which converts at
  once, and read with `Time()`, so neither conversion compiles. No docinfer test sets
  `time.Local`.
- Against `utc-times.md`: callers convert where a time leaves their code, and a package's
  `TestMain` sets a non-UTC zone.
- Resolution: `schema.TimeOf` at the two stamps, `Time()` in the report, and a `main_test.go`
  per test package setting Asia/Kolkata, since the times are stamped at run time.

## Report shape

What the spike does:

- **Pages and keys.** One page a run, `runs/<run id>/report.html`. Inputs are keyed by their
  absolute path and sorted by it; the same content under two paths is shown twice.
- **Inline.** A vision text or a transcript: its first 1,000 runes, then a link to the stored
  text under `texts/`. A text or Markdown output: a link into the docproc run directory, no
  text. A chunk: counted, never shown. An image: an `img` whose source is in the docproc run
  directory. A window: a link there. No vector.
- **What it reads.** The run's manifest, its whole chunk record, and the stored texts, all in
  docinfer's directory. It opens nothing of docproc's, but links into it by a path computed at
  render time (C17).

What docproc's report does (`cmd/docproc/internal/report`, schema "2"):

- `runs/<run>/report/index.html` and one `<sha256>.html` per distinct content the run's inputs
  hold. The index lists every input by path, each linked to its content's page; an input
  stopped before its own event has no SHA-256 and no page.
- A content's page lists every input holding it, then its part tree once, with every text,
  Markdown, and record output inline and whole, a PNG or JPEG as an `img`, a WAV as a player,
  each at its content's file.
- It reads its own run directory alone; every link into it is relative; it reads no clock; a
  render replaces the report directory whole; `docproc report` and `docproc run --report` make
  it.

What Cerebro asks (data-intelligence.md:38-45): docinfer's report "its pages linked to docproc's
by the input's SHA-256 and holding the full vision texts, transcripts, and chunks", beside a
docproc report that is "every output inline, uncut".

Recommendation, for the command round to settle:

- **(a) Key by input content, per run**: `runs/<run>/report/index.html` and `<sha256>.html`,
  the file name docproc's page for the same content has. The cross link is then one relative
  path, to `runs/<docproc run>/report/<sha256>.html` under the manifest's `docproc.dir`, and the
  two reports read side by side. Per run rather than per directory, since a run's lines are its
  own and only `results/` and `texts/` are shared. Two things the directory must hold for this:
  each input's own SHA-256 (a chunk line's part is the part it was cut from, not the input, so
  the manifest or the record needs an input list), and the run's inferences (C15). An input
  with no SHA-256 keeps its row, its error, and its metadata chunk on the index.
- **(b) Inline and uncut; the cut goes.** Per-content pages remove the reason for it. Each
  image's vision text whole, with its outcome and why it is incomplete; each transcript whole,
  with its windows, failed windows, and seams; each source's chunks, every one, with its index
  and span, a rejected one with its error; no vector. The chunker's chunks don't overlap and
  cover a text but for trimmed white space (`chunk/doc.go:12-15`), so a source can be shown once,
  as its chunks in order, rather than the text and then the chunks again; that halves the page.
  The heaviest content in the finding, `Large Desktop.one` with `text_heavy.pdf`'s 848 pages at
  about 3,700 characters each, comes to a few megabytes of text on one page, which docproc's
  page for the same content already carries as outputs.
- **(c) Read docinfer's directory alone, and say what the links are.** Every text on the page
  comes from `texts/`, the chunk record, and the manifest, so the pages are whole with the
  docproc run directory absent, and a test renders them that way. The images and windows are
  docproc's and are not copied. The choice left is how they appear:
  - link only: each content's page links to docproc's page for it, which shows the image and
    plays the window;
  - or also an `img` beside each vision text, its source docproc's content file
    (`rundir.ContentPath`, part of schema "2"), a link the report never opens.

  The second keeps the image beside its text, which is the page's use; either way a moved
  directory costs links, never text. Both need the relative path from the manifest's
  `docproc.dir`, so the package doc states that the bytes depend on where the two directories
  sit, or the run records the relative path once and the render reuses it.

## Names

Spike names in the command that don't match go-ai v0.3.0, go-cli-sdk v0.4.0, go-core v0.12.0,
or docproc v0.3.0.

| Spike, at `6a631f1` | Right name |
| --- | --- |
| `github.com/JaimeStill/spike-ai-pipeline-client`, package `client` | `github.com/standards-lab/go-ai`, package `ai` |
| `*client.Client` (`config.go:484`) | `*ai.Client` |
| `client.New(client.Config{BaseURL, Query, HTTP, Format, Token})` (`config.go:485-490`) | `ai.New(baseURL, httpClient, wireFormat, ai.Options{Query, Token, Header, Retry, StreamIdleTimeout, TranscriptionMode})`; a nil client or format panics |
| `cfg.Token = func(context.Context) (string, error)` | `ai.Options.Token`, an `ai.TokenSource` |
| `completions.New()`, `messages.New()` (`config.go:497`, `:501`) | `completions.New(completions.Options{})`, `messages.New(messages.Options{})`; `responses.New(responses.Options{})` is unchanged |
| the efforts none, low, medium, high, xhigh (`config.go:58-61`) | `protocol.ReasoningEffort`'s seven, with `ReasoningEffortMinimal` and `ReasoningEffortMax` |
| `models`' own `GET <base_url>/models` (`models.go:121`) | `(*ai.Client).Models`, returning `[]ai.Model{ID, Native}` |
| the listing's `status.value` decoded by hand (`models.go:163-167`) | read from `ai.Model.Native` |
| no transcription mode | `ai.Options.TranscriptionMode`: `ai.TranscribeRoute` (zero) or `ai.TranscribeChat` |
| `client.TranscriptionRequest{File: f}` (`transcribe/transcribe.go:93-94`, the library's) | `ai.TranscriptionRequest{Open: func(ctx) (io.ReadCloser, error)}` |
| `*client.APIError`, `client.TranscriptionUsage` | `*ai.APIError`, `ai.TranscriptionUsage` |
| `cli.Run(ctx, root, args, streams, cli.WithGraph(graph, node))` (`app.go:45`) | `cli.Program{Root, Graph, LifecycleConfig}.Run(ctx, args, streams)` |
| `Graph.Observe(func(string))` (`apptest.go:31`) | `Graph.Observe(func(graph.Ref))`, recording `ref.Name()` |
| docproc `apptest.Recorder` (`apptest.go:24`) | `cmd/docinfer`'s own `apptest.Recorder` |
| `internal/docproc/rundir`, `internal/docproc/schema` | `github.com/standards-lab/docproc/rundir`, `rundir/schema` |
| `rundir.Latest(root)`, `rundir.ReadManifest`, `rundir.ReadRecord`, `schema.Version` check (`source.go:42-55`) | `rundir.Open(root)`, then `Dir.Latest`, `Dir.Manifest`, `Dir.Record`; `Open` refuses another schema |
| `rundir.ValidID` (`infer/commands.go:39`, `report/commands.go:30`) | none: each `Dir` method refuses an id that isn't a run |
| `rundir.WriteFile`, `rundir.WriteRunFile` (`outdir.go:108-162`) | none exported: `outdir`'s own |
| `schema.Time(t)`, `time.Time(t)` (`execute.go:68`, `report.go:164`) | `schema.TimeOf(t)`, `t.Time()` |
| an output's `Ref` under `outputs/` (`source.go:69`, `report.go:210-215`) | the output's `SHA256`, opened through `Dir.Content()`, its file `rundir.ContentPath(sha256)` |
| `outdir.Open` (makes), `outdir.OpenExisting` (reads) | `Create` and `Open`, as `rundir` names them (C11) |
| `Dir.Held`, `Dir.Hold` under `results/` | `docinfer.FileCache` (layers.md) |
| package `infer`, command `run` | package `run` |
| `go.mod`: go-cli-sdk v0.2.0, go-core v0.10.0 | go-cli-sdk v0.4.0, go-core v0.12.0, as `cmd/docproc` requires |
