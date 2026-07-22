# prod-dead-code — production validation

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
