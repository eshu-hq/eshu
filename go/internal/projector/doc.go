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
// The neutral internal/projector/intent contract is the boundary for extracted
// reducer-intent family packages. Azure, EC2, GCP, Kubernetes, RDS, S3,
// security, workload-cloud-relationship, incident-routing, AWS-relationship,
// AWS-cloud-image, IAM CAN_ASSUME, IAM instance-profile-role,
// package-source-correlation, cloud-inventory-admission, code-taint-evidence,
// code-interproc-evidence, SBOM-attestation-attachment,
// service-catalog-correlation, secrets-IAM-trust-chain, CI/CD
// run-correlation, container-image-identity, and supply-chain-impact
// builders live in their internal/projector child packages; this root
// package owns lookup construction and lifetime, family assembly, and
// enqueue.
// OCI registry projection keeps digest-addressed manifests, indexes, and
// descriptors as canonical identity while treating tags as mutable weak
// observations that can enrich queries but do not mint image identity.
// OCI, Git (including static workflow-image evidence), AWS, Azure, and GCP
// image-reference evidence emits one container_image_identity reducer
// intent per scope generation via
// containerimageidentity.BuildContainerImageIdentityReducerIntent; the
// reducer owns the cross-source join.
// AWS resource observations stay source-local until buildAWSCloudRuntimeDriftReducerIntent
// emits one aws_cloud_runtime_drift reducer intent for the AWS scope
// generation; the reducer owns ARN joins and unmanaged/orphan admission.
// GCP and Azure cloud resource observations emit one multi_cloud_runtime_drift
// reducer intent via
// multicloudruntimedrift.BuildMultiCloudRuntimeDriftReducerIntent (issue
// #5759); AWS resource facts alone do not trigger it, since
// aws_cloud_runtime_drift exclusively owns AWS drift findings and the
// reducer filters any AWS-provider row its shared canonical-uid evidence
// loader also resolves before publication.
// Direct code_interproc_evidence facts emit direct interproc reducer intents;
// code_function_summary facts emit summary persistence intents, and the reducer
// runs fixpoint TAINT_FLOWS_TO projection only after its durable
// summary/source/graph-id stores are updated.
// RDS posture observations emit one rds_posture_materialization reducer intent;
// the reducer waits for CloudResource readiness and owns posture property
// projection on existing RDS nodes. The RDS posture reducer-intent builder
// lives in the internal/projector/rds child package.
// Azure cloud resource and relationship observations emit reducer intents for
// Azure CloudResource node readiness and relationship edge projection; the
// reducer owns exact ARM-id endpoint resolution.
// EC2 posture observations emit one ec2_internet_exposure_materialization
// reducer intent keyed to the EC2 instance-node readiness phase; the reducer
// owns exposure derivation from EC2, ENI, and security-group evidence. The
// EC2 instance-node, instance-identity, block-device KMS posture, internet
// exposure, and USES_PROFILE edge reducer-intent builders live in the
// internal/projector/ec2 child package. S3 LOGS_TO, external-principal-grant,
// and internet-exposure reducer-intent builders live in the
// internal/projector/s3 child package; the reducer owns edge and posture
// projection. The workload-cloud-relationship reducer-intent builder lives in
// the internal/projector/workloadcloud child package; the reducer owns
// workload-endpoint resolution and USES edge projection. The incident-routing
// reducer-intent builder lives in the internal/projector/incidentrouting child
// package; the reducer owns routing comparison and IncidentRoutingEvidence
// projection. The AWS relationship reducer-intent builder lives in the
// internal/projector/awsrelationship child package; the reducer owns the
// bounded relationship join, the canonical-nodes readiness gate, and edge
// projection. The IAM CAN_ASSUME reducer-intent builder and its
// aws_iam_permission decode wrapper live in the internal/projector/iamcanassume
// child package; the reducer owns principal resolution, the canonical-nodes
// readiness gate, and trust-edge projection.
// Package-registry identity emits package source-correlation and
// supply_chain_impact reducer intents so manifest-backed consumption and
// vulnerability findings can catch up when package evidence arrives after
// source intelligence. The package-source-correlation reducer-intent builder
// lives in the internal/projector/packagesource child package; the reducer
// owns hint classification and consumption admission. The
// supply-chain-impact reducer-intent builder lives in the
// internal/projector/supplychainimpact child package via
// supplychainimpact.BuildSupplyChainImpactReducerIntent; the reducer owns the
// cross-source vulnerability-to-package-to-deployment join.
// When a Postgres-backed runtime configures PackageRegistryIdentityLocker,
// package-registry canonical writes also take transaction-scoped package UID
// advisory locks before calling the graph writer. This coordinates ingester,
// standalone projector, and bootstrap-index processes without serializing
// unrelated package identities.
// SBOM and attestation documents emit sbom_attestation_attachment reducer
// intents; source-local components enrich the reducer decision but do not attach
// themselves to images in the projector. That reducer-intent builder lives in
// the internal/projector/sbomattestation child package; the reducer owns
// subject-digest admission and attachment writes.
// PagerDuty incident and incident-routing facts emit one
// incident_routing_materialization reducer intent; declared/applied/live routing
// comparison and graph admission remain reducer-owned.
// EntityTypeLabel keeps parser/content entity labels, including Terraform
// backend/import/refactor/check and lockfile-provider labels, aligned with graph
// schema support.
package projector
