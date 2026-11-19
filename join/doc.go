// Package join is docinfer's join of window transcripts: [Join] merges the transcripts of an audio
// part's windows, in plan order, into one [Transcript], splicing each pair of neighbouring windows
// where their shared seconds were heard twice, with the best-offset merge Hugging Face
// transformers stitches its chunked speech recognition with.
//
// Each seam's search is bounded by the seconds the two windows share at a [Request]'s
// WordsPerSecond, rounded up and at least 2 words, so a phrase the speaker repeats further from
// the seam is never the splice. WordsPerSecond has no default: one below 1 is refused with
// [ErrRequest]. A refused or missing window is the empty string, its seams unmatched and the
// windows either side concatenated around it. The words before the latest seam are settled: a
// caller joining a stream's windows as they arrive may treat them as final.
//
// The package exports:
//
//   - [Join], which merges window transcripts and their seams' seconds as a [Request] says
//   - [Request], and its WordsPerSecond, the words a second of a seam is searched over
//   - [Transcript], the joined Words and Seams, and its method [Transcript.Text], the words
//     joined by spaces
//   - [Seam], how one seam was merged: Matched, Matches, Overlap, and Position, with JSON tags
//   - [ErrRequest], the cause of a refused request
package join
