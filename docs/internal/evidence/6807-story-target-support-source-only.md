# 6807 Story Target-Support Source-Only Rollup

The target-support fallback counts active Jira/work-item and PagerDuty
incident-routing facts only when target-linked evidence is absent. A fact with
no reference-array keys, with only some empty reference arrays, or with all
three arrays empty is source-only evidence. A fact with a nonempty
`candidate_refs`, `evidence_refs`, or `linked_entities` array is excluded.

## Theory Proof

Before the change, a PostgreSQL 18.6 fixture shim evaluated the former
three-valued predicate as NULL for `{}` and for payloads with only some
reference-array keys. `WHERE` removes NULL rows, which caused the zero or
undercount. The CASE form evaluates each absent or non-array key to FALSE
before applying `jsonb_array_length`, so the enclosing negation admits the
source-only fact. The shim retained the indexed active-generation LATERAL plan;
this change adds no index, join, or ordering change.

## Performance Evidence

Performance Evidence: On ops-qa PostgreSQL 18.3, the exact aggregate ran
five times per form with the same 12 fact kinds, interleaved in one read-only
repeatable-read snapshot. Both forms used
`fact_records_scope_generation_idx`, made 9,768 probes, and reported 49,469
shared-buffer hits.

| Form | Runs (ms) | Median | Result |
| --- | --- | --- | --- |
| Current predicate | 127.853, 101.093, 101.380, 109.773, 95.115 | 101.380ms | `0\|0\|0` |
| CASE predicate | 122.418, 101.452, 101.925, 97.717, 94.027 | 101.452ms | `236\|0\|236` |

The CASE form corrects the result without a material latency regression; both
medians are below the one-second read budget.

## Regression Proof

The live fixture in
`go/internal/query/service_story_target_support_live_test.go` seeds active and
stale generations, tombstones, foreign generation pointers, pending
generations, duplicate fact kinds, target-linked facts, no reference keys,
partial empty reference keys, non-array reference values, all-empty arrays,
and a nonempty-array exclusion.

RED, before the CASE change:

```bash
cd go && ESHU_POSTGRES_DSN="$ESHU_POSTGRES_DSN" \
  CGO_CFLAGS='-O2 -g -std=gnu17' \
  go test ./internal/query -run '^TestServiceStoryTargetSupportSQLSemanticsLive$' \
  -count=1 -v -timeout=90s
```

The source-only aggregate returned `2|1|1` against the initial RED fixture,
which required `4|3|1`. The final fixture adds a non-array reference value,
requires `5|4|1`, and checks that the story reports
`support_source_only_not_target_linked` with the corrected coverage count.
After the CASE change and final story assertion, the focused command passed
against the disposable PostgreSQL 18.6 instance (exit 0, 2.230s package time).
The full `go test ./internal/query -count=1 -timeout=5m` suite passed without
the disposable fixture DSN (exit 0, 11.477s package time).

NornicDB no-regression check: the existing live service-story selection test,
`TestServiceStoryTruncationSelectionIsDeterministicLiveNornicDB`, passed against
an isolated, pinned v1.3.3 NornicDB instance (exit 0, 8.112s package time).
Its attached-platform graph read emitted a 1.191s slow-read warning; that
query is outside this Postgres-only aggregate and is not a before/after
measurement of the changed path. The disposable graph volume was removed.

No-Observability-Change: the fallback remains an aggregate query inside the
existing `postgres.query` span family for service-story support evidence. It
adds no graph query, metric, span name, label, log field, queue, worker, or
runtime setting. A NornicDB query regression is inapplicable because this
Postgres-only aggregate does not call the graph backend.
