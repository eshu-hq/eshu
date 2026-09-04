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

// containerImageIdentityFormatImageRef forwards to
// [containerimage.ContainerImageIdentityFormatImageRef].
// internal/reducer/supply_chain_impact_anchor_consensus.go still compares
// identity_format against this constant to prefer a v2 row over a legacy row
// sharing the same logical key; that family has not moved out of root
// (#6061).
const containerImageIdentityFormatImageRef = containerimage.ContainerImageIdentityFormatImageRef

// ociRepositoryID forwards to [payloadcore.OCIRepositoryID]. It was a
// one-line forwarder inside the container-image-identity family before that
// family moved to [containerimage] (#6061); the still-in-root
// supply_chain_impact_active_filter.go depends on it under this unqualified
// spelling.
func ociRepositoryID(payload map[string]any) string {
	return payloadcore.OCIRepositoryID(payload)
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
