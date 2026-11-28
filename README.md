# docinfer

docinfer infers over what docproc extracts. `github.com/standards-lab/docinfer` is its Go base
module, the inference library above go-ai's tier: over the parts docproc's dispatcher prepares, it
describes images, transcribes audio windows, extracts by a caller's prompt and JSON schema, and
chunks and embeds text. It yields each result in batches as it is made, and holds every model
result behind a cache, so content seen before is replayed rather than asked again.

The base module requires docproc and go-ai alone, at the versions `go.mod` names, and calls go-ai
directly. Only the layers above go-ai's tier use it.

## Install

```sh
go get github.com/standards-lab/docinfer
```

The module's releases are tagged `vX.Y.Z`, and [`CHANGELOG.md`](CHANGELOG.md) states what each
changes. Each package's documentation, `go doc github.com/standards-lab/docinfer/...`, is the
reference for its exported names, and its doc comment lists them.

The PostgreSQL cache is a nested module, released on its own `postgres/vX.Y.Z` tags with its own
[`CHANGELOG.md`](postgres/CHANGELOG.md):

```sh
go get github.com/standards-lab/docinfer/postgres
```

## Usage

A host builds a go-ai client per endpoint, one `Bounds` shared by every pipeline, a cache, and a
`Pipeline`, then runs each job's inputs: the output events docproc's dispatcher yields, and the
input's metadata text on the input's own event.

```go
client, err := ai.New("http://localhost:8000/v1", http.DefaultClient,
	completions.New(completions.Options{}), ai.Options{})
if err != nil {
	return err
}
// The requests in flight per capability: vision, transcription, extraction, embeddings.
bounds, err := docinfer.NewBounds(4, 4, 2, 8)
if err != nil {
	return err
}
cache, err := docinfer.NewFileCache("/var/cache/docinfer")
if err != nil {
	return err
}
p, err := docinfer.New(docinfer.Config{
	// Served, the model the endpoint serves under Model, keys the cache.
	Vision:     &docinfer.Vision{Client: client, Model: "vision", Served: "vision-model", Prompt: "Describe the image.", MaxTokens: 1024},
	Embeddings: docinfer.Embeddings{Client: client, Model: "embedding", Served: "embedding-model"},
	// count is a chunk.Counter, such as over go-ai's Tokenize.
	Chunker:    chunk.Splitter{Bound: 480, Runes: 8000, Count: count, Model: "embedding-model"},
	Dimensions: 1024,
	Cache:      cache,
	Bounds:     bounds,
	Pending:    16,
	EmbedBatch: 32,
	BatchBytes: 512 << 10,
	Join:       join.Request{WordsPerSecond: 3},
})
if err != nil {
	return err
}

// d is a docproc dispatch.Dispatcher, in one of its inputs.
inputs := func(yield func(docinfer.Input, error) bool) {
	for ev, err := range d.Dispatch(ctx, in) {
		switch {
		case err != nil:
			yield(docinfer.Input{}, err)
			return
		case ev.Output != nil:
			if !yield(docinfer.Input{Event: ev}, nil) {
				return
			}
		case ev.Warning == nil && ev.Part.Depth == 0:
			if !yield(docinfer.Input{Event: ev, Metadata: docinfer.MetadataText(ev.Part)}, nil) {
				return
			}
		}
	}
}

for r, err := range p.Run(ctx, d.Content(), inputs, docinfer.Options{Final: true}) {
	if err != nil {
		return err // docinfer.Permanent(err) says whether a redelivery could change it
	}
	switch {
	case r.Inference != nil: // a description or transcript, already held
	case r.Batch != nil: // embedded chunks, ready to index
	case r.Done != nil: // the job's counts, last
	}
}
```

A nil `Vision` or `Transcription` skips images or windows, counted; only `Options.Extract`,
`Document`, and `Timeline` need `Extraction`.

## Running a job

Package `job` runs one job end to end: docproc's dispatch of the job's input, then the pipeline's
inference over what it prepares. A `Runner` feeds every dispatch event to the pipeline, puts each
inference's text into the dispatcher's content store, and yields the job's updates in one order:
an output's event before its inference and batches, and a part's own event after its outputs',
warnings', and children's. For a store's input it yields a `Checkpoint` at the low watermark,
every `CheckpointEvery` settled messages; a host keeps the last one's `After` and resumes a
redelivered job from it, none with `Options.Document`.

```go
// d is a docproc dispatch.Dispatcher and p the Pipeline above; in is the job's dispatch.Input,
// cursor its kept checkpoint, and final whether this is its last delivery.
runner, err := job.New(job.Config{Dispatcher: d, Pipeline: p, CheckpointEvery: 100})
if err != nil {
	return err
}

in.After = cursor // the last checkpoint's After kept for this input, or 0 to start
for u, err := range runner.Run(ctx, in, docinfer.Options{Final: final}) {
	if err != nil {
		return err // docinfer.Permanent(err) says whether a redelivery could change it
	}
	switch {
	case u.Event != nil: // a part's own, an output's, or a warning's event
	case u.Inference != nil: // a description, transcript, or extraction, its text already in the content store
	case u.Batch != nil: // embedded chunks, ready to index
	case u.Checkpoint != nil:
		cursor = u.Checkpoint.After // keep it: a redelivery resumes after it
	case u.Done != nil: // the attempt's parts by outcome, outputs, and counts, last
	}
}
```

A content miss stops the job, but drops its part's index entry first, so a redelivery prepares
the part again rather than miss again.

## Packages

| Package | What it does |
| --- | --- |
| `docinfer` (the root) | The pipeline over go-ai's client: `Config`, `New`, and `Pipeline.Run`; `Bounds`; the `Cache`, its `Key` and `Held`, and `FileCache` and `MemoryCache`; the go-ai failure table and `Permanent`; and the shapes of its results. |
| `job` | One job's dispatch and inference: `Config`, `New`, and `Runner.Run`, yielding `Update`s, `Checkpoint`s, and the `Done` that ends them. |
| `docinfertest` | The `Cache` conformance suite every implementation runs, the module's and a provider's. |
| `chunk` | The token-measured chunker: `Splitter`, its `Counter`, `Chunk`, `Span`, and `Version`. |
| `join` | A part's window transcripts joined at their seams. |
| `integration` | The integration tier, behind the `integration` build tag. |

## Providers

A provider is a nested module that implements `Cache` over a database. Each has its own tags and
README, which states its use, the host's responsibilities, and its tests:

| Module | What it is | Tags |
| --- | --- | --- |
| [`github.com/standards-lab/docinfer/postgres`](postgres/README.md) | `postgres.New(ctx, db)`: a `docinfer.Cache` in PostgreSQL over sqlate, one row per key, a batch's `Get` one query, its `Migrations` the host applies, and `Verify`. | `postgres/vX.Y.Z` |

It requires the base module, and the base module doesn't require it. It passes `docinfertest`'s
suite.

## No defaults

No bound, cap, or setting has a library default. `New` refuses, matching `ErrRequest`, a value
below 1: the chunker's `Bound` and `Runes`, the dimensions, `Pending`, `EmbedBatch`, `BatchBytes`,
`Join.WordsPerSecond`, and each `MaxTokens` but transcription's, which 0 leaves unsent. It refuses
an empty model or served model and a nil `Chunker.Count`; `NewBounds` refuses any of its four
bounds below 1. A request's time is the host's too: its transport's `ResponseHeaderTimeout`,
and go-ai's `Options.StreamIdleTimeout` for extraction's stream. docinfer adds no deadline.
[`docs/no-defaults.md`](docs/no-defaults.md) holds the reasoning.

## The consumer's contract

`Run` yields a part's results as they are made, which may be before the part's own event, whatever
that event comes to. A consumer drops a quarantined or failed part's inferences and chunks by that
part's own event.

## Development

Tasks run through [mise](https://mise.jdx.dev), from the repository root, after `mise install`;
[`docs/development.md`](docs/development.md) holds the reasoning behind the tasks, CI, and the
harness:

```sh
mise run check                 # build, vet, format, fix, tidy, test, and lint every module, sqlint the postgres module's SQL, then run the lints; writes nothing
mise run currency              # report requirements, Go, tools, action pins, and compose image pins behind their latest
mise run upgrade               # upgrade every module's go directive and requirements, and the tools, to their latest
mise run test:integration      # run the integration tiers against the AI_*_BASE_URL endpoints
mise run integration           # run the providers' live tiers: integration:postgres
mise run integration:postgres  # run the postgres module's live tier against the compose PostgreSQL
```

The unit tests are hermetic: they drive the pipeline's go-ai clients against go-ai's `aitest`
fake endpoint, and the postgres module's cache against sqlate's scripted driver.

`test:integration` runs the base module's and `cmd/docinfer`'s integration tiers against the live
endpoints that go-ai's tier's `AI_<ROLE>_*` variables name, listed in
[`docs/development.md`](docs/development.md#mise); a scenario whose variable is unset skips, naming
it. The check builds the tiers but never runs them, nor does CI, because the models are on the
organization's own machines.

`integration` runs every provider's live tier, continuing past a failure; `integration:postgres`
is the one provider tier. `integration:postgres` starts the compose harness's PostgreSQL alone,
runs the postgres module's integration tests, `docinfertest`'s `Cache` suite among them, and stops
the service whether they pass or fail, printing its log on a failure. CI's integration job runs
`integration` on a push to main and on demand, never on a pull request.

The compose harness, [`compose.yml`](compose.yml), is test-only: an empty PostgreSQL on port 5437,
which `DOCINFER_POSTGRES_PORT` moves.

## Documentation

- [`docs/development.md`](docs/development.md): the reasoning behind the tasks, CI, the release
  workflow, the compose harness, the lint configuration, and the tests.
- [`docs/internals.md`](docs/internals.md): the reasoning behind the root package's and `job`'s
  unexported machinery.
- [`docs/join-of-work-in-flight.md`](docs/join-of-work-in-flight.md): why the pipeline joins work
  in flight with its own claim table.
- [`docs/no-defaults.md`](docs/no-defaults.md): why no bound, cap, setting, or request time has a
  library default.
- [`docs/library-evaluation.md`](docs/library-evaluation.md): the reasoning behind the review the
  library was built from.
- [`docs/acceptance.md`](docs/acceptance.md): the evidence from the live rows.
- [`acceptance/README.md`](acceptance/README.md): the recipes behind that evidence.

## License

[Apache License 2.0](LICENSE).
