# stagedaudit

The staged stale-winners audit tick (eshu-hq/eshu#10451) assembles the
standby read phase from per-scope batches and hands the result to the
unchanged writer-side verification.

## Flow

1. `StagedAuditor.AuditWinners` pins one reader member for the tick.
2. Every active (scope, generation) pair fetches one narrow batch on that
   member, each in its own short transaction with a fresh snapshot.
3. Batches compact client-side into a per-key winner map (streaming; the map
   is the only cross-batch state).
4. The stored winners table fetches once on the same member; the diff emits a
   superset of the single-statement scan's candidates.
5. Candidate keys resolve to verification identifiers (stored row first, else
   one point lookup per new key); unresolvable keys count, never silently
   pass.
6. The shared writer-side verification re-checks every candidate under the
   resweep lock and stays authoritative. Any read failure defers the tick
   with no writer contact.

## Invariants

- No cross-batch transaction or cursor ever pins a standby snapshot.
- No partial diff reaches the writer: one failed batch aborts the whole tick.
- Fail-closed caps abort, never truncate: 20,000 rows per batch, 1,000,000
  staged facts, 250,000 distinct keys.
- The legacy writer path and the split scan are untouched; routing selects
  this auditor only under the staged-audits mode.
