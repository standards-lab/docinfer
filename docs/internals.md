# Internals

The reasoning behind the root package's unexported machinery, and `job`'s, for a reader changing it.
What `Pipeline.Run` promises its consumer is in its documentation, [`run.go`](../run.go), and the
failure table in [`doc.go`](../doc.go); the join of work in flight has
[its own page](join-of-work-in-flight.md).

## One coordinator

A job is a `run`. Run's own goroutine is its coordinator, the only goroutine that yields: every
other goroutine of the job hands it an event on one channel, and the coordinator alone keeps the
counts, the embedded chunks waiting for a batch, the window groups, the page layers, and the
settlements. The job's context is cancelled when Run stops, which ends every request in flight;
Run waits for the job's goroutines before it returns, and nothing derived from the job is yielded
once its context has ended.

`spawn` recovers a goroutine's panic once its deferred calls, a lease's release among them, have
run; the first panic stops the job, and `repanic` raises it on Run's goroutine, so it reaches Run's
caller rather than ending the process.

## Admission and settlement

The feeder takes a slot, one of `Config.Pending`, before it pulls each input, so the input stream
is never read ahead of the inputs admitted; the coordinator frees the slot once the input is
finished. The coordinator yields an input's echo before `admit` starts its work, so nothing
derived from the input can reach it ahead of the echo. What an event settled is yielded after
what the event yielded of its own, so an input's settlement follows everything derived from it.

An input finishes once its chunks are complete and, when its text is extracted, its extraction is
in. An input's `splitEvent`, its count of chunks, reaches the coordinator before anything of its
extraction, so the input is held open for the extraction. An image's inference reaches the
coordinator before its chunks, so it is yielded before the batches that hold them.

`settleInput` settles an input at once unless `held` names its position: with `Options.Timeline`
set, a video part's own event is entered in `held` as it is admitted, so the work the event needs
itself only marks it ready, and its settlement waits for `releaseTimeline`, which settles each
ready held position once the part's timeline inference is out, or at once when its timeline
sends no call.

## Window groups

A group is one input's part's windows from one stage, keyed by the input's `Origin`, the part's ID,
and the stage: part IDs are unique only within an input, each input's own part `/`, so a run over
several inputs keeps their windows apart by `Origin`. A window the group can't place, its index
already placed, past its bound, or after it closed, is counted `Unplaced` as it is admitted and
holds its slot no longer. A window placed while the bound was unknown, and past the bound the group
joins at, is transcribed but left out of the join: `joinGroup` moves its count from its outcome, and
from `Held`, to `Unplaced`, keeping its request and tokens, so no text is lost uncounted. A window
holds its input's slot only until its own transcript is in, never for its group's later windows, so
a group longer than `Config.Pending` can't stall the inputs it waits for. A joined group's
transcript takes on the slot of the window or plan record that completed it, so its chunks count
against `Pending` as their input's would; a group joined at the end of the inputs holds no slot,
since no input waits on it.

A group's bound is its plan's count, or one past its last window, as far as they are known; a
plan's count decides over a window without `MetaSeamAfter`. At the end of the inputs with neither
known, the group joins up to the highest index placed, and an index not placed by then was never
cut. A group whose plan record's count is still being read ends once it is read, so its windows
past the last one placed are listed too.

A window is sent as `window.wav` or `window.mp3`: the extension is what a chat-mode `Transcribe`
reads the clip's encoding from, and the route's file part's name. An incomplete transcript is held
with its reason, or else its status, or else `incomplete`, so a held incomplete transcript is never
read as a transcribed empty one.

## Page layers

With `Vision.LayerWords` above 0, the coordinator keeps a `layer` for each document-stage page
text it admits, keyed by the input's `Origin` and part ID, then by stage and `dispatch.MetaPage`.
A later text of the same page replaces it, and the page's image takes it out, so a text matches
one image at most. The part's own event, an event with neither `Output` nor `Warning`, drops the
part's layers. An image admitted before its page's text finds no layer and is described.

The text's goroutine counts the layer's words as it reads the text and closes the layer's `done`
channel. The image's goroutine waits on `done`, or on the job's context, then either describes the
image or hands the coordinator a `skipEvent`, which counts the image `Skipped` and settles its
input.

## Document and timeline extraction

Each `splitEvent` carrying an extracted source text adds it to `documentTexts` with its input's
position in pull order. The coordinator loop runs until the inputs have ended and no work is
active; only then does `startDocument` start the document, once, sorting the texts by position,
so the document text waits for every input to settle and is the last work of the job. Its part is
the first depth-0 part `noteRoot` saw, `/` when none.

A timeline is kept per video-stage part, keyed as a window group is, by `Origin`, part ID, and
stage. `admitFrame` places each frame at its `MetaTime` and, with vision set, counts it `pending`
until `frameDescribed` takes its description; a transcript's join hands the timeline its words, cut
at the join's seams, per window. `checkTimeline` starts the timeline once nothing is pending, its
group, if it has one, has been joined, and either its part's own event was admitted or the inputs
have ended; a timeline started is closed, so `timelineOf` gives no timeline to a frame or window
admitted later. A timeline's text is built once, as it starts.

`extractText` is the one path both texts take; a source text's extraction stays one call. A text of
at most `WindowRunes` characters is one call. A longer one is cut by a `chunk.Splitter` whose
count is a text's characters, and its window replies are combined by `combineRuns`, which packs
consecutive replies greedily under `WindowRunes`; a round with no run of two replies sends all of
them as one run, so every round shrinks the replies and the rounds end. Each call goes through
the same `ask` as a source text's extraction, so the claim table, the bounds, the cache, and the
failure table treat it alike, keyed by its own input text's SHA-256. A combine is keyed by
`combineIdentity`, the extraction identity with the `combine` member, so a change to
`combineInstruction` changes `combineRequest` and misses only combines; the golden keys pin
`combineInstruction`'s SHA-256 per combine version.

## Batches

The batch builder keeps the length a batch's JSON would marshal to, without marshalling it: an
element's JSON is the same within the batch as alone, so each addition costs its element's length,
and a comma after the first of its kind.

## Requests

`Bounds.do` is the one runner every capability's model request goes through, so each is bounded the
same way. A request holds a slot from before its call until after it returns, a panic included, and
waits for one only as long as its context lasts.

Each capability's request has a version, the first member of its settings: it changes when the
request's construction changes, so results held under the old construction miss rather than
replay. Settings are encoded with nothing escaped that JSON doesn't require, so a prompt's `<`,
`>`, and `&` read as written, and an extraction's prompt and schema are hashed, so a key's settings
stay small whatever their size. A windowed extraction's combine call adds its own version, `combine`, after
the request's, and `Profile` reports it with the extraction's settings. `checkExtract` validates the extraction request's own construction,
with stand-ins for its model and text, so the rules for an Extract's name and schema are go-ai's
alone.

## The job

`Runner.Run` runs the pipeline under a context of the job's own, cancelled as the job stops, so a
dispatch the pipeline's input side is blocked in ends rather than holds the pipeline's stop; the
job's context is cancelled before the pipeline is stopped, whether by a failed put or by a loop
stopped early. The caller's context is kept beside it to tell a cancellation from the job's own
stop: once the caller's context has ended, its error is the job's, however the stop surfaced. The
pipeline always ends with its counts or an error, so a run without counts yields no `Done`.

The `watermark` tracks, by position in pull order, each echoed input's After (a message's last
output's cursor, or zero) and whether it has settled; an input settles after its echo. Its low
mark is the first input not yet settled. A settlement that moves the low mark past at least
`CheckpointEvery` messages since the last checkpoint makes one checkpoint, at the last of them;
an input with no store passes no message, and makes none.

With `Options.Document` set, `Run` sets `in.After` to zero and never checkpoints: the document text
is every input's text, held only in the run, and a store resumed after a message never pulls the
messages before it, so their text would be missing. A redelivery dispatches the input from its
start, its finished work replayed through the dispatcher's index and the pipeline's cache. A
timeline needs no such rule, since its video part's frames and transcript come before the last
output of the message that holds the part.

`drop` removes the index entry of the part whose content miss stopped the job. The miss's event
is the output's event the pipeline was fed, which names its part, the part's SHA-256, and its
stage: the entry's key. The dispatch has stopped by then, so no claim of the job's records the
entry again. A failed drop is joined to the stop, which both then match.
