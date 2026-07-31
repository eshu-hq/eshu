# Reducer Cloud Domain Projections — Multi-Cloud Runtime Drift

Split from `cloud-projections.md` (issue #5759 follow-up) to keep that file
under the repository's 500-line cap, mirroring the `#5786` README split that
created `cloud-projections.md` in the first place. GCP/Azure provider
partitioning, the read-side AWS aggregation, and the full
No-Regression/Observability test evidence for `DomainMultiCloudRuntimeDrift` /
`MultiCloudRuntimeDriftHandler` live here; the GCP, secrets/IAM, S3, RDS, EC2,
and PagerDuty projections stay in
[`cloud-projections.md`](cloud-projections.md).

## Multi-Cloud Runtime Drift (issues #1997, #1998, #5759)

`DomainMultiCloudRuntimeDrift` reuses the AWS structural drift join
(`cloudruntime.Classify`) but keys every candidate on canonical
`cloud_resource_uid` so GCP and Azure share AWS's
orphaned/unmanaged/ambiguous/unknown vocabulary. `multicloud.BuildCandidates`
skips rows whose provider identity does not resolve to a canonical uid
(counted as unresolved, never fabricated), emits config evidence only when a
config layer is actually present (so an unmanaged resource is never falsely
promoted to managed), and lets a reducer `ambiguous`/`unknown` override win
over the bare structural join so conflicting or unproven ownership is never
presented as managed. `MultiCloudRuntimeDriftHandler` writes
`reducer_multi_cloud_runtime_drift_finding` facts through
`PostgresMultiCloudRuntimeDriftWriter`, read back by
`postgres.MultiCloudRuntimeDriftFindingStore`. The domain is graph-neutral and
additive: it registers only when both a `MultiCloudRuntimeDriftEvidenceLoader`
and writer are wired.

`go/internal/projector`'s `buildMultiCloudRuntimeDriftReducerIntent` enqueues
this domain when a scope generation carries `gcp_cloud_resource` or
`azure_cloud_resource` facts (#5759; before that fix it was registered but
never enqueued, so it fired for no scope). **Provider partitioning:** AWS
stays exclusively `DomainAWSCloudRuntimeDrift`'s; the shared evidence loader
still joins AWS rows into the same `cloud_resource_uid` keyspace for
implementation reuse, but `Handle`'s `excludeAWSOwnedRows` drops every
AWS-provider row before publication so this domain never republishes a
finding `DomainAWSCloudRuntimeDrift` already owns. The provider-neutral read surface
(`list_cloud_runtime_drift_findings`, `POST /api/v0/cloud/runtime-drift/findings`,
`export_cloud_runtime_drift_packet`) separately aggregates
`reducer_aws_cloud_runtime_drift_finding` rows back in at READ time
(`MultiCloudRuntimeDriftFindingStore.ListActiveFindingsAcrossProviders`,
`go/internal/query/cloud_runtime_drift_aggregate.go`), so `provider=aws` and an
unfiltered query return real AWS findings instead of an empty page --
`excludeAWSOwnedRows` still guarantees the write side never duplicates one, and
an AWS-origin row's status/missing-evidence/warning-flags are derived through
the SAME classification `list_aws_runtime_drift_findings` uses, so one row
never yields two safety verdicts.

No-Regression Evidence (#5759): `go test ./internal/correlation/drift/multicloud
./internal/correlation/rules -count=1` proves the GCP/Azure classifications, uid keying,
unresolved/converged skips, and declared-config non-overwrite. `go test ./internal/reducer
-run 'MultiCloud' -race -count=1` proves publication, no-emit-before-durable-write,
redaction, idempotent replay (stable fact id, `stable_fact_key`), concurrent-worker key
stability, and (`TestMultiCloudRuntimeDriftHandlerExcludesAWSOwnedRowsFromPublication`,
#5759) that an AWS row mixed with GCP/Azure rows is dropped, not duplicated. `go test
./internal/projector -run 'MultiCloudRuntimeDrift|FanOutParity' -count=1` proves the enqueue
trigger fires for GCP/Azure scopes and stays silent for AWS-only. `go test
./internal/storage/postgres -run 'MultiCloud' -count=1` proves the scope-bounded,
active-generation-joined write-side read; `go test ./internal/query -run 'CloudRuntimeDrift'
-count=1` proves the read-side AWS aggregation and shared safety-verdict derivation; `go
test ./internal/correlation/drift/cloudruntime ./internal/reducer -run
'AWSCloudRuntimeDrift' -count=1` proves the AWS path did not regress.

Observability Evidence (#5759): the handler reuses the existing
`eshu_dp_correlation_orphan_detected_total`,
`eshu_dp_correlation_unmanaged_detected_total`, and
`eshu_dp_correlation_rule_matches_total` counters, labeled by the bounded
`multi_cloud_runtime_drift` pack name and rule name only — never the canonical
uid, raw identity, provider scope, tags, or addresses. Admitted-finding logs
carry a bounded `drift.provider` label and route the correlation key through
`telemetry.SafeResourceLogAttrs`, so raw provider identities never reach logs.
