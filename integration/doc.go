// Package integration is docinfer's integration tier: tests behind the integration build tag that
// drive the exported API black-box, through docinfer.New and docinfer.Pipeline.Run, against
// live endpoints. `mise run test:integration` runs them verbose; the hermetic check builds, vets,
// fixes, and lints them under the tag but never runs them, and no CI job runs them, as the models
// are on the organization's own machines. The package exports nothing: it is its tests.
//
// The endpoints are go-ai's tier's roles, read from the same variables, so one environment drives
// both tiers. Each role is an endpoint named by an environment variable, its API root including
// /v1, given to ai.New as it is, with no default, and spoken to in Chat Completions:
//
//   - AI_EMBEDDING_BASE_URL: an embedding model. Every pipeline requires embeddings, so every
//     scenario needs it besides its own role.
//   - AI_VISION_BASE_URL: a model that reads images. TestVision runs on it.
//   - AI_TRANSCRIPTION_BASE_URL: an endpoint that serves the transcription route,
//     audio/transcriptions. TestTranscription/route runs on it in route mode.
//   - AI_AUDIO_BASE_URL: a model that reads audio in a chat request. TestTranscription/chat runs
//     on it in chat mode.
//   - AI_AGENT_BASE_URL: a model with structured output, the role go-ai's tier asks for a strict
//     JSON schema. TestExtraction runs on it.
//
// Each role may also name its credentials, as go-ai's tier reads them: AI_<ROLE>_TOKEN, sent as the
// bearer token through Options.Token, and AI_<ROLE>_HEADER, one or more "Name: value" lines
// separated by newlines, each trimmed and a blank one skipped, sent through Options.Header. With
// neither set, the role's requests carry no credential. A header line with no colon or no name
// fails the scenario naming the variable and the line, and a header ai.New refuses fails it naming
// the role's base URL; no failure prints a token or a header value. A scenario sends to the model
// AI_<ROLE>_MODEL names, or, with it unset, to the one model its endpoint's model list names,
// failing when the list names none or more than one. The reading of the credentials and the model
// is untagged, so the unit tier tests it.
//
// `mise run test:integration` fills an unset AI_<ROLE>_TOKEN from the committed fnox.toml, as
// docs/development.md, "Secrets", says.
//
// A scenario whose variable is unset skips, naming the variable: its own role's first, then
// AI_EMBEDDING_BASE_URL. With none set, the tier skips whole and passes.
//
// Each scenario asserts live behavior, never a model's exact text, then runs the same inputs over
// the same content a second time and wants that run held whole: each inference Held with the
// first run's text and outcome, every chunk held, no progress, and no model asked, no request and
// no token counted. The cache is a docinfer.FileCache in the test's own temporary directory.
//
//   - TestVision: shapes.png, an image output, is described: one inference of outcome described
//     with non-empty text, its text chunked as kind vision.
//   - TestTranscription: phrase.wav, an audio window, the one window of its group, is transcribed
//     in route and in chat mode: one transcript of outcome transcribed with non-empty text and no
//     window failed, chunked as kind transcript.
//   - TestEmbeddings: a text output, a Markdown output, and the input's metadata are chunked as
//     kinds output and metadata, every chunk embedded and none rejected.
//   - TestExtraction: a text output is extracted by a strict schema of two required string
//     members: one inference of outcome extracted whose text is JSON valid against the schema, an
//     object of exactly those members, each a string.
//
// In every scenario, every chunk's vector, read by docinfer.DecodeVector, has the configured
// Dimensions. No variable states them: the tier embeds one probe text first and takes its
// vector's length, as go-ai's TestEmbed takes it, and the pipeline then fails a vector of another
// length, permanent.
//
// docinfer has no default bounds, so the tier sets its own small values: a Bounds of one request
// in flight per capability, Pending 2, EmbedBatch 8, BatchBytes 64 KiB, a Chunker Bound of 400
// tokens, counted by the embeddings client's Tokenize as documents, Join's WordsPerSecond 3,
// vision's MaxTokens 256, and extraction's 1024, room for a reasoning model to reason ahead of its
// answer. Each Run is bounded by five minutes.
//
// integration/testdata/README.md tells where the media came from: go-ai's integration testdata
// at v0.4.0.
package integration
