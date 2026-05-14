// Package projector owns source-local projection stages that turn committed
// facts into canonical graph writes and publish readiness for shared,
// reducer-owned domains.
//
// Stages in this package read fact envelopes, build canonical node and edge
// payloads, classify durable failure metadata, and hand writes to the Cypher
// writers. Projection must be idempotent: queue retries, duplicate claims, and
// partial graph writes must converge on the same graph truth instead of
// creating hidden second paths. A claimed generation can stop without ack or
// fail when its heartbeat returns ErrWorkSuperseded, which means a newer
// same-scope generation replaced stale local polling work. Projector code does
// not make cross-source admission decisions; those belong to internal/reducer.
// OCI registry projection keeps digest-addressed manifests, indexes, and
// descriptors as canonical identity while treating tags as mutable weak
// observations that can enrich queries but do not mint image identity.
// AWS resource observations stay source-local until buildAWSCloudRuntimeDriftReducerIntent
// emits one aws_cloud_runtime_drift reducer intent for the AWS scope
// generation; the reducer owns ARN joins and unmanaged/orphan admission.
// EntityTypeLabel keeps parser/content entity labels, including Terraform
// backend/import/refactor/check and lockfile-provider labels, aligned with graph
// schema support.
package projector
