// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/containerimage"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

// This file is the transitional compatibility surface for the container-image
// identity family that moved to [containerimage] (issue #6061). It carries
// only the names that still have a caller: the reducer root's own
// registration and handler wiring, cmd/reducer's writer construction,
// internal/storage/postgres' identity writers and evidence loaders,
// internal/replay/costcounting's cost test, and the sibling
// kubernetes_correlation/aws_resource_running_image/supply_chain_impact
// families' shared image-reference helpers. Everything else the family
// exports is reached as containerimage.X, and each entry here is deleted once
// its last caller has moved.

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

// parseContainerImageRef forwards to [containerimage.ParseContainerImageRef].
// internal/reducer/kubernetes_correlation_classify.go and
// internal/reducer/kubernetes_correlation_index.go still parse image
// references this way; that family has not moved out of root (#6061).
func parseContainerImageRef(raw string) (parsedContainerImageRef, bool) {
	return containerimage.ParseContainerImageRef(raw)
}

// parsedContainerImageRef aliases [containerimage.ParsedContainerImageRef] so
// every unqualified use in kubernetes_correlation_classify.go keeps compiling
// unchanged.
type parsedContainerImageRef = containerimage.ParsedContainerImageRef

// digestFromImageRef forwards to [containerimage.DigestFromImageRef].
// internal/reducer/aws_resource_running_image.go still resolves a running
// image's digest this way; that family has not moved out of root (#6061).
func digestFromImageRef(raw string) string {
	return containerimage.DigestFromImageRef(raw)
}

// normalizeContainerRepositoryKey forwards to
// [containerimage.NormalizeContainerRepositoryKey].
// internal/reducer/kubernetes_correlation_index.go still normalizes
// repository keys this way; that family has not moved out of root (#6061).
func normalizeContainerRepositoryKey(raw string) string {
	return containerimage.NormalizeContainerRepositoryKey(raw)
}

// containerImageIdentityFormatImageRef forwards to
// [containerimage.ContainerImageIdentityFormatImageRef].
// internal/reducer/supply_chain_impact_anchor_consensus.go still compares
// identity_format against this constant to prefer a v2 row over a legacy row
// sharing the same logical key; that family has not moved out of root
// (#6061).
const containerImageIdentityFormatImageRef = containerimage.ContainerImageIdentityFormatImageRef

// ociRepositoryID forwards to [payloadcore.OCIRepositoryID]. It was a
// one-line forwarder inside the container-image-identity family before that
// family moved to [containerimage] (#6061); several still-in-root families
// (kubernetes_correlation_index.go, supply_chain_impact_active_filter.go)
// depend on it under this unqualified spelling.
func ociRepositoryID(payload map[string]any) string {
	return payloadcore.OCIRepositoryID(payload)
}

// boolPayload forwards to [payloadcore.BoolPayload]. Same history as
// [ociRepositoryID]: a pre-existing one-line forwarder that rode along in the
// container-image-identity family's files, still used unqualified by
// kubernetes_correlation_index.go.
func boolPayload(payload map[string]any, key string) bool {
	return payloadcore.BoolPayload(payload, key)
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

// containerImageIdentityPayload forwards to
// [containerimage.ContainerImageIdentityPayload].
func containerImageIdentityPayload(
	write ContainerImageIdentityWrite,
	decision ContainerImageIdentityDecision,
	canonicalID string,
) map[string]any {
	return containerimage.ContainerImageIdentityPayload(write, decision, canonicalID)
}
