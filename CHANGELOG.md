# Changelog

All notable changes to `github.com/standards-lab/docinfer`, docinfer's Go base module, are
documented here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and
the module adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html). Each release's
section is headed by the version its `vX.Y.Z` tag carries, and is the release's notes. Its exported
API is its contract: a change to it is a change here. This changelog covers the base module only;
each nested module keeps its own.

## [Unreleased]

## [v0.1.0] - 2026-11-19

The first release: the inference library above go-ai's tier. It describes images, transcribes
audio windows, extracts by a caller's prompt and JSON schema, and chunks and embeds text, over the
parts docproc's dispatcher prepares, yielding each result as it is made and holding every model
result in a cache. The module requires docproc v0.3.0 and go-ai v0.4.0 alone.

`chunk` is copied from spike-ai-pipeline's `internal/docinfer/chunk` at 6a631f1, and `join` from
spike-ai-pipeline-processing v0.9.0's (4402f7c) `audio/join.go`, each with only its module and
import paths changed, then built to the design. The spike's `infer`, `vision`, `transcribe`, and
`embed` packages, and its `outdir`'s `Key`, `Held`, `Hold`, `Span`, and `Seam`, are not copied:
they are rewritten to the design against go-ai v0.4.0. `docinfertest`, the claim table, `Bounds`,
extraction, and the integration tier are new.

### Added

- `Pipeline`, built by `New` from a `Config`: a go-ai client and settings per capability
  (`Vision`, `Transcription`, `Extraction`, and `Embeddings`), the chunker, the `Cache`, the
  `Bounds`, the inputs admitted at once (`Pending`), the texts per embedding call (`EmbedBatch`),
  a batch's byte cap (`BatchBytes`), the embedding space's `Dimensions`, and the transcript join's
  `join.Request`. Nothing has a default: `New` panics, naming the field, on a missing client,
  cache, or bounds, and refuses a value below 1 or an empty model, matching `ErrRequest`.
  `Pipeline.Profile` reports each capability's `Identity`, the dimensions, and the chunker's
  settings.
- `Pipeline.Run`, one job over its `Input`s, a dispatch event's output or the input's metadata
  text (`MetadataText`), with `Options`: an `Extract`, a caller's prompt and schema, and `Final`,
  the job's last delivery. It yields each `Result` as it is made: each `Inference`, each
  extraction's `Progress`, each `Batch` of embedded chunks once its embeddings complete, and the
  job's `Counts` last. Every result is held before anything derived from it is yielded. Images
  are described, audio windows transcribed, grouped, and joined, text and Markdown outputs and
  the metadata chunked, and every chunk embedded, its vector read by `DecodeVector`.
- The go-ai failure table: a refused status is the item's outcome, held; a permanent or transient
  failure stops the job, `Permanent` telling them apart; a failed call stops it too, but on the
  last delivery its item is recorded failed, never held. A content miss is a `MissingError`.
- `Bounds`, from `NewBounds`, the requests in flight per capability across every pipeline built
  on it. The pipeline joins work in flight across its jobs by key, so each key is asked of a model
  once, single keys and embedding batches alike.
- `Cache`, its `Get` of many keys in one call and its `Put`, with `FileCache` (`NewFileCache`) and
  `MemoryCache` (`NewMemoryCache`). `Key` is a result's key, hashing `Version`, its `Identity`,
  and its content's SHA-256; `Held` is a held value's JSON. A golden key per capability pins the
  form.
- `docinfertest.Cache`, the cache contract every implementation runs, the module's two and a
  provider's.
- `chunk.Splitter`, which cuts a text along its headings, paragraphs, and sentences, then hard,
  into chunks of at most its `Bound` of runes, each with its byte `Span`; `chunk.Version`, which a
  golden corpus pins.
- `join.Join`, which merges an audio part's window transcripts at their seams, each seam's search
  bounded by its seconds at a `Request`'s `WordsPerSecond`, into a `Transcript`.
- The integration tier, `integration`, behind the `integration` build tag, which `mise run
  integration` runs against the live endpoints go-ai's `AI_<ROLE>_*` variables name.
- The check, `mise run check`, run by CI; `mise run currency` and `mise run upgrade`; and the
  release workflow, which makes a GitHub release from `CHANGELOG.md` for a `v*` tag and from
  `<module>/CHANGELOG.md` for a `<module>/v*` tag.

[Unreleased]: https://github.com/standards-lab/docinfer/compare/v0.1.0...HEAD
[v0.1.0]: https://github.com/standards-lab/docinfer/releases/tag/v0.1.0
