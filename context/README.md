# docinfer context

docinfer is the inference library above go-ai's tier: it runs vision, transcription, extraction,
chunking, and embedding over the parts docproc dispatches, calling go-ai directly, and hands
results out in batches as they are made, behind a result cache its host can back with PostgreSQL.
Cerebro (capstone.data-intelligence) is its first host. Its base module requires docproc and go-ai;
its PostgreSQL cache, `docinfer/postgres`, and its command, `cmd/docinfer`, are nested modules.

Its goals live in the coordinator's roadmap (`standards-lab/context/roadmap.toml`,
v1.ai.inference). It is built from spike-ai-pipeline's docinfer at 6a631f1, and its boundaries are
settled by docproc's `context/layers.md` until its design note takes them over.

## Capability map

- **Pipeline**: the root package's run over go-ai's client: vision, transcription, extraction, and
  embedding, with bounds, cancellation, and batches as made.
- **Cache**: the result cache interface, with filesystem and memory implementations.
- **Chunk and join**: chunking extracted text, and joining transcribed audio windows at their seams.
- **Job**: one job's dispatch then inference, its checkpoints and its outputs counted by part ID.
- **postgres**: the cache in PostgreSQL over sqlate.
- **cmd/docinfer**: the command over a cmd/docproc run directory, with its own directory and
  report.
