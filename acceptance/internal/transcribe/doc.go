// Package transcribe is transcribe-eval's measurement: docinfer's chat-mode transcription, at one
// Transcription.MaxTokens, over a seeded draw of a docproc run's audio inputs, each drawn input's
// windows and audio plan records fed as cmd/docinfer feeds them, recording per window what the
// model replied. The draw is the run's audio inputs sorted by path and shuffled by a PCG seeded
// with Config.Seed.
//
// With Config.Texts, each window's docinfer text goes to <texts>/<label>/<sha>.txt, empty unless
// transcribed, and the last reply's raw content, which docinfer drops for an incomplete reply, to
// <sha>.reply.txt, so compare.py can compare two runs' texts per window.
//
// Its exported identifiers:
//
//   - [Config] is one run's settings, and [Run] runs it.
//   - [Result] is one run's JSON line, of [Transcript] and [WindowRecord] entries.
package transcribe
