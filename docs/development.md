# Development

The reasoning behind docinfer's development configuration: the mise tasks, CI, the release
workflow, the compose harness, the lint configuration, and the tests. The tasks themselves are
listed in the [README](../README.md#development).

## mise

`mise.toml` pins every tool a task runs, marathon's `lint-context` and `lint-references` among
them, and turns off mise's automatic install, so a task never fetches a tool mid-run: a missing
tool is a `mise install` away.

- **Modules.** `GO_MODULES` is the space-separated list of module directories, kept in sync with
  `go.work`'s use list: `.` is the base module, `postgres` the PostgreSQL cache's, and
  `cmd/docinfer` the command's.
- **The live tier's DSN.** `DOCINFER_DSN` names the compose harness's PostgreSQL, whose port
  follows `DOCINFER_POSTGRES_PORT` as `compose/postgres.yml`'s does. Each test creates and drops
  its own database on it.
- **The check** runs every module with `GOWORK=off`, as a consumer resolves it, and vets and lints
  each module twice, the second time with the `integration` build tag, so the integration tiers
  are checked without running. sqlint is the postgres module's tool, so it runs from that
  directory and resolves the engine `sqlint.toml` names through the module's own requirements.
  The module steps and sqlint stop at the first failure; `lint-context`, `lint-references`, and
  `scripts/lint-secrets.sh` then all run before the check fails on any, and a lint missing from
  PATH fails it naming `mise install`. `lint-secrets` checks `fnox.toml` with no key
  ([Secrets](#secrets)).
- **`test:integration`** runs each module's `integration/` package, where one exists, against the
  endpoints go-ai's tier's variables name, which pass through from the caller's environment:
  `AI_EMBEDDING_BASE_URL`, which every scenario needs, `AI_VISION_BASE_URL`,
  `AI_TRANSCRIPTION_BASE_URL` (route mode), `AI_AUDIO_BASE_URL` (chat mode), and
  `AI_AGENT_BASE_URL` (extraction), each an API root including `/v1`. Each role may add
  `AI_<ROLE>_TOKEN`, a bearer token, and `AI_<ROLE>_HEADER`, one or more `Name: value` lines;
  unset, none is sent, and no failing scenario prints a token or header value. `AI_<ROLE>_MODEL`
  names the model of an endpoint that lists more than one. A scenario whose variable is unset
  skips, naming it, so with none set the run skips whole and passes. It runs verbose, so the log
  lists each scenario passed or skipped, and it never starts a server. It fills an unset
  `AI_<ROLE>_TOKEN` from the committed `fnox.toml` ([Secrets](#secrets)).
  [`integration/doc.go`](../integration/doc.go) tells each scenario in full.
- **`integration:postgres`** starts the compose harness's PostgreSQL alone, runs the postgres
  module's integration suite, and stops the service whether the suite passes or fails, printing
  its log on a failure. **`integration`** runs every provider's live tier one module at a time,
  continuing past a failure; the postgres module has the only one.
- **`upgrade`** moves each go directive to the current Go minor with no toolchain line, skips
  `go get` for a module with no direct requirement, and passes `--no-global` so it writes this
  `mise.toml` alone, never the global configuration.

## Currency

`scripts/currency.sh` scans each module's direct requirements and go directive, the mise tools,
the workflows' action pins, and the compose harness's image pins. crane lists an image's tags for
the image scan; it is development-time, as jq is, and no CI job installs either.

- Each scan captures its command's output in a variable before reading it, so under `set -e` a
  scan command that fails, such as a `go list` missing a `go.sum` entry, fails currency instead
  of reporting nothing.
- The workflows scanned are the project root's and the git top-level's, de-duplicated: one
  directory when the project is the repository, two when a repository nests the project in a
  subdirectory, as the template repository does. A project not yet in git has only its own.
- An image's latest tag follows `architecture/principles/rolling-currency.md`.
- A compose service builds from `compose/<service>/Dockerfile`, whose `FROM` line is its one pin.
  With no Dockerfiles, the glob stays unexpanded and is skipped.

`.claude/marathon.toml` names `mise run currency` as marathon's currency command; it reads the
network, so CI never runs it.

## Secrets

`fnox.toml` holds docinfer's one development-time secret, `AI_GATEWAY_TOKEN`, the model gateway's
key for the integration tier, encrypted to two recipients: `laptop` and the offline `backup`.
Only `test:integration` is granted it, and the task copies it into `AI_<ROLE>_TOKEN` for each role
whose `AI_<ROLE>_BASE_URL` is set and whose `AI_<ROLE>_TOKEN` is unset. Any OpenAI-compatible
endpoint's API key works in its place, or leave it unset to send no credential.

The [development-secrets
principle](https://github.com/standards-lab/architecture/blob/main/principles/development-secrets.md)
gives the convention and every step, including supplying your own value.

## CI and releases

CI's check job runs on every pull request into main, every push to main, and on demand. It
installs only what the check runs: Go, golangci-lint, marathon's lints, and the jq and yq
`lint-secrets` reads. The integration job runs `mise run integration` on a push to main and on
demand, never on a pull request, below the per-PR rate by design; it installs only Go, docker
being the runner's. No job runs the base module's AI tier, `test:integration`, whose models are on
the organization's own machines.

The release workflow runs on a pushed tag; nothing in CI tags. A tag is the base module's,
`vX.Y.Z`, or a nested module's, `<module>/vX.Y.Z`: `postgres/vX.Y.Z` or `cmd/docinfer/vX.Y.Z`. The tag's prefix, empty for the base module, picks the module's own
changelog, `CHANGELOG.md` at the root or `<module>/CHANGELOG.md`, and the release notes are the
version's section, headed `## [vX.Y.Z] - <date>`. A release has no assets: the module proxy
serves the module from the tag, and docinfer builds no system tool.

## The compose harness

`compose.yml` is test-only: PostgreSQL for the postgres module's live tier. Each service builds
from `compose/<service>/Dockerfile`, which pins its image and carries its configuration and health
check, and keeps its data in tmpfs, so every start begins empty.

- **Port.** 5437 by default, so the harness coexists with sqlate's on 5433, blobfs's on 5434,
  go-messaging's on 5435, docproc's on 5436, and a service on 5432. `DOCINFER_POSTGRES_PORT`
  moves it.
- **Credentials.** The user, password, and database are the ones `DOCINFER_DSN` names:
  development values, not secrets.
- **Health.** The service is healthy once the server accepts TCP connections to the app
  database. The probe goes over TCP, not the socket, because the first start's initialization
  runs a temporary server on the socket alone, which would report ready before the real server
  starts.

## Workspace

`go.work` spans the modules in `GO_MODULES`, so a change to the base module is seen by the
postgres and `cmd/docinfer` modules, which require it, before it is tagged. `go.work` and `go.work.sum` are
committed: the repository is multi-module by design, and the committed sum keeps the check's tree
clean.

## Lint configuration

Each module's `.golangci.yml` enables the standard set (errcheck, govet, ineffassign, staticcheck,
unused) plus depguard, which holds the module to its allowed imports, testpackage, which keeps
tests black-box, and nolintlint, which requires each suppression to name its linter and give its
reason. golangci-lint reads the configuration nearest the directory it runs in, so the root's
serves the base module and the postgres and `cmd/docinfer` modules each carry their own, the
root's but for depguard's lists.

- Test files are linted with the code they test, so the depguard rules hold them too.
- testpackage's skip regexp is narrowed to `export_test.go`, the one white-box file allowed.
  `allow-packages` is narrowed from its default, `main`, so a command's tests are black-box too,
  in `main_test`: it names only the empty package name, which no package has, because an empty
  list leaves the default in force.
- depguard's allow entries match by prefix unless they end in `$`, so each module's entries are
  anchored: the module as itself or a package under it, not a sibling such as `docinfer-extra`.
  The longest match wins, so a deny entry overrides an allow entry's prefix.
- The base module depends on nothing but the standard library, docproc, and go-ai, in every Go
  file, tests included; its deny entries keep the postgres and `cmd/docinfer` modules, which
  require the base, out of it.
- The postgres module's production files may import the standard library, docinfer's base
  packages, whose `Cache` it implements, sqlate's packages and its PostgreSQL engine, and pgx v5.
  Its tests add sqlate/postgres's `pgtest`. The deny entries keep the `cmd/docinfer` module out
  despite docinfer's prefix, and the test-support packages, `docinfertest`, `sqltest`, and
  `pgtest`, which import `testing`, out of the production files despite their modules' prefixes.
- The `cmd/docinfer` module's files may import the standard library, docinfer's base packages,
  docproc's and go-ai's, go-cli-sdk, and go-core's packages. The deny entries keep the postgres
  module out despite docinfer's prefix, and the test-support packages, `docinfertest`, go-ai's
  `aitest`, and go-core's `graphtest`, which import `testing`, out of the production files.

`postgres/sqlint.toml` holds the postgres module's SQL conventions; the base module holds no SQL.
The engine names the native forms a standard-tier file may not use, and the sources name the
pattern catalogs the statements' includes resolve against: sqlate's own. The cache's statements
are named for their operation, keep the delimiter out of comments and literals, and use no native
form in a standard-tier file; a native file is exempt by its tier and carries its port note. Each
migration holds a single statement. The migrations role has no native-forms check: the DDL is
PostgreSQL's, the only engine's.

## Tests

The root package's tests never sleep. A test holds a request at aitest's `Gate`, and sees a
waiter of the claim table, or a request waiting for a `Bounds` slot, through the hooks in
`export_test.go`; an extraction's progress is coalesced by a clock the test steps per text delta.
Every wait a test makes on a job is bounded by `failsafe`, as aitest's `Gate.Await` bounds its
own: a stall is a finding, so a wait that runs out fails the test rather than hang it. `job`'s
tests order a job's steps the same way, holding its index record, a content put, or an embedding
until the test lets it go, each hold bounded by `failsafe`.

Every package's `TestMain` sets `time.Local` to Asia/Kolkata, whose offset is never zero, so a
time the code leaves unconverted to UTC reads differently from one it converts; `time/tzdata`
loads the zone without the host's zoneinfo.

The golden keys pin `Key`'s form under `docinfer.Version`, which names their file. Rewrite them
with `go test . -run TestGoldenKeys -update` only with a new `Version`, never to make a changed
key pass under the old one: a changed key misses every result held under the old, and a key of
the old form over a new shape decodes wrong.

The golden corpus of chunks, `chunk/testdata/chunks-v<Version>.json`, pins how texts are cut under
`chunk.Version`. Rewrite it with `go test ./chunk -run TestGoldenCorpus -update` only with a new
`chunk.Version`, never to make a changed cut pass under the old one: docinfer's Profile reports the
version, so chunks cut differently under the same version would be taken for the old.

`postgres`'s golden test pins every released migration as `Migrations` returns it: its name, the
SHA-256 of its up and down texts, the files' bodies without their `--|` header, and whether it runs
in a transaction, which that header carries. A released migration never changes: a new migration
adds a row, and a changed row is a breaking change to the schema's version line.
