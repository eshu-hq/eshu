# internal/correlation/drift/cloudruntime Agent Rules

This package is deterministic helper Go for the `aws_cloud_runtime_drift` rule
pack. It classifies ARN-keyed AWS/Terraform evidence, builds candidates, and
records bounded admitted-finding metrics. It MUST NOT query Postgres, write
Cypher, publish graph phases, or decide deployment truth.

## Read First

MUST read these before editing:

1. `README.md` and `doc.go`.
2. `classify.go`, `candidate.go`, `telemetry.go`, and focused tests.
3. `../../rules/aws_cloud_runtime_drift_rules.go`.
4. `docs/public/services/collector-aws-cloud.md` and telemetry docs before
   changing observation or counter contracts.

## Local Invariants

- ARN is the primary join key and `BuildCandidates` MUST sort by ARN.
- `Classify` is exclusive. Cloud-only is orphaned; cloud plus state with no
  config is unmanaged; cloud plus state plus config emits no candidate.
- Unknown and ambiguous findings may be supplied by caller evidence and MUST
  remain provenance/status signals, not invented deployment truth.
- Every candidate MUST carry `EvidenceTypeCloudResourceARN`; the rule pack
  structural gate depends on it.
- Management status, missing evidence, warning flags, and raw tags are evidence
  atoms only. Do not put ARN, account ID, address, tag key, or tag value in
  metric labels.
- Raw AWS tags MUST NOT be normalized into environment, platform, or service
  truth in this package.

## Change Rules

- New finding kind: update classifier/candidate evidence, telemetry summary,
  tests, rule/docs, and cardinality proof.
- Candidate evidence changes MUST keep
  `rules.AWSCloudRuntimeDriftRulePack` selectors aligned.
- Reducer loader or graph publication wiring belongs outside this package.
- Metric label changes require telemetry contract updates and operator-facing
  proof.

## Proof

Run the focused gate for any edit:

```bash
cd go
go test ./internal/correlation/drift/cloudruntime -count=1
go vet ./internal/correlation/drift/cloudruntime
go doc ./internal/correlation/drift/cloudruntime
```

Classifier or admission-shape changes require
`go test ./internal/correlation/... -count=1` and correlation truth proof.
Docs-only edits also need the package-doc verifier for this directory and
`git diff --check`.
