// Package job runs one job of docinfer: docproc's dispatch of a job's input, then docinfer's
// inference over what it prepares, under the job's context, yielding the job's updates in one
// order.
//
// [Checkpoint]'s and [Done]'s JSON is under docinfer.Version "1", added to and never changed;
// [Update] has no JSON of its own, since each of its fields' types is its own shape.
//
// The package exports:
//
//   - [Config], its Dispatcher, Pipeline, and CheckpointEvery, and [New], which makes a [Runner]
//     of it
//   - [Update], one thing a run yields: an event, an inference, a progress, a batch, a
//     [Checkpoint], or the [Done] that ends it
//   - [Checkpoint], a store's cursor, its After
//   - [Done], one attempt's Parts by outcome, Outputs, and the pipeline's Counts
package job
