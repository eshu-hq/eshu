// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package containerimageidentity builds the container_image_identity reducer
// intent from one immutable scope generation. The trigger fires on the
// earliest accepted fact across a closed set of candidate kinds, in original
// generation order: OCI digest/tag/referrer facts, AWS/Azure/GCP
// image-reference facts, an AWS relationship whose decoded TargetType is
// "container_image", a CI/CD artifact whose artifact_type is
// "container_image", static CI/CD workflow-image evidence, a Git
// content-entity carrying container_images metadata, a repository `file`
// fact that is a Dockerfile (added, edited, or tombstoned) or a tombstoned
// GitHub Actions workflow file, a signed SLSA provenance statement, or a
// signature-verification result. Only envelope-level fields and the AWS
// relationship's typed TargetType are read; the reducer's
// DomainContainerImageIdentity handler owns the cross-source digest-first
// join, decision classification, and BUILT_FROM/DERIVED_FROM graph writes.
// The AWS-relationship decode uses this package's own local
// factschema.DecodeAWSRelationship call (factschema_decode_aws.go); root's
// prior classified wrapper of the same seam had this trigger as its only
// caller and was removed rather than kept unused. The sole caller here
// discards the decode error, so the two calls are behavior-identical for
// this trigger. The intent's
// source-system label is the shared two-tier projectorintent.SourceSystem
// fallback (trimmed SourceRef.SourceSystem, else trimmed CollectorKind) —
// the pre-extraction local helper had the identical body, so this is a
// behavior-preserving substitution, not a change. Root projector assembly
// owns lookup construction and lifetime, invocation order, queue writes,
// retries, and telemetry.
package containerimageidentity
