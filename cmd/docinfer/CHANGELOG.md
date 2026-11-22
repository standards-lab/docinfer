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
- `docinfer run <docproc-dir> --out <dir> [--run <id>]`, which for now checks each capability's
  model through its own client's model listing, within 30 seconds, and prints the table of their
  states: loaded or the listing's status, listed, unlisted, unchecked (the endpoint gives no
  listing), unreachable, or skipped. An unlisted model or an unreachable endpoint exits 1.
- The built-binary integration tier, under the `integration` build tag, run by
  `mise run test:integration`: it builds `docinfer` and asserts `version`, the root help, and an
  unknown command's usage error through its arguments, output, and exit status.

[Unreleased]: https://github.com/standards-lab/docinfer/commits/HEAD/cmd/docinfer
