// Package report is docinfer's run report and its command, docinfer report <dir> [--run <id>]:
// static HTML pages, rendered with html/template, written into a run's report directory,
// runs/<run>/report/, which outdir.Dir.ReportDir names. It reads docinfer's directory alone, the
// run's manifest, its chunk record, its inferences record, and the texts, and never docproc's run
// directory, which its pages link into.
//
// The report has an index page, index.html, and one page per input content, named by the input's
// SHA-256, <sha256>.html, the name docproc's page for the same content has. The index lists every
// input, sorted by path, with its error and its counts, linked to its content's page where it has
// a SHA-256; an input whose docproc line was an error has none, and its row is unlinked. Each line
// of the records names its input by path, which the manifest's inputs map to the input's
// SHA-256; for an unended run, whose manifest lists no input yet, a line of the input's own part,
// at depth 0, maps it, and an input with no such line keeps an unlinked row.
//
// A content's page lists the inputs holding it and links docproc's page for it, then shows, every
// text inline and whole:
//
//   - each image vision ran on, with its outcome, its reason, and its text, beside an img at
//     docproc's content file, rundir.ContentPath, linked to docproc's page
//   - each transcript, with its outcome, its windows, each linked to its content file, the
//     windows not transcribed, its seams, and its text
//   - each source text once, grouped by its part's ID and SHA-256, its kind, and its text's
//     SHA-256, as its chunks in index order: each chunk's index, span, text, and a rejected
//     chunk's error, and never its vector
//
// An image or transcript, and a chunk, that two inputs of the same content both name shows once,
// as its first line in the record has it. A text texts/ misses says so; a text whose SHA-256 isn't
// its name is shown marked damaged. Every text is escaped, a byte that isn't UTF-8 shown as U+FFFD.
//
// Every link into docproc's run directory is relative, through the manifest's docproc directory,
// which the run recorded relative to docinfer's directory, so the pair of directories moves whole.
// Rendering reads no clock: every time is the manifest's, in UTC, and every list has a fixed
// order, so the same directory renders the same bytes in any local zone, docproc's run directory
// present or not. An unended run renders the lines its records hold, its pages marked not ended.
// The pages load nothing from elsewhere: no script, font, or style sheet, their style inline,
// following the reader's light or dark scheme.
//
// The command renders the latest run of the directory that ended, or the run --run names, one
// that never ended among them, and prints the index page's absolute path. It declares no node:
// it reads no configuration and builds no client. A directory that isn't docinfer's, or is of
// another schema, a --run that isn't one of its runs, or no --run and no run that ended, exits 1,
// the error naming it.
//
// The package exports:
//
//   - [Commands], which returns the report command, declaring no node
//   - [Make], which renders a run's report and prints its index page's path
//   - [Render], which renders a run of docinfer's directory into its report directory, replacing
//     earlier pages whole
//   - [IndexPage], the index page's file name
package report
