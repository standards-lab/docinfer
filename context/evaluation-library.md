# Evaluation: the spike's library code

Scope: spike-ai-pipeline at 6a631f1, `internal/docinfer/infer/` (`commands.go`, `execute.go`,
`progress.go`, `sources.go`, `transcription.go`, `vision.go`), `vision/`, `transcribe/`, `embed/`,
`chunk/`, and `outdir/outdir.go`'s `Key`, `Held`/`Hold`, `Span`, and `Seam` only, with their tests
under `app/`, `chunk/`, and `outdir/`; and spike-ai-pipeline-processing at v0.9.0, `audio/join.go`
and `join_test.go`. Checked against `architecture/principles/*.md`, Go Elemental's
`principles/*.md`, docproc's `context/layers.md` (its docinfer sections, the go-ai surface, "docinfer's
packages", "The nested modules", "Requests to go-ai"), go-ai v0.3.0 as released, docproc v0.3.0
(`dispatch`), and `standards-lab/context/data-intelligence.md`. Paths below are relative to
`internal/docinfer/` at 6a631f1 unless they start with `audio/` (processing, v0.9.0), `go-ai:`, or
`docproc:` (each at v0.3.0). Nothing was run; every finding is from reading.

## Findings

### Orchestration

**L1. issue: the run is whole-record phases over in-memory maps, so none of its control flow ports.**
`infer/execute.go:97-112`, `infer/execute.go:232-375`, `infer/sources.go:95-171`.
`occurrences` reads the whole record and every text output into memory, then vision runs over
every input, then transcription, then embeddings, and the chunk lines are built at the end from a
`results` map of every vector. One run-wide context stops everything. Against layers.md "Batches
as made" and "One job, and its cancellation", and data-intelligence.md's asks ("results in batches
as made", "cancellation per job"). Resolution: port the rules, not the flow. The rules worth
carrying: an image or window is asked once per content and settings; a rejected embedding batch is
retried a text at a time; a refused window joins as empty; an incomplete reply keeps its text; a
result is held as it completes, before anything derived from it is yielded.

**L2. simplification: the per-run dedupe maps and the hashing go.**
`infer/vision.go:77-112`, `infer/transcription.go:83-114`, `infer/transcription.go:285-296`,
`infer/execute.go:237-272`. `sums`, `images`, `windows`, `cuts`, and `results` exist to send each
distinct content once within a run, and each file is read to hash it (an image twice: here and at
`vision/vision.go:100`). In docinfer the cache and the join of work in flight give the same
guarantee across jobs, and `dispatch.Output.SHA256` is already filled (`docproc:dispatch/stage.go:231`),
so content is opened only on a miss, as layers.md "The pipeline" (`Input`) states. Resolution: no
set of digests per job; `Counts` by occurrence, as "docinfer's packages" already settles.

**L3. issue: inference builds its own clients, mid-run, from configuration.**
`infer/execute.go:31-36`, `infer/execute.go:293`, `infer/vision.go:119`,
`infer/transcription.go:121`. `job` carries `*config.Config` and `*http.Client`, and each phase
calls `Endpoint.Client` only when it has something pending, so an embeddings client that can't be
built is found after vision and transcription have spent their requests. Against
`composition-root.md` (nothing below the root constructs a dependency), `validation-first.md`, and
`tool-beside-library.md` (the logic lives in the command's package, taking the command's
configuration type). Resolution: layers.md's `Config` of built `*ai.Client`s per capability;
`docinfer.New` panics by name on a nil required field and refuses a bad value, as docproc's
`dispatch.New` does (`docproc:dispatch/dispatch.go:146`).

**L4. issue: the terminal's progress is threaded through the inference.**
`infer/progress.go:1-183`, `infer/vision.go:130`, `infer/transcription.go:135`,
`infer/execute.go:281-290`, `infer/execute.go:321-340`. Each capability's completion callback
writes a terminal line before it holds the result, and `embedTexts` keeps waiter tallies for the
per-input line. Against `tool-beside-library.md`. Resolution: nothing of `progress.go` enters the
root package; `cmd/docinfer` prints from `Result` values (`Inference.Outcome`, `Held`, `TookMS`,
`Batch`), and the embed result's `Last` flag (`embed/embed.go:26-31`), which exists only for the
progress line, goes with it.

**L5. simplification: three copies of one bounded fan-out, each serializing storage under its lock.**
`vision/vision.go:58-96`, `transcribe/transcribe.go:44-82`, `embed/embed.go:44-71`. The semaphore,
wait group, cancel-cause, and `done`-under-mutex are the same code three times, and `done` writes
the text and the held result while holding the mutex (`infer/vision.go:126-153`), which with a
PostgreSQL cache is a network round trip per result, one at a time. Against layers.md "docinfer's
go-ai use" (concurrency moves into `Bounds`; no `Describer`, `Transcriber`, or `Embedder`).
Resolution: one unexported runner over `Bounds`; `Cache.Put` outside any lock; `Run`'s `yield`
called from `Run`'s goroutine alone, and `Run` waits for its requests in flight before it returns,
as the spike's `wg.Wait` does.

**L6. issue: outputs are sent to a model whatever their part came to, and streaming makes that
unconditional.** `infer/sources.go:88-94`. The spike embeds a quarantined or failed part's outputs
by choice. In docinfer the output's event precedes its part's own event (layers.md "One job":
"an output's update precedes anything derived from it"), so the pipeline can't know the outcome
when it sends. Resolution: state it in the root's doc as the contract, a consumer dropping a
quarantined part's chunks by the part's event; or have `job` hold a part's outputs until its own
event, which costs the "as made" order.

### The go-ai mapping

**L7. issue: every `*APIError` is a rejection, and any 4xx is held.**
`infer/execute.go:209-211`, `vision/vision.go:118-119`, `transcribe/transcribe.go:99-100`,
`embed/embed.go:104-109`. A 429 or 401 is held as a verdict on the content, and a 5xx is a hole
the run goes on past. `app/vision_test.go:263-306` and `app/transcription_test.go:249-296` assert
exactly this (400 held, 503 recorded and not fatal). Against layers.md "docinfer's go-ai use" (the
table). Resolution: the table, one test per row; these two tests are rewritten, not ported. With
v0.3.0 the client also retries 502 and 504 and exposes `APIError.RetryAfter`, so the redelivery
row can carry the delay.

**L8. issue: a rejected embedding batch is re-sent a text at a time on any status.**
`embed/embed.go:104-115`. A batch answered 429, 503, or 401 becomes one request per text against
the endpoint that just turned it away, each then retried by go-ai's policy. The only test rejects
with a 400 (`app/embed_test.go:378`). Against layers.md's table. Resolution: split a batch only on
a refused status (400, 413, 415, 422); anything else takes its row.

**L9. issue: failures that aren't an `*APIError` have no row.**
`vision/vision.go:124-131` (a `failed` status stops the run), `transcribe/transcribe.go:101-102`.
The table covers statuses, transport failures, and cancellation. Unplaced: a 2xx reply with
`protocol.StatusFailed`; `*format.UnsupportedError` (refused before sending, such as an effort the
format lacks, `go-ai:format/error.go:14`); `*ai.IncompleteTranscriptError` (chat-mode
transcription, `go-ai:transcribe.go:306`); a reply that can't be decoded. Against layers.md
"docinfer's go-ai use" (`Permanent`). Resolution: add the rows. `UnsupportedError` is permanent;
`IncompleteTranscriptError` is the window's outcome (incomplete, held under its settings, as a
cut vision reply is); a failed status and an undecodable reply take the 500 row.

**L10. issue: a model's refusal is held as an empty description.**
`vision/vision.go:137-155`. `text` reads `OutputText` parts only, so a completed reply whose
message is a `protocol.Refusal` part (`go-ai:format/completions/decode.go:76`) yields "" with
status completed, is counted described, and is held. layers.md "What neither caller uses" rules
out the refusal *events*, not the part. Against layers.md "docinfer's shapes" (`Outcome` refused).
Resolution: a completed reply with a refusal part and no output text is outcome refused, held with
`Held.Refused` the refusal's text; decide the same for extraction, where a cloud fallback makes it
likelier.

**L11. issue: the vector's length is checked after it is held.**
`infer/execute.go:317`, `infer/execute.go:417-429`. `measure` compares dimensions across the whole
run at the end, after every vector is in the cache, so one wrong-length vector fails that run and
every rerun until the file is deleted. Against layers.md "The pipeline" (`Config.Dimensions`:
"a vector of another length fails the job, permanent"). Resolution: check against
`Config.Dimensions` before `Put`, and again on `Get`, where a held vector of another length is the
same permanent error. `measure`'s norms are `cmd/docinfer`'s report, not the library's.

**L12. issue: no bound on a request, and a stalled one now pins shared state.**
`vision/vision.go:114`, `transcribe/transcribe.go:93`, `embed/embed.go:89`; the spike's
`http.Client` has no timeout (`app/infrastructure.go:31-32`). In docinfer a call that never
answers holds a `Bounds` slot shared by every job on the replica, and every waiter joined to it.
Against `timeouts.md` (each bound has one owner) and go-ai's contract (`go-ai:client.go:96`: the
library sets none). Resolution: the root's doc names the owner, the host: the transport's
`ResponseHeaderTimeout` for `Generate`, `Embed`, and `Transcribe`, `Options.StreamIdleTimeout` for
extraction's stream. docinfer adds no default (`baseline-standards.md`).

**L13. issue: transcription is written against a reader, a path, and a client that never retries.**
`transcribe/transcribe.go:87-95`, `transcribe/doc.go:8-9`. v0.3.0 takes `Open`, called per
attempt, and retries `Transcribe`; a content store has no path to take `FileName` from, and in
`TranscribeChat` the name's extension selects the audio format (`go-ai:transcribe.go:89`).
`TranscriptionMode` also changes what `Prompt` and `Language` mean and so the transcript, but it is
set in `ai.Options` and unreadable from the `Client`, so it isn't in `transcriptionSettings`
(`infer/transcription.go:25-27`). Against go-ai v0.3.0 and layers.md "The cache" (`Key`).
Resolution: `Open` is `content.Open(ctx, sha256)`, its error kept by the closure so a miss becomes
`MissingError` whatever go-ai wraps it in; `FileName` is made from `Output.MediaType` (docproc's
windows are `audio/wav`, `docproc:stages/audio.go:122`); `docinfer.Transcription` gains the mode
as a field the host states, and `Identity` includes it.

**L14. simplification: `text` restates a helper go-ai keeps unexported.**
`vision/vision.go:135-155`, `go-ai:transcribe.go:399`. Both walk a response's items and parts for
its output text, and they differ (role filter, the joiner). Extraction needs it a third time.
Against `resolution.md` (docinfer reads a finer resolution than its purpose needs). Resolution:
one unexported helper in the root for vision and extraction; or ask v1.ai.client for an exported
one and drop docinfer's.

**L15. issue: extraction has no spike code and no spike evidence.**
Nearest code: `vision/vision.go:99-133`. `GenerateStream` folded by an `Accumulator`,
`JSONSchemaFormat` through Chat Completions, progress coalesced to one a second, a stream that
breaks part way, and whether a partial extraction is held are all unproven. Against
data-intelligence.md ("extraction by a caller's prompt and schema"). Resolution: the design names
each of those behaviors and its test against `internal/endpointtest`, and the acceptance run
exercises extraction against the live gateway.

### The cache and its key

**L16. simplification: embeddings are keyed by the chunk bound and chunker version, which change
nothing about a vector.** `infer/execute.go:235`, `infer/execute.go:260`;
`app/embed_test.go:313-335` asserts a changed bound re-sends every chunk. A vector is a function
of the model and the chunk's text, and the key already holds the text's SHA-256, so a text within
both bounds (every metadata text) is re-embedded for nothing. layers.md disagrees with itself
here: `Identity`'s comment and `Embeddings.Identity()` say settings none ("docinfer's shapes",
"docinfer's go-ai use"), and the paragraph under the table says the chunker's `Settings`. Against
data-intelligence.md "Caching" (content seen before is replayed). Resolution: key by
`{embeddings, model, ""}` and the chunk's SHA-256; the chunker stays in `Profile` only.

**L17. simplification: three settings encodings, and nothing versions how a request is built.**
`infer/vision.go:27-29` (unquoted, relies on field order), `infer/transcription.go:25-27`
(quoted), `infer/execute.go:235` (space-separated). layers.md keeps "the settings strings the
spike's", but the cache starts empty ("The cache"), so nothing depends on them. And only the
chunker has a `Version`: moving the vision prompt into `Instructions`, or reordering the image and
the prompt (`vision/vision.go:104-112`), changes every result and no key. Resolution: one encoding
for every capability, the length-prefixed fields `Key` already uses or a struct's JSON; a
per-capability request version in the settings, changed when the request's construction changes.

**L18. issue: a held value that doesn't decode fails every run, and `Held` can't say what version it is.**
`outdir/outdir.go:90-102`. The spike makes an undecodable file an error, not a miss, by choice.
layers.md's `Held` is "versioned by Version" but carries no version, and `Key` includes none, so a
value of another shape and a corrupt one look alike. Against layers.md "The cache" ("a miss costs
one model call"). Resolution: decide and state it: either `Version` joins `Key`'s hash, so a new
shape misses, or a value that doesn't decode is a miss that the next `Put` replaces. Also decide
how a held refusal is dropped: `Cache` has `Get` and `Put` only.

### Transcription and the join

**L19. issue: a part's window count is read from a Meta key docproc v0.3.0 never sets, and nothing
tells a streaming pipeline the windows are all in.** `infer/sources.go:123`,
`infer/sources.go:176-187`, `infer/transcription.go:193-206`. The spike takes `count` from the
window's meta, else the highest index seen, over the whole record. docproc v0.3.0: "No window
carries it ... its plan record, last, counts them" (`docproc:dispatch/shapes.go:80-82`), and the
spike drops record outputs. So a trailing window the stage failed to cut is invisible, and
layers.md's `Input` ("never records"; the part's own event only as depth 0's metadata) gives `Run`
no end-of-windows signal for a nested part. Against docproc v0.3.0 and layers.md "The pipeline".
Resolution: the design picks the signal: the plan record output (`MediaAudioPlan`,
`MediaVideoPlan`), admitted as an input for its count alone, or every part's own event passed to
`Run`. Keep the spike's rule that the first event of an index wins, since events repeat.

**L20. issue: the seam bound is fixed at 16 words on the claim that the windows say nothing.**
`infer/transcription.go:254-256`, `audio/join.go:12-15`. v0.3.0 windows carry `MetaSeamBefore` and
`MetaSeamAfter` as spans in seconds, and the plan record its overlap, and `join.go` gives the rule
(about 8 words a second). A stage configured with a longer overlap is under-searched silently.
Against layers.md "The pipeline" (`join.Request` "states docinfer's own seam-word setting").
Resolution: derive the bound per seam from the seam's span, with `Config.Join` as the host's
words-per-second or its override; or keep one fixed setting and say the overlap it assumes.

**L21. simplification: `partKey`, `windowGroup`, and the outcomes pass are `Part.ID` and the stage's
`Identity`.** `infer/sources.go:56-86`, `infer/sources.go:98-113`. Six fields rebuild an identity
docproc v0.3.0 states (`dispatch.Part.ID`), and the first pass over the record exists to put each
part's outcome on its chunks, which layers.md removes ("outdir.Part's outcome leaves in the part's
own event"). Resolution: group windows by `{Part.ID, Event.Stage}`; read kinds and Meta by
`dispatch`'s constants, not literals (`infer/sources.go:17-24`, `:178`, `:183`).

**L22. simplification: the join moves with three things it doesn't need.**
`audio/join.go:1` (`//go:build linux` on pure string code; docproc's STANDARDS.md keeps the tag on
the exec runner alone), `audio/join.go:93-96` (`audio.ErrRequest`, another package's error, for a
bound that can be checked once), `infer/transcription.go:269-270` with `outdir/outdir.go:433-443`
(a field-by-field copy between two identical `Seam` types). Against `validation-first.md` and
layers.md "The pipeline" (`join`). Resolution: no build tag; `join.Request` validated at
`docinfer.New`, `join` with its own `ErrRequest`; one `join.Seam` with the JSON tags.
`TestJoin`'s table and the settled-prefix tests move as they are.

### Chunking and shapes

**L23. simplification: `Splitter` beside `Split`, and `Span` beside `Start`/`End`.**
`chunk/chunk.go:14-18`, `chunk/chunk.go:31-36`, `infer/execute.go:362`. layers.md keeps the free
`Split(text, bound)` and adds `Splitter{Bound}.Split(text)`, two spellings of one call; with L16
`Splitter.Settings` feeds `Profile.Chunker` only. Against `baseline-standards.md` ("A type is
introduced only when it is needed"). Resolution: one of them. `Splitter` if `Settings` stays;
otherwise `Split` and a `Settings(bound)` function, with `Config.Chunker` an int. The `Span` move
is right and removes the copy at `execute.go:362`.

**L24. issue: invalid bounds are coerced, and the design adds library defaults.**
`vision/vision.go:66`, `transcribe/transcribe.go:52`, `embed/embed.go:48-50`, `chunk/chunk.go:32`
(asserted by `chunk/chunk_test.go:171-176`), `audio/join.go:93`. A concurrency, batch, or bound
below 1 silently becomes 1, and a zero `SeamWords` becomes 16. layers.md adds `BatchBytes`
"512 KiB by default" and a seam-word default. Against `validation-first.md` and
`baseline-standards.md` as docproc's STANDARDS.md reads it ("the library ships no policy ... no
library default, and `cmd/docproc` ships the operator's starting values"). Resolution:
`docinfer.New` and `NewBounds` refuse a value below 1, matching one `ErrRequest`; the design either
follows docproc (no default; `cmd/docinfer` ships the starting values) or states each default as an
exported constant and why it differs.

**L25. simplification: `mediaType`'s fallbacks are dead against docproc v0.3.0.**
`infer/vision.go:217-231`. Every image output carries `MediaType` (`docproc:stages/document.go:284`,
`picture.go:84`, `video.go:176`), and content opened by SHA-256 has no extension to read.
Resolution: use `Output.MediaType`; an image output without one is the input's defect, refused.

### Tests

**L26. issue: tests that can't fail.**
`app/embed_test.go:371`, `app/vision_test.go:525`, `app/transcription_test.go:435` assert only
`most > 2`, which a serial implementation passes, on a 20 ms sleep.
`chunk/chunk_test.go:164-166` and `audio/join_test.go:161-178` call a pure function twice in one
process and compare. Against `tests-and-docs.md` ("A test proves a behavior only if it can fail").
Resolution: `Bounds`' test holds requests at the fake endpoint until the bound is reached and
asserts it is reached and never passed, with no sleep; drop the two determinism tests.

**L27. issue: nothing pins the formats the cache depends on.**
`outdir/outdir_test.go:88-105` proves keys differ, not what a key is; no test ties
`chunk.Version` "1" to its output (`chunk/chunk.go:9-11`). A change to `Key`'s bytes or to the
splitting passes every test and orphans, or silently mixes, every held result. Against
`tests-and-docs.md` and layers.md "The cache". Resolution: a golden key per capability, and a
golden corpus of chunks named for `Version`, each failing with "change the version".

## Join of work in flight

**What the spike does.** Nothing: it has no join. One process runs one run, each capability a
phase over every input, and it removes duplicates before it sends, through per-run maps by content
SHA-256 (`infer/vision.go:77-112`, `infer/transcription.go:83-114`, `infer/execute.go:237-272`).
Two processes over one `--out` would each send, and the later `Hold` replaces the earlier
(`outdir/outdir.go:106-111`). layers.md lists the join as new ("docinfer's packages").

**What the rule needs.** A waiter gets the leader's result; a waiter whose leader ends without a
result (its context cancelled, or a failure that isn't final) asks the model itself; and by "One
job, and its cancellation" a cancelled waiter must stop without waiting out the leader's request
(vision ran to 38.9 s an image under load).

**Whether singleflight can meet it.** Yes, with code around it (`golang.org/x/sync` v0.25.0,
`singleflight/singleflight.go`):

- `Do(key, fn)` gives every caller the leader's `(v, err)`. A leader whose context is cancelled
  returns its `context.Canceled` to every waiter; nothing retries. The waiter must test its own
  `ctx.Err()`, never `errors.Is(err, context.Canceled)`, and must know it wasn't the leader, which
  `shared` doesn't say (it is true for the leader too): a flag the closure sets.
- The error path meets the rule without `Forget`: `doCall` deletes the key under the group's lock
  as it releases the waiters, so a waiter that sees a non-final error and calls again starts a
  new flight or joins one, and only one of N waiters becomes the new leader. `Forget` is for
  starting a second flight while the first still runs, which the rule doesn't ask for.
- `Do` blocks until the leader returns, so a cancelled waiter is stuck. `DoChan` returns a
  channel the waiter selects on beside its own `ctx.Done()`; it runs `fn` on a new goroutine for
  the leader too.
- The flight's value must be job-independent: the raw result or error. `Options.Final` turning a
  500 into outcome `failed` is the leader's job's reading and is applied by each caller outside
  the flight.
- Values are `any` (no generic form), and a leader's panic is re-raised in the waiters, with
  `DoChan` as `go panic`, which nothing recovers.
- It is one key, one function. `Embed` answers a batch of keys in one request: a job must learn
  which of its chunks are already in flight before it assembles its batch, and singleflight has
  no claim that doesn't block. Embeddings need either a batcher behind a per-key `DoChan` (a
  goroutine a chunk, parked on the batch) or their own table anyway.

**Own code.** A mutex, a `map[string]*call`, and a done channel per call: `claim(keys)` returns
which are the caller's and which are waited for, the wait selects on the caller's context, and a
call that ends without a result wakes its waiters to claim again. About sixty lines, typed, one
mechanism for single keys and batches. It must get right what singleflight has hardened: release
in a `defer` so a panicking leader doesn't strand waiters, delete the key before waking them, and
leave no goroutine behind. Those are unit tests under `-race`: leader cancelled, leader's
transient failure, waiter cancelled, leader panics, a batch partly in flight.

**Dependency line.** Go Elemental's `dependencies.md` admits `golang.org/x/...` for an adjacent
library's base ("packages as stable as it"), and its Sourcing rule says in-house for a capability
with no specification or threat model whose failure shows in a unit test. docinfer's STANDARDS.md
now reads "the base module requires docproc and go-ai only"; go-ai requires nothing and docproc
`golang.org/x/text` alone, so `x/sync` would be a new module in the graph and that line would be
restated.

**Either way, settle:** the leader `Put`s before it releases, so a waiter's result equals a cache
read and counts as `Held`, its tokens the leader's job's alone; a waiter holds no `Bounds` slot;
a refusal is a result and is shared, and a permanent error is final and is shared.

**Recommendation.** Own code. The embedding batch decides it: singleflight covers vision,
transcription, and extraction with a wrapper nearly as long as the table, and still leaves
embeddings needing a second mechanism. singleflight is the better choice only if the design joins
single-key capabilities alone and lets concurrent jobs embed the same chunk twice.

## Names

go-ai, as layers.md and the spike have it, against v0.3.0:

| spike / layers.md | go-ai v0.3.0 |
| --- | --- |
| module `github.com/JaimeStill/spike-ai-pipeline-client`, package `client`; "go-ai's `client` and `protocol`" | `github.com/standards-lab/go-ai`, package `ai`, the module root; the root and its `protocol` |
| `client.Client`; `Client *client.Client` in `docinfer.Vision`, `Transcription`, `Extraction`, `Embeddings` | `ai.Client`; `*ai.Client` |
| `client.New(client.Config{BaseURL, Query, Token, HTTP, Format, Retry})` | `ai.New(baseURL, httpClient, wireFormat, ai.Options{Query, Token, Header, Retry, StreamIdleTimeout, TranscriptionMode})`; a nil `httpClient` or `wireFormat` panics |
| `Config.Retry *RetryPolicy`, nil retries nothing | `Options.Retry RetryPolicy`, a value; its zero retries nothing |
| retry on 408, 429, 503 | 408, 429, 502, 503, 504 |
| `completions.New()` (and `messages`, `responses`) | `completions.New(completions.Options{})`, each format with its `Options` |
| `client.APIError{Status, Message, Type, Code, Body}`; "can't read the reply's Retry-After" | `ai.APIError`, plus `RetryAfter time.Duration` |
| `TranscriptionRequest.File io.Reader` | `TranscriptionRequest.Open func(ctx) (io.ReadCloser, error)`, called per attempt; plus `Native url.Values` |
| "`Transcribe` is never retried" (layers.md; `transcribe/doc.go:8`) | retried through the one loop, the clip reopened |
| transcription always the OpenAI-compatible route | `Options.TranscriptionMode`: `TranscribeRoute` (zero) or `TranscribeChat`; `ai.TranscriptionInstruction`; `*ai.IncompleteTranscriptError` |
| the spike's own listing, a raw GET (`models/models.go:121-130`); "go-ai's `/models` listing ... outside both" | `(*ai.Client).Models(ctx) ([]ai.Model, error)`, `Model{ID, Native}` |
| a consumer key "in a header other than the bearer token, only if the gateway asks" (a request) | `Options.Header http.Header` |
| `MinMaxOutputTokens` shown under `protocol.Request` | `responses.MinMaxOutputTokens`, the Open Responses format's alone |
| `protocol.ReasoningEffort` of five efforts | seven: `minimal` and `max` added; a format refuses one its spec lacks with `*format.UnsupportedError` |
| `client.EmbeddingRequest`, `Embeddings`, `EmbeddingUsage`, `Transcription`, `TranscriptionUsage`, `TokenSource` | the same names under `ai`; `EmbeddingRequest` gains `Native` |
| `protocol.Request`, `Message`, `InputImage`, `InputText`, `OutputText`, `DataURL`, `ReasoningParam`, `Usage`, `Status*`, `IncompleteDetails`, `JSONSchemaFormat`, `Accumulator` | unchanged; `Response` gains `ID` and `Model`, `Usage` gains `InputTokensDetails`, `ReasonContextWindow` is new |

docproc and processing names in the same surface, against docproc v0.3.0:

| spike | docproc v0.3.0 / docinfer |
| --- | --- |
| `internal/docproc/schema` `Event`, `Part`, `File`, `ContentType`, `Stage`; `source.Run` | `dispatch.Event`, `dispatch.Part` (with `ID`), `dispatch.ContentType`, `dispatch.Identity`; no `File` in the library |
| `outdir.Output{Kind, Name, MediaType, Ref, Meta}` | `dispatch.Output`, by `SHA256`, no `Ref` |
| `"text"`, `"markdown"`, `"image"`, `"audio"` (`infer/sources.go:17-24`) | `dispatch.TextOutput`, `MarkdownOutput`, `ImageOutput`, `AudioOutput` |
| `meta["window"]`, `meta["count"]` | `dispatch.MetaWindow`; no window carries `MetaCount` |
| `audio.Join`, `JoinRequest`, `Seam`, `Transcript`, `DefaultSeamWords`, `ErrRequest` (processing) | `join.Join`, `Request`, `Seam`, `Transcript` in docinfer; docproc v0.3.0's `audio` has no join |
| `outdir.Key(capability, model, settings, content)`, `Dir.Held`, `Dir.Hold` | `docinfer.Key(Identity, content)`, `Cache.Get`, `Cache.Put`, `Held` |
| `outdir.Span`, `outdir.Seam`, `outdir.Line`, `outdir.Source` | `chunk.Span`, `join.Seam`, `docinfer.Chunk`, `docinfer.Source` |
