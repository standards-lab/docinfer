# docinfer context

docinfer is the inference library above go-ai's tier: it runs vision, transcription, extraction,
chunking, and embedding over the parts docproc dispatches, calling go-ai directly, and hands
results out in batches as they are made, behind a result cache its host can back with PostgreSQL.
Its base module requires docproc and go-ai; its PostgreSQL cache, `docinfer/postgres`, is a nested
module; its command, `cmd/docinfer`, is another.

Its goals live in the coordinator's roadmap (`standards-lab/roadmap/index.toml`,
v1.ai.inference). Its lineage is in the workspace's
[references catalog](https://github.com/standards-lab/org/blob/main/references.md). Its package
docs and, for `cmd/docinfer`, its command notes settle its boundaries.

## Capability map

- **Pipeline** (built): [doc.go](../doc.go).
- **Cache** (built): [doc.go](../doc.go), [docinfertest/doc.go](../docinfertest/doc.go).
- **Chunk and join** (built): [chunk/doc.go](../chunk/doc.go), [join/doc.go](../join/doc.go).
- **Job** (built): [job/doc.go](../job/doc.go).
- **postgres** (built): [postgres/doc.go](../postgres/doc.go).
- **cmd/docinfer** (built): [command.md](command.md), [command-api.md](command-api.md),
  [command-report.md](command-report.md).

## Notes

- [`command.md`](command.md): cmd/docinfer's evaluation, C1–C24 less the report's.
- [`command-api.md`](command-api.md): cmd/docinfer's API, its names, and its release.
- [`command-report.md`](command-report.md): the report's shape, its findings, and the directory it
  renders from.
