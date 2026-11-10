# EPSS Derived Targets Evidence (Phase 2 Item 2.1)

Issue eshu-hq/eshu#9876. The in-tree coordinator plans bounded FIRST EPSS
targets from the distinct CVE ids on active canonical winners; the collector
resolves them at claim time through the run's `requested_scope_set`.

Runtime paths touched: one `SELECT DISTINCT cve_id ... WHERE
suppression_state = 'active'` read per schedule tick in
`scheduleVulnerabilityIntelligenceWork`, a pure-CPU pack over the returned
ids, one extra transport dispatch per EPSS scope claim, and per-plan
counter/log/span writes.

No-Regression Evidence: the planner addition runs once per schedule tick,
not per work item, and is skipped entirely when `derive_from_findings` is
disabled (the reader is not even called, and the service records no EPSS
observation and logs no findings-derived EPSS line). The store read is one sequential
scan over the small winners table per tick — no new index, no DDL, no
transaction change. The claim dispatch tries configured targets first and
only parses transport for unknown scope ids, so NVD, OSV, and explicit EPSS
claims take the exact code path they took before. The derivation allocates
only the output slices (one per chunk plus one id copy); there is no per-item
retry, lease, or queue change. Scale measured at HEAD `4494f116ef` through the production
planner (`deriveEPSSFindingTargets` with no operator-set `target_limit`, so the
default 100-target budget applies; `target_limit` bounds derived targets, never
CVE ids): 447 distinct CVEs → 5 targets / 5 FIRST requests, 2235 → 23 / 23,
4470 → 45 / 45, matching the `ceil(n/100)` chunking with nothing over budget.
Max joined CVE chars measured 1399 on 13-char `CVE-2024-NNNN` fixture ids and
1699 on 16-char real-world-length ids (`go test
./internal/coordinator/vulnerability/ -run TestScratchEPSSScaleProbe -v`;
scratch probe, removed after the run — the committed
`TestDeriveEPSSFindingTargetsDefaultLimitPlansQAAtFiveTargets` pins the 447 →
5-target figure). Past the budget (12000 CVEs) the planner keeps the first 100
targets and counts the remaining 2000 ids as `derived_target_budget_exhausted`
skip evidence, never silently. `go test`
on `go/internal/coordinator/...`, `go/internal/workflow/`,
`go/internal/collector/vulnerabilityintelligence/...`,
`go/cmd/collector-vulnerability-intelligence/`, and
`go/cmd/workflow-coordinator/` exits 0; the postgres package passes except
the pre-existing `ESHU_DEPLOY_CHECKOUT`-gated schema test, which is
untouched by this diff and fails for a missing external checkout.

Post-Deploy QA Query (read-only; arbiter-required): after enabling
`derive_from_findings` in QA, at least 95% of CVE-keyed active findings must
carry a `vulnerability.epss_score` fact:

```sql
WITH cve_findings AS (
    SELECT DISTINCT cve_id
    FROM supply_chain_impact_canonical_winners AS w
    -- Effective-state denominator, matching listActiveFindingCVEIDsQuery
    -- since #9805 reader #10: a hidden state past suppression_expires_at
    -- reads 'expired' and stays visible.
    WHERE (CASE
      WHEN w.suppression_state NOT IN ('not_affected','accepted_risk','false_positive','ignored','advisory_retracted')
        OR w.suppression_expires_at IS NULL
      THEN w.suppression_state
      WHEN w.suppression_expires_at <= NOW()
      THEN 'expired'
      ELSE w.suppression_state
    END) NOT IN ('not_affected','accepted_risk','false_positive','ignored','advisory_retracted')
      AND w.cve_id <> ''
      AND w.cve_id ~* '^CVE-[0-9]{4}-[0-9]{4,}$'
),
epss AS (
    SELECT DISTINCT payload->>'cve_id' AS cve_id
    FROM fact_records
    WHERE fact_kind = 'vulnerability.epss_score'
      AND is_tombstone = FALSE
)
SELECT COUNT(*) AS cve_findings,
       COUNT(epss.cve_id) AS with_epss,
       ROUND(100.0 * COUNT(epss.cve_id) / NULLIF(COUNT(*), 0), 2) AS pct_with_epss
FROM cve_findings LEFT JOIN epss USING (cve_id);
-- PASS when pct_with_epss >= 95.
```

Observability Evidence: per-plan counts on the existing
`vulnerability_intelligence` plan span (`epss_derived_targets`,
`epss_distinct_cves`, `epss_excluded_non_cve`, `epss_over_budget_cves`);
two new counters
`eshu_dp_workflow_coordinator_epss_derived_admitted_total` (no labels) and
`eshu_dp_workflow_coordinator_epss_derived_skipped_total` (closed `reason`
label: `derived_target_non_cve_identifier`,
`derived_target_budget_exhausted`); and one INFO
`workflow coordinator planned findings-derived EPSS targets` line with the
same counts. No CVE id reaches a label, span attribute, or log field; the
metric test pins admitted=3, skipped reasons 2/100, and exclusion of
non-EPSS and empty evidence. No metric, span, log key, route, queue table,
or runtime knob was renamed.

## Activation Transport Build (Arbiter Decision 2, Landed Disabled)

The 2026-11-07 arbiter ruling keeps derived EPSS disabled anywhere
user-visible until the FIRST scope record lands on #9876, and requires the
enablement to land as code + config across four repos. This section records
the `drive/9876-epss-transport` build, which stays disabled: no QA config
flip, no `ScopeQuery` stuffing, no SPDX invention.

- **eshu-sdk**: additive `Claim.ScopeCVEIDs` (`scope_cve_ids`, at most
  `MaxScopeCVEIDs` = 100) with validator, JSON schema, schema goldens, and
  round-trip/empty-serialization tests mirroring `claim_scope_queries`.
  Empty omits from the wire so old satellites read byte-identical claims.
- **Satellite** (`eshu-collector-vulnerability-intelligence`, release after
  `v0.1.3`): `ParseSettings` accepts `derive_from_findings` (sources
  exactly `first_epss`, anything else fails closed); the handler resolves
  `vuln-intel://first_epss/_findings/<chunk>` to a `first_epss` target
  built from the claim's transported CVE ids only when derived EPSS is
  enabled and 1-100 ids ride the claim. Absent or malformed transport on a
  findings scope is terminal (`findings_transport`), never the retryable
  `unknown_scope` loop. Proven against a fake FIRST server with request
  counts (1 claim = 1 FIRST request; terminal paths = 0 requests), with the
  `first_epss` source-state recording pinned for `_findings` scopes.
- **Core**: the activation validator applies the in-tree findings rules;
  `ActivationDerivation.Enabled()` includes `DeriveFromFindings` while
  `PlanKeyDerivation` precedence is unchanged; the activation service reads
  finding CVEs only when enabled, plans through
  `PlanActivationDerivedWork` (now returning the EPSS observation), reports
  the observation on the in-tree log line and counters, and records
  admitted `_findings` scopes as last-planned. Findings chunks pace to one
  full finding-set fetch per FIRST publication day (sliding 24h from the
  last plan; Decision 5, measured by the pacing and service tests).
- **Deploy**: the satellite-plan Helm gate drops `derive_from_findings`
  from the unresolvable set only for the new satellite version; the render
  still fails when findings are enabled against a `v0.1.3` pin.
- **Attribution** (Decision 3): `EPSS scores are provided by FIRST (Forum of
  Incident Response and Security Teams, Inc.), https://www.first.org/epss`
  on the `first_epss` snapshot terms (license stays `NOASSERTION`) and on
  `security-intelligence-source-coverage.md` and the
  vulnerability-scanner read contract. Wording approval only, not legal
  clearance.

## Transport Build No-Regression Note (Core Commit)

- No-Regression Evidence: `BenchmarkEmit` (claim emit path touched by
  `request.go`) baseline at `52e0cbae93` vs after (this commit), both
  `-benchtime 100x -count 3`, `go1.26.10 linux/arm64`, unit fixtures, no
  DB: identical allocs per op on every sub-bench (130/55/55/55/55),
  ns/op and B/op within run-to-run noise (e.g. vault_live 9873 vs 9959
  ns/op, 11935 vs 11942 B/op). `BenchmarkInTreeSchedulable` likewise:
  identical allocs (6/16), in-tree_1000_targets 420592-426908 vs
  426242-429541 ns/op (within-run variance exceeds the delta).
  Focused suites green after: `collector`, `extensionhost`,
  `extensionhost/vulnerability`, `sbom/runtime`, `coordinator`,
  `coordinator/vulnerability`, `vulnsource`, `workflow` (`-count=1`),
  plus `sdk/go/factschema/fixturepack`; `go vet` clean on all touched
  packages. Terminal queue/row counts: N/A, no queue change.
- Observability Evidence: no new metric, span, log, or status keys. The
  activation path reuses the in-tree EPSS instruments from #9967
  (`RecordEPSSDerivedPage`, existing log line and counters) and emits
  only when the findings section is enabled; disabled derivations read
  nothing (no CVE read, no progress read) and record nothing, pinned by
  the activation and service tests.
- Why safe: legacy scopes resolve the zero transport without touching
  the run store, and `omitempty` keeps the wire byte-identical on
  non-sharded claims; the enabled path adds two bounded reads per plan
  tick plus pacing that caps fetches at one full finding-set pass per
  FIRST publication day (Decision 5).

## Addendum: read predicate now expiry-aware (2026-11-10, #9805 reader #10)

The `WHERE suppression_state = 'active'` read quoted above (and the same
predicate in the post-deploy QA query's denominator) is superseded:
`listActiveFindingCVEIDsQuery` now selects effectively visible winners —
read-time effective suppression state `NOT IN` the hidden set, with a
hidden state whose `suppression_expires_at` has passed reading `expired`.
The stored-state read kept an expired suppression's CVE out of derivation
forever while the findings reads reported the row visible. Proof and
before/after numbers live in the census addendum of
`9805-suppression-expiry-and-tick-shim-20261110.md`; rerun the QA coverage
query with the effective-state denominator.
