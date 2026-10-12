# Lease-expiry proof evidence (#7939)

Performance Evidence: The local baseline and corrected expiry cell ran on
2026-10-11 at tested commit `17c48cbe8edbd355a0cccf1ea104379ae613ae87`.
Both used fresh PostgreSQL 18 Alpine and the Compose-pinned NornicDB
`fix-500-e022384c` backend. Inputs were the shared GCP, synthetic multiscope,
SQL, code-call, documentation, and rationale cassettes, with four workers.
The baseline took 17 seconds; the held-handler expiry cell took 14 seconds.
These are single functional runs, not a latency benchmark or speedup claim.
Both canonical graph digests matched; terminal residuals and dead letters
were zero. The corrected cell increased reclaim from zero to one and proved
replacement attempt two before releasing the original handler.

Observability Evidence: The controlled original cell observed a claim, then
committed its ACK before the expiry statement. The statement affected zero
rows and the unchanged reclaim assertion failed. This proves an ineffective
injection schedule is possible; historical hosted ACK timing remains unproven.
The corrected cell records held, expired, replacement, release, and finished
identities plus the owner marker. Failure uploads retain these bounded records.
The untagged constructor preserves executor identity and ignores the barrier
environment. The enabled hook holds one handler outside graph permits and
transactions; race tests prove unrelated and replacement work can progress.
Production worker count, lease, heartbeat, SQL fencing, and blocking checks
remain unchanged. The evidence supports the fault seam and convergence, not
production throughput or a full local run of all 49 cells.
