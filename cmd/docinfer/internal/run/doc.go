// Package run is docinfer's run command:
// docinfer run <docproc-dir> --out <dir> [--run <id>] [--report].
// Before it reads anything, it checks each capability's model through that capability's own
// client, as its [Config] holds it: one (*ai.Client).Models call per client, all at once and
// bounded at 30 seconds together, so each lists through the same query, token, headers, and
// retry policy the run's requests carry. It prints the table of their states, CAPABILITY,
// ENDPOINT, MODEL, and STATE, a capability per row, vision, transcription, then embeddings, and
// then each warning on a line of its own:
//
//   - loaded, or the status the listing gives the model, its entry's status.value, as llama.cpp's
//     router gives one; a status other than loaded, such as unloaded, is a warning
//   - listed: the listing names the model with no status
//   - unlisted: the listing doesn't name the model, which stops the run
//   - unchecked: the endpoint answered with an *ai.APIError, so it gives no listing, a warning
//   - unreachable: the listing failed any other way, which stops the run
//   - skipped: the configuration leaves the capability unset
//
// The check resolves each capability's served model, the docinfer Served its results are keyed
// under, from the listing's entry for its model: the entry's root, as vLLM lists it, or else the
// capability's served_model in the configuration. A served_model the root disagrees with stops
// the run, and so does a capability with neither, each error naming the capability, the values,
// and the served_model to set. The embeddings' served model is the chunker's Model too.
//
// A stopped check returns its error once the table is printed.
//
// The run then reads docproc's run directory through docproc's rundir: the latest run that ended,
// Dir.Latest's, or the run --run names, which rundir refuses when it isn't one of the directory's
// runs; a --run naming a run that hasn't ended is refused too, matching rundir.ErrNotEnded. It
// opens docinfer's directory at --out with outdir.Create and begins a run there, its manifest
// recording the build, the docproc run and its directory relative to --out, each capability's
// identity, its model as its requests name it, endpoint, and bounds, vision's layer_words, and the
// capabilities skipped.
// It feeds every line of docproc's record to docinfer.Pipeline.Run, each input's depth-0 part line
// with its metadata text, as the job's last delivery, docinfer.Options.Final, so an image, window,
// or chunk a model fails on is recorded failed and the run goes on. Each inference's text goes into
// the directory's texts, each inference into its inferences record, and each batch into its chunk
// record, as the pipeline yields them, over the directory's held results. Each line names the input
// its part was found in by the input's path, as docproc's record gives it: the run passes each
// record line's input to the pipeline as its docinfer.Input's Origin, which keeps two inputs' parts
// of one ID apart, and names each inference and chunk by the Origin the pipeline echoes on it, so a
// part two inputs share is named by each input under its own results, whatever order they arrive
// in.
//
// Its lines come from the pipeline's results alone: a first line naming the run and docproc's,
// a line per docproc input once everything derived from it is out, its path, images, windows,
// chunks, and failures (images refused or failed, windows not transcribed, and chunks refused or
// failed), and, once the run ends, a line per capability with its counts and a last line,
// counting the inputs, the outputs read, the chunks, and the failures. A run that stops, for an
// error or a cancellation, writes its manifest with its end and its error, keeps the records it
// wrote, prints no last line, and returns its error, so the command exits 1.
//
// With --report, a run that ended renders its report once its last line is printed, through
// report.Make, which prints the index's absolute path, as docinfer report does. A run that stops
// renders nothing, and neither does a cancellation, whose context the render checks before it
// writes a page. A render that fails returns its error, naming the docinfer report command that
// renders the pages again, so the command exits 1, the run's manifest and records as it left them.
//
// The command's help ends with its exit status: 0 for a run that ended, refusals and failures
// recorded and counted by the last line; 1 for a run that stopped, never began, or was cancelled,
// or a report that rendered nothing; 2 for a usage error.
//
// The run builds its docinfer.Bounds from each capability's concurrency, 1 for a capability the
// configuration leaves unset, and 1 for extraction, which the command never runs.
package run
