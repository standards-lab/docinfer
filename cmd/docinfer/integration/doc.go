// Package integration is docinfer's built-binary integration tier: the harness that runs the
// command as the built binary, one shot at a time, and, under the integration build tag, the
// suite that asserts its behavior through the interfaces a terminal uses: its arguments, its two
// output streams, and its exit code. It is built on go-core's processtest; nothing in the runtime
// exists for the tests' sake.
//
//   - [Main] is the suite's TestMain: it clears the DOCINFER_ environment and builds the command
//     once per run.
//   - [Run] runs the built program once with its arguments and returns its processtest.Result.
//
// The tier runs from `mise run test:integration`. The harness is untagged, so the unit tier
// type-checks it on every check; the suite files carry the integration tag.
//
// TestCLI_RunAndReport runs docinfer run --report and then docinfer report against live endpoints,
// read from go-ai's tier's variables as the base module's integration tier reads them:
// AI_EMBEDDING_BASE_URL, which the run needs and without which the scenario skips, naming it;
// AI_VISION_BASE_URL; and AI_TRANSCRIPTION_BASE_URL, transcribed in route mode, or else
// AI_AUDIO_BASE_URL, in chat mode. A capability whose variable is unset is left out of the run.
// Each role's AI_<ROLE>_MODEL names its model, or its endpoint's one listed model is taken, and
// AI_<ROLE>_TOKEN and AI_<ROLE>_HEADER, "Name: value" lines, its credentials. The suite writes
// them, with the embedding space's dimensions, which it measures by embedding one probe text, into
// config.json and secrets.json in a temporary configuration directory that --config names, each
// role an endpoint of the completions format named for it, so no credential appears on the logged
// command line. A role's served_model is written only when its endpoint's model list gives the
// model no root, its model then, so the run takes a root itself. The embeddings section declares no
// input-type convention, input_types {}, and counts tokens through the endpoint
// AI_TOKENIZE_BASE_URL names, the root an embedding endpoint serves tokenize at, such as
// http://localhost:8000, with AI_TOKENIZE_TOKEN and AI_TOKENIZE_HEADER as its credentials, or, with
// it unset, through the embedding endpoint itself, as a gateway forwards it. The docproc run
// directory the suite writes holds a short text input, and an image, the base module's
// integration/testdata/shapes.png, for vision, with a scanned PDF page, a document-stage text
// layer of over 100 words and its page image, and an audio window, its phrase.wav, for
// transcription. The suite wants run to exit 0 with its run's manifest ended with no error and
// naming each capability configured, vision's line counting the page image skipped at
// layer_words' starting value, and the report's index, whose path the run prints last; then report
// over the same directory to exit 0 and print the same path. Each run is bounded by processtest's
// Failsafe.
package integration
