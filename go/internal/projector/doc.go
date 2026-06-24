// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package projector owns source-local projection stages that turn committed
// facts into canonical graph writes, repository-scoped content rows,
// source-backed repository ref metadata, and readiness for shared,
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
// Content materialization only runs for scopes whose metadata carries an
// explicit repo_id; cloud, registry, and provider scopes without repository
// ownership still project canonical and reducer-owned evidence but do not write
// repository content rows or source ref metadata.
// OCI registry projection keeps digest-addressed manifests, indexes, and
// descriptors as canonical identity while treating tags as mutable weak
// observations that can enrich queries but do not mint image identity.
// OCI, Git, AWS, Azure, and GCP image-reference evidence emits one
// container_image_identity reducer intent per scope generation; the reducer
// owns the cross-source join.
// AWS resource observations stay source-local until buildAWSCloudRuntimeDriftReducerIntent
// emits one aws_cloud_runtime_drift reducer intent for the AWS scope
// generation; the reducer owns ARN joins and unmanaged/orphan admission.
// Direct code_interproc_evidence facts emit direct interproc reducer intents;
// code_function_summary facts emit summary persistence intents, and the reducer
// runs fixpoint TAINT_FLOWS_TO projection only after its durable
// summary/source/graph-id stores are updated.
// RDS posture observations emit one rds_posture_materialization reducer intent;
// the reducer waits for CloudResource readiness and owns posture property
// projection on existing RDS nodes.
// Azure cloud resource and relationship observations emit reducer intents for
// Azure CloudResource node readiness and relationship edge projection; the
// reducer owns exact ARM-id endpoint resolution.
// EC2 posture observations emit one ec2_internet_exposure_materialization
// reducer intent keyed to the EC2 instance-node readiness phase; the reducer
// owns exposure derivation from EC2, ENI, and security-group evidence.
// Package-registry identity emits package source-correlation and supply-chain
// impact reducer intents so manifest-backed consumption and vulnerability
// findings can catch up when package evidence arrives after source intelligence.
// When a Postgres-backed runtime configures PackageRegistryIdentityLocker,
// package-registry canonical writes also take transaction-scoped package UID
// advisory locks before calling the graph writer. This coordinates ingester,
// standalone projector, and bootstrap-index processes without serializing
// unrelated package identities.
// SBOM and attestation documents emit sbom_attestation_attachment reducer
// intents; source-local components enrich the reducer decision but do not attach
// themselves to images in the projector.
// PagerDuty incident and incident-routing facts emit one
// incident_routing_materialization reducer intent; declared/applied/live routing
// comparison and graph admission remain reducer-owned.
// EntityTypeLabel keeps parser/content entity labels, including Terraform
// backend/import/refactor/check and lockfile-provider labels, aligned with graph
// schema support.
package projector
