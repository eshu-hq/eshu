# AWS Cloud Runtime

## Purpose

`internal/collector/awscloud/awsruntime` adapts AWS service scanners to the
workflow-claimed collector runtime. It parses `(account_id, region,
service_kind)` claim targets, authorizes them against configured target scopes,
acquires claim-scoped credentials, records durable scanner-side status, and
returns collected generations for the shared collector commit path.

## Ownership boundary

This package owns claim parsing, target authorization, credential lease
coordination, per-account concurrency, production scanner selection, AWS
pagination checkpoint invalidation, and scanner-side scan-status updates. It
does not own AWS service response mapping, fact-envelope identity, workflow row
persistence, commit-side status updates, graph writes, reducer admission, or
query behavior.

```mermaid
flowchart LR
  A["workflow.WorkItem"] --> B["ClaimedSource.NextClaimed"]
  B --> C["Target authorization"]
  C --> D["AccountLimiter.Acquire"]
  D --> E["CredentialProvider.Acquire"]
  B --> I["CheckpointStore.ExpireStale"]
  E --> H["DefaultScannerFactory.Scanner"]
  H --> F["ServiceScanner.Scan"]
  F --> G["collector.CollectedGeneration"]
```

## Exported surface

See `doc.go` for the godoc contract.

- `Config` - collector instance and target-scope authorization configuration.
- `TargetScope` - account, allowed regions, allowed services, and credential
  routing.
- `AccountLimiter` - in-process per-account claim limiter and concurrency
  observer.
- `CredentialConfig` - non-secret credential mode, role ARN, and external ID.
  Command config validation requires central AssumeRole scopes to carry both a
  same-account role ARN and an external ID; local workload identity scopes must
  not carry AssumeRole routing fields.
- `Target` - one authorized AWS claim target.
- `CredentialProvider` - acquires a claim-scoped credential lease.
- `CredentialLease` - releases temporary credential material after a scan.
- `AWSConfigLease` - exposes claim-scoped AWS SDK configuration to service
  adapters.
- `SDKCredentialProvider` - production credential provider using workload
  identity or STS AssumeRole.
- `DefaultScannerFactory` - production scanner dispatcher. It holds the
  runtime-wide tracer, instruments, checkpoint store, and redaction key and
  dispatches every claim through the init-time scanner registry. It has no
  compile-time dependency on individual service packages.
- `Register`, `LookupBuilder`, `RegisteredServiceKinds` - the scanner
  registry primitive. Service `runtimebind` sub-packages call `Register` from
  `init()` so new scanners stay pure-additive.
- `ScannerDeps`, `ScannerRegistration`, `ScannerBuilder` - the registry
  contract. Builders consume `ScannerDeps`; bindings install
  `ScannerRegistration` records. `ScannerRegistration.RequiresRedactionKey`
  declares that the scanner cannot run without a redaction key.
- `SupportedServiceKinds` and `SupportsServiceKind` - registry-backed
  service-kind introspection used by command-side target-scope validation so
  startup checks cannot drift from scanner availability.
- `ServiceRequiresRedactionKey` and `ServiceKindsRequiringRedactionKey` -
  registry-backed redaction-key introspection. The command derives the
  `ESHU_AWS_REDACTION_KEY` pre-flight requirement and its missing-key error
  message from these, so the requirement stays in lockstep with the
  `RequiresRedactionKey` flags the bindings register.
- `ScannerFactory` - creates a service scanner for one target and lease.
- `ServiceScanner` - scans one service claim into fact envelopes.
- `CheckpointStore` - durable pagination checkpoint store used by long service
  scans.
- `ScanStatusStore` - durable scanner-side status store for start, API count,
  throttle count, warning, and partial-run evidence.
- `ClaimedSource` - implements the collector claimed-source contract.
- `FixtureSource` - offline `collector.Source` for fixture/replay mode. It needs
  no credentials, no AWS SDK, and no network: it converts a `FixtureConfig` into
  the same `aws_resource` / `aws_relationship` envelopes the live scanners emit
  via `awscloud.NewResourceEnvelope` / `awscloud.NewRelationshipEnvelope`.
- `FixtureConfig`, `FixtureScope`, `FixtureResource`, `FixtureRelationship` -
  the declarative offline estate `FixtureSource` replays. `FixtureScope` derives
  a stable `aws:<account>:<region>:<service>` scope id and a clock-independent
  generation id when those fields are blank, so re-ingest is idempotent.

## Fixture mode (offline replay)

`FixtureSource` is the offline twin of `ClaimedSource`. The `collector-aws-cloud`
command wires it into a non-claimed `collector.Service` when run with
`-mode fixture -config <estate.json>` (default mode stays `claimed-live`). Each
`Next` yields one scope generation; the source reports the batch drained, then
restarts it on the next poll, mirroring the GCP fixture source. Because the
emitted envelopes are byte-identical to live facts, the offline path exercises
the same projector and `aws_cloud_runtime_drift` reducer behavior. A checked-in
estate and its drift intent live in
`go/cmd/collector-aws-cloud/testdata/fixture-estate.json` and
`tests/fixtures/aws_runtime_drift/`; the compose proof is
`scripts/verify_aws_runtime_drift_compose.sh`.

## Dependencies

- `internal/collector` for `CollectedGeneration` and `FactsFromSlice`.
- `internal/collector/awscloud` for claim boundaries and warning envelopes.
- `internal/collector/awscloud/checkpoint` for durable pagination checkpoint
  scope and store contracts.
- `internal/facts` for warning fact types.
- `internal/redact` for the runtime-shared redaction key carried in
  `ScannerDeps`.
- `internal/scope` for AWS scope and collector identity.
- `internal/telemetry` for shared instruments carried in `ScannerDeps`.
- `internal/workflow` for durable work item claims.
- AWS SDK for Go v2 `config`, `sts`, and credential cache support.

This package no longer imports individual `services/<svc>` or `awssdk`
packages directly. Each scanner registers itself from
`services/<svc>/runtimebind/init()`, and the command pulls every binding
through `awsruntime/bindings`. That keeps adding a new AWS scanner additive:
no file in this package changes.

## Telemetry

This package starts claim, credential, and scan spans through `ClaimedSource`.
Service `awssdk` adapters emit per-API call counters, throttle counters, and
pagination spans. The command registers the instruments:

- `eshu_dp_aws_api_calls_total`
- `eshu_dp_aws_throttle_total`
- `eshu_dp_aws_claim_concurrency`
- `eshu_dp_aws_assumerole_failed_total`
- `eshu_dp_aws_budget_exhausted_total`
- `eshu_dp_aws_pagination_checkpoint_events_total`
- `eshu_dp_aws_resources_emitted_total`
- `eshu_dp_aws_relationships_emitted_total`
- `eshu_dp_aws_tag_observations_emitted_total`
- `eshu_dp_aws_org_access_skipped_total`
- `eshu_dp_aws_scan_duration_seconds`
- `aws.collector.claim.process`
- `aws.credentials.assume_role`
- `aws.service.scan`
- `aws.service.pagination.page`

## Permission Gap Evidence

Smithy service API errors with `AccessDenied`, `AccessDeniedException`,
`UnauthorizedOperation`, `UnauthorizedException`, `ForbiddenException`,
`UnsupportedOperation`, or `UnsupportedOperationException` are terminal for the
claimed AWS scope. The runtime records `aws_scan_status.status=failed` with
`failure_class=permission_denied` or `failure_class=unsupported_permission`,
then returns a terminal classified error so `collector.ClaimedService` records
the claim as terminal instead of retrying the same denied scope until the
generic attempt budget fires. Plain transport and request-send failures remain
retryable `collect_failure` errors. A newer generation may reclaim a prior
terminal permission-gap row only when its start timestamp is newer, preserving
the AWS scan-status conflict domain against stale older starts.

No-Regression Evidence:

- `cd go && go test ./internal/collector/awscloud/awsruntime -run 'TestClaimedSource(ClassifiesDeniedSmithyAPIErrorsAsTerminalPermissionGaps|KeepsTransportFailureRetryable)|TestStartScanStatus(ClassifiesStaleFenceAsTerminal|IncrementsStaleFenceCounter)' -count=1`
- `cd go && go test ./internal/storage/postgres -run 'TestAWSScanStatusStore(AllowsNewGenerationAfterTerminalPermissionGap|AllowsNewGenerationOverOrphanedRunningRow|ReturnsTypedStaleFenceError)' -count=1`

These cover denied IAM-style, EC2 unauthorized, unsupported-operation,
transient transport, stale-fence classification, recovery after permissions are
fixed, orphaned-row handoff, and stale-fence rejection.

Observability Evidence: no new metric name or JSON field was needed. Operators
see the permission gap through existing AWS scan status fields
(`status`, `commit_status`, `failure_class`, and `failure_message`), existing
API pressure counters (`eshu_dp_aws_api_calls_total`,
`eshu_dp_aws_throttle_total`), and the scan span/latency path
(`aws.service.scan`, `eshu_dp_aws_scan_duration_seconds{result="error"}`).

## Refactor Evidence (Scanner Registry Self-Registration)

The init-time scanner registry refactor (#762) replaces the central switch
in `registry.go` with `Register`/`LookupBuilder` plus per-service
`runtimebind` packages. Plumbing only; no per-claim path changes.

Collector Performance Evidence: `cd go && go test
./internal/collector/awscloud/... -count=1 -race` covers every scanner
builder through `awsruntime.DefaultScannerFactory.Scanner`. The path the
runtime now executes for each claim is one `awsruntime.LookupBuilder`
call — a `sync.RWMutex.RLock` around a single map read — followed by the
same builder call the legacy switch executed. The RLock is uncontended in
production because every `Register` call completes during `init()` before
`main` runs; after process start the registry is effectively read-only,
so RLock acquisition is a handful of atomic operations per claim with no
writer to wait on (nanosecond-scale). No new I/O is introduced. `go test
./internal/collector/awscloud/awsruntime -count=1 -race -run
TestConcurrentRegister` proves the registry stays race-free even under 32
concurrent Register calls, which is well beyond the production pattern.

Collector Observability Evidence: every per-service telemetry instrument
listed above keeps emitting from the same SDK adapters. The runtimebind
init wires the same `awssdk.NewClient`/`awssdk.NewClientWithCheckpoints`
constructors, so `eshu_dp_aws_api_calls_total`,
`eshu_dp_aws_pagination_checkpoint_events_total`,
`eshu_dp_aws_resources_emitted_total`,
`aws.service.scan`, and `aws.service.pagination.page` retain identical
labels, cardinality, and span shape.

No-Observability-Change: the awscloud runtime telemetry contract is
untouched. Init-time registration emits no metrics, spans, or logs of
its own. Failure modes are programmer errors (duplicate registration,
empty service_kind, nil builder) and surface as process-start panics, which
operators already see in `runtime.startup.failed` log records.

Collector Deployment Evidence: the refactor changes no Docker Compose
service, Helm chart, ConfigMap, environment variable, port, or readiness
gate. The `collector-aws-cloud` binary keeps the same `/healthz`,
`/readyz`, `/metrics`, and `/admin/status` surfaces and the same
ServiceMonitor configuration. The only deployment-visible change is one
new blank import in each binary that calls `awsruntime.SupportsServiceKind`
(collector-aws-cloud, workflow-coordinator, webhook-listener).

## Refactor Evidence (Derived Supported-Service Guard)

The supported-service guard refactor (#785) replaces the two hardcoded
want-lists in `registry_supported_services_test.go` and
`bindings/bindings_test.go` with a derived check. The expected scanner set is
computed at test time from the `services/<svc>/runtimebind/` directories on
disk and the runtimebind blank imports parsed from `bindings.go` (see
`internal/guardset`). Adding a scanner now touches zero want-lists; it appends
one `merge=union` line to `bindings.go` and adds its own files.

No-Regression Evidence: this is a test-only and docs-only change. No
non-test runtime file changed. The production scanner registry, the
`DefaultScannerFactory` dispatch path, and the per-claim `LookupBuilder` read
are byte-for-byte unchanged from the #762 self-registration refactor. The
guard's value is preserved and proven: `go test
./internal/collector/awscloud/awsruntime/... -count=1 -race` passes, and the
`Diff` helper has a unit-tested negative case ("dir present but not imported")
in `internal/guardset/guardset_test.go`. Manually removing one blank import
from `bindings.go` makes both guard tests fail with
`services/<svc>/runtimebind/ exists but bindings.go does not blank-import it`
and `len(SupportedServiceKinds()) = N-1, want N`, then passes again once the
import is restored.

No-Observability-Change: the awscloud runtime telemetry contract is untouched.
The change adds only test-support code and documentation; it emits no metrics,
spans, or logs and alters no existing signal. The instruments listed under
Telemetry above keep their identical names, labels, cardinality, and span
shape.

## Gotchas / invariants

- `AcceptanceUnitID` is JSON with `account_id`, `region`, and `service_kind`.
  The runtime does not parse ARNs or free-form strings to discover claim scope.
- Credential acquisition happens after target authorization. An unauthorized
  claim never receives credentials.
- `CredentialLease.Release` runs after scanner construction and scan attempts.
  Implementations must clear temporary credential material there.
- `SDKCredentialProvider` loads AWS SDK config with adaptive retries and passes
  required STS external IDs for central AssumeRole scopes.
- `DefaultScannerFactory` is the only production registry for service scanners;
  add full-scan services there and update `supportedServiceKinds` instead of
  branching in the command.
- ECS, Lambda, Security Hub, and Organizations service scans require a
  non-empty redaction key because environment values, Security Hub action target
  descriptions, and Organizations account email/name values are treated as
  sensitive even when source labels look harmless.
- EC2 service scans collect network topology only. They do not emit EC2
  instance inventory facts.
- Target scopes default to one active claim per account when
  `max_concurrent_claims` is unset.
- STS or workload-identity failures emit an `assumerole_failed` warning fact for
  the claimed generation and record `credential_failed` scan status.
- Stale pagination checkpoints are expired at claim start for the current
  workflow generation before credentials are acquired.
- Scanner-side status records API call counts and throttle counts before fact
  commit. The command's commit wrapper records whether the fenced fact
  transaction later committed or failed.
- Route 53 alias targets are reported DNS evidence only; do not infer workload
  or deployable-unit truth in the runtime.
- Lambda aliases, event-source mappings, image URIs, execution roles, subnets,
  and security groups are reported join evidence only; do not infer workload or
  deployable-unit truth in the runtime.
- EKS clusters, OIDC providers, node groups, add-ons, IAM roles, subnets, and
  security groups are reported join evidence only; do not infer Kubernetes
  workload or deployable-unit truth in the runtime.
- SQS scanners must stay metadata-only. The runtime registry wires the SQS SDK
  adapter, but it must not broaden the service contract to message reads,
  message mutations, or queue policy persistence.
- SNS scanners must stay metadata-only. The runtime registry wires the SNS SDK
  adapter, but it must not broaden the service contract to publishing,
  subscription mutations, policy persistence, data-protection-policy
  persistence, or raw non-ARN endpoint persistence.
- EventBridge scanners must stay metadata-only. The runtime registry wires the
  EventBridge SDK adapter, but it must not broaden the service contract to
  PutEvents, rule/target mutations, event bus policy persistence, target input
  payload persistence, input-transformer persistence, HTTP-parameter
  persistence, or raw non-ARN target persistence.
- GuardDuty scanners must stay metadata-only. The runtime registry wires the
  GuardDuty SDK adapter, but it must not broaden the service contract to
  finding-body reads, filter criteria reads, threat intel/IP list content
  fetches, invitation/member mutations, publishing destination mutations, set
  mutations, or finding feedback mutations.
- S3 scanners must stay metadata-only. The runtime registry wires the S3 SDK
  adapter, but it must not broaden the service contract to object inventory,
  bucket policy persistence, ACL grant persistence, replication persistence,
  lifecycle persistence, notification persistence, or mutation APIs.
- RDS scanners must stay metadata-only. The runtime registry wires the RDS SDK
  adapter, but it must not broaden the service contract to database
  connections, database names, master usernames, secrets, snapshots, log
  contents, Performance Insights samples, schemas, tables, row data, or
  mutation APIs.
- DynamoDB scanners must stay metadata-only. The runtime registry wires the
  DynamoDB SDK adapter, but it must not broaden the service contract to item
  reads, table scans, table queries, stream record reads, backup/export payload
  reads, resource-policy persistence, PartiQL calls, or mutation APIs.
- CloudWatch Logs scanners must stay metadata-only. The runtime registry wires
  the CloudWatch Logs SDK adapter, but it must not broaden the service contract
  to log event reads, log stream payload reads, Insights query calls, export
  payload reads, resource-policy persistence, subscription payload reads, or
  mutation APIs.
- CloudFront scanners must stay metadata-only. The runtime registry wires the
  CloudFront SDK adapter, but it must not broaden the service contract to
  object reads, origin payload reads, distribution config payload persistence,
  policy-document persistence, certificate body reads, private-key handling,
  origin custom header value persistence, or mutation APIs.
- API Gateway scanners must stay metadata-only. The runtime registry wires the
  API Gateway SDK adapter, but it must not broaden the service contract to API
  execution, export, API key, authorizer secret, policy JSON, integration
  credential, stage variable, template body, payload, or mutation APIs.
- Secrets Manager scanners must stay metadata-only. The runtime registry wires
  the Secrets Manager SDK adapter, but it must not broaden the service contract
  to secret value reads, version payload reads, resource-policy persistence,
  external rotation partner metadata persistence, or mutation APIs.
- SSM scanners must stay metadata-only. The runtime registry wires the SSM SDK
  adapter, but it must not broaden the service contract to parameter value
  reads, history value reads, raw description persistence, raw allowed-pattern
  persistence, raw policy JSON persistence, decryption, or mutation APIs.
- Athena scanners must stay metadata-only. The runtime registry wires the
  Athena SDK adapter, but it must not broaden the service contract to
  StartQueryExecution, StopQueryExecution, GetQueryResults, GetQueryExecution,
  ListQueryExecutions, named-query SQL body reads, prepared-statement query
  body reads, query history persistence, or mutation APIs.
- Glue scanners must stay metadata-only. The runtime registry wires the Glue
  SDK adapter, but it must not broaden the service contract to StartCrawler,
  StartJobRun, BatchStopJobRun, Create*/Update*/Delete* APIs, job script body
  reads, job default-argument value persistence, secret-shaped argument key
  persistence, connection password reads, JDBC credential URL persistence,
  connection property value persistence, table column statistics with sample
  values, classifier custom-pattern reads, workflow graph payload reads, or
  workflow run state reads. The SDK adapter must call GetConnections with
  HidePassword=true and GetWorkflow with IncludeGraph=false.
- Step Functions scanners must stay metadata-only. The runtime registry wires
  the Step Functions SDK adapter, but it must not broaden the service contract
  to StartExecution, StopExecution, CreateStateMachine, UpdateStateMachine,
  DeleteStateMachine, SendTaskSuccess, SendTaskFailure, or any other mutation
  or execution-payload API. It must not persist execution input, execution
  output, execution history events, activity task tokens, or literal
  Parameters/ResultPath/ResultSelector/InputPath/OutputPath/Result contents
  from a state machine definition; only state names, state types, transitions,
  and Task Resource ARNs are persisted.
- Access Analyzer scanners must stay metadata-only. The runtime registry wires
  the Access Analyzer SDK adapter, but it must not broaden the service contract
  to external finding-body persistence, archive-rule filter persistence,
  policy-generation output, per-action unused-access detail persistence,
  GetFinding, or mutation APIs.
- KMS scanners must stay metadata-only. The runtime registry wires the KMS SDK
  adapter, but it must not broaden the service contract to any cryptographic
  operation (Encrypt, Decrypt, GenerateDataKey, GenerateDataKeyPair,
  GenerateDataKeyPairWithoutPlaintext, GenerateDataKeyWithoutPlaintext, Sign,
  Verify, ReEncrypt, GenerateMac, VerifyMac, DeriveSharedSecret, GetPublicKey,
  GenerateRandom) or key lifecycle mutation (CreateKey, ScheduleKeyDeletion,
  CancelKeyDeletion, EnableKey, DisableKey, EnableKeyRotation,
  DisableKeyRotation, PutKeyPolicy, CreateGrant, RevokeGrant, RetireGrant,
  ReplicateKey, ImportKeyMaterial, DeleteImportedKeyMaterial,
  UpdateKeyDescription, CreateAlias, UpdateAlias, DeleteAlias, TagResource,
  UntagResource, RotateKeyOnDemand, UpdatePrimaryRegion). It must not call
  GetKeyPolicy or persist key policy Statement bodies, grant encryption
  contexts, or key material.
- Organizations scanners must stay metadata-only. The runtime registry wires
  the Organizations SDK adapter, forces API calls to the `us-east-1` control
  plane, and requires management-account or delegated-administrator
  credentials. It must not broaden the service contract to policy body reads,
  account lifecycle mutations, policy mutations, service-access mutations, or
  delegated-admin mutations. Non-org-aware credentials must surface an
  `organizations_org_access_skipped` warning and
  `eshu_dp_aws_org_access_skipped_total` rather than failed or fabricated
  organization truth.
- This package classifies only deterministic top-level AWS API permission gaps
  as terminal. The caller owns retry timing, claim failure persistence, and the
  generic attempt budget through `collector.ClaimedService`.

## Related docs

- `docs/public/services/collector-aws-cloud.md`
- `docs/public/guides/collector-authoring.md`
