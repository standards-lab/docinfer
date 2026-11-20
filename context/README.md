# docinfer context

docinfer is the inference library above go-ai's tier: it runs vision, transcription, extraction,
chunking, and embedding over the parts docproc dispatches, calling go-ai directly, and hands
results out in batches as they are made, behind a result cache its host can back with PostgreSQL.
Cerebro (capstone.data-intelligence) is its first host. Its base module requires docproc and go-ai;
its PostgreSQL cache, `docinfer/postgres`, and its command, `cmd/docinfer`, are nested modules.

Its goals live in the coordinator's roadmap (`standards-lab/context/roadmap.toml`,
v1.ai.inference). It is built from spike-ai-pipeline's docinfer at 6a631f1, and its boundaries are
settled by its design note, which supersedes docproc's `context/layers.md` docinfer sections as of
docproc commit 673ed3d.

## Capability map

- **Pipeline**: the root package's run over go-ai's client: vision, transcription, extraction, and
  embedding, with bounds, cancellation, and batches as made. It echoes each input as it admits it,
  and reports each input settled once everything derived from it is out.
- **Cache**: the result cache interface, with filesystem and memory implementations, and
  `docinfertest`, the suite every implementation passes.
- **Chunk and join**: chunking extracted text, and joining transcribed audio windows at their seams.
- **Job**: the `job` package runs one job: docproc's dispatch, then the pipeline's inference, its
  updates in one order. It yields a checkpoint at the low watermark of settled messages, and a
  `Done` that counts parts by part ID. A content miss drops its part's index entry, so a
  redelivery prepares the part again.
- **postgres**: the cache in PostgreSQL over sqlate.
- **cmd/docinfer**: the command over a cmd/docproc run directory, with its own directory and
  report.

## Notes

- [`design.md`](design.md): docinfer's design, reviewed from the spike; the one home of its API,
  and the task that releases each tag.
