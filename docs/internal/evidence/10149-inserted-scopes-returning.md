# No-Regression Evidence: #10149 per-item inserted scopes (RETURNING variant)

## Baseline

#10154 joined the replan commit to the authorized `_replan` scope set, but
the admission reported counts only (`InsertedWorkItems`), so a scope the
open-target guard duplicate-dropped or the work-item insert refused still
retired its rows as drained. The unguarded insert statement
(`enqueueWorkflowWorkItemsSuffix`, `ON CONFLICT DO NOTHING`) is the shipped
hot path.

## After measurement

- Guarded admission reports per-item accepted scopes via a new
  `RETURNING scope_id` suffix (`enqueueWorkflowWorkItemsScopesSuffix`),
  used only by `enqueueWorkItemBatchScopesWithExecutor`
  (`go/internal/storage/postgres/inserted_scopes.go`); the unguarded
  statement diff is pure addition (no `-` lines), prefix/suffix constants
  untouched.
- `RunAdmission.InsertedScopeIDs` (nil-by-default, additive) carries the
  accepted scopes; the commit joins survivors on authorized AND inserted.
  Same INSERT, same conflict arbiter, same batch bound
  (`len(scopes) <= len(items)` enforced in code); rows are fully scanned
  and closed before any later statement on the transaction.
- Backend: hermetic suites (fakes) plus live-Postgres scratch-instance
  legs for per-item attribution and lock safety (executor-run; instance
  since dropped).
- Input shape: guard-dropped scope row and insert-refused row
  (`insertConflictReplanStore`, matching the #4586 index-refusal shape);
  repeat-tick scenario with poll-still-recorded assertion.
- Terminal counts: survivors commit only when a covering scope was both
  authorized and inserted; dropped rows stay pending with the poll
  recorded; `InsertedWorkItems += len(batchScopes)` keeps the #4586
  report-only-accepted-rows invariant.

## Telemetry and status evidence

- No-Observability-Change: no new metric, span, log key schema, route,
  worker, lease, or runtime knob. Drops stay operator-visible through one
  Info line per held pass (`held_denied_rows`,
  `held_duplicate_or_refused_rows`, `drained_rows` + collector id) beside
  the existing survivor list, using the pre-existing `Service.Logger`.
- Operators diagnose the path through the same Postgres query spans and
  `eshu_dp_postgres_query_duration_seconds` as before.

## Why the change is safe

The unguarded hot path is byte-identical; the RETURNING variant runs only
on the guarded admission with the same batch bound and conflict behavior.
The contract change is additive (nil-by-default field, unchanged
`(int, error)` wrapper), counts-only backends flow nil IDs only into a
commit path they never reach, and the satellite keeps
`ErrGuardedAdmissionNotSupported`. Same-shape precedent: #4444
(Exec-to-Query + RETURNING on an existing ON CONFLICT statement, accepted
on hermetic + live proof).

## Proof commands (all exit 0)

- `go test ./internal/coordinator/ -count=1` → ok (incl. `-race` in floor)
- `go test ./internal/workflow/... -count=1` → ok (contention 40.4s)
- Focused postgres suites + live attribution/lock-safety legs → PASS
- `go vet`, `gofmt -l` (empty), `git diff --check` → clean
- Preliminary `eshu-code-review`: P0/P1/P2/P3 all zero
  (`/tmp/10149-review-verdict.md` in the lane record)
