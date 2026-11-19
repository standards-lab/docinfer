// Package chunk is docinfer's chunker: a [Splitter] cuts one text into chunks of at most its Bound
// of characters, runes, each a byte span of the text, for the embeddings capability to embed.
//
// A text is cut along its structure, coarsest first. A text within the bound is one chunk.
// Otherwise it is cut before each Markdown heading, an ATX heading line ("#" to "######" and a
// space) outside a fenced code block, and the sections are packed greedily, each run of whole
// sections that fits the bound one chunk; a section that doesn't fit alone is cut the same way at
// the next level: at blank lines into paragraphs, then into sentences, after ".", "!", or "?" and
// any closing quote or bracket before whitespace, or "。", "！", or "？", and at each line break,
// so a list's items part without punctuation; and last, a hard cut every bound runes.
//
// Each chunk is trimmed of the white space at its ends, and its [Span] is that of the trimmed
// text, so a chunk's Text is always the text's [Start, End) bytes. The chunks come in the text's
// order, none overlaps another, and together they cover the text but its trimmed white space. No
// cut falls inside a UTF-8 character: an invalid byte counts as one character, as range over a
// string has it. An empty or white-space-only text, or stretch of one, yields no chunk. A Bound
// has no default: one below 1 is refused with [ErrRequest].
//
// [Version] names the splitting: docinfer reports it in its Profile through [Splitter.Settings],
// and a golden corpus of chunks, testdata/chunks-v<Version>.json, pins it, so a change to how
// texts are cut must change it.
//
// The package exports:
//
//   - [Splitter], its Bound, and its methods [Splitter.Split], which cuts a text into [Chunk]s,
//     and [Splitter.Settings], its version and bound
//   - [Chunk], a chunk's Text and [Span], and [Span], its Start and End bytes
//   - [Version], the splitting's version
//   - [ErrRequest], the cause of a refused Bound
package chunk
