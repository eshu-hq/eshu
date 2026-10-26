# AWS Cloud Collector — Extraction Prep Census (`#8852`)

Status: **PREP ONLY**. No code moves, no deploy, no in-tree removal. This
manifest records the current in-tree truth so the cutover gate (`#4047`)
can compare in-tree and external outputs on identical inputs.

Scope: `go/cmd/collector-aws-cloud/` and
`go/internal/collector/awscloud/` including `catalog/`, `service/`,
`awsruntime/`, `checkpoint/`, `freshness/`, `recordpolicy/`. Out of scope:
`sdk/go` (`#8851`), all other collectors, org repos, deployment values.
Companion drafts already in-tree: [PRODUCER_GRANT_DRAFT](PRODUCER_GRANT_DRAFT.md),
[SHIM_MAP](SHIM_MAP.md), [ROLLBACK_DRAFT](ROLLBACK_DRAFT.md). The cutover
plan follows the Jira lane (`#8858`): one activation per in-tree scope, new
instance id, same scope id, in-tree producer disabled in the same values
change.

## 1. Current packages

### Binary: `go/cmd/collector-aws-cloud/`

| File | Role | Key symbol |
| --- | --- | --- |
| `main.go:34,82,124,197` | entry point, `-mode=fixture\|claimed-live\|cassette\|record` | `parseArgs`, `run`, `buildRuntimeRunner` |
| `config.go:59` | env config, `ESHU_COLLECTOR_INSTANCES_JSON` shape | `loadRuntimeConfig`, `selectAWSInstance`, `mapTargetScope` |
| `config.go:143` | instance validation | `validateAWSInstance` (requires `CollectorKind "aws"`, enabled, claims-enabled) |
| `config.go:44,181` | claim-scoped target config load | `awsTargetScopeConfiguration`, `mapTargetScope` |
| `service.go:40` | fixture wiring | `buildCollectorService` |
| `service.go:69` | cassette replay wiring | `buildCassetteService` |
| `service.go:93` | claimed wiring | `buildClaimedService` |
| `service.go:148` | claim ID mint | `newClaimID` (`aws-claim-` prefix) |
| `record.go:29,57` | record-mode entry, claimed-live wiring minus stores | `runRecord`, `buildRecordSource` |
| `status_committer.go:34,59` | commit-outcome scan-status bridge | `newAWSStatusCommitter`, `CommitClaimedScopeGeneration` |
| `status_committer.go:146` | claim metadata to status boundary | `boundary` |
| `fixture_config.go:58` | offline fixture estate load | `loadFixtureConfig` |
| `doc.go` | package contract | — |

No unclaimed poll mode exists for live truth: the binary runs claimed-live,
fixture, cassette, or record. Target credentials resolve through
`target_scopes[].credentials` (`config.go:50-57`); secrets travel by env
reference, never inline.

### Library: `go/internal/collector/awscloud/`

| File | Role | Key symbol |
| --- | --- | --- |
| `types.go:9` | durable collector kind | `CollectorKind = "aws"` |
| `types.go:13` | shared scope/generation/claim identity | `Boundary` |
| `types.go:25,40,53,71,115,156,220,255,267,314` | observation contracts | `ResourceObservation` … `WarningObservation` |
| `envelope.go:19,72,125,192` | 4 core envelope constructors | `NewResourceEnvelope` … `NewWarningEnvelope` |
| `envelope.go:232,277` | shared commit + identity | `newEnvelope`, `validateBoundary` |
| `dns_envelope.go:18` | DNS envelope | `NewDNSRecordEnvelope` |
| `posture_envelope.go:26` | RDS posture envelope | `NewRDSInstancePostureEnvelope` |
| `ec2_posture_envelope.go:27` | EC2 posture envelope | `NewEC2InstancePostureEnvelope` |
| `s3_posture_envelope.go:76` | S3 posture envelope | `NewS3BucketPostureEnvelope` (observation `s3_posture_envelope.go:26`) |
| `iam_permission_envelope.go:32` | IAM permission envelope | `NewIAMPermissionEnvelope` |
| `resource_policy_permission_envelope.go:33` | resource-policy permission envelope | `NewResourcePolicyPermissionEnvelope` |
| `s3_external_principal_grant_envelope.go:74` | S3 grant envelope | `NewS3ExternalPrincipalGrantEnvelope` |
| `security_group_rule_envelope.go:90` | SG rule envelope | `NewSecurityGroupRuleEnvelope` |
| `scan_status.go:28,49,53,93,100,118` | scan/commit status vocabulary + rows | `ScanStatusRunning` … `ScanStatusCommit` |
| `apicall.go:16,24,37,54` | bounded per-claim API accounting | `APICallEvent`, `APICallStatsRecorder` |
| `partition.go:14,29,45,55` | ARN partition derivation | `PartitionAWS` …, `PartitionForRegion`, `PartitionFromARN` |
| `redaction.go:13,24,42` | launch redaction policy | `RedactionPolicyVersion`, `RedactString`, `ClassifyStackOutput` |
| `condition_identity.go:6` | conditioned-statement identity | `addConditionSummaryIdentity` |
| `factschema_helpers.go` | SDK encode helpers | `stringValuePtr` …, `awsPayloadAttributes` |
| `extractor.go:68` | intentionally empty typed-depth registry | `AttributeExtraction` (AWS Config lane reserved, nothing registered) |
| `doc.go` | package contract | — |

### `catalog/` (23 production files)

Service-kind and resource-type constants plus per-category grouping:
`compute.go` (`ServiceEC2`), `network.go`, `network_edge.go`,
`storage.go`, `database.go`, `database_specialized.go`, `security.go`,
`security_threat.go`, `messaging.go`, `analytics.go`, `analytics_data.go`,
`analytics_ml.go`, `api.go`, `api_management.go`, `devtools.go`,
`management.go`, `operations.go`, `g1_platform.go`, `g1_services.go`,
`g2_common.go`, `g2_governance.go`, `g2_services.go`, `doc.go`.
`resource_type_contract_test.go` pins catalog tokens to
`eshu-sdk/factschema/aws/v1`.

### `service/` (133 service packages)

One package per AWS service family (`ec2`, `s3`, `iam`, …). Each centers
on `scanner.go` + `types.go` with `runtimebind/bind.go` registration and
`awssdk/` adapters (verified `s3/scanner.go:25`, `ec2/scanner.go`).
Scanners convert SDK structs directly into `ResourceObservation`s and call
the `New*Envelope` constructors; there is no generic tag-observation
builder — tags ride inside the `aws_resource` payload. All scanners obey
the service-kind canonicalization guard
(`servicekind_guard_test.go`).

### `awsruntime/`

| File | Role | Key symbol |
| --- | --- | --- |
| `source.go:29,48` | only live emission entry | `ClaimedSource`, `NextClaimed` |
| `source.go:222,251` | emission counting, claim validation | `recordEmissionCounts`, `validate` |
| `source.go:374` | deletion attestation stamping | `TrustedDeletion: target.AllowEmptySuccessor \|\| …` |
| `types.go:44,60,86,99,117,121,126,129` | runtime contracts | `Config`, `TargetScope`, `Target`, `CredentialProvider`, `ScannerFactory`, `ServiceScanner`, `CheckpointStore`, `ScanStatusStore` |
| `registry.go:37,43,50` | builder dispatch | `SupportedServiceKinds`, `SupportsServiceKind`, `Scanner` |
| `registry_core.go:23,47,54` | registration contract | `ScannerDeps`, `ScannerBuilder`, `ScannerRegistration` |
| `credentials.go:21,24,130` | claim-scoped credentials | `SDKCredentialProvider.Acquire`, `roleSessionName` |
| `limiter.go:12,21,44` | per-account concurrency | `AccountLimiter`, `NewAccountLimiter`, `Acquire` |
| `scan_status.go:120,139,167,251` | status lifecycle + failure classes | `startScanStatus`, `classifyServiceScanError`, `observeScanStatus`, `awsFactStats` |
| `freshness.go:23` | unchanged-content hint | `awsFreshnessHint` |
| `fixture_source.go:29,44,54` | offline replay source | `FixtureSource`, `FixtureConfig`, `FixtureScope` |
| `record_source.go:30,43` | record-mode source | `RecordSource`, `Next` |
| `bindings/bindings.go` | 134 blank-import registrations | one line per service `runtimebind` |
| `doc.go` | package contract | — |

### `checkpoint/`, `freshness/`, `recordpolicy/`, `internal/`

- `checkpoint/types.go:23,35,42,52,61` — claim-fenced pagination resume:
  `Scope`, `Key`, `Checkpoint`, `Store`, `ScopeFromBoundary`.
  Postgres owner: `pagination_checkpoint.go:114,122`
  in `storage/postgres/cloud/aws`
  (`AWSPaginationCheckpointStore`, `NewAWSPaginationCheckpointStore`).
- `freshness/types.go:19,26,41,53,81,157` — EventBridge trigger contract:
  `EventKind`, `TriggerStatus`, `Trigger`, `StoredTrigger`, `Validate`,
  `ScopeID` (`aws:<account>:<region>:<service>`).
  `freshness/eventbridge.go:55` normalizes Config change + CloudTrail API
  events (`NormalizeEventBridge`).
- `recordpolicy/policy.go:20` — record-mode pseudonym table
  (`Policy()`; fail-closed on unclassified keys).
- `internal/partitionguard/`, `internal/relguard/` — AST guards keeping
  scanners on partition/relationship contracts.

Every production file is listed. Failure mapping lives in one place
(`awsruntime/scan_status.go:139-165`); pagination resumes through
claim-fenced checkpoints; `ErrScanStatusStaleFence`
(`scan_status.go:14`, `awsruntime/scan_status.go:54`) makes orphaned-row
rejection terminal.

## 2. Fact kinds and versions

All 13 kinds are `1.0.0`, each producer record already cutover-shaped
(`AuthorityMode: core_issued_grant`,
`PlannedRepository: eshu-hq/eshu-collector-aws-cloud`):

| Kind | Constructor | Producer record |
| --- | --- | --- |
| `aws_resource` | `envelope.go:19` | `cloud/aws.go:138` |
| `aws_relationship` | `envelope.go:72` | `cloud/aws.go:138` |
| `aws_tag_observation` | (no production emitter; tags ride in `aws_resource` payload; counted in `source.go:222`) | `cloud/aws.go:138` |
| `aws_dns_record` | `dns_envelope.go:18` | `cloud/aws.go:138` |
| `aws_image_reference` | `envelope.go:125` | `cloud/aws.go:138` |
| `aws_security_group_rule` | `security_group_rule_envelope.go:90` | `cloud/aws.go:138` |
| `aws_iam_permission` | `iam_permission_envelope.go:32` | `cloud/aws.go:138` |
| `aws_resource_policy_permission` | `resource_policy_permission_envelope.go:33` | `cloud/aws.go:138` |
| `aws_warning` | `envelope.go:192` | `cloud/aws.go:138` |
| `ec2_instance_posture` | `ec2_posture_envelope.go:27` | `cloud/ec2_instance_posture.go:75` |
| `rds_instance_posture` | `posture_envelope.go:26` | `cloud/rds_posture.go:70` |
| `s3_bucket_posture` | `s3_posture_envelope.go:76` | `cloud/s3_bucket_posture.go:71` |
| `s3_external_principal_grant` | `s3_external_principal_grant_envelope.go:74` | `cloud/s3_external_principal_grant.go:70` |

Declared reducer domains and read surfaces (from the same records):
`aws_cloud_runtime_drift` / `GET /api/v0/cloud/inventory` (`aws.go:114-125`);
`ec2_instance_node_materialization` / `GET /api/v0/cloud/resources`
(`ec2_instance_posture.go:59-70`); `rds_posture_materialization` /
`GET /api/v0/cloud/resources` (`rds_posture.go:53-64`);
`s3_internet_exposure_materialization` / `GET /api/v0/cloud/resources`
(`s3_bucket_posture.go:54-65`); `s3_external_principal_grant_materialization` /
`GET /api/v0/secrets-iam/posture-summary`
(`s3_external_principal_grant.go:53-64`). Three `aws`-family kinds
(`aws_dns_record`, `aws_tag_observation`, `aws_warning`) are declared
`Disposition: unconsumed` (`aws.go:127-135`).

Cross-family note: the grant binds identity + kind + schema + scope, so
the extracted `aws-cloud` producer cannot emit under a sibling's
authority; see [PRODUCER_GRANT_DRAFT](PRODUCER_GRANT_DRAFT.md).

## 3. Consumers (must agree at cutover)

- Canonical projection: `aws_resource_materialization.go:39` in `reducer`
  (`aws_resource_materialization`), `reducer/ec2instance/`
  (`ec2_instance_node_materialization`), `registry_additive_domains.go:325` in `reducer`
  (`aws_cloud_runtime_drift`), `go/internal/reducer/s3_external_principal_grant_domain.go`.
- Query/read: `GET /api/v0/cloud/inventory`, `GET /api/v0/cloud/resources`,
  `GET /api/v0/secrets-iam/posture-summary` (fact-registry `ReadSurface`s).
- Coordinator: `aws_scheduled_service.go:16` in `coordinator`
  `scheduleAWSScheduledWork` and
  `service_aws_freshness.go:77` in `coordinator`
  `scheduleAWSFreshnessWork` plan claim rows per active instance via
  `planner.go:43` in `coordinator/planner/aws/scheduled`
  `PlanAWSScheduledWork`; stays core-owned.
- Entrypoint contract: **gap** — `go/internal/collector/entrypoints/collector_entrypoints.yaml`
  covers only `collector-pagerduty` and `collector-jira`; no
  `collector-aws-cloud` entry exists yet. Onboarding it is a cutover
  prerequisite (§8). The claim-path shim is mapped in [SHIM_MAP](SHIM_MAP.md).
- Offline parity input: `go/cmd/collector-aws-cloud/testdata/fixture-estate.json`
  (fixture mode). No recorded parity pack exists yet for the cutover
  comparison (see §8).

## 4. Durable-state owner

The collector owns **no** durable state. Durable writes stay core-owned:

- Fact commit: `postgres.NewIngestionStore` (via `service.go:109`
  claimed wiring, wrapped by `status_committer.go:34`).
- Claim lifecycle: `collector.ClaimedService` (`service.go:116-145`).
- Pagination resume: `AWSPaginationCheckpointStore`
  (`pagination_checkpoint.go:114`), claim-fenced per
  (`checkpoint/types.go:20-31`).
- Scan observability: `AWSScanStatusStore` (`scan_status.go:221`).
- In-tree transient state only: per-claim `APICallStatsRecorder`
  (`apicall.go:43`), `awsEnvelopeStats` (`awsruntime/scan_status.go:239`);
  both reset per claim and never persist. `AllowEmptySuccessor`
  (`config.go:44`, `awsruntime/types.go:47`) is an explicit operator
  attestation stamped onto the generation (`source.go:374`), not stored
  state.

Admission, persistence, reducer queue, projection, and retraction remain
Eshu core per the extraction policy. The external producer must submit a
bounded result; it never receives Postgres, graph, queue, lease, reducer,
API, MCP, or workflow handles.

## 5. Protocols

- AWS SDK v2 with adaptive retry (`credentials.go:24-53`); credential
  modes are local workload identity or central assume-role with
  claim-scoped session names (`roleSessionName`).
- Per-account claim concurrency via `AccountLimiter` (`limiter.go:21,44`).
- Claim validation resolves one authorized
  `(account, region, service_kind)` target by `AcceptanceUnitID`
  (`source.go:251-312` per [SHIM_MAP](SHIM_MAP.md)); retries reuse
  `item.GenerationID = item.SourceRunID`, so they are idempotent.
- EventBridge freshness: Config change + CloudTrail API signals normalized
  by `freshness/eventbridge.go:55`, coalesced on
  `account:region:service_kind` (`freshness/types.go:148-160`).
- Auth: AWS IAM (STS assume-role); facts carry metadata-only identifiers,
  never secret values. Sensitive launch values are redacted through
  `RedactString` (`redaction.go:24`); scan-status rows are scrubbed by
  `SanitizeScanStatusMessage` (`scan_status.go:76`).
- Target wire shape: `ESHU_COLLECTOR_INSTANCES_JSON` instance
  `Configuration.target_scopes[]` (`config.go:34-48`); runtime shape
  `TargetScope` (`awsruntime/types.go:60`). Secrets travel by env
  reference, never inline.
- Record mode: one credentialed pass writes a pseudonymized canonical
  cassette (`record.go:29-49`), classified by `recordpolicy/policy.go:20`
  (fail closed).
- Bounded retries with `APICallStats` accounting and sustained-throttle /
  budget-exhausted warnings; generation lag observed via the scan-status
  observe path (`awsruntime/scan_status.go:167`).

## 6. Artifacts

- Today: single in-tree binary `collector-aws-cloud`
  (`main.go:49`). No published artifact from this PREP.
- Satellite: **no `eshu-hq/eshu-collector-aws-cloud` repository exists
  yet** (verified `gh repo view` resolves nothing). Creating it and the
  `v0.1.0` release is a cutover prerequisite.
- At cutover: digest-pinned OCI artifact executed via the
  component-extension host `process` runner with the seeded binary and
  manifest. No artifact is built or published by this PREP.

## 7. Migration stage

`planned` moving toward `ready_for_cutover`: grant, shim, and rollback
drafts exist; remaining gates are the satellite repository + release,
SDK-path reconcile, parity proof (§3, §8), producer delegation (`#6709`),
activation template (`#6710`, `#8894`), rollback rehearsal, entrypoints
onboarding, and the per-collector cutover gate (`#4047`). Roadmap owners:
`#6707` inventory, `#6708` SDK/fact-schema, `#6709` producer delegation,
`#6710` template and compatibility harness, `#4047` per-collector
cutover gate.

## 8. Prerequisites for cutover

1. Satellite `eshu-hq/eshu-collector-aws-cloud` repository created and
   `v0.1.0` released at supported SDK pins (never `latest`, no workspace
   replaces).
2. First-party producer delegation live (`#6709`); grant drafts recorded
   in [PRODUCER_GRANT_DRAFT](PRODUCER_GRANT_DRAFT.md).
3. Activation entry from the `#6710`/`#8894` template with the
   compatibility harness matrix (newest collector × oldest core and
   reverse).
4. Parity proof on representative inputs: record a cassette via
   `-mode=record` (`record.go:29`) and diff in-tree vs external output on
   the same inputs (AC pattern from `#8863`; `#4047` gate).
5. Producer authority single-holder enforcement and rehearsed rollback
   ([ROLLBACK_DRAFT](ROLLBACK_DRAFT.md)). Cutover order: Jira (`#8858`)
   first, then PagerDuty, then AWS — new instance id, same scope id
   (`aws:<account>:<region>:<service_kind>`), in-tree producer disabled
   in the same values change.
6. Internal→public shim map agreed ([SHIM_MAP](SHIM_MAP.md)).
7. `collector-aws-cloud` onboarded to
   `go/internal/collector/entrypoints/collector_entrypoints.yaml` (only
   pagerduty + jira entries exist today).
