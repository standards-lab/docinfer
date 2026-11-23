# Changelog

All notable changes to `github.com/standards-lab/docinfer/cmd/docinfer`, the docinfer command, are
documented here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and
the module adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html). Each release's
section is headed by the version its `cmd/docinfer/vX.Y.Z` tag carries, and is the release's
notes. Its command line, output directory, and exit codes are its contract: a change to any of
them is a change here. This changelog covers the cmd/docinfer module only; the base module and
the postgres module keep their own.

## [Unreleased]

### Added

- The module. `main.go` sits at its root, so
  `go install github.com/standards-lab/docinfer/cmd/docinfer@<version>` installs `docinfer`. Its
  composition root, `internal/app`, and its fixtures, `internal/apptest`, sit under `internal/`,
  started from go-cli-sdk-template's template/v0.3.1 on go-cli-sdk v0.4.0 and go-core v0.12.0,
  and `integration` holds the built-binary tier's harness. It requires the docinfer base module
  at v0.2.0, go-cli-sdk, and go-core.
- `docinfer version`, which prints two lines, `command <version>` and `library <version>`: the
  command's version and the docinfer base module's it was built with, each from the binary's
  build info, or `(devel)` for a binary that carries none. It builds nothing and reads no
  configuration.
- The configuration, read from the directory the root flag `--config` or `DOCINFER_CONFIG`
  names, the flag winning, through go-core's layered load: `config.json`, its `DOCINFER_ENV`
  overlay, `secrets.json`, and its overlay. It holds `endpoints`, each with `base_url`, `format`
  (`completions`, `messages`, or `responses`), an optional `profile` of the format's options,
  `query`, `response_header_timeout` (starting at 180s, on the endpoint's transport), `retry`
  (`max_attempts` 3, `base_delay` 500ms, and `max_delay` 30s), and `token` and `headers`, which
  belong in the secrets file; `vision` (`max_tokens` 2048, `reasoning` none, any effort go-ai
  accepts), `transcription` (`mode` route or chat, set on transcription's own client, chat
  refused without the completions format, `words_per_second` 8), each optional and skipped when
  unset; `embeddings`, required (`dimensions`, `batch` 16, `chunk_size` 2000), each capability
  with `endpoint`, `model`, and `concurrency` 4; and `run` (`pending` 16, `batch_bytes` 524288).
  `DOCINFER_` and a setting's path overrides it, such as
  `DOCINFER_ENDPOINTS_<NAME>_RESPONSE_HEADER_TIMEOUT`. A configuration error fails the command
  before it reads anything, labelled `config`, and a client go-ai refuses is named for its
  endpoint.
- `docinfer run <docproc-dir> --out <dir> [--run <id>]`, which first checks each capability's
  model through its own client's model listing, within 30 seconds, and prints the table of their
  states: loaded or the listing's status, listed, unlisted, unchecked (the endpoint gives no
  listing), unreachable, or skipped. An unlisted model or an unreachable endpoint exits 1. It
  then reads docproc's latest run that ended, or the one `--run` names, and feeds every record
  line, each input's metadata text with its part line, to the pipeline as the job's last
  delivery, printing a line per docproc input as its work ends, a line of counts per capability,
  and a last line counting the inputs, outputs, chunks, and failures. A run that ends exits 0,
  refused or failed images, windows, and chunks recorded and counted; a run that stops, never
  begins, or is cancelled exits 1. A `--run` that isn't one of docproc's runs, or names one that
  hasn't ended, is refused before anything is written.
- The output directory at schema "2": `outdir.json`, `{"schema": "2"}`, which reading refuses
  for any other schema, naming both, and which `results/`, `texts/`, or `runs/` without it marks
  as schema "1"; `results/`, the held results; `texts/<xx>/<sha256>`, each inference's text; and
  per run `runs/<run>/manifest.json`, written as the run starts and as it ends, with the build's
  versions, UTC start and end, docproc's directory relative to the output directory and its run,
  each input's path and SHA-256 or error, each capability's identity, endpoint, and bounds, the
  skipped capabilities, the embedding space, the counts, the outputs read, and the run's error;
  `runs/<run>/chunks.jsonl`, a line per chunk with its part and source inline, appended per
  batch; and `runs/<run>/inferences.jsonl`, each vision and transcription inference, refused and
  failed ones among them by part ID and content SHA-256, appended as made, so a stopped run
  keeps what it made. Each chunk line and inference line names, as `"input"`, the path of the
  input its part was found in.
- `docinfer report <dir> [--run <id>]`, which renders a run's report, the latest that ended or
  the one `--run` names, into `runs/<run>/report/` and prints its index's absolute path: an
  `index.html` listing every input, sorted by path, with its counts, and a `<sha256>.html` per
  input content, the name docproc's page for the same content has, which it links. A content's
  page shows each vision text whole, with its outcome, its reason, and an `img` at docproc's
  content file; each transcript, with its windows, those not transcribed, and its seams; and
  each source text once, as its chunks in order, with each rejected chunk's error and no vector.
  An input with no SHA-256 keeps an unlinked row with its error, a text whose SHA-256 isn't its
  name shows marked damaged, and an unended run's pages are marked not ended. It reads
  docinfer's directory alone and no clock, so the same directory renders the same bytes, docproc's
  directory present or not. It exits 1 when it renders nothing and 2 for a usage error.
- `docinfer run --report`, which, once a run has ended and its last line is printed, renders
  its report as `docinfer report` does and prints the index's absolute path. A run that stops
  or is cancelled renders nothing. A render that fails exits 1, leaving the run's manifest and
  records as the run wrote them, and names the `docinfer report <dir> --run <id>` that renders
  the pages again.
- An exit-status footer at the end of every command's help, the root's and `version`'s among
  them: 0 for a run that ended, refused and failed images, windows, and chunks recorded and
  counted by the last line; 1 for a run that stopped, never began, or was cancelled, or a report
  that rendered nothing; 2 for a usage error, and for help.
- The built-binary integration tier, under the `integration` build tag, run by
  `mise run test:integration`: it builds `docinfer` and asserts `version`, the root help, and an
  unknown command's usage error through its arguments, output, and exit status. Against live
  endpoints, read from go-ai's `AI_<ROLE>_*` variables for the embedding, vision, and
  transcription or audio roles, it runs `docinfer run --report` over a docproc run directory it
  writes and then `docinfer report`, wanting each to exit 0, the run's manifest ended, and the
  report's index printed. A role whose variable is unset is left out of the run, and with
  `AI_EMBEDDING_BASE_URL` unset the scenario skips.

[Unreleased]: https://github.com/standards-lab/docinfer/commits/HEAD/cmd/docinfer
