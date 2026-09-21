# Projection runtime

## Purpose

Drive one projection of one scope generation end to end: admit the facts,
build the canonical materialization and the content records, write them
through the injected writers, publish the graph-projection phase checkpoints,
and collect the reducer intents the downstream queue consumes.

## Ownership boundary

This package owns projection sequencing and the writer contracts. It does not
own row shape (`../canonical`), typed decoding or quarantine classification
(`../decode`), per-family stage reduction (`../stage`), or failure
classification (`../failure`). It does not own the work queue, the claim or
the lease — `internal/projector` itself owns the service loop that calls this
package.

## Exported surface

- `Runtime` — the unit of work, carrying the writers and the identity locker.
- `Result` — what one projection reports back to the service loop.
- `ReducerIntent`, `ReducerIntentWriter`, `IntentResult` — the reducer-intent
  vocabulary the queue consumes.
- `CanonicalWriter`, `PackageRegistryIdentityLocker` — the injected
  collaborator interfaces.
- `BuildContentRecord`, `BuildContentEntityRecord`, `BuildReducerIntent` — the
  record builders the per-family stages call.

## Three behaviors worth knowing before changing anything here

**Admission runs before any writer.** A fact whose schema version the
projector does not support, or whose `generation_id` does not match the
generation under projection, fails the whole projection. It is never written
partially: a half-written generation is wrong graph truth, which the
repository's accuracy rule ranks above throughput.

**Payloads are borrowed, not cloned.** Projection reads the caller's fact
payloads in place. Mutating one corrupts the caller's facts and the replay
comparison that reads them afterwards, so `TestBuildProjectionDoesNotMutateInputFactPayloads`
pins it and `BenchmarkProjectionCloneRemovalProof` measures what the borrow
buys.

**Package-registry identity writes are bracketed by a lock.** Concurrent
projections of different scopes share the package identity keyspace;
`PackageRegistryIdentityLocker` is what keeps them from interleaving on it.
Removing the bracket is a concurrency defect, not an optimization.

See `doc.go` for the full godoc contract.
