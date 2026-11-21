# docinfer context

docinfer is the inference library above go-ai's tier: it runs vision, transcription, extraction,
chunking, and embedding over the parts docproc dispatches, calling go-ai directly, and hands
results out in batches as they are made, behind a result cache its host can back with PostgreSQL.
Cerebro (capstone.data-intelligence) is its first host. Its base module requires docproc and go-ai;
its PostgreSQL cache, `docinfer/postgres`, and its command, `cmd/docinfer`, are nested modules.

Its goals live in the coordinator's roadmap (`standards-lab/roadmap/index.toml`,
v1.ai.inference). It is built from spike-ai-pipeline's docinfer at 6a631f1. Its package docs and,
for `cmd/docinfer`, its command notes settle its boundaries, superseding docproc's
`context/layers.md` docinfer sections as of docproc commit 673ed3d.

## Capability map

- **Pipeline** (built): [doc.go](../doc.go).
- **Cache** (built): [doc.go](../doc.go), [docinfertest/doc.go](../docinfertest/doc.go).
- **Chunk and join** (built): [chunk/doc.go](../chunk/doc.go), [join/doc.go](../join/doc.go).
- **Job** (built): [job/doc.go](../job/doc.go).
- **postgres** (built): [postgres/doc.go](../postgres/doc.go).
- **cmd/docinfer** (planned): [command.md](command.md), [command-api.md](command-api.md),
  [command-report.md](command-report.md).

## Notes

- [`command.md`](command.md): cmd/docinfer's evaluation, C1–C24 less the report's.
- [`command-api.md`](command-api.md): cmd/docinfer's API, its names, and its release.
- [`command-report.md`](command-report.md): the report's shape, its findings, and the directory it
  renders from.
