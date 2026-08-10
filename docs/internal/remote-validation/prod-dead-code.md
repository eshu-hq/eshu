# prod-dead-code — production validation

Validation-Slug: prod-dead-code
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: code_quality.dead_code passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.
B12-Assertion: code_quality.dead_code -> mcp:find_dead_code

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


Capability: `code_quality.dead_code` (tools `find_dead_code`,
`investigate_dead_code`, `find_cross_repo_dead_code`).
Production profile: `required_runtime: deployed_services`,
`max_scope_size: multi_repo_platform`, `p95_latency_ms: 7000`,
`max_truth_level: derived`.

## Claim validated

Graph-backed dead-code candidate scan with partial root modeling and language
maturity metadata; the cross-repo workflow classifies deterministic consumer
evidence and returns ambiguous or stale ownership as `unknown` rather than a
false negative/positive. Exact promotion (vs. `derived`) remains gated on
broader roots and full reachability modeling, which this artifact does not
claim.

## Committed reproducible evidence

**Cross-repo classification and scan contract** —
`go/internal/query/code_dead_code_cross_repo_test.go` and
`go/internal/query/code_dead_code_scan_test.go` cover the graph-backed
candidate scan, ambiguous/stale-ownership handling, and root modeling.
Reproduce:

```bash
cd go && go test ./internal/query -run TestHandleDeadCode -count=1
cd go && go test ./internal/query -run 'CrossRepoDeadCode' -count=1
```

**Language-maturity and root-modeling coverage** —
`go/internal/query/code_dead_code_language_maturity_test.go` and the
per-language root files (e.g. `code_dead_code_go_roots_test.go`,
`code_dead_code_python_roots_test.go`) prove the declared partial root
modeling per language.

**Full-stack Docker Compose reachability run** —
`scripts/verify_graph_analysis_compose.sh` seeds a fixture repository, runs
the collector/reducer pipeline, and asserts a dead-code candidate response
(`DEAD_CODE_FILE`) against a live Compose stack. Reproduce (requires Docker
Compose):

```bash
scripts/verify_graph_analysis_compose.sh
```

## Notes

No private data: cited tests and the Compose fixture use synthetic
repositories; no production credentials or deployment-specific values appear
in this artifact.

Related: #5407 (artifact-existence gate), #5552 (burn-down).
