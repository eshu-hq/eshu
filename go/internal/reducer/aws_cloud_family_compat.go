// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "github.com/eshu-hq/eshu/go/internal/reducer/awscloud"

// This file is the reducer root's compatibility surface for the AWS
// cloud-image and AWS cloud-runtime-drift families, which moved to
// [awscloud] (issue #6061). It carries only the names that still have a
// caller: the reducer root's own registration and handler wiring
// (defaults.go, defaults_handlers.go, defaults_additive_domains_*.go),
// cmd/reducer's writer construction, internal/storage/postgres's transaction
// and failure-class wiring, and internal/replay/costcounting's cost test.
// Everything else the family exports is reached as awscloud.X, and each
// entry here is deleted once its last caller has moved.

// CloudResourceContainerImageEdgeWriter persists and retracts canonical
// CloudResource -> ContainerImage edges. See
// [awscloud.CloudResourceContainerImageEdgeWriter].
type CloudResourceContainerImageEdgeWriter = awscloud.CloudResourceContainerImageEdgeWriter

// AWSCloudImageMaterializationHandler reduces one AWS cloud-image
// materialization follow-up into canonical CloudResource -> ContainerImage
// edge writes. See [awscloud.AWSCloudImageMaterializationHandler].
type AWSCloudImageMaterializationHandler = awscloud.AWSCloudImageMaterializationHandler

// AWSCloudImageNodesNotReadyFailureClass identifies an in-handler
// readiness-gate miss for the AWS cloud-image domain. See
// [awscloud.AWSCloudImageNodesNotReadyFailureClass].
const AWSCloudImageNodesNotReadyFailureClass = awscloud.AWSCloudImageNodesNotReadyFailureClass

// awsCloudImageMaterializationDomainDefinition forwards to
// [awscloud.ImageMaterializationDomainDefinition].
func awsCloudImageMaterializationDomainDefinition() DomainDefinition {
	return awscloud.ImageMaterializationDomainDefinition()
}

// AWSCloudRuntimeDriftEvidenceLoader supplies the joined AWS cloud,
// Terraform-state, and Terraform-config rows classified by the
// aws_cloud_runtime_drift rule pack. See
// [awscloud.AWSCloudRuntimeDriftEvidenceLoader].
type AWSCloudRuntimeDriftEvidenceLoader = awscloud.AWSCloudRuntimeDriftEvidenceLoader

// AWSCloudRuntimeDriftFindingWriter publishes admitted AWS runtime drift
// candidates into the durable canonical truth surface. See
// [awscloud.AWSCloudRuntimeDriftFindingWriter].
type AWSCloudRuntimeDriftFindingWriter = awscloud.AWSCloudRuntimeDriftFindingWriter

// AWSCloudRuntimeDriftWrite is the durable publication request for one AWS
// runtime drift reducer intent. See [awscloud.AWSCloudRuntimeDriftWrite].
type AWSCloudRuntimeDriftWrite = awscloud.AWSCloudRuntimeDriftWrite

// AWSCloudRuntimeDriftWriteResult summarizes durable AWS runtime drift
// publication. See [awscloud.AWSCloudRuntimeDriftWriteResult].
type AWSCloudRuntimeDriftWriteResult = awscloud.AWSCloudRuntimeDriftWriteResult

// AWSCloudRuntimeDriftHandler evaluates AWS runtime drift evidence and
// publishes admitted orphan/unmanaged findings as durable reducer facts. See
// [awscloud.AWSCloudRuntimeDriftHandler].
type AWSCloudRuntimeDriftHandler = awscloud.AWSCloudRuntimeDriftHandler

// AWSCloudRuntimeDriftReadinessChecker reports whether a Terraform
// state_snapshot scope is still mid-ingestion. See
// [awscloud.AWSCloudRuntimeDriftReadinessChecker].
type AWSCloudRuntimeDriftReadinessChecker = awscloud.AWSCloudRuntimeDriftReadinessChecker

// AWSCloudRuntimeDriftFencingTokenIssuer supplies the database-issued,
// monotonically increasing fencing token the admission check and durable
// rows are stamped with. See [awscloud.AWSCloudRuntimeDriftFencingTokenIssuer].
type AWSCloudRuntimeDriftFencingTokenIssuer = awscloud.AWSCloudRuntimeDriftFencingTokenIssuer

// AWSCloudRuntimeDriftTx is the narrow transactional surface the drift
// writer needs. See [awscloud.AWSCloudRuntimeDriftTx].
type AWSCloudRuntimeDriftTx = awscloud.AWSCloudRuntimeDriftTx

// AWSCloudRuntimeDriftBeginner opens a transaction for the drift write. See
// [awscloud.AWSCloudRuntimeDriftBeginner].
type AWSCloudRuntimeDriftBeginner = awscloud.AWSCloudRuntimeDriftBeginner

// PostgresAWSCloudRuntimeDriftWriter persists admitted AWS runtime drift
// findings into the shared fact store. See
// [awscloud.PostgresAWSCloudRuntimeDriftWriter].
type PostgresAWSCloudRuntimeDriftWriter = awscloud.PostgresAWSCloudRuntimeDriftWriter

// AWSCloudRuntimeDriftWriteSupersededFailureClass classifies a write whose
// evidence-read watermark is older than one already admitted. See
// [awscloud.AWSCloudRuntimeDriftWriteSupersededFailureClass].
const AWSCloudRuntimeDriftWriteSupersededFailureClass = awscloud.AWSCloudRuntimeDriftWriteSupersededFailureClass

// AWSCloudRuntimeDriftStatePendingFailureClass classifies a Handle call
// deferred because a Terraform state_snapshot scope has not finished
// ingesting. See [awscloud.AWSCloudRuntimeDriftStatePendingFailureClass].
const AWSCloudRuntimeDriftStatePendingFailureClass = awscloud.AWSCloudRuntimeDriftStatePendingFailureClass
