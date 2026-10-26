# Internal → Public Shim Map (`#8852`, analysis artifact)

This is a **design map, not an implementation**. It records how each
in-tree seam becomes a public SDK exchange at extraction, so the
`#6710` template repository and the `#4047` parity gate share one
interpretation. Nothing here changes runtime behavior.

## 1. Claim path: `ClaimedSource` / `NextClaimed` → SDK `Result` / `Claim`

There is no unclaimed poll path for live AWS truth: `ClaimedSource.NextClaimed`
(`awsruntime/source.go:48`) is the only live emission entry. It validates the
claim (`source.go:251-280`), resolves one authorized
`(account, region, service_kind)` target by `AcceptanceUnitID`
(`source.go:282-312`), acquires the per-account slot, assumes claim-scoped
credentials, and reuses `item.GenerationID` (= `item.SourceRunID`, enforced at
`source.go:267-269`) so retries are idempotent. (Fixture and cassette modes in
`go/cmd/collector-aws-cloud/service.go:40,64` are credential-free replay twins;
they are not part of the live claim boundary.)

| In-tree outcome | SDK `ResultState` | Host action |
| --- | --- | --- |
| envelopes committed (scan success) | `complete` | validate facts, complete claim |
| credential acquisition fails (`source.go:86-106`) → `assumerole_failed` warning envelope + `credential_failed` status | `complete` with warning (`assumerole_failed` `aws_warning`) | validate facts, complete claim (generation observed no provider truth, so it stays hint-less and always stores, `source.go:131-135`) |
| scan success with `budget_exhausted` / `throttle_sustained` / `organizations_org_access_skipped` warning facts | `partial` + `Status{warning}` + `aws_warning` facts | commit reachable evidence |
| scan success, unchanged content (`awsFreshnessHint`, `source.go:135`) | `complete` (duplicate-safe; store skips the duplicate, `#8003`) | complete, no writes |
| scan error: `AccessDenied*` / `Unauthorized*` / `ForbiddenException` → `permission_denied` (`scan_status.go:152-165`) | `terminal` | terminal-fail claim |
| scan error: `UnsupportedOperation*` → `unsupported_permission` (`scan_status.go:152-165`) | `terminal` | terminal-fail claim |
| scan error: stale-fence status rejection (`ErrScanStatusStaleFence`, `scan_status.go:86-101`, start/observe/commit phases) | `terminal` | terminal-fail claim (stops the `#612` orphaned-row retry loop) |
| any other scan error (transport, throttling past SDK budget) | `retryable` | release claim, bounded retry |
| cancellation | (no result) | stop, no commit |

Duplicate delivery stays safe: stable keys are deterministic
(`facts.StableID` over account/region/identity tuples, `envelope.go`,
`*_envelope.go`), `FactID` folds kind + stable key + scope + generation
(`envelope.go:268`), and generation-scoped commit is idempotent at the core
boundary. `AllowEmptySuccessor` (per-target `config.go:213` and instance-wide
`awsruntime/types.go:56`, stamped as `TrustedDeletion` at
`source.go:374`) travels as an explicit operator attestation on the
generation, never as producer-stored state.

| In-tree field | SDK `Claim` |
| --- | --- |
| `item.CollectorKind` (`scope.CollectorAWS`) | `collector_kind` |
| `item.SourceSystem` (`scope.CollectorAWS`) | `source_system` |
| `item.CollectorInstanceID` vs `Config.CollectorInstanceID` | `instance_id` (mismatch rejects, `source.go:261-263`) |
| `item.ScopeID` (`"aws:<account>:<region>:<service>"`, `freshness/types.go:157`) | `scope.id` |
| `item.GenerationID` (= `SourceRunID`) | `generation.id` (reused on retry) |
| `item.CurrentFencingToken` / claim target fencing token | `fencing_token` |
| `item.Attempt`, deadline, `WorkItemID` | `attempt`, `deadline`, `work_item_id` |
| per-target scan config (allowed regions/services, credential routing, empty-successor attestation) | `config_handle` (opaque; resolved host-side) |

Stale-claim rule (core-owned): the workflow control store, not the
producer, decides lease expiry and supersession; checkpoints expire per
generation (`source.go:139-148`, `checkpoint/types.go`), and scan-status
mutations reject stale fences (`awscloud.ErrScanStatusStaleFence`). The
producer must treat a claim it did not `.Claim()` as foreign and never write
for it.

## 2. The 13 `aws` / posture kinds at `1.0.0` → SDK `FactDeclaration`

One declaration per kind, each `schema_versions: ["1.0.0"]`. Producer
records already bind all 13 to `collector-aws-cloud` with
`AuthorityMode: core_issued_grant` and
`PlannedRepository: eshu-hq/eshu-collector-aws-cloud`:

```json
[
  {"kind": "aws_resource", "schema_versions": ["1.0.0"],
   "source_confidence": ["reported"]},
  {"kind": "aws_relationship", "schema_versions": ["1.0.0"],
   "source_confidence": ["reported"]},
  {"kind": "aws_tag_observation", "schema_versions": ["1.0.0"],
   "source_confidence": ["reported"]},
  {"kind": "aws_dns_record", "schema_versions": ["1.0.0"],
   "source_confidence": ["reported"]},
  {"kind": "aws_image_reference", "schema_versions": ["1.0.0"],
   "source_confidence": ["reported"]},
  {"kind": "aws_security_group_rule", "schema_versions": ["1.0.0"],
   "source_confidence": ["reported"]},
  {"kind": "aws_iam_permission", "schema_versions": ["1.0.0"],
   "source_confidence": ["reported"]},
  {"kind": "aws_resource_policy_permission", "schema_versions": ["1.0.0"],
   "source_confidence": ["reported"]},
  {"kind": "aws_warning", "schema_versions": ["1.0.0"],
   "source_confidence": ["reported"]},
  {"kind": "ec2_instance_posture", "schema_versions": ["1.0.0"],
   "source_confidence": ["reported"]},
  {"kind": "rds_instance_posture", "schema_versions": ["1.0.0"],
   "source_confidence": ["reported"]},
  {"kind": "s3_bucket_posture", "schema_versions": ["1.0.0"],
   "source_confidence": ["reported"]},
  {"kind": "s3_external_principal_grant", "schema_versions": ["1.0.0"],
   "source_confidence": ["reported"]}
]
```

Kind/version sources: `go/internal/facts/cloud/aws.go` (9-kind `aws`
family, `AWSFamilyDeclaration`), `ec2_instance_posture.go`,
`rds_posture.go`, `s3_bucket_posture.go`, `s3_external_principal_grant.go`.
Envelope constructors: `NewResourceEnvelope`, `NewRelationshipEnvelope`,
`NewImageReferenceEnvelope`, `NewWarningEnvelope` (`envelope.go`);
`NewDNSRecordEnvelope` (`dns_envelope.go`);
`NewSecurityGroupRuleEnvelope`-family via `security_group_rule_envelope.go:90`;
`NewIAMPermissionEnvelope` (`iam_permission_envelope.go:32`);
`NewResourcePolicyPermissionEnvelope`
(`resource_policy_permission_envelope.go:33`);
`NewEC2InstancePostureEnvelope` (`ec2_posture_envelope.go:27`);
`NewRDSInstancePostureEnvelope` (`posture_envelope.go:26`);
`NewS3BucketPostureEnvelope` (`s3_posture_envelope.go:76`);
`NewS3ExternalPrincipalGrantEnvelope`
(`s3_external_principal_grant_envelope.go:74`).

SDK `Fact` mapping: `stable_key` ← envelope stable key,
`source_confidence` ← `reported` above,
`source_ref.scope_id` ← claim `scope_id` (`"aws:<account>:<region>:<service>"`),
`source_ref.generation_id` ← committed generation, `payload` ← typed
factschema payload (`EncodeAWS*`), `redactions` ← versioned redaction
markers (`RedactionPolicyVersion = "aws-launch-2026-05-14"`,
`redaction.go:12`; fingerprints only, never raw values).

## 3. `TargetConfig` JSON (operator shape, unchanged by extraction)

`awsRuntimeConfiguration` (`go/cmd/collector-aws-cloud/config.go:34-47`)
`target_scopes[]` keys: `account_id` (12 digits, must match the
`role_arn` account for `central_assume_role`), `allowed_regions`
(required, no wildcards/empties), `allowed_services` (required,
registry-checked via `SupportsServiceKind`, no wildcards),
`max_concurrent_claims`, `credentials` (`mode` ∈
`central_assume_role | local_workload_identity`, `role_arn`,
`external_id` — static access keys are refused at `config.go:223-227`),
`allow_empty_successor` (defaults false; also settable instance-wide via
`ESHU_AWS_ALLOW_EMPTY_SUCCESSOR`). Static credential material never
appears in configuration: `central_assume_role` carries only routing
(role ARN + external ID), `local_workload_identity` carries neither.
Freshness triggers validate the same triple without wildcards
(`freshness/types.go:80-113`); `ScopeID` / `AcceptanceUnitID`
(`freshness/types.go:156-177`) are the JSON the claim runtime parses at
`awsruntime/source.go:314`.

## 4. Telemetry the external producer must preserve

| Signal | Name | Emitted at |
| --- | --- | --- |
| span | `aws.collector.claim.process` (`telemetry/contract.go:717`) | `awsruntime/source.go:64` |
| span | `aws.credentials.assume_role` (`telemetry/contract.go:718`) | `awsruntime/source.go:157` |
| span | `aws.service.scan` (`telemetry/contract.go:719`) | `awsruntime/source.go:172` |
| span | `aws.service.pagination.page` (`telemetry/contract.go:720`) | scanner pagination |
| counter | `eshu_dp_aws_api_calls_total` (`AWSAPICalls`) | per API call |
| counter | `eshu_dp_aws_throttle_total` (`AWSThrottles`) | per throttled call |
| counter | `eshu_dp_aws_assumerole_failed_total` (`AWSAssumeRoleFailed`) | `source.go:242` |
| counter | `eshu_dp_aws_budget_exhausted_total` (`AWSBudgetExhausted`) | `scan_status.go:198` |
| counter | `eshu_dp_aws_pagination_checkpoint_events_total` (`AWSCheckpointEvents`) | checkpoint save/expire |
| counter | `eshu_dp_aws_resources_emitted_total` (`AWSResourcesEmitted`) | `source.go:204` |
| counter | `eshu_dp_aws_relationships_emitted_total` (`AWSRelationshipsEmitted`) | `source.go:204` |
| counter | `eshu_dp_aws_tag_observations_emitted_total` (`AWSTagObservationsEmitted`) | `source.go:204` |
| counter | `eshu_dp_aws_freshness_events_total` (`AWSFreshnessEvents`) | freshness trigger ingest |
| counter | `eshu_dp_aws_org_access_skipped_total` (`AWSOrgAccessSkipped`) | `scan_status.go:205` |
| counter | `eshu_dp_aws_scan_status_stale_fence_total` (`AWSScanStatusStaleFence`, `operation` ∈ start/observe/commit) | `scan_status.go:103` |
| counter | `eshu_dp_aws_relationship_edges_total` (`AWSRelationshipEdges`) | reducer projection |
| counter | `eshu_dp_aws_cloud_image_edges_total` (`AWSCloudImageEdges`) | reducer projection (`#5450`) |
| histogram | `eshu_dp_aws_scan_duration_seconds` (`AWSScanDuration`) | `source.go:177` |
| gauge | `eshu_dp_aws_claim_concurrency` (`AWSClaimConcurrency`, per account) | `service.go:106` |

Attributes stay `service`, `account`, `region`, `operation`, `reason`,
`result`, `resource_type`. ARNs, account emails/names, secret values,
resource names, tags, and request IDs must never appear in metric
labels (`awscloud/AGENTS.md` invariants; scan-status rows sanitize via
`SanitizeScanStatusMessage`, `scan_status.go:74`). The host additionally
records grant decisions per emission (`#6709`).

No-Observability-Change: this PREP adds no signals; the counters,
histogram, gauge, and four spans above already cover the claimed,
credential, scan, checkpoint, freshness, and status paths. The external
producer must preserve them verbatim.

## 5. Known deltas (in-tree vs external, by design)

1. **Grant check**: in-tree needs none (first-party, same binary).
   External fails closed on missing/expired/revoked grant at install,
   activation, and every emission. See
   [PRODUCER_GRANT_DRAFT](PRODUCER_GRANT_DRAFT.md).
2. **Commit boundary**: in-tree commits through the claimed-service
   ingestion store plus the scan-status committer
   (`service.go:93-146`, `status_committer.go`). External returns facts;
   core commits.
3. **Claim control**: in-tree `ClaimedService` owns lease/heartbeat plus
   the per-account limiter (`service.go:105-107`). External receives a
   bounded `Claim` and returns a `Result`; transitions stay
   `eshu-workflow`.
4. **Durable state that stays core-owned**: pagination checkpoints
   (`checkpoint/`, `ExpireStale` per generation), `aws_scan_status`
   rows, freshness triggers (`freshness/`), and the coordinator plan.
   The external producer receives no Postgres, graph, queue, lease,
   reducer, API, MCP, or workflow handles.
5. **FactID construction**: the host builds the envelope, so `FactID`
   values change by construction. Parity is judged on kind, stable key,
   payload, scope, and generation first, then on reducer, API, and MCP
   read truth (arbiter ruling on `#8852`).
6. **Credential acquisition**: in-tree resolves credentials inside the
   claim (`SDKCredentialProvider`, STS AssumeRole or workload identity).
   The external producer receives claim-scoped credential routing in
   `config_handle`; raw credential material never crosses the boundary.
