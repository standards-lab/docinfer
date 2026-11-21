# cmd/docinfer's report

This note states cmd/docinfer's planned report and the directory it renders from;
[command.md](command.md) holds the other findings, and [command-api.md](command-api.md) the layout.

## The report's shape

The spike renders one page a run, `runs/<run id>/report.html`, its inputs keyed and sorted by
absolute path, so the same content under two paths shows twice. A vision text or transcript is cut
at 1,000 runes, then linked under `texts/`; a text or Markdown output is a link into docproc's run
directory; a chunk is counted, never shown; an image is an `img` and a window a link into docproc's
directory, by a path computed at render time from the manifest's absolute `docproc.dir` (C17).
docproc's report is `runs/<run>/report/index.html` and a `<sha256>.html` per distinct content, the
index listing every input linked to its content's page and each page holding the content's part
tree with every output inline and whole; it reads its own directory alone, links relatively, and
reads no clock. Cerebro asks for docinfer's pages "linked to docproc's by the input's SHA-256 and
holding the full vision texts, transcripts, and chunks" (data-intelligence, lines 38-45).

**Decision.** Per run, one page per input content: `runs/<run>/report/index.html` and a
`<sha256>.html` per content, the file name docproc's page for the same content has, so the cross
link to it is one relative path. Schema "2" records each input's SHA-256 in an input list, since a
chunk line's part is the part it was cut from, not the input; an input with no SHA-256 keeps its
row, its error, and its metadata chunk on the index. Everything is inline and whole: each vision
text with its outcome, each transcript with its windows, failed windows, and seams, and each source
once, as its chunks in order (index, span, a rejected chunk's error, no vector), since the chunks
don't overlap and cover the text but for trimmed white space (`chunk/doc.go:12-15`). Images and
windows stay docproc's and are never copied: each vision text has an `img` beside it at docproc's
`rundir.ContentPath`, and each content page links to docproc's page for it. The run records the
relative path to docproc's directory once, so the rendered bytes don't depend on when the report
is rendered, and a moved directory costs links, never text. The report renders from docinfer's own
directory alone (`texts/`, the record, the manifest, and the inferences file, C15), and a test
renders it with docproc's directory absent.

**Rejected: one page a run.** It is the spike's, and the reason for its cut; the same content
under two paths shows twice, and nothing links to docproc's page by SHA-256.

**Rejected: a page per input path.** The same content under two paths would show twice, and the
page's name would not be docproc's.

**Rejected: keeping the 1,000-rune cut.** The median vision text is 1,419 characters
(`docs/findings/docinfer.md:72`), so the cut hides most of what the page is for, and per-content
pages remove its reason.

**Rejected: link-only images and windows.** It loses the image beside its text, which is the page's
use.
## Findings

**C16. issue: one page a run, texts cut at 1,000 runes, no chunk text.**
`report/report.go:22-31`, `report/report.go:296-309`, `report/report.html.tmpl:132`,
`report/report.html.tmpl:175-177`, `report/doc.go:19-20`, `docs/findings/docinfer.md:72`.
Against data-intelligence, lines 42-43. **accepted**, docinfer: pages per input content, every
text whole, as "The report's shape" settles.

**C17. issue: the page's links depend on where docproc's run directory is.**
`report/report.go:65-90`, `report/doc.go:25-31`, `app/report_test.go:118-126`. Against the
package doc's own "the same directory renders the same bytes". **accepted**, docinfer: the run
records the relative path to docproc's directory once, as "The report's shape" settles, and the
file-URL branch, unreachable on Unix, goes.

**C18. issue: the report groups a source's lines by a line of index 0.**
`report/report.go:200-227`. Against layers "docinfer's shapes" (a chunk's key). **accepted**,
docinfer: lines are grouped by their part's ID, source kind and text SHA-256, and index, which a
schema "2" line carries.

**C19. issue: the run command always renders, and the report command differs from docproc's.**
`infer/execute.go:119`, `infer/execute.go:125`, `report/report.go:45-53`,
`report/commands.go:36-41`. Against `cmd/docproc`'s `run --report` and `report`. **accepted**,
docinfer: `docinfer run --report`, never after a cancellation, a failed render exit 1 with the run
intact; `docinfer report <dir> [--run <id>]` takes the latest run that ended (C4);
`report.Render(ctx, dir, id)`, and a `Make` that prints the index's absolute path.

**C20. issue: neither command's help states its exit status.** `infer/commands.go:25-29`,
`report/commands.go:21-25`, `app/report_test.go:188`. Against `cli-applications.md` and
`cmd/docproc`'s `exitStatus` footer. **accepted**, docinfer: a footer on each, stating that
recorded refusals exit 0, what exits 1, and what the last line counts.

## The directory

```go
package outdir
// Open opens the directory at root to read it, creating nothing; Create makes it, its marker,
// results/, and texts/ when they are absent. Each refuses another schema, naming both, so no "1"
// directory's results/ and texts/ are reused (C11).
func Open(root string) (*Dir, error)
func Create(root string) (*Dir, error)
func (d *Dir) Runs() ([]string, error)                   // its runs' ids, in order
func (d *Dir) Latest() (string, error)                   // the latest run that ended
func (d *Dir) Manifest(id string) (Manifest, error)
func (d *Dir) Lines(id string) iter.Seq2[Line, error]    // the chunk record, vectors left out (C12)
func (d *Dir) Inferences(id string) iter.Seq2[docinfer.Inference, error]
func (d *Dir) Cache() *docinfer.FileCache                // over results/
func (d *Dir) Texts() dispatch.Content                   // over texts/
func (d *Dir) ReportDir(id string) (string, error)       // runs/<run>/report, not made
func (d *Dir) Begin(m Manifest) (*Run, error)            // on a Dir from Create
func (r *Run) Inference(i docinfer.Inference) error      // one line, written through
func (r *Run) Batch(b docinfer.Batch) error              // its lines, written through
func (r *Run) End(m Manifest) error                      // the manifest, its end set

type Manifest struct {
    Schema       string          `json:"schema"`            // "2"
    Run          string          `json:"run"`               // a version 7 UUID
    Docinfer     Build           `json:"docinfer"`          // {command, library}, from the
                                                             // build info (C13)
    Start        schema.Time     `json:"start"`             // docproc's rundir/schema.TimeOf (C24)
    End          schema.Time     `json:"end,omitzero"`      // marks the record whole (C12)
    Docproc      Docproc         `json:"docproc"`
    Inputs       []Input         `json:"inputs"`            // the docproc run's, in order
    Capabilities []Capability    `json:"capabilities"`
    Skipped      []Skipped       `json:"skipped,omitempty"`
    Space        *Space          `json:"embedding_space,omitempty"`
    Counts       docinfer.Counts `json:"counts"`
    Outputs      int             `json:"outputs"`           // the docproc run's output lines read
    Error        string          `json:"error,omitzero"`
}
type Build struct { Command, Library string } // "command", "library"
type Docproc struct {
    Dir string `json:"dir"` // docproc's run directory, relative to this one, recorded once (C17)
    Run string `json:"run"`
}
type Input struct { // one input of docproc's run (the report's shape)
    Path   string `json:"path"`             // as docproc's record gives it
    SHA256 string `json:"sha256,omitzero"`  // its depth-0 part's; none for an error line
    Error  string `json:"error,omitzero"`
}
type Capability struct { // C14
    docinfer.Identity         // inline: capability, model, settings, the cache's key
    Endpoint    string `json:"endpoint"`
    BaseURL     string `json:"base_url"`
    Format      string `json:"format"`
    Concurrency int    `json:"concurrency"`
    Batch       int    `json:"batch,omitzero"`      // embeddings
    ChunkSize   int    `json:"chunk_size,omitzero"` // embeddings
}
type Skipped struct { Name, Reason string } // "name", "reason": not configured
type Space struct { … } // the measured embedding space and its norms, as the spike's
// Line is a batch's chunk with what its indexes name inline, since chunks.jsonl holds no batches.
type Line struct {
    Digest      string          `json:"digest"`
    chunk.Chunk                 // inline: text, span
    Index       int             `json:"index"`
    Vector      string          `json:"vector,omitzero"`
    Error       string          `json:"error,omitzero"`
    Part        dispatch.Part   `json:"part"`
    Source      docinfer.Source `json:"source"` // its part index 0: Part is inline
}
```

`outdir` owns its whole-file write, its run list, and its id check, each `Dir` method refusing an
id that isn't one of its runs (C10). A record is appended, never renamed, so a stopped run keeps
the lines it made, and a reader of a run whose manifest has no `end` drops a last line with no
newline, as `rundir`'s does (C12). The report hashes each text it reads from `texts/`, and renders
one whose SHA-256 isn't its name as damaged: `ReadText`'s digest check is kept, in the report
(C10, C23). The manifest's capabilities tie it to its held results by the same `Identity` (C14),
and its inputs list each input's SHA-256, since a chunk line's part is the part it was cut from,
not the input. A refused or failed image or window is found in `inferences.jsonl`, by its part's
## The report

ID and content SHA-256 (C15).

The report follows "The report's shape": per run, `report/index.html` and a `<sha256>.html` per
input content, the name docproc's page for the same content has, so the cross link is one relative
path; an input with no SHA-256 keeps its row, its error, and its metadata chunk on the index; each
vision text inline and whole with its outcome and an `img` at docproc's `rundir.ContentPath`; each
transcript with its windows, failed windows, and seams; each source once, as its chunks in order,
grouped by part ID, source kind and text SHA-256, and index (C18), with no vector. It renders from
docinfer's directory alone, reads no clock, and a test renders it with docproc's directory absent.
`report.Render(ctx, dir, id)` writes the pages; its golden pages over a committed directory hold a
long text, a damaged text, and an unended run (C23).

