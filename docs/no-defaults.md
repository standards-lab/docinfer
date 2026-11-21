# No library defaults

Why docinfer has no default for any bound, cap, setting, or request time. The values each `New`
refuses are in the package documentation and the [README](../README.md#no-defaults); this page
holds the reasoning.

## Bounds, caps, and settings

The spike docinfer was reviewed from coerced invalid bounds: its vision, transcription, and
embedding fan-outs, its chunker, and its join each turned a bad value into a working one. Measured
against validation-first and the baseline standards, docinfer does as docproc does: it adds no
library default and coerces nothing. `New` and `NewBounds` refuse a value below 1, matching
`ErrRequest`, and each host ships its operator's starting values.

A seam's bound in the join is derived per seam from the seconds the two windows share, at
`join.Request.WordsPerSecond`, rather than fixed at the spike's 16 words on the claim that the
windows say nothing about it. `WordsPerSecond` has no default either.

## Starting values

`Pending` and `BatchBytes` have no library default; a host ships its own. 512 KiB is the starting
`BatchBytes`: it keeps a batch under NATS's default 1 MB `max_payload`, a 768-dimension vector
being 4,096 B in base64. docproc's layers proposed that value as a library default, and docinfer
dropped it.

## A request's time

A request's time is the host's. The spike set no bound on a request, and a request that stalls
in the pipeline pins shared state, such as a `Bounds` slot. Measured against the timeouts
principle and go-ai's client contract, the host owns the bound: it bounds `Generate`, `Embed`,
`Transcribe`, and the `/models` listing with its transport's `ResponseHeaderTimeout`, and
extraction's stream with go-ai's `Options.StreamIdleTimeout`, which ends a stalled stream without
cutting off a long one. docinfer sets no deadline and no default of its own, and the root's
package documentation says so.
