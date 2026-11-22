# fact_records snapshot-keyed layout: ranking and replica census (#11202)

Refs #11202. Part of #10860. Follows #10881, which measured `HASH(scope_id)` as a
no-go for removal cost (see
[the #10881 measurement record](10881-fact-records-hash-partition-measurements.md)).
This record covers boxes 1 and 2 of #11202: rank the removal cost first, then
census the read replica. Boxes 3 to 6 (candidates, kill criteria, copies) follow only
if these two do not close the issue.

## Decision rule, written before any measurement (2026-11-22 04:50Z)

Deadline for boxes 1 and 2: 06:47Z, two hours from the start. A failed attempt does
not restart the clock.

**Box 1, rank first.** Removal is "a top cost" if retention's delete plus VACUUM
write-ahead log (WAL) is at least **20%** of all `fact_records` write WAL (inserts,
deletes, vacuum) at the retained five-index profile, which is the index set the table
keeps after #10880 moves reads off it. If removal is below 20% at that profile, the
issue closes as a no-go on this evidence, and the record says so. The 153-index
profile is measured as the "today" comparison and decides nothing by itself, because
#10880 removes those indexes before any layout change.

How the share is derived: per-row WAL for removal and for insertion is measured by the
#10881 R1 method on a real whole-scope sample (same removal list, `VACUUM (FREEZE)` and
`CHECKPOINT` before the arm, per-backend WAL, one cell at a time). It is scaled by the
production row-operation counts already recorded on #10881 and #10860 (201.8M inserts
and 73.0M deletes on the primary in the measured period of 2026-11-17). The primary's
statistics are out of bounds for this work, so the counts are inherited, not re-read,
and the share is an estimate with that stated limit.

**Box 2, census.** Read-only, on the QA read replica, aggregate numbers only (no scope,
repository, host or account names in this file). Measured: generations per scope by
status; rows per generation by status; lifetime of a superseded generation (activation
to supersession); generations created per day. Nothing is built or copied from it.

Kill criteria for any later candidate belong to box 4 and are written before any
candidate is measured; none is measured here.
