# Evidence: #4797 typed tombstone payload guard

Scope: `DeltaMaterializationFromGenerations` now rejects a tombstoned
`directory` fact whose payload is missing required `path` instead of silently
omitting it from `DeltaDeletedDirectoryPaths`.

Baseline: before the change, the gen2 tombstone loop read `payload["path"]` with
a type assertion and appended only non-empty strings. A malformed tombstone was
therefore ignored, leaving the offline delta materialization with no deleted
directory path and no error.

After measurement: `TestDeltaMaterializationRejectsMalformedTombstonedDirectory`
failed before the implementation (`error = nil`) and passes after the guard
calls `requireString(payload, "path")`. The gen2 loop remains a single O(n)
in-memory scan over the collected envelopes and adds no storage, graph, queue,
or worker work.

No-Regression Evidence:

- `GOCACHE=/tmp/eshu-4797-replay-gocache go test ./internal/replay/offlinetier -run TestDeltaMaterializationRejectsMalformedTombstonedDirectory -count=1`
- `GOCACHE=/tmp/eshu-4797-replay-gocache go test ./internal/replay/offlinetier -count=1`
- `make pre-pr`
- `GOCACHE=/tmp/eshu-4797-golden-gocache go test ./internal/goldengate/... ./cmd/golden-corpus-gate/ -count=1`
- `bash scripts/test-verify-golden-corpus-gate.sh`

Backend/version and input shape: this guard runs before any backend write and is
independent of NornicDB or Neo4j behavior. The affected input is the in-memory
gen2 fact stream for the replay delta tier; the real-backend retraction proof
remains `scripts/verify-replay-tier.sh` / CI for the live NornicDB phase.

Terminal queue or row counts: not applicable to this local seam. No Postgres
queue rows or graph rows are claimed, written, retried, or drained by the new
validation path.

No-Observability-Change:

No runtime spans, metrics, logs, or status fields changed. The operator-facing
signal for this replay-only malformed input is the returned error before graph
projection begins; live graph truth and idempotency coverage stay with the
existing offlinetier backend tests.
