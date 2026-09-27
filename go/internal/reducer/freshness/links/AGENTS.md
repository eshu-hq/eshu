# AGENTS.md — changed_since_link reducer domain

## Read first

1. `README.md` and `doc.go` in this directory.
2. `go/internal/storage/postgres/freshness/links/README.md` for the ledger,
   the link transaction and its lock order.
3. `docs/internal/evidence/7127-changed-since-link-writer.md` for gates
   G1-G16.

## Invariants

- The runner never succeeds on a retryable outcome. `drainScope` stops the
  scope, and the cursor stays where it was.
- With `ESHU_CHANGED_SINCE_LINK_ENABLED` unset or false, `cmd/reducer` builds
  no runner and the domain issues no SQL (gate G14, pinned by
  `cmd/reducer/changed_since_link_wiring_test.go`).
- No metric label may carry a scope or generation identifier. Identifiers
  belong on the span and in the log line.
- Do not lower `Workers` or the slot count to hide a concurrency defect. The
  per-scope cursor row is the fence. Slots are a measured memory and CPU
  budget (#7127 ruling 8.3), not a correctness device.

## Verification

```bash
cd go && go test -race ./internal/reducer/freshness/links ./cmd/reducer -count=1
```
