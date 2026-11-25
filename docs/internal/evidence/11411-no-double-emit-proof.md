# 11411 double-emission fix: proof record

Refs #11411. A writer pass that added rows under new fact ids while retiring
that key's stored same-generation rows staged the key twice in one
transaction (fact_upsert capture plus retract DISTINCT staging, about half a
second apart on a slow retraction update). The fix hoists the keep set and
drops retired keys from `CaptureWinnersDirtyFacts`, mirroring the retract
tombstone predicate. No production query, lock, metric, or tick-shape change.

- Performance Evidence: failing-first RED then GREEN on the post-#11400 tree.
  RED pre-fix: `TestWriteStagesOneDirtyRowWhenOnePassAddsAndRetiresTheSameKey` fails
  with the doubled capture (upsert names K while production retract stages K:
  2 dirty rows, one pass). GREEN post-fix: 4/4 subtests pass; mirror test
  `TestExcludeRetiredWinnersDirtyIdentifiersMirrorsTheRetraction` 7/7.
  Backend: local disposable Postgres 18.6 via template-clone harness (no QA
  writes). Input shape: QA-shaped seeded facts plus the one-key near-miss
  pair; full package suite green (`go test
  ./internal/reducer/supplychain/core/ -count=1` ok 2.35s, reducer tree ok
  7.29s), `go vet` clean. Terminal counts: zero deadlocks, zero production
  diff beyond the writer exclusion (9 files under
  `go/internal/reducer/supplychain/core/`, the live-test expectation update
  in `go/internal/storage/postgres/`, plus this note). Why safe: the exclusion mirrors
  the retract predicate on all six tombstone axes (generation, keep set,
  fencing token, scoped stamp, active-only, partial evidence); key derivation
  matches the SQL RETURNING rule; independent review verdict MERGE-READY with
  zero P0/P1/blocking-P2.
- Observability Evidence: no new telemetry. Capture-source counts shift from
  `fact_upsert` to `fact_retract` for retired keys, but both sources already
  emit metrics, so existing dashboards keep working with no new series.
