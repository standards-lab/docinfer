// Package outdir is docinfer's directory, the one run --out names, read and written, under schema
// "2":
//
//	outdir.json                    the directory's schema, {"schema": "2"}
//	results/<capability>/<xx>/<hash>.json
//	                               the held results, a docinfer.FileCache, shared by every run
//	texts/<xx>/<sha256>            each inference's text, a dispatch.FileContent, no extension
//	runs/<run>/manifest.json       each run's [Manifest], written as it starts and as it ends
//	runs/<run>/chunks.jsonl        its chunk record, one [Line] per chunk, appended per batch
//	runs/<run>/inferences.jsonl    its inferences, one [Inference] per line, appended as made
//	runs/<run>/report/             its report, which [Dir.ReportDir] names; the report's writer
//	                               makes it, not this package
//
// A run's id is a version 7 UUID, in its lowercase form, so the ids sort by when each run began.
//
// # Opening the directory
//
// [Open] opens the directory to read it, creating nothing: a root holding no outdir.json fails
// with an error matching fs.ErrNotExist. [Create] opens it to write it, making the directory, its
// outdir.json, results/, and texts/ when they are absent. Each refuses a directory of another
// schema, naming both, as it does one holding results/, texts/, or runs/ with no outdir.json,
// which schema "1" wrote, so no schema "1" directory's held results or texts are reused. Every
// method that takes a run's id refuses one that isn't a run of the directory, a directory under
// runs/ named by a version 7 id and holding its manifest, with an error matching fs.ErrNotExist.
//
// # Writing a run
//
// One process writes a run: nothing here locks the directory. [Dir.Begin] writes the run's
// manifest and opens its two records; [Run.Inference] and [Run.Batch] append to them, each
// call's lines written through as one write, with no fsync, so a run that stops keeps every line
// it wrote and a run killed part way loses at most its last write; [Run.End] syncs and closes the
// records and writes the manifest again, its end set, which marks the records whole. Every other
// file is written whole, to a temporary name in its directory, synced, and renamed. A run whose
// manifest has no end is unended: it is still running, or it was killed. [Dir.Lines] and
// [Dir.Inferences] drop an unended run's last line when it has no newline, the one a killed run
// may have torn; a caller reads the manifest's end to know whether the run ended.
//
// The package exports:
//
//   - [Version], the directory's schema
//   - [Dir], from [Open] or [Create]: [Dir.Runs], the runs in the order they began,
//     [Dir.Latest], the latest that ended, [Dir.Manifest], [Dir.Lines], and [Dir.Inferences], a
//     run's manifest and records, [Dir.ReportDir], a run's report directory, [Dir.Cache], the
//     held results, and [Dir.Texts], the inferences' texts
//   - the writer's, on a Dir from Create: [Dir.Begin], which begins a [Run], [Run.ID],
//     [Run.Inference], [Run.Batch], and [Run.End]
//   - the manifest's shapes: [Manifest], [Build], [Docproc], [Input], [Capability], [Skipped],
//     and [Space], with [UnitTolerance]
//   - [Line], one line of the chunk record, and [Inference], one line of the inferences record,
//     each naming the input its part was found in by the input's path
package outdir
