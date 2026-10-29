# Count a stored fresh source state as fresh only inside its window (#9423)

Status: Accepted. Part of #9209; follows #9420.

**Decision.** A stored `fresh` vulnerability source state counts as fresh only
while `last_success_at` is after the read's cutoff, `now - window`. The window
is `vulnsource.DefaultFreshnessWindow` (14 days) for every source. No new
number is introduced. Readiness and the admin status snapshot both apply it,
each against one clock per read. The writer does not change.

## Window source of truth per source

Two writers record `vulnerability_source_states`: the in-tree collector
(`vulnruntime.ClaimedSource.sourceStateStale`) and the component-extension
host recorder (`checkpoint.Recorder`). Both default to
`vulnsource.DefaultFreshnessWindow` when their `FreshnessWindow` field is zero.
No production caller sets that field: `rg -n 'FreshnessWindow' go --type go`
finds assignments only in tests. So the window does not vary by source or by
instance.

| Source | Scope ids | Window the writer uses | Window the reader uses |
| --- | --- | --- | --- |
| OSV (configured and derived) | `vuln-intel://osv/...` | `vulnsource.DefaultFreshnessWindow` | same |
| NVD | `vuln-intel://nvd/...` | `vulnsource.DefaultFreshnessWindow` | same |
| CISA KEV | `vuln-intel://cisa/kev` | `vulnsource.DefaultFreshnessWindow` | same |
| FIRST EPSS | `vuln-intel://first/epss` | `vulnsource.DefaultFreshnessWindow` | same |
| GLAD | `vuln-intel://glad/...` | `vulnsource.DefaultFreshnessWindow` | same |

Readiness used to keep its own `14 * 24 * time.Hour` constant. It now aliases
`vulnsource.DefaultFreshnessWindow`, so the reader and the writers cannot
drift. If a per-source window is ever added to the writers, the readers must
take it from the same place. That would be a new decision, not a change here.

## What changed

- **Status snapshot.** `vulnerabilitystore.ReadVulnerabilitySourceStates` now
  takes the snapshot's as-of time. It reports a stored `fresh` row as `stale`
  when `last_success_at` is missing or not after `asOf - window`. Other states
  pass through unchanged. Before this, the status snapshot printed the stored
  value, so a stopped writer's last rows read `freshness=fresh` indefinitely.
  The aging runs in Go after the scan, against the one as-of time that the
  whole status snapshot already uses. `VulnerabilitySourceStatusQuery` is
  byte-identical.
- **Readiness.** No behavior change was needed. Configured rows were already
  aged in Go by `ageSourceStates`, and derived rows in SQL against `$22`. Both
  use the single cutoff computed once per read (`time.Now() - window`), inside
  the repeatable-read snapshot. There is no per-row clock. An aged row is
  `stale` in `source_states[]`, and `BuildReadiness` turns it into
  `target_incomplete` with `<source>:stale` in `incomplete_reasons`. That is
  the reason readiness reports. The only change is the window constant.
- **Deactivated instances.** Readiness (configured and derived target CTEs)
  and the status query (#9420) already skip rows of a disabled or deactivated
  instance. The tests below now pin that.

Known limit, not changed: configured readiness rows reach Go through
`TO_CHAR(... 'SS')`, which truncates `last_success_at` to whole seconds. A row
whose success falls less than one second after the cutoff reads `stale` one
second early. The error points toward stale, never toward fresh, so changing
the hot-path SQL for it is not justified.

## Tests

- `TestReadVulnerabilitySourceStatesAgesFreshRowsAtAsOf` (unit) fails on base
  (`at cutoff`, `out of window`, `fresh without success` read `fresh`) and
  passes after. It covers in-window, a sub-second in-window boundary, the
  cutoff itself, out-of-window, a missing success, and a failed row.
- `TestVulnerabilitySourceStatusAgesFreshRowsLive` (new live test, one slot)
  runs the real status query on PostgreSQL. The live in-window row stays
  fresh, the live 15-day-old row reads stale, and a deactivated instance's
  recent row is not listed. Mutant: comparing only the zero time made it fail
  with `freshness = "fresh", want "stale"`.
- `TestSupplyChainImpactReadinessSuccessExpiresAtReadTimeLive` (existing live
  test, cases folded in, no new slot) covers NVD, KEV, and EPSS rows out of
  window, a KEV row in window (13 days), and a deactivated instance's fresh
  row. It passes on base too, because readiness already aged its rows.
  Mutants: dropping the Go `ageSourceStates` call failed the three
  out-of-window cases, and dropping `deactivated_at IS NULL` from the
  configured target CTE failed the retired case.

## Performance Evidence

The QA environment read replica was unreachable during this change (expired
SSO session). Plans were measured on a local private PostgreSQL 18.6 cluster,
labeled local. The seed was one bootstrapped schema, 181 configured NVD/EPSS/KEV
targets and states, and one derived npm instance with 50,300 scopes and package
keys. That is far above the 576 rows the QA replica held when #9420 measured
it. Each run was `EXPLAIN (ANALYZE, BUFFERS)`, 7 interleaved runs with the
order alternating per round, median reported.

| Read | SQL change | Median execution (local) | Plan |
| --- | --- | --- | --- |
| Readiness, CVE request (configured-heavy) | none | 5.06 ms | unchanged text |
| Readiness, 300-package derived request (loser class) | none | 9.12 ms | unchanged text |
| Status snapshot query | none | 18.39 ms | Limit, top-N heapsort over Hash Join on `collector_instances`, Seq Scan on states |

No-Regression Evidence: `git diff origin/main` changes no SQL line in
`readiness_postgres_query*.go` or `VulnerabilitySourceStatusQuery`, so the
before and after plans are the same statement. The added cost is one time
comparison per returned status row (at most 200). On the local seed,
`ReadVulnerabilitySourceStates` with aging took 14.6 ms wall time for 200 rows.

## Observability Evidence

No-Observability-Change: no metric, span, or log key is added. The aged state
shows up in the signals operators already read: `freshness_state` in
`/admin/status` `vulnerability_sources` and in the readiness `source_states[]`,
and `<source>:stale` in readiness `incomplete_reasons`. The findings and
explain handlers already log a readiness fail-closed cause at WARN.
