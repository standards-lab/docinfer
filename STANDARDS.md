# docinfer standards

The judgement calls the standards-reviewer applies to docinfer, beyond what `mise run check`
enforces: each one line, with a pointer to the architecture page that applies. They come from
docinfer's design review, `context/design.md`, and apply to the base module, `postgres`, and
`cmd/docinfer`.

- `architecture/principles/repository-topology.md`: docinfer is an adjacent library whose base
  module, over go-ai, sits above go-ai's tier, so only the layers above it use docinfer; `postgres`
  and `cmd/docinfer` pin only their own levels.
- `architecture/principles/tool-beside-library.md`: `cmd/docinfer` is a thin consumer of the
  library, and nothing of its terminal progress enters the root package (L4).
- `architecture/principles/composition-root.md`: the library builds no client; each capability's
  `*ai.Client` is built by the host, in `cmd/docinfer`'s `internal/app` a graph node (L3, C1).
- `architecture/principles/validation-first.md`: `docinfer.New`, `NewBounds`, and `job.New` refuse
  a bad value before any job runs, `join.Request` validated at `docinfer.New`, and `Run` refuses an
  `Extract` before it reads an input; `cmd/docinfer`'s configuration fails its Build before the
  docproc run is read.
- `architecture/principles/resolution.md`: docinfer calls go-ai's client as go-ai presents it:
  no helper restating go-ai, no `/models` request of its own, and no re-validation of what go-ai
  validates (L14, C5, C7).
- `architecture/principles/rolling-currency.md`: every module's requirements and the root's mise
  pins are current; `cmd/docinfer` starts from go-cli-sdk-template's template/v0.3.0 (C2).
- `architecture/principles/minimal-footprint.md`: the base module requires docproc and go-ai
  alone; the join of work in flight is docinfer's own, `golang.org/x/sync` rejected
  (`context/design.md`, "Join of work in flight").
- `architecture/principles/service-tiers.md`: `Cache` is the interface, `FileCache` and
  `MemoryCache` its implementations in the base module, and `postgres` its provider module, over
  sqlate, each an adapter behind the one interface.
- `architecture/principles/downward-dependencies.md`: nothing at or below go-ai's tier imports
  docinfer; `postgres` and `cmd/docinfer` depend down on the base module.
- `architecture/standards/go-elemental/principles/dependencies.md`: the bottom-up line across the
  base, `postgres`, and `cmd/docinfer` `go.mod` files, none required by a module beneath it.
- `architecture/principles/independent-releases.md`: each module tags on its own, the base module
  before any that requires it, and no tagged module carries a replace directive.
- `architecture/standards/go-elemental/principles/construction-and-execution.md`: `docinfer.New`
  and `job.New` each take a `Config` struct, as docproc's `dispatch.New` does, since each needs
  more than four required inputs; each panics by name on a nil required field and refuses a bad
  value matching `docinfer.ErrRequest`, `job` declaring none of its own. `NewBounds` takes its
  four bounds positionally. No functional options.
- `architecture/standards/go-elemental/principles/baseline-standards.md`: the library ships no
  policy, no default for bounds, batch bytes, words per second, timeouts, or retry; `cmd/docinfer`
  ships the operator's starting values.
- `architecture/standards/go-elemental/principles/timeouts.md`: each bound has one owner, and a
  model request's is the host's transport: `ResponseHeaderTimeout`, and
  `ai.Options.StreamIdleTimeout` for a stream. docinfer sets none.
- `architecture/standards/go-elemental/principles/utc-times.md`: a time converts where it leaves
  the code, through `schema.TimeOf`, and each test package's `TestMain` sets a non-UTC zone.
- `architecture/standards/go-elemental/principles/tests-and-docs.md`: unit tests run against
  go-ai's `aitest` fake endpoint with no sleeps, a stall being a finding; real endpoints run behind
  `-tags integration`, the base module's in `integration/`, which `mise run test:integration`
  runs against the endpoints go-ai's `AI_<ROLE>_*` variables name and no CI job runs, and the
  command's in `cmd/docinfer/integration` over `processtest`; every `Cache` passes
  `docinfertest`'s one suite; golden keys and a chunk corpus pin the cache's formats.
- `architecture/standards/go-elemental/principles/release-and-ci.md`: the check, currency, the
  root `go.work`, a changelog per module, and per-module tags, with CI as the gate.
- `architecture/standards/go-elemental/principles/dsl-driven-services.md`: `postgres`'s cache,
  its authored statements and migrations over sqlate.
- `architecture/standards/go-elemental/principles/cli-applications.md`: `cmd/docinfer`'s
  configuration and model clients are graph nodes, and its help states its exit status.
- `architecture/standards/go-elemental/principles/topology-and-naming.md`: the base module, the
  `postgres` provider sub-module, and the `cmd/docinfer` command module, and their tags; the
  inference package is the module's root, `docinfer`.
- `architecture/standards/go-elemental/principles/domain-files.md`: `cmd/docinfer`'s command
  packages under `internal/`, `run` and `report`, only.
- The exception for a command nested in its library's repository: since the module's one public
  surface is the command, `cmd/docinfer` keeps `main.go` at its module's root, so
  `go install github.com/standards-lab/docinfer/cmd/docinfer@<version>` installs `docinfer`, and
  keeps its packages under `internal/`: `app`, `apptest`, its command packages, `run` and
  `report`, and `outdir`, which they share. `cli-applications.md` has `cmd/<name>/main.go`
  instead, and `topology-and-naming.md` has domain packages at the module's root level. The
  module's one package outside `internal/` is `integration`, the built-binary tier's harness, at
  the module's root, where go-cli-sdk-template's template/v0.3.0 places it.
