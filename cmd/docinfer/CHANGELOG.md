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
- The built-binary integration tier, under the `integration` build tag, run by
  `mise run test:integration`: it builds `docinfer` and asserts `version`, the root help, and an
  unknown command's usage error through its arguments, output, and exit status.

[Unreleased]: https://github.com/standards-lab/docinfer/commits/HEAD/cmd/docinfer
