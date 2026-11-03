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
disabled (the reader is not even called). The store read is one sequential
scan over the small winners table per tick — no new index, no DDL, no
transaction change. The claim dispatch tries configured targets first and
only parses transport for unknown scope ids, so NVD, OSV, and explicit EPSS
claims take the exact code path they took before. The derivation allocates
only the output slices (one per chunk plus one id copy); there is no per-item
retry, lease, or queue change. Scale measured at HEAD `547d8dc55a` through the production
planner: 447 distinct CVEs → 5 targets / 5 FIRST requests (max 1699 joined
chars), 2235 → 23 / 23, 4470 → 45 / 45, matching the `ceil(n/100)` budget
with margin under the 2000-char cap for real-world id lengths. `go test`
on `go/internal/coordinator/...`, `go/internal/workflow/`,
`go/internal/collector/vulnerabilityintelligence/...`,
`go/cmd/collector-vulnerability-intelligence/`, and
`go/cmd/workflow-coordinator/` exits 0; the postgres package passes except
the pre-existing `ESHU_DEPLOY_CHECKOUT`-gated schema test, which is
untouched by this diff and fails for a missing external checkout.

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
