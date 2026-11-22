// Package run is docinfer's run command: docinfer run <docproc-dir> --out <dir> [--run <id>].
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
// A stopped check returns its error once the table is printed.
package run
