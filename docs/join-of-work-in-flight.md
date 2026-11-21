# Join of work in flight

Why the pipeline joins work in flight with its own claim table rather than
`golang.org/x/sync/singleflight`. What the join promises is in `Pipeline.Run`'s documentation,
[`run.go`](../run.go); this page holds the reasoning behind its mechanism.

## The waiter rule

The pipeline asks a model once per key, across every job a replica runs: a result one job is
asking a model for, another job waits for rather than asks again. A waiter gets the leader's
result; a waiter whose leader ends without a result asks the model itself; and a cancelled waiter
stops without waiting out the leader's request.

The spike docinfer was reviewed from had no join: one process ran one run, and per-run maps keyed
by content SHA-256 removed duplicates before sending. The cache and the join give the same
guarantee across jobs, so the pipeline keeps no set of digests per job.

## The claim table

docinfer's own claim table: a mutex, a map of calls, and a done channel per call. `claim(keys)`
returns which keys are the caller's and which are waited for; the wait selects on the caller's
context; a call that ends without a result wakes its waiters to claim again. It is one mechanism
for single keys and batches. Release is deferred, so a panicking leader doesn't strand its
waiters, and the key is deleted before they wake, so a waiter that claims it again finds it free.

A caller claims its keys before it reads the cache, so a key released meanwhile is held already,
since a claimant puts before it releases. A key repeated in one claim is waited for, like one
another caller holds. An embeddings call takes its keys' claims before it hands anything to the
coordinator, so no waiter waits on that goroutine's send, and a lease is used by one goroutine at
a time, handed on by `take`.

Its tests run through `Run`, under `-race`: a leader cancelled, a leader's transient failure, a
waiter cancelled, a leader that panics, and a batch partly in flight. The base module requires
docproc and go-ai alone, so the join adds no third module.

## Rejected: singleflight

`golang.org/x/sync/singleflight` meets the waiter rule only through `DoChan`, a leader flag the
closure sets, the waiter's own `ctx.Err()` check, and a retry loop around it. And `Embed` answers
a batch of keys, which singleflight's one key, one function can't claim without blocking, so
embeddings would need a second mechanism.

## Either way

The leader `Put`s before it releases, so a waiter's result equals a cache read and counts as
`Held`, its tokens the leader's job's alone. A waiter holds no `Bounds` slot. A refusal is a
result and is shared, and a permanent error is final and is shared.
