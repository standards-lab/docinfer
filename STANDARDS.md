# docinfer standards

The judgement calls the standards-reviewer applies to docinfer, beyond what `mise run check`
enforces: each one line, with a pointer to the architecture page that applies. They come from
docinfer's design review, `context/design.md`, and apply to the base module, `postgres`, and
`cmd/docinfer`.

- `architecture/principles/repository-topology.md`: docinfer sits above go-ai's tier, in the
  templates' layer; its place follows its base module.
- `architecture/principles/minimal-footprint.md`: the base module requires docproc and go-ai
  alone; the join of work in flight is docinfer's own, `golang.org/x/sync` rejected
  (`context/design.md`, "Join of work in flight").
- `architecture/principles/downward-dependencies.md`: nothing at or below go-ai's tier imports
  docinfer; `postgres` and `cmd/docinfer` depend down on the base module.
- `architecture/standards/go-elemental/principles/dependencies.md`: the bottom-up line across the
  base, `postgres`, and `cmd/docinfer` `go.mod` files, none required by a module beneath it.
- `architecture/principles/independent-releases.md`: each module tags on its own, the base module
  before any that requires it, and no tagged module carries a replace directive.
- `architecture/standards/go-elemental/principles/construction-and-execution.md`: `docinfer.New`
  and `NewBounds` validate, refuse with `ErrRequest`, and panic by name on a nil required field;
  structs with methods, no functional options.
- `architecture/standards/go-elemental/principles/baseline-standards.md`: the library ships no
  policy, no default for bounds, batch bytes, words per second, timeouts, or retry; `cmd/docinfer`
  ships the operator's starting values.
- `architecture/standards/go-elemental/principles/timeouts.md`: the host owns each request's
  bound, the transport's `ResponseHeaderTimeout` and `ai.Options.StreamIdleTimeout`; docinfer adds
  none.
- `architecture/standards/go-elemental/principles/utc-times.md`: a time converts where it leaves
  the code, through `schema.TimeOf`, and each test package's `TestMain` sets a non-UTC zone.
- `architecture/standards/go-elemental/principles/tests-and-docs.md`: unit tests run against
  go-ai's `aitest` fake endpoint with no sleeps, a stall being a finding; real endpoints run behind
  `-tags integration` in `cmd/docinfer/integration` over `processtest`; every `Cache` passes
  `docinfertest`'s one suite; golden keys and a chunk corpus pin the cache's formats.
- `architecture/standards/go-elemental/principles/release-and-ci.md`: the check, currency, the
  root `go.work`, a changelog per module, and per-module tags, with CI as the gate.
- `architecture/standards/go-elemental/principles/dsl-driven-services.md`: `postgres`'s cache,
  its authored statements and migrations over sqlate.
- `architecture/standards/go-elemental/principles/cli-applications.md`: `cmd/docinfer`'s
  configuration and model clients are graph nodes, and its help states its exit status.
- `architecture/standards/go-elemental/principles/domain-files.md`: `cmd/docinfer`'s command
  packages under `internal/` only.
- The exception for a command nested in its library's repository: since the module's one public
  surface is the command, `cmd/docinfer` keeps `main.go` at its module's root, so
  `go install github.com/standards-lab/docinfer/cmd/docinfer@<version>` installs `docinfer`, and
  keeps its packages under `internal/`: `app`, `apptest`, and its command packages.
  `cli-applications.md` has `cmd/<name>/main.go` instead, and `topology-and-naming.md` has domain
  packages at the module's root level. The module's one package outside `internal/` is
  `integration`, the built-binary tier's harness, at the module's root, where
  go-cli-sdk-template's template/v0.3.0 places it.
