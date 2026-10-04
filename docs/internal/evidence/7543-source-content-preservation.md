# Literal source content preservation

This prerequisite was found during the native writer checks for #7543 and
#7245. The collector and shaping layer preserve source text, but the projector
decoded `content_body` and `source_cache` with the metadata decoder, which
trims whitespace. The Postgres entity writer trimmed source cache again.
File storage retained the supplied digest even when the projector changed
the body. The fix preserves source text through those boundaries.

The raw accessor retains the existing string and `fmt.Stringer` conversions.
Identifier, path, digest and metadata decoding keep their existing rules.
No payload map is mutated. Missing or unsupported values retain their prior
builder behavior. Empty strings and whitespace-only source strings are covered.

## Correctness proof

The initial regression exited 1 on leading whitespace, trailing newlines,
whitespace-only bodies, entity snippets and the bound source-cache SQL value.
The corrected tests pass. File SQL arguments retain the body and supplied
digest. Entity SQL arguments retain source cache. Alias, empty, Stringer and
unsupported-scalar cases are included.

Focused command, from `go/`:

```sh
go test -p 1 -count=1 -run '^(TestPayloadRawTextPreservesStringShapedScalars|TestBuildContentRecordPreservesRawBodyAndSuppliedDigest|TestBuildContentEntityRecordPreservesSourceCacheWithPathAndTypeAliases|TestContentBuildersKeepStringerTextAndIgnoreNonStringScalars|TestContentWriterPassesRawBodyAndDigestToSQL|TestContentWriterPreservesRawSourceCacheParameter)$' ./internal/projector/decode ./internal/projector/runtime ./internal/storage/postgres
```

Nearby projection, canonical entity identity, batched insert and tombstone
tests also pass. These tests use an executor fake to inspect the production
writer's SQL parameters. They do not prove a deployed migration or database
throughput. The fix adds no SQL, transaction, queue or lease change.

## Measured projection cost

Benchmark Evidence: five paired runs of the existing
`BenchmarkProjectionCloneRemovalProof/Borrow` fixture, with 5,000 mixed facts,
on Go 1.27.1, darwin/arm64. Baseline is `89340121c`; candidate is `0b541d708`.
Both binaries use the candidate tree; the baseline overlays the unchanged
base revision's `projection.go`. The new raw helper is unused by that baseline
production path. The fixture exercises file bodies through `buildProjection`.
Its entity facts carry no source cache. It uses no database or graph server.

Each pair runs the baseline and candidate with these flags:

```sh
./projection.test -test.run='^$' -test.bench='^BenchmarkProjectionCloneRemovalProof/Borrow$' -test.benchtime=100ms -test.count=1
```

| Metric | Before | After |
|---|---:|---:|
| Median time per 5,000-fact projection | 6.380203 ms | 6.388806 ms |
| Median allocated bytes | 17,581,218 | 17,581,229 |
| Median allocations | 73,994 | 73,994 |

The median time difference is +0.135%. Same-pair candidate/baseline ratios
have mean 1.003787 and sample standard deviation 0.006889. This is a
correctness fix with no material regression in this fixture, not a speedup
or collector throughput claim. Start, maximum and end load1 were 2.4502,
below half the 18 logical CPUs. Builds completed before measurement; both
other lanes paused local builds and tests. All owned process groups exited.
The prior-base run was superseded after a cleanup dependency changed on main;
the figures above come from rebuilt binaries on the new base.

No-Regression Evidence: the focused and neighboring tests plus the paired
projection benchmark cover the changed decoding path. Physical native writer
reconciliation, representative ingest throughput and cold API/MCP p95 remain
separate acceptance work for #7543/#7245. Previously stored trimmed rows are
not repaired by this change; replay from retained source facts is needed.

No-Observability-Change: no metric, span, log field, route or runtime setting
changes. Existing projector stage signals, writer results and database spans
remain available. This change corrects source bytes passed to the existing
writer and leaves its query count, batching and error handling unchanged.
