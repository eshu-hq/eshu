# Graph adoption-only guard evidence (#7033)

The new `ESHU_GRAPH_SCHEMA_ADOPT_ONLY` flag is off by default. The guard
changes bootstrap startup only; it does not execute the code-topic read path.

No-Regression Evidence: before and after were compiled from exact base
`761498979` (the clean comparison worktree at `07e69f1` has no Go changes from
that base) and candidate `6532fbed`, respectively, using Go 1.26.9 and clang.
The same `TestRunPassesNeo4jBackendToSchemaApplicator` fixture selected the
`neo4j` backend but used fake Postgres and graph executors: zero database rows,
one fake graph-apply callback, no actual database or storage. With
`GOMAXPROCS=1`, each compiled binary ran the test 3,000 times per sample,
interleaved B/A/A/B/B/A/A/B. Baseline elapsed samples were 562, 559, 557,
and 560 ms (median 559.5 ms); candidate samples were 566, 571, 568, and
564 ms (median 567 ms), or +7.5 ms per 3,000 runs (+1.34%). All eight runs
exited 0. The default path has no material measured regression in this local
shim; the small difference is not a deployed latency claim. Backend version,
terminal queue count, and live row count are not applicable to this fake-store
fixture. The opt-in refusal has no baseline-equivalent output: focused tests
show zero graph-DDL calls and zero new marker writes for an incomplete or
unavailable catalog. No live Neo4j adoption or #7033 endpoint timing was
measured here, and this result does not establish the <1s query budget.

No-Observability-Change: the guard adds no metric or span. Existing structured
`bootstrap.graph.adoption_incomplete` and `runtime.startup.failed` logs report
catalog refusal and the top-level startup failure. Operators can use these
events to distinguish a safe refusal from an applied graph schema; this local
fixture did not verify production log delivery.
