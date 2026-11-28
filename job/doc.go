// Package job runs one job of docinfer: docproc's dispatch of a job's input, then docinfer's
// inference over what it prepares, under the job's context, yielding the job's updates in one
// order.
//
// A [Runner], made by [New] from a [Config], runs many jobs at once, each by [Runner.Run]: every
// dispatch event is fed to the pipeline as one input, the depth-0 part's own event with its
// metadata text, and each inference's text is put into the dispatcher's content store before its
// update. A job's ID is its host's: Run takes the job's input alone.
//
// A job has one content store, the dispatcher's: the dispatcher stores each output's content there
// before its event, the pipeline opens its inputs' content from it, and Run puts each inference's
// text into it. job keeps no output's file past its event, and no quarantined part's file. A
// process that dies leaves its files in the dispatcher's working directory, so a host empties that
// directory when it starts, and its work queue redelivers the job.
//
// The order of updates: each event comes as the pipeline echoes it, before anything derived from
// it, and each inference, progress, and batch as made. So an output's event precedes its
// inference and the batches holding its chunks, and a part's own event follows its outputs',
// warnings', and children's. No output is held back until its part's event: a consumer drops a
// quarantined or failed part's results by that part's own event. [Done] comes last, its parts
// counted once per part ID, by each part's last own event.
//
// The checkpoint watermark: a store's input yields [Checkpoint]s at its low watermark. A message
// is the stretch of inputs, in pull order, that ends at an output event with a non-zero After,
// and it is settled once everything derived from its inputs is out. The watermark is the last
// message whose inputs, and every earlier input, have settled; once it has passed at least
// Config.CheckpointEvery messages since the last checkpoint, or since the start, Run yields a
// Checkpoint of its message's After, never the highest After seen. A host keeps the last
// Checkpoint.After and resumes a redelivered job by setting dispatch.Input.After to it. An input
// with no store yields none, and a finished job none at its end. job threads no mail: a consumer
// threads the record outputs with docproc's email.Threader, so no threader state joins the
// checkpoint.
//
// A run with docinfer.Options.Document set yields no Checkpoint and ignores dispatch.Input.After,
// dispatching the input from its start: a store resumed after a message never pulls the messages
// before it again, so their text would be missing from the document text. Its redelivery replays
// the job from the start, through the dispatcher's index and the pipeline's cache. A timeline
// changes nothing: a video part's frames and transcript come before the last output of the message
// that holds it, and its timeline before the part's own event settles, so no checkpoint falls
// inside one.
//
// A content miss is transient: a *docinfer.MissingError stops the job, but first drops the index
// entry of the output's part, under the part's SHA-256 and the event's stage, so a redelivery
// prepares the part again and puts its content back rather than replay the entry and miss again.
// Every other error stops the job too, yielded alone and last, keeping what docinfer.Permanent
// reads of it. A cancelled context yields its error alone, last, in place of any other, a content
// miss's included, which then drops nothing; a loop stopped early stops the job the same way,
// yielding nothing. Either ends once the dispatch has removed the job's files, and other jobs on
// the Runner go on.
//
// [Checkpoint]'s and [Done]'s JSON is under docinfer.Version "1", added to and never changed;
// [Update] has no JSON of its own, since each of its fields' types is its own shape.
//
// The package exports:
//
//   - [Config], its Dispatcher, Pipeline, and CheckpointEvery, and [New], which makes a [Runner]
//     of it
//   - [Runner] and [Runner.Run], one job's run of a dispatch.Input under docinfer.Options
//   - [Update], one thing a run yields: an event, an inference, a progress, a batch, a
//     [Checkpoint], or the [Done] that ends it
//   - [Checkpoint], a store's cursor, its After
//   - [Done], one attempt's Parts by outcome, Outputs, and the pipeline's Counts
package job
