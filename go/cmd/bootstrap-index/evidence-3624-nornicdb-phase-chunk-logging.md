# Bootstrap NornicDB Phase-Chunk Logging Evidence

## Context

Issue: #3624, with #3586 as the end-to-end performance umbrella.

The bounded current-main run `post4474-currentmain-bounded-20260701t184833z`
stopped after enough pre-reducer signal. It showed source-local canonical graph
writes still consuming the first several minutes before reducer drain could be
the main event:

- `canonical_write`: 119 samples, 760.146s summed, 6.388s average, 50.253s max.
- `entities`: 117 samples, 509.552s summed.
- `files`: 126 samples, 239.685s summed.
- `directories`: 126 samples, 98.603s summed.

The bootstrap-index NornicDB executor already logged failed chunks and logged
successful concurrent entity chunks, but successful non-entity phase chunks
such as `files`, `directories`, and `directory_edges` were invisible at chunk
level. The next bounded run therefore could not split a slow `files` or
`directories` phase into chunk count, row count, and per-chunk duration.

## Evidence

No-Regression Evidence: `go test ./cmd/bootstrap-index -run
TestBootstrapNornicDBPhaseGroupExecutorLogsSuccessfulChunks -count=1 -v` first
failed because successful non-entity phase chunks emitted no log line. After the
change, the test passes and proves successful bootstrap NornicDB phase-group
chunks report phase, true chunk index/count across the same phase group,
statement range, statement count, row count, duration, and a bounded operator
statement summary.

No-Regression Evidence: `go test ./cmd/bootstrap-index -count=1` passes after
the logging change.

Observability Evidence: successful bootstrap NornicDB phase-group chunks now log
`bootstrap nornicdb phase-group chunk completed` with `phase`, `chunk_index`,
`chunk_count`, `statement_start`, `statement_end`, `statement_count`,
`row_count`, `duration_s`, and `first_statement`. The `first_statement` value
uses the operator-safe summary path so bootstrap logs can distinguish phases
without exposing file paths or entity IDs. This adds no metric, worker,
queue-domain, Cypher shape, batch size, timeout, retry policy, or graph-write
semantics change. Statement chunk sizes are unchanged; the executor now keeps
the full same-label phase group together until the existing chunker assigns
bounded transactions, so the logs show useful `1/N`, `2/N`, ... ordinals instead
of repeated `1/1` flushes.
