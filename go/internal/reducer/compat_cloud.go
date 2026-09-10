// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

// This file is the reducer root's compatibility surface for the cloud and platform-inventory families (awscloud, containerimage, kubernetescorrelation, obscoverage, cloudjoin)
// (issue #6061). It merges the per-family *_compat.go files listed below
// with no behavior change: every alias and forwarder is preserved
// byte-identical under its stanza marker. A family move adds a stanza
// to the matching bucket file and NEVER creates a new *_compat.go
// (docs/internal/design/reducer-target-tree.md). Each entry is deleted
// once its last caller has moved; see the importer-migration child issue.
//
// Stanzas merged here:
//   - aws_cloud_family_compat.go
//   - container_image_identity_compat.go
//   - kubernetes_correlation_compat.go
//   - observability_coverage_compat.go
//   - cloud_resource_join_index_compat.go

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/awscloud"
	"github.com/eshu-hq/eshu/go/internal/reducer/cloudjoin"
	"github.com/eshu-hq/eshu/go/internal/reducer/containerimage"
	"github.com/eshu-hq/eshu/go/internal/reducer/kubernetescorrelation"
	"github.com/eshu-hq/eshu/go/internal/reducer/obscoverage"
)

// Stanza: aws_cloud_family_compat.go (merged; do not recreate this file).
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

// Stanza: container_image_identity_compat.go (merged; do not recreate this file).
// This file is the transitional compatibility surface for the container-image
// identity family that moved to [containerimage] (issue #6061). It carries
// only the names that still have a caller: the reducer root's own
// registration and handler wiring, cmd/reducer's writer construction,
// internal/storage/postgres' identity writers and evidence loaders,
// internal/replay/costcounting's cost test, and the sibling
// aws_resource_running_image/supply_chain_impact families' shared
// image-reference helpers. Everything else the family exports is reached as
// containerimage.X, and each entry here is deleted once its last caller has
// moved.

// ContainerImageIdentityWrite is the durable publication input one
// container-image-identity execution submits. See
// [containerimage.ContainerImageIdentityWrite].
type ContainerImageIdentityWrite = containerimage.ContainerImageIdentityWrite

// ContainerImageIdentityDecision is one image reference's resolved identity
// outcome. See [containerimage.ContainerImageIdentityDecision].
type ContainerImageIdentityDecision = containerimage.ContainerImageIdentityDecision

// ContainerImageIdentityWriteResult summarizes durable publication. See
// [containerimage.ContainerImageIdentityWriteResult].
type ContainerImageIdentityWriteResult = containerimage.ContainerImageIdentityWriteResult

// ContainerImageIdentityWriter persists reducer-owned image identity truth.
// See [containerimage.ContainerImageIdentityWriter].
type ContainerImageIdentityWriter = containerimage.ContainerImageIdentityWriter

// ContainerImageIdentityHandler joins Git/runtime image references with
// active OCI registry facts and publishes image-reference-keyed identity
// decisions. See [containerimage.ContainerImageIdentityHandler].
type ContainerImageIdentityHandler = containerimage.ContainerImageIdentityHandler

// ContainerImageIdentityPriorSupport is one prior authoritative support row
// eligible for carry-forward while collector completeness holds retirement.
// See [containerimage.ContainerImageIdentityPriorSupport].
type ContainerImageIdentityPriorSupport = containerimage.ContainerImageIdentityPriorSupport

// ContainerImageIdentityTransaction is the narrow atomic write surface used by
// the identity writer for outcome-independent publications followed by
// cleanup of unreachable legacy outcome-keyed rows. See
// [containerimage.ContainerImageIdentityTransaction].
type ContainerImageIdentityTransaction = containerimage.ContainerImageIdentityTransaction

// PostgresContainerImageIdentityWriter is the legacy outcome-keyed Postgres
// writer. See [containerimage.PostgresContainerImageIdentityWriter].
type PostgresContainerImageIdentityWriter = containerimage.PostgresContainerImageIdentityWriter

// PostgresContainerImageIdentitySupportWriter is the digest-v3 support-set
// Postgres writer cmd/reducer constructs. See
// [containerimage.PostgresContainerImageIdentitySupportWriter].
type PostgresContainerImageIdentitySupportWriter = containerimage.PostgresContainerImageIdentitySupportWriter

// ContainerImageExistenceLookup reports which candidate target ContainerImage
// node uids are already materialized in the canonical graph. See
// [containerimage.ContainerImageExistenceLookup].
type ContainerImageExistenceLookup = containerimage.ContainerImageExistenceLookup

// GraphContainerImageExistenceLookup implements ContainerImageExistenceLookup
// against the canonical graph. See
// [containerimage.GraphContainerImageExistenceLookup].
type GraphContainerImageExistenceLookup = containerimage.GraphContainerImageExistenceLookup

// ContainerImageProvenanceEdgeWriter projects exact_digest container image
// identity decisions into canonical BUILT_FROM graph edges. See
// [containerimage.ContainerImageProvenanceEdgeWriter].
type ContainerImageProvenanceEdgeWriter = containerimage.ContainerImageProvenanceEdgeWriter

// ContainerImageDerivedFromEdgeWriter projects Dockerfile base-image lineage
// into canonical DERIVED_FROM graph edges. See
// [containerimage.ContainerImageDerivedFromEdgeWriter].
type ContainerImageDerivedFromEdgeWriter = containerimage.ContainerImageDerivedFromEdgeWriter

// BuildContainerImageIdentityDecisions forwards to
// [containerimage.BuildContainerImageIdentityDecisions].
func BuildContainerImageIdentityDecisions(envelopes []facts.Envelope) []ContainerImageIdentityDecision {
	return containerimage.BuildContainerImageIdentityDecisions(envelopes)
}

// digestFromImageRef forwards to [containerimage.DigestFromImageRef].
// internal/reducer/aws_resource_running_image.go still resolves a running
// image's digest this way; that family has not moved out of root (#6061).
func digestFromImageRef(raw string) string {
	return containerimage.DigestFromImageRef(raw)
}

// containerImageBuiltFromRows forwards to
// [containerimage.ContainerImageBuiltFromRows].
// provenance_edges_bench_test.go and container_image_identity_slsa_test.go
// (root-staying benchmark/unit tests) still call this unqualified.
func containerImageBuiltFromRows(decisions []ContainerImageIdentityDecision) []map[string]any {
	return containerimage.ContainerImageBuiltFromRows(decisions)
}

// containerImageDerivedFromRows forwards to
// [containerimage.ContainerImageDerivedFromRows].
func containerImageDerivedFromRows(decisions []ContainerImageIdentityDecision, owningRepositoryID string) []map[string]any {
	return containerimage.ContainerImageDerivedFromRows(decisions, owningRepositoryID)
}

// containerImageBuiltFromProvenanceEvidenceSource forwards to
// [containerimage.ContainerImageBuiltFromProvenanceEvidenceSource].
const containerImageBuiltFromProvenanceEvidenceSource = containerimage.ContainerImageBuiltFromProvenanceEvidenceSource

// containerImageDerivedFromProvenanceEvidenceSource forwards to
// [containerimage.ContainerImageDerivedFromProvenanceEvidenceSource].
const containerImageDerivedFromProvenanceEvidenceSource = containerimage.ContainerImageDerivedFromProvenanceEvidenceSource

// containerimage re-declares the root GraphQueryRunner locally rather than
// importing it, because a family package must not import the reducer root and
// Go interfaces are satisfied structurally. That arrangement is only safe while
// the two method sets stay identical, and nothing about it is checked at the
// declaration sites -- a change to either interface would go unnoticed until
// some distant wiring site failed to compile, with an error pointing at the
// wiring rather than at the divergence.
//
// These two assignments pin it in both directions, so the method sets must
// match exactly. Either interface gaining, losing, or re-signing a method fails
// the build here, naming the real problem.
//
// containerimage.activeRepositoryFactLoader is deliberately NOT pinned: it is
// unexported, so no assertion can reach it from this package. Anyone widening
// that interface has to re-check its root counterpart by hand.
var (
	_ containerimage.GraphQueryRunner = GraphQueryRunner(nil)
	_ GraphQueryRunner                = containerimage.GraphQueryRunner(nil)
)

// Stanza: kubernetes_correlation_compat.go (merged; do not recreate this file).
// This file is the transitional compatibility surface for the kubernetes
// correlation family that moved to [kubernetescorrelation] (issue #6061). It
// carries only the name that still has a caller outside the reducer module
// boundary: internal/storage/postgres' reducer queue readiness SQL, which
// matches this exact string when it decides to re-enqueue rather than
// dead-letter. Everything else the family exports is reached as
// kubernetescorrelation.X, and this entry is deleted once its last caller has
// moved.

// KubernetesCorrelationNodesNotReadyFailureClass is the retryable failure
// class the #388 node-slice readiness gate returns. internal/storage/postgres'
// reducer queue matches this exact string when it decides to re-enqueue rather
// than dead-letter, so the literal is a storage contract, not just a Go
// identifier. See
// [kubernetescorrelation.KubernetesCorrelationNodesNotReadyFailureClass].
const KubernetesCorrelationNodesNotReadyFailureClass = kubernetescorrelation.KubernetesCorrelationNodesNotReadyFailureClass

// Stanza: observability_coverage_compat.go (merged; do not recreate this file).
// This file is the transitional compatibility surface for the observability
// coverage correlation and materialization family that moved to [obscoverage]
// (issue #6061). Reducer-root call sites keep their current spelling; each
// entry is deleted once its last caller has moved into a family subpackage.

// ObservabilityCoverageCorrelationHandler correlates observability coverage
// evidence into durable provenance-only decisions. See
// [obscoverage.ObservabilityCoverageCorrelationHandler].
type ObservabilityCoverageCorrelationHandler = obscoverage.ObservabilityCoverageCorrelationHandler

// ObservabilityCoverageCorrelationWriter persists reducer-owned observability
// coverage correlations. See
// [obscoverage.ObservabilityCoverageCorrelationWriter].
type ObservabilityCoverageCorrelationWriter = obscoverage.ObservabilityCoverageCorrelationWriter

// ObservabilityCoverageCorrelationWrite carries decisions for durable
// publication for one scope generation. See
// [obscoverage.ObservabilityCoverageCorrelationWrite].
type ObservabilityCoverageCorrelationWrite = obscoverage.ObservabilityCoverageCorrelationWrite

// ObservabilityCoverageCorrelationWriteResult summarizes durable coverage
// writes. See [obscoverage.ObservabilityCoverageCorrelationWriteResult].
type ObservabilityCoverageCorrelationWriteResult = obscoverage.ObservabilityCoverageCorrelationWriteResult

// ObservabilityCoverageMaterializationHandler projects exact observability
// coverage decisions into canonical COVERS graph edges. See
// [obscoverage.ObservabilityCoverageMaterializationHandler].
type ObservabilityCoverageMaterializationHandler = obscoverage.ObservabilityCoverageMaterializationHandler

// ObservabilityCoverageEdgeWriter persists and retracts canonical COVERS
// edges. See [obscoverage.ObservabilityCoverageEdgeWriter].
type ObservabilityCoverageEdgeWriter = obscoverage.ObservabilityCoverageEdgeWriter

// PostgresObservabilityCoverageCorrelationWriter stores reducer-owned
// observability coverage correlation decisions in the shared fact store. See
// [obscoverage.PostgresObservabilityCoverageCorrelationWriter].
type PostgresObservabilityCoverageCorrelationWriter = obscoverage.PostgresObservabilityCoverageCorrelationWriter

// ObservabilityCoverageEvidenceSource forwards to
// [obscoverage.ObservabilityCoverageEvidenceSource].
func ObservabilityCoverageEvidenceSource() string {
	return obscoverage.ObservabilityCoverageEvidenceSource()
}

// observabilityCoverageMaterializationDomainDefinition forwards to
// [obscoverage.MaterializationDomainDefinition].
func observabilityCoverageMaterializationDomainDefinition() DomainDefinition {
	return obscoverage.MaterializationDomainDefinition()
}

// ObservabilityCoverageNodesNotReadyFailureClass identifies an in-handler
// readiness-gate miss for observability coverage materialization. See
// [obscoverage.ObservabilityCoverageNodesNotReadyFailureClass].
const ObservabilityCoverageNodesNotReadyFailureClass = obscoverage.ObservabilityCoverageNodesNotReadyFailureClass

// Stanza: cloud_resource_join_index_compat.go (merged; do not recreate this file).
// This file is the reducer root's compatibility surface for the AWS
// CloudResource join index, which moved to [cloudjoin] (issue #6061) so the
// iamcan family can build and read it without importing the root. Root call
// sites keep their current spelling.

// cloudResourceJoinIndex is the root spelling of
// [cloudjoin.CloudResourceJoinIndex].
type cloudResourceJoinIndex = cloudjoin.CloudResourceJoinIndex

// buildCloudResourceJoinIndex forwards to
// [cloudjoin.BuildCloudResourceJoinIndex].
func buildCloudResourceJoinIndex(envelopes []facts.Envelope) (cloudResourceJoinIndex, []quarantinedFact, error) {
	return cloudjoin.BuildCloudResourceJoinIndex(envelopes)
}

// cloudResourceUID forwards to [cloudjoin.CloudResourceUID].
func cloudResourceUID(accountID, region, resourceType, resourceID string) string {
	return cloudjoin.CloudResourceUID(accountID, region, resourceType, resourceID)
}
