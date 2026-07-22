# prod-aws-runtime-drift-read-model — production validation

This proof-ID is cited by five production rows sharing the same reducer-owned AWS runtime drift
read model:

- `iac_management.find_unmanaged_resources` (tool `find_unmanaged_resources`)
- `iac_management.get_status` (tool `get_iac_management_status`)
- `iac_management.explain_status` (tool `explain_iac_management_status`)
- `iac_management.propose_terraform_import_plan` (tool `propose_terraform_import_plan`)
- `aws_runtime_drift.findings.list` (tool `list_aws_runtime_drift_findings`)

Production profile (shared shape): `required_runtime: deployed_services`,
`max_scope_size: aws_account_or_scope[_plus_arn]`, `p95_latency_ms: 1000`-`5000`,
`max_truth_level: derived`.

## Claim validated

Bounded active-generation reads over reducer-materialized AWS runtime drift facts, using the
issue 124 `IaCManagementFinding` taxonomy (exact/derived/ambiguous/stale/unknown outcomes and
rejected promotion status), including a security-review gate that refuses safety-gated findings
and never runs Terraform.

## Committed reproducible evidence

**Findings list (outcomes, taxonomy, scope enforcement)** — `go/internal/query/aws_runtime_drift_test.go`:
`TestHandleAWSRuntimeDriftFindingsReturnsOutcomes`,
`TestHandleAWSRuntimeDriftFindingsScopedAccountOnlyNeverCallsStore`,
`TestHandleAWSRuntimeDriftFindingsScopedOutOfGrantScopeNeverCallsStore`,
`TestHandleAWSRuntimeDriftFindingsScopedInGrantReturnsRealRowData`, and
`TestHandleAWSRuntimeDriftFindingsRequiresBoundedScope`. Reproduce:

```bash
cd go && go test ./internal/query -run TestHandleAWSRuntimeDriftFindings -count=1
```

**Unmanaged-resource status (`find_unmanaged_resources`)** — `go/internal/query/iac_management_test.go`:
`TestHandleUnmanagedCloudResourcesRequiresBoundedScope`,
`TestHandleUnmanagedCloudResourcesRejectsWildcardAccountScope`,
`TestHandleUnmanagedCloudResourcesReturnsMaterializedFindings`, and
`TestHandleUnmanagedCloudResourcesDefaultsToActionableAWSFindingKinds`. Reproduce:

```bash
cd go && go test ./internal/query -run TestHandleUnmanagedCloudResources -count=1
```

**Exact-ARN status and evidence grouping (`get_status`/`explain_status`)** — same file:
`TestHandleIaCManagementStatusReturnsExactARNStatus` and
`TestHandleIaCManagementExplanationGroupsEvidence`; taxonomy coverage in
`go/internal/query/iac_management_status_test.go`: `TestDeriveIaCManagementStatusCoversTaxonomy`
and `TestAWSRuntimeDriftRowToIaCManagementExpandsReadModelFields`. Reproduce:

```bash
cd go && go test ./internal/query -run "TestHandleIaCManagementStatus|TestHandleIaCManagementExplanation|TestDeriveIaCManagementStatusCoversTaxonomy" -count=1
```

**Terraform import plan (refuses Terraform execution, refuses unsafe findings)** —
`go/internal/query/iac_import_plan_test.go`:
`TestHandleTerraformImportPlanCandidatesReturnsSafeS3Candidate`,
`TestHandleTerraformImportPlanCandidatesReturnsSafeLambdaCandidate`,
`TestHandleTerraformImportPlanCandidatesRejectsProviderResourceID`, and
`TestHandleTerraformImportPlanCandidatesRefusesSensitiveFinding`. Reproduce:

```bash
cd go && go test ./internal/query -run TestHandleTerraformImportPlanCandidates -count=1
```

**Security-review gate** — `go/internal/query/iac_management_safety_test.go`:
`TestAWSRuntimeDriftRowToIaCManagementRedactsSensitiveEvidenceValues`,
`TestHandleIaCManagementStatusCarriesSecurityReviewGate`, and
`TestIaCManagementSafetySummaryCountsReviewAndRedactions`. Reproduce:

```bash
cd go && go test ./internal/query -run TestIaCManagementSafety -count=1
cd go && go test ./internal/query -run TestAWSRuntimeDriftRowToIaCManagementRedacts -count=1
```

**Scoped-token route family enforcement** — `go/internal/query/auth_scoped_iac_replatforming_grant_test.go`:
`TestIaCManagementFamilyRoutesFilterByScopeGrant`,
`TestIaCManagementFamilyRoutesEmptyGrantShortCircuits`,
`TestIaCManagementStatusRoutesEnforceScopeGrant`, and
`TestIaCManagementFamilyRoutesUnscopedCallerUnaffected`. Reproduce:

```bash
cd go && go test ./internal/query -run TestIaCManagementFamilyRoutes -count=1
cd go && go test ./internal/query -run TestIaCManagementStatusRoutesEnforceScopeGrant -count=1
```

**Deployed Docker Compose driver over the reducer read model** —
`scripts/verify_aws_runtime_drift_compose.sh` drives the AWS runtime drift collector/reducer
pipeline against a live Docker Compose stack, exercising the `aws-runtime-drift-read-model`
runtime the production profile names for all five capabilities above. Reproduce (requires a
local Docker Compose stack):

```bash
scripts/verify_aws_runtime_drift_compose.sh
```

**Contract declaration** — `go/internal/query/openapi_aws_runtime_drift_test.go`:
`TestOpenAPISpecIncludesAWSRuntimeDriftFindings` and `go/internal/query/openapi_iac_safety_test.go`:
`TestOpenAPIIaCManagementSafetyGateFields`. Reproduce:

```bash
cd go && go test ./internal/query -run "TestOpenAPISpecIncludesAWSRuntimeDriftFindings|TestOpenAPIIaCManagementSafetyGateFields" -count=1
```

## Notes

No private data: cited tests use synthetic AWS ARNs and account scopes; the compose driver
reads only local fixture-derived Terraform state, never live cloud accounts or credentials.

Related: #5552 (burn-down).
