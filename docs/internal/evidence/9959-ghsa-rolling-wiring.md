# GHSA rolling wiring: performance and observability evidence (#9959)

No-Regression Evidence: baseline `origin/main` at `7a792e80e6` (GHSA planner
unreachable from configuration); after this change at the branch head the
added work is admission/planning-only: document refusal is O(targets) over
the instance's own small bounded config document, the planner guard is one
O(targets) scan beside the existing anchor scan, and per vuln-intel instance
tick the wiring adds one config validate+unmarshal in the same FromConfig
pattern as the owned/installed/findings/NVD sections. Backend/version: no
SQL, Cypher, schema DDL, or graph-write change; the pinned Neo4j backend
and the pre-existing PK cursor store are untouched (cursor advance/commit
transaction unchanged). Input shape: collector config documents with at
most a handful of targets and two GHSA advisory types (reviewed, malware).
With the section disabled (the default), no cursor query runs — pinned by
the disabled-skips-reader service test (reader calls = 0); enabled adds two
primary-key cursor reads per instance tick. Terminal shape: unit plans
assert exact item counts (one deterministic write-once window scope per
advisory type, anchor dropped planned or held, no static fall-through), so
queue cardinality changes only by the intended window scopes.

Observability Evidence: the plan span and
`IntelligenceDerivationObservation` report `ghsa_window_planned` /
`ghsa_window_held` as counts only (never bounds, scope ids, or configured
values); refusal and guard errors are fixed strings and the planner-guard
path is fatal-loud (reconcile records `reconcile_error`, the coordinator
logs and exits), so an operator sees the refusal at startup/reconcile with
the instance id. No new metric, span, or log key is introduced; existing
signals diagnose the new path.

Why safe: GHSA stays not activatable pending #10465 S6, the section is off
by default, and the golden-vector window math is untouched
(`ghsa_window_golden.json` green on both sides in the focused suites).
