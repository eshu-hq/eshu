# AGENTS.md — changed_since_link reducer domain

## Read first

1. `README.md` and `doc.go` in this directory.
2. `go/internal/storage/postgres/freshness/links/README.md` for the ledger,
   the link transaction and its lock order.
3. `docs/internal/evidence/7127-changed-since-link-writer.md` for gates
   G1-G16.

## Invariants

- The runner never succeeds on a miss or a failure. A non-counting miss
  (`cursor_locked`, `generation_locked`, `slot_busy`) writes nothing and moves
  to the next candidate; a counting failure goes through `RecordFailure`,
  which poisons at `MaxAttempts` (#7127 ruling 8.10, gates G16a and G16b).
  Never make a lock miss count, and never drop the attempt limit.
- With `ESHU_CHANGED_SINCE_LINK_ENABLED` unset or false, `cmd/reducer` builds
  no runner and the link domain issues no SQL (gate G14, pinned by
  `cmd/reducer/changed_since_link_wiring_test.go`). Generation retention still
  prunes the ledger tables with the switch off: G14 covers the link domain,
  not the tables.
- No metric label may carry a scope or generation identifier. Identifiers
  belong on the span and in the log line.
- A rebase (`Break` `prior_pruned` with `Kind` root) is `outcome=linked` and
  also one `prior_pruned` chain break; only a break with `Kind` none is
  `outcome=break`.
- The orphan probe scans three ledger tables; keep it sampled at most once per
  `orphanProbeInterval`, never once per cycle (busy cycles run back to
  back).
- Do not lower `Workers` or the slot count to hide a concurrency defect. The
  per-scope cursor row is the fence. Slots are a measured memory and CPU
  budget (#7127 ruling 8.3), not a correctness device.

## Verification

```bash
cd go && go test -race ./internal/reducer/freshness/links ./cmd/reducer -count=1
```
