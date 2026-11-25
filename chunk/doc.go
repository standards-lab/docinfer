// Package chunk is docinfer's chunker: a [Splitter] cuts one text into chunks of at most its Bound
// of tokens, as its Count measures them, and at most its Runes of characters, each a byte span of
// the text, for the embeddings capability to embed.
//
// The measure is the host's: a [Counter] gives a text's tokens in the embedding model's own
// tokenizer, as the embedding of that text would take them, special tokens and any document
// prefix included, so a chunk within the Bound is one the model takes whole. Token counts don't
// add across a join, since each count holds the special tokens once and a tokenizer can merge
// across a boundary, so every chunk is measured as it is emitted: Split never yields a chunk Count
// measures over the Bound.
//
// The Runes bound is the host's too: the characters, Unicode code points, a chunk holds, for a
// server that refuses an input by its length before tokenizing it, such as vLLM, which refuses
// one over 16 characters a token of its context. A text of long white-space runs a tokenizer
// collapses can be within the Bound and far over that length. Characters add, so the Runes bound
// takes no count: a stretch over it is cut without being measured, and a run is packed only
// while its characters are within it. Every chunk is within both bounds, and its Text is the
// text's own, never collapsed. Unlike the count, Runes holds no document prefix: a host that
// prepends one leaves it room.
//
// A text is cut along its structure, coarsest first. A text within both bounds is one chunk. A
// unit "over the bound" below is over either. Otherwise it is cut before each Markdown heading, an ATX heading line ("#" to "######"
// and a space) outside a fenced code block, into sections, each measured once. The sections are
// packed greedily, a run of whole sections one chunk while the sum of their counts, less the empty
// text's count for each join, is within the bound and its characters within Runes; a run of more than one is measured, and one
// over the bound gives up its last section until it fits. A section over the bound alone is cut
// the same way at the next level: at blank lines into paragraphs, then into sentences, after ".",
// "!", or "?" and any closing quote or bracket before whitespace, or "。", "！", or "？", and at
// each line break, so a list's items part without punctuation. Last, a stretch over the bound
// with no such cut is cut hard: each chunk the longest run of whole characters, at most Runes of
// them, the search finds within the bound, by measuring, its end interpolated from the counts and
// bisected. A character that alone measures over the bound is refused with [ErrRequest].
//
// A text within both bounds takes one count, and a stretch over Runes none of its own. One over it takes one count for the text, one for the
// empty text, one for each unit at each level it is cut at, and one for each packed run, and a
// rare one more for each unit a run gives up: a text of paragraphs that fit takes about its
// paragraphs and its chunks in counts. A hard cut takes a handful of counts for each chunk, and
// one for a chunk Runes ends within the Bound.
//
// Each chunk is trimmed of the white space at its ends, and its [Span] is that of the trimmed
// text, so a chunk's Text is always the text's [Start, End) bytes. The chunks come in the text's
// order, none overlaps another, and together they cover the text but its trimmed white space. No
// cut falls inside a UTF-8 character: an invalid byte counts as one character, as range over a
// string has it. An empty or white-space-only text, or stretch of one, yields no chunk. Nothing has
// a default: a Bound or Runes below 1 or a nil Count is refused with [ErrRequest]. A count's error stops the
// text's splitting and is returned wrapped.
//
// [Version] names the splitting, and [Splitter.Settings] reports it with the Bound, the Runes, and
// the Model, the served model whose tokenizer Count measures by: the chunker's identity, which docinfer
// reports in its Profile. A golden corpus of chunks, testdata/chunks-v<Version>.json, pins it
// under a deterministic count, each case with its Bound and Runes, so a change to how texts are
// cut must change it.
//
// The package exports:
//
//   - [Splitter], its Bound, Runes, Count, and Model, and its methods [Splitter.Split], which cuts
//     a text into [Chunk]s, and [Splitter.Settings], its version, bound, runes, and model
//   - [Counter], the host's measure of a text in the embedding model's tokens
//   - [Chunk], a chunk's Text and [Span], and [Span], its Start and End bytes
//   - [Version], the splitting's version
//   - [ErrRequest], the cause of a refused Bound, Runes, Count, or text
package chunk
