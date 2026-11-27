# Changelog

All notable changes to `github.com/standards-lab/docinfer/cmd/docinfer`, the docinfer command, are
documented here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and
the module adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html). Each release's
section is headed by the version its `cmd/docinfer/vX.Y.Z` tag carries, and is the release's
notes. Its command line, output directory, and exit codes are its contract: a change to any of
them is a change here. This changelog covers the cmd/docinfer module only; the base module and
the postgres module keep their own.

## [Unreleased]

## [v0.3.0] - 2026-11-27

This release embeds each input's metadata records: its chunk records gain a metadata source per
office, PDF, media, or picture record and per mail message or OneNote page, rendered by docinfer
v0.4.0. It requires docinfer v0.4.0, go-ai v0.5.0, and docproc v0.10.0.

### Changed

- The module requires docinfer v0.4.0 and docproc v0.10.0. `chunks.jsonl` carries a source of
  kind `metadata` for each metadata record docinfer renders, its `output` the record and its
  `sha256` the rendered text's, beside the input's own metadata text, whose `output` is absent.

## [v0.2.0] - 2026-11-25

This release runs against the live rows: each capability's served model from its listing, e5's
input types and token chunking within a character bound, vision's card sampling, image bound,
and header timeout, and chat transcription's reply bound and temperature 0. It requires docinfer
v0.3.0, go-ai v0.5.0, and docproc v0.9.0.

### Added

- `served_model` on each capability, `vision`, `transcription`, and `embeddings`: the model the
  deployment serves under the capability's `model`, which keys its results. `run` takes the served
  model from the model listing's entry for `model`, its `root`, as vLLM lists it, or else from
  `served_model`, which has no starting value. A `served_model` the listing's `root` disagrees
  with, or a capability with neither, stops the run before it reads docproc's run, naming the
  capability, both values, and the `served_model` to set.
- `embeddings.input_types`, the embedding model's input-type conventions, go-ai's
  `EmbeddingProfile` `input_types` object, such as multilingual-e5's
  `{"document":{"prefix":"passage: "},"query":{"prefix":"query: "}}`, set on the embeddings
  client, which sends every chunk as a document; `{}` declares none.
- `embeddings.tokenizer`, the endpoint whose tokenize route counts a chunk's tokens, built as its
  own client with the embeddings' `input_types`, so a document's count takes its prefix: through a
  gateway the embeddings' own endpoint, straight to vLLM an endpoint whose `base_url` is the
  server's root, such as `http://framework-2:8003`. It must front the same deployment as the
  embeddings' endpoint: the chunker's identity names the embeddings' served model, and nothing
  checks the tokenizer's, since a vLLM server's root has no model listing.
- `vision.native`, a JSON object of members the endpoint defines, sent in every vision request
  and keyed into vision's identity, starting at Gemma 4's card sampling,
  `{"temperature":1.0,"top_p":0.95,"top_k":64}`, sent explicitly so it is keyed. A configured
  `vision.native` replaces it whole, and `{}` sends no native member, vision's identity then
  carrying none. One that isn't a JSON object with unique member names fails the configuration.
- The manifest's capabilities gain `request_model`, the model the requests named, beside the
  identity's `model`, now the served model.
- `embeddings.chunk_runes`, override `DOCINFER_EMBEDDINGS_CHUNK_RUNES`: the most characters,
  Unicode code points, an embedded input holds, its document prefix included, starting at 8,192,
  the length vLLM refuses an input over, before tokenizing it, at a `max_model_len` of 512. The
  chunker's `Runes` is `chunk_runes` less the document prefix's characters, 8,183 under e5's
  `"passage: "`; a `chunk_runes` below 1, or not over the prefix's characters, fails the
  configuration's Build before docproc's run is read.
- `transcription.max_tokens`, override `DOCINFER_TRANSCRIPTION_MAX_TOKENS`: the most tokens a
  chat-mode transcription reply takes, sent as the request's `MaxOutputTokens` and keyed into
  transcription's identity, starting at 512 in chat mode and at 0, sending none, in route mode. A
  window cut off at it is recorded incomplete, reason `max_output_tokens`, its partial text joined
  into the transcript, or trimmed before a loop, reason `repetition`. A nonzero
  `max_tokens` in route mode, which has no field for it, or a negative one, fails the
  configuration's Build before docproc's run is read, naming the fix: `max_tokens` 0, or `mode`
  chat.
- `transcription.native`, override `DOCINFER_TRANSCRIPTION_NATIVE`: a JSON object of members the
  endpoint defines, sent in every chat-mode transcription request and keyed into transcription's
  identity, starting at `{"temperature":0}` in chat mode, so a window's transcript is the same on
  every run, and at none in route mode. A configured `transcription.native` replaces it whole, and
  `{}` sends no native member, transcription's identity then carrying none. One that isn't a JSON
  object with unique member names, or a non-empty one in route mode, which sends no JSON body,
  fails the configuration's Build before docproc's run is read, naming the fix: `native` `{}`, or
  `mode` chat.
- `vision.max_pixels`, override `DOCINFER_VISION_MAX_PIXELS`: the most pixels, width × height, an
  image is sent at, wired to `docinfer.Vision.MaxPixels` and keyed into vision's identity. A
  larger PNG or JPEG is downscaled to it, its aspect ratio kept, and re-encoded in its own format;
  any other image goes as it is. It starts at 2,580,480, the vision row's Gemma 4 budget at 1,120
  soft tokens (1,120 × 9 patches × 16² px): Gemma 4's processor resizes every image, aspect kept,
  to at most that, so more is wasted transfer and server decode. 0 sends every image as it is; a
  negative one fails the configuration's Build before docproc's run is read.

### Changed

- **Breaking:** `embeddings.input_types` and `embeddings.tokenizer` are required, with no
  starting value; a configuration without them fails its Build before docproc's run is read.
- **Breaking:** `embeddings.chunk_size` is in tokens, as the tokenizer counts them, its document
  prefix included, starting at 480, in place of 2,000 characters.
- Transcription's identity settings gain `"max_tokens"`, and `"native"` when one is sent, and its
  request version is "3", so every transcript held before misses and is transcribed again, a
  window held incomplete with no text among them.
- A vision reply that loops to `max_tokens`, a line, a sentence, or a short cycle repeated to the
  bound, is trimmed to before the repeat and recorded incomplete, reason `repetition`, in place of
  `max_output_tokens` and the whole looping text.
- Vision's request version is "2" and its identity settings gain `"max_pixels"`, so every image
  description held before misses and is described again.
- The manifest's `space.chunker`, the chunker's settings, gains `"runes"`, so chunk spans held
  under the former settings miss and are recut.
- The module requires docproc v0.9.0, docproc's release before the capstone; docinfer's use of
  its API is unchanged, and it reads run directories docproc v0.4 wrote as before.
- The endpoint `vision.endpoint` names starts its `response_header_timeout` at 600s, every other
  endpoint keeping 180s, since a non-streaming reply's header waits for the whole reply, up to
  vision's `max_tokens`; an endpoint vision shares with another capability takes vision's start,
  and a configured value still replaces it.

### Fixed

- A text of long white-space runs, such as a multi-column PDF's layout extraction, gave chunks
  within `chunk_size`'s tokens but tens of thousands of characters, which vLLM refused with a 400
  before tokenizing them, recorded as refusals; `chunk_runes` now bounds them.
- `run` transcribed only the first audio or video input of a docproc run: every input's own part
  is `/`, so the library grouped all their windows as one and dropped the later inputs' windows
  uncounted, the run ending 0. `run` names each line's input to the library as its `Origin`, so
  each input's windows join into its own transcript, and the transcription line counts the windows
  placed in no transcript, `unplaced`, after `failed`.
- A part two inputs share, such as a section two OneNote files both hold, had its chunk and
  inference lines named by whichever of the inputs had been fed last when each result arrived, so
  a rerun could name some differently. Each line now names the input whose results it holds, the
  `Origin` the library echoes on each inference and source, whatever order the results arrive in.

## [v0.1.0] - 2026-11-23

The first release, from spike-ai-pipeline's `cmd/docinfer` at
[6a631f1](https://github.com/JaimeStill/spike-ai-pipeline/tree/6a631f1/cmd/docinfer), rebuilt on
go-cli-sdk-template template/v0.3.1 to the design in `context/command.md`.

### Added

- The module, with `main.go` at its root, so
  `go install github.com/standards-lab/docinfer/cmd/docinfer@<version>` installs `docinfer`. Its
  composition root, `internal/app`, and the root's fixtures, `internal/apptest`, start from
  go-cli-sdk-template's template/v0.3.1 on go-cli-sdk v0.4.0 and go-core v0.12.0, and
  `integration` holds the built-binary tier's harness. It requires the docinfer base module at
  v0.2.0, docproc v0.4.1, go-ai v0.4.0, go-cli-sdk, and go-core.
- `docinfer version`, which prints two lines, `command <version>` and `library <version>`: the
  command's version and that of the docinfer base module it was built with, each read from the
  binary's build info, or `(devel)` for a binary that carries none. It builds nothing and reads no
  configuration.
- The configuration, read from the directory that the root flag `--config` or `DOCINFER_CONFIG`
  names, the flag winning, through go-core's layered load: `config.json`, its `DOCINFER_ENV`
  overlay, `secrets.json`, and its overlay. A setting's starting value follows its name:
  - `endpoints`, each with `base_url`; `format` (`completions`, `messages`, or `responses`); an
    optional `profile` of the format's options; `query`; `response_header_timeout` 180s, set on
    the endpoint's transport; `retry` (`max_attempts` 3, `base_delay` 500ms, and `max_delay`
    30s); and `token` and `headers`, which belong in the secrets file.
  - `vision` (`max_tokens` 2048, and `reasoning` none, or any other effort go-ai accepts) and
    `transcription` (`mode` route or chat, set on transcription's own client, chat refused
    without the completions format, and `words_per_second` 8). Each is optional, and skipped
    when unset.
  - `embeddings`, required (`dimensions`, `batch` 16, and `chunk_size` 2000).
  - In each capability, `endpoint`, `model`, and `concurrency` 4.
  - `run` (`pending` 16 and `batch_bytes` 524288).

  `DOCINFER_` followed by a setting's path overrides it, such as
  `DOCINFER_ENDPOINTS_<NAME>_RESPONSE_HEADER_TIMEOUT`. A configuration error fails the command
  before it reads anything, labelled `config`, and a client that go-ai refuses is named for its
  endpoint.
- `docinfer run <docproc-dir> --out <dir> [--run <id>]`. It first checks each capability's model
  through its own client's model listing, within 30 seconds, and prints a table of their states:
  loaded or the listing's status, listed, unlisted, unchecked (the endpoint gives no listing),
  unreachable, or skipped. An unlisted model or an unreachable endpoint exits 1. It then reads
  docproc's latest run that ended, or the one `--run` names, and feeds the pipeline every record
  line, each input's metadata text with its part line, as the job's last delivery. It prints a
  line per docproc input as that input's work ends, a line of counts per capability, and a last
  line counting the inputs, outputs, chunks, and failures. A run that ends exits 0, with refused
  or failed images, windows, and chunks recorded and counted; a run that stops, never begins, or
  is cancelled exits 1. A `--run` that isn't one of docproc's runs, or that names a run that
  hasn't ended, is refused before anything is written.
- The output directory, at schema "2":
  - `outdir.json`, `{"schema": "2"}`. Reading refuses any other schema, naming both, and takes a
    directory holding `results/`, `texts/`, or `runs/` without the marker for schema "1".
  - `results/`, the held results.
  - `texts/<xx>/<sha256>`, each inference's text.
  - `runs/<run>/manifest.json`, written as the run starts and as it ends. It holds the build's
    versions; the UTC start and end; docproc's directory, relative to the output directory, and
    its run; each input's path and SHA-256 or error; each capability's identity, endpoint, and
    bounds; the skipped capabilities; the embedding space; the counts; the outputs read; and the
    run's error.
  - `runs/<run>/chunks.jsonl`, a line per chunk with its part and source inline, appended per
    batch.
  - `runs/<run>/inferences.jsonl`, each vision and transcription inference, the refused and failed
    ones among them by part ID and content SHA-256, appended as made.

  Because both records are appended as the run goes, a stopped run keeps what it made. Each
  chunk line and inference line names, as `"input"`, the path of the input its part was found in.
- `docinfer report <dir> [--run <id>]`, which renders the report of the latest run that ended, or
  of the one `--run` names, into `runs/<run>/report/` and prints its index's absolute path. The
  report is an `index.html` listing every input, sorted by path, with its counts, and a
  `<sha256>.html` per input content, named as docproc's page for the same content is, which it
  links. A content's page shows each vision text whole, with its outcome, its reason, and an `img`
  of docproc's content file; each transcript, with its windows, those not transcribed, and its
  seams; and each source text once, as its chunks in order, with each rejected chunk's error and
  no vector. An input with no SHA-256 keeps an unlinked row with its error, a text whose SHA-256
  isn't its name is marked damaged, and an unended run's pages are marked not ended. The report
  reads docinfer's directory alone and reads no clock, so the same directory renders the same
  bytes whether or not docproc's directory is present. It exits 1 when it renders nothing and 2
  for a usage error.
- `docinfer run --report`, which, once a run has ended and printed its last line, renders the
  run's report as `docinfer report` does and prints the index's absolute path. A run that stops
  or is cancelled renders nothing. A failed render exits 1, leaves the run's manifest and records
  as the run wrote them, and its error names the `docinfer report <dir> --run <id>` that renders
  the pages again.
- An exit-status footer at the end of every command's help, the root's and `version`'s included:
  0 for a run that ended, with refused and failed images, windows, and chunks recorded and
  counted by the last line; 1 for a run that stopped, never began, or was cancelled, or a report
  that rendered nothing; 2 for a usage error or for help.
- The built-binary integration tier, under the `integration` build tag, which
  `mise run test:integration` runs. It builds `docinfer` and asserts `version`, the root help, and
  an unknown command's usage error through their arguments, output, and exit status. Against live
  endpoints, read from go-ai's `AI_<ROLE>_*` variables for the embedding, vision, and
  transcription or audio roles, it runs `docinfer run --report` over a docproc run directory it
  writes, then `docinfer report`, and wants each to exit 0, the run's manifest to be ended, and
  the report's index to be printed. A role whose variable is unset is left out of the run, and
  with `AI_EMBEDDING_BASE_URL` unset the scenario skips.

[Unreleased]: https://github.com/standards-lab/docinfer/compare/cmd/docinfer/v0.3.0...HEAD
[v0.3.0]: https://github.com/standards-lab/docinfer/compare/cmd/docinfer/v0.2.0...cmd/docinfer/v0.3.0
[v0.2.0]: https://github.com/standards-lab/docinfer/compare/cmd/docinfer/v0.1.0...cmd/docinfer/v0.2.0
[v0.1.0]: https://github.com/standards-lab/docinfer/releases/tag/cmd/docinfer/v0.1.0
