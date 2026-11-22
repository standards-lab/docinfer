// Package integration is docinfer's built-binary integration tier: the harness that runs the
// command as the built binary, one shot at a time, and, under the integration build tag, the
// suite that asserts its behavior through the interfaces a terminal uses: its arguments, its two
// output streams, and its exit code. It was started from go-cli-sdk-template's template/v0.3.1
// integration package and is built on go-core's processtest; nothing in the runtime exists for
// the tests' sake.
//
//   - [Main] is the suite's TestMain: it clears the DOCINFER_ environment and builds the command
//     once per run.
//   - [Run] runs the built program once with its arguments and returns its processtest.Result.
//
// The tier runs from `mise run test:integration`. The harness is untagged, so the unit tier
// type-checks it on every check; the suite files carry the integration tag.
package integration
