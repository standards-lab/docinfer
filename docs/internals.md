# Internals

The reasoning behind the root package's unexported machinery, for a reader changing it. What
`Pipeline.Run` promises its consumer is in its documentation, [`run.go`](../run.go), and the
failure table in [`doc.go`](../doc.go); the join of work in flight has
[its own page](join-of-work-in-flight.md).

## One coordinator

A job is a `run`. Run's own goroutine is its coordinator, the only goroutine that yields: every
other goroutine of the job hands it an event on one channel, and the coordinator alone keeps the
counts, the embedded chunks waiting for a batch, the window groups, and the settlements. The
job's context is cancelled when Run stops, which ends every request in flight; Run waits for the
job's goroutines before it returns, and nothing derived from the job is yielded once its context
has ended.

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

## Window groups

A group is one part's windows from one stage. A window holds its input's slot only until its own
transcript is in, never for its group's later windows, so a group longer than `Config.Pending`
can't stall the inputs it waits for. A joined group's transcript takes on the slot of the window
or plan record that completed it, so its chunks count against `Pending` as their input's would; a
group joined at the end of the inputs holds no slot, since no input waits on it.

A group's bound is its plan's count, or one past its last window, as far as they are known; a
plan's count decides over a window without `MetaSeamAfter`. At the end of the inputs with neither
known, the group joins up to the highest index placed, and an index not placed by then was never
cut. A group whose plan record's count is still being read ends once it is read, so its windows
past the last one placed are listed too.

A window is sent as `window.wav` or `window.mp3`: the extension is what a chat-mode `Transcribe`
reads the clip's encoding from, and the route's file part's name. An incomplete transcript is held
with its reason, or else its status, or else `incomplete`, so a held incomplete transcript is never
read as a transcribed empty one.

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
stay small whatever their size. `checkExtract` validates the extraction request's own construction,
with stand-ins for its model and text, so the rules for an Extract's name and schema are go-ai's
alone.
