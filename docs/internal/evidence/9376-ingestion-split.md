# Ingestion helper split (#9376, PR-A)

This note records the evidence for moving four helper functions out of
`go/internal/storage/postgres/ingestion.go`. The move is a precondition for the
claim-plane commit fence, which does not fit in a 497-line file under the
500-line cap.

No-Regression Evidence: the change moves `logCommitStage`, `now`, `drainFacts`
and `drainFactsAndCheckStream` into `ingestion_commit_stage.go` and
`ingestion_stream_error.go` without editing a line of their text. Baseline: the
tree at `ebaf4dfa0d`. After: the same tree with the helpers in two new files.
Backend: Go 1.26 and Postgres 18.6 for the package tests; the helpers contain
no SQL. Input shape: any claimed commit, since the helpers run on every
`commitScopeGeneration`. The multiset of removed and added lines is equal except
the new file's license header, package line and imports, so the compiled
statement sequence is unchanged. `TestIngestionStoreCommitClaimedScopeGenerationFencesClaimInTransaction`
passes, which pins the in-tree statement sequence. No lock, lease, queue or
graph path is read or written differently, and terminal queue and fact counts
are unchanged because no statement changed.

No-Observability-Change: no metric, span, log field, status field or telemetry
label is added, removed or renamed. `logCommitStage` keeps its stage names and
fields; only its file moves.
