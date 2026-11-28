# cmd/docinfer's API

This note states cmd/docinfer's API: layers' docinfer sections at 673ed3d, restated in
go-ai's and docproc's released names, with [command.md](command.md)'s findings applied. A finding's
id marks where it changes layers. The directory's `outdir` and the report are
[command-report.md](command-report.md).

## The module

```text
github.com/standards-lab/docinfer/cmd/docinfer  the docinfer command; requires the library,
                                                docproc, go-ai, go-cli-sdk, and go-core (C2)
cmd/docinfer     (module) main.go at the module's root, so go install installs the command
  internal/app       the composition root, started from go-cli-sdk-template's latest template/v*
                     release (C1, C2)
  internal/apptest   its fixtures: Recorder, Builds, Halt, and a fixture per node (C22)
  internal/run       the run command: docproc's rundir in, Pipeline.Run, outdir out (C3, C4, C5)
  internal/report    the report command and run --report's render (C16–C19)
  internal/outdir    docinfer's directory at schema "2", the one package run and report share
  integration        the built-binary tier, tagged, against the endpoints AI_<ROLE>_* name (C21)
```

## The command

`cmd/docinfer` runs the pipeline over a `cmd/docproc` run directory and writes its own. Its
composition root is a graph (C1): a node for the configuration, read from the directory `--config`
or `DOCINFER_CONFIG` names in the node's constructor; a node per capability's `*ai.Client`, built
as `ai.New(baseURL, httpClient, wireFormat, ai.Options{…})` and reporting its error named for its
endpoint (C7); and a node for `run.Config`, holding `profile.Settings` and `profile.Clients`, which
`run.Commands(n.RunConfig)` passes to `profile.Resolve` and `profile.New`. A configuration error
fails the Build, labelled with its node, before the docproc run is read.

The configuration ships the operator's starting values, which the library leaves none
(`docs/no-defaults.md`):

```text
endpoints.<name>     base_url; format: completions, messages, or responses; profile, the format's
                       Options, decoded as tools/infer's formatOptions is (C6); query; retry
                       {max_attempts 3, base_delay 500ms, max_delay 30s} (C6);
                       response_header_timeout 180s, and 600s on the endpoint vision names,
                       shared or not, since a non-streaming reply's header waits for the whole
                       reply, up to vision's max_tokens, and no stream timeout (C9);
                       token and headers in the secrets file (C6)
vision               endpoint, model, served_model, prompt, max_tokens 2048, reasoning none (a
                       protocol.ReasoningEffort, refused by its UnmarshalText, C7), native
                       {"temperature":1.0,"top_p":0.95,"top_k":64}, Gemma 4's card sampling
                       sent explicitly so it is keyed, replaced whole when set, {} sending none,
                       max_pixels 2580480, the vision row's Gemma 4 budget at 1,120 soft
                       tokens (1,120 × 9 patches × 16² px, aspect kept), a larger PNG or JPEG
                       downscaled to it, 0 sending every image as it is, a negative one
                       refused, layer_words 100, a PDF page's image skipping vision when its
                       page's text layer holds at least that many words, 0 sending every page
                       image, a negative one refused, concurrency 4
transcription        endpoint, model, served_model, prompt, language, mode route (the start) or
                       chat, set on this capability's own client as ai.TranscribeRoute or
                       ai.TranscribeChat, chat for an endpoint with no transcription route and
                       refused without the completions format (C6), max_tokens 512 in chat
                       mode and 0, sending none, in route mode, a nonzero one refused there,
                       native {"temperature":0} in chat mode and none in route mode, replaced
                       whole when set, {} sending none, a non-empty one refused in route
                       mode, words_per_second 8, concurrency 4
extraction           endpoint, model, served_model, max_tokens 4096, window_runes 16384,
                       concurrency 4
embeddings           endpoint, model, served_model, dimensions, input_types (required, no
                       starting value), tokenizer (required, no starting value; it must front
                       the embeddings' deployment, which no listing checks, since the chunker's
                       identity names the embeddings' served model), batch 16,
                       chunk_size 480 tokens, chunk_runes 8192 characters (vLLM's 16 a token
                       at max_model_len 512), each with the document prefix, the chunker's
                       Runes chunk_runes less the prefix's characters, concurrency 4
run                  pending 16, batch_bytes 524288
```

Each override is `DOCINFER_` and its setting's path, composed where it is read (C8). `validate`
keeps what is docinfer's: a capability's endpoint exists, and the counts are at least 1 (C7). Each
endpoint has one `http.Client` over a cloned default transport with its `ResponseHeaderTimeout`,
and each capability's client has its endpoint's `Options` with `Retry`, `Header`, `Query`,
`Token`, and, for transcription, `TranscriptionMode`: `ai.TranscribeRoute` for `route` and
`ai.TranscribeChat` for `chat`. A setting that changes a result is in the manifest.

The commands, each with a footer stating that recorded refusals exit 0, what exits 1, and what the
last line counts (C20):

```text
docinfer run <docproc-dir> --out <dir> [--run <id>] [--extract <file>] [--report]
    infers over docproc's run, Dir.Latest's, the latest that ended, or --run's, validated as
    docproc's is (C4); --extract's file is Options.Extract; --report renders the run's
    report after it, never after a cancellation, a failed render exiting 1 with the run intact
    (C19); a cancelled run exits 1 (C23)
docinfer report <dir> [--run <id>]
    renders a run's report, the latest that ended or --run's; Make prints the index's absolute
    path (C19)
```

Before it reads docproc's run, `run` checks each capability's model with `profile.Resolve`, one
`(*ai.Client).Models` per client, listing through the run's query, token, headers, and retry,
reading `status.value` from `ai.Model.Native`; an `*ai.APIError` is "no listing", any other
error "unreachable". The states, the table, and the 30 s bound stay (C5).

`run` opens docproc's directory with `rundir.Open` and feeds `Pipeline.Run` from `Dir.Record` and
`Dir.Content()`, each output line's event as an `Input` and each input's part line with
`MetadataText`'s text and the record's `File` lines after it (C4); its count line and progress lines
are in `output.go`, printed from `Result` values alone (`docs/library-evaluation.md`, Composition).

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

`cmd/docinfer`'s tests write the docproc run directories they read with `rundir.Create` and
`Begin`, and drive go-ai's `aitest`, its model list included (C22); each test package's
`main_test.go` sets Asia/Kolkata (C24). `integration` runs the built binary as a subprocess on
go-core's `processtest`: `run --report` and `report` against the endpoints go-ai's `AI_<ROLE>_*`
variables name, skipped when they are unset; the check vets and lints it without running it
(C21). Extraction meets the live gateway in the base module's tier, `integration/extract_test.go`,
since the command runs none.

## Names

Every name of the command's that layers' docinfer sections write, against this API:
**carried** under its name (its shape changed where the reason says), **renamed**, or **dropped**.

| layers | verdict | reason |
| --- | --- | --- |
| module `github.com/standards-lab/docinfer/cmd/docinfer` | carried | go-cli-sdk and go-core at their latest (C2) |
| `apptest.Serve` | dropped | go-ai's `aitest` (C22; `docs/library-evaluation.md`, Rejected) |
| the manifest's `Attempt` | dropped | Bunrei's manifest, outside docinfer's API |
| `outdir.Line` | renamed | `docinfer.Chunk` in the library; `outdir.Line` stays the command's |
| `outdir.Part` | dropped | its outcome is the part's own event |
| `outdir.Dir.WriteChunks` | dropped | the record is appended per batch (C12) |
| `outdir.Line`'s `Input`, `File`, `ContentType` | dropped | the input's file fields: its part holds what it has |
| `Transcript` (the command's) | dropped | a transcription `Inference` |
| `Failure` | dropped | `inferences.jsonl` holds each refused or failed image and window by part ID and content SHA-256 (C15) |
| `Docproc` | carried | its `dir` relative, recorded once (C17) |
| `Capability` (the command's) | carried | holds `docinfer.Identity` and typed bounds (C14) |
| `Skipped` | carried | |
| `Space` | carried | the spike's measured embedding space and its norms |
| `Line` (the command's) | carried | its part and source inline; grouped by C18's key |
| `Recorder` | carried | `internal/apptest`'s own (C22) |
| `results/` | carried | a `FileCache` |
| `texts/` | carried | a `dispatch.FileContent`, no `.txt` (C10) |
| `chunks.jsonl` | carried | appended per batch (C12) |
| the manifest | carried | `manifest.json`, gaining `docinfer`, `inputs`, and the relative `docproc.dir` (C13, C17) |
| `cmd/docinfer` (the spike's `main.go`) | carried | at the module's root (C3) |
| `app`, `apptest` | carried | `internal/app`, `internal/apptest`, from the latest `template/v*` release (C2, C3) |
| `config` | dropped | the node constructors (C1) |
| `infer` | renamed | `internal/run` (C3) |
| `commands.go` | carried | `run`'s |
| `progress.go` | renamed | `run`'s `output.go` (C3; no progress in the library, `docs/library-evaluation.md`) |
| `models` | dropped | folded into `run`, over `(*ai.Client).Models` (C5) |
| `source` | dropped | `rundir` (C4) |
| `outdir` | carried | `internal/outdir` (C3) |
| `report` | carried | `internal/report` (C3) |

Names the command adds: `outdir.json`, `inferences.jsonl`, `report/`, `Manifest`'s `Docinfer`,
`Inputs`, and `Outputs`, `Build`, `Input`, `outdir.Inference`, and `integration`.

## Release

The tag, the module it versions, and the task of v1.ai.inference that releases it.

| Tag | Module | Task | Requires |
| --- | --- | --- | --- |
| `cmd/docinfer/v0.1.0` | `cmd/docinfer` | docinfer | the base module and docproc, go-ai, go-cli-sdk, and go-core at their latest |
| `cmd/docinfer/v0.2.0` | `cmd/docinfer` | acceptance | docinfer v0.3.0 and go-ai v0.5.0 |

`cmd/docinfer`'s first CHANGELOG entry links spike-ai-pipeline 6a631f1.
