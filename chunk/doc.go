// Package chunk is docinfer's chunker: a [Splitter] cuts one text into chunks of at most its Bound
// of tokens, as its Count measures them, each a byte span of the text, for the embeddings
// capability to embed.
//
// The measure is the host's: a [Counter] gives a text's tokens in the embedding model's own
// tokenizer, as the embedding of that text would take them, special tokens and any document
// prefix included, so a chunk within the Bound is one the model takes whole. Token counts don't
// add across a join, since each count holds the special tokens once and a tokenizer can merge
// across a boundary, so every chunk is measured as it is emitted: Split never yields a chunk Count
// measures over the Bound.
//
// A text is cut along its structure, coarsest first. A text that measures within the bound is one
// chunk. Otherwise it is cut before each Markdown heading, an ATX heading line ("#" to "######"
// and a space) outside a fenced code block, into sections, each measured once. The sections are
// packed greedily, a run of whole sections one chunk while the sum of their counts, less the empty
// text's count for each join, is within the bound; a run of more than one is measured, and one
// over the bound gives up its last section until it fits. A section over the bound alone is cut
// the same way at the next level: at blank lines into paragraphs, then into sentences, after ".",
// "!", or "?" and any closing quote or bracket before whitespace, or "。", "！", or "？", and at
// each line break, so a list's items part without punctuation. Last, a stretch over the bound
// with no such cut is cut hard: each chunk the longest run of whole characters the search finds
// within the bound, by measuring, its end interpolated from the counts and bisected. A character
// that alone measures over the bound is refused with [ErrRequest].
//
// A text within the bound takes one count. One over it takes one count for the text, one for the
// empty text, one for each unit at each level it is cut at, and one for each packed run, and a
// rare one more for each unit a run gives up: a text of paragraphs that fit takes about its
// paragraphs and its chunks in counts. A hard cut takes a handful of counts for each chunk.
//
// Each chunk is trimmed of the white space at its ends, and its [Span] is that of the trimmed
// text, so a chunk's Text is always the text's [Start, End) bytes. The chunks come in the text's
// order, none overlaps another, and together they cover the text but its trimmed white space. No
// cut falls inside a UTF-8 character: an invalid byte counts as one character, as range over a
// string has it. An empty or white-space-only text, or stretch of one, yields no chunk. Nothing has
// a default: a Bound below 1 or a nil Count is refused with [ErrRequest]. A count's error stops the
// text's splitting and is returned wrapped.
//
// [Version] names the splitting, and [Splitter.Settings] reports it with the Bound and the Model,
// the served model whose tokenizer Count measures by: the chunker's identity, which docinfer
// reports in its Profile. A golden corpus of chunks, testdata/chunks-v<Version>.json, pins it
// under a deterministic count, so a change to how texts are cut must change it.
//
// The package exports:
//
//   - [Splitter], its Bound, Count, and Model, and its methods [Splitter.Split], which cuts a text
//     into [Chunk]s, and [Splitter.Settings], its version, bound, and model
//   - [Counter], the host's measure of a text in the embedding model's tokens
//   - [Chunk], a chunk's Text and [Span], and [Span], its Start and End bytes
//   - [Version], the splitting's version
//   - [ErrRequest], the cause of a refused Bound, Count, or text
package chunk
