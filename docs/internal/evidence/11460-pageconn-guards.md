# #11460 follow-up: page-conn forwarding evidence

Refs #11460. Part of #10972.

Production builds `*InstrumentedDB` around the backfill store, but only
`SQLDB` implemented `backfill.PageConnProvider`, so every production page
silently took the unguarded fallback leg. This change forwards
`OpenBackfillPageConn` through `*InstrumentedDB` (loud error otherwise).

## No-Regression Evidence: page-conn forwarding

The guarded leg issues the same paged SELECTs as the fallback leg, plus one
`SET LOCAL statement_timeout` and one `SET LOCAL application_name` per page
open by design (safety, not speed). The delegation itself is one interface
assertion per page open; a page covers up to 5,000 rows.

Measured post-fix on scratch Postgres 18 (disposable databases):

- Hermetic RED-first tests fail pre-fix (`fallback leg taken`) and pass
  post-fix (package suite 0.048s).
- `TestBackfillBoundsLiveProof` passes through production-shaped wiring
  (3x12000 facts, mean 0.14s per load); the same proof against the pre-fix
  wrapper fails, so it discriminates the leg taken.
- Full `internal/storage/postgres/backfill` package green (1.582s;
  11.972s under `-race`).

No before/after wall comparison is claimed: pre-fix production never ran the
guarded leg, so there is no valid pre-fix timing baseline for it. The claim
is bounded to what was measured: identical statements, identical row counts,
and green discriminating proofs.

## No-Observability-Change: page-conn forwarding

No new metrics, spans, or log lines. The existing per-page `application_name`
(`eshu-backfill-per-commit` and siblings) is now observed on the wire as the
contract always stated; the unguarded fallback emitted none.
