// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"strings"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// containerImageDerivedFromProvenanceEvidenceSource tags DERIVED_FROM edges
// projected from container_image_identity decisions. It is distinct from the
// BUILT_FROM evidence source the same handler writes
// (container_image_provenance_edges.go) so each edge type's retract-first pass
// only ever deletes its own domain's edges
// (docs/internal/design/5472-graph-projection-policy.md).
const containerImageDerivedFromProvenanceEvidenceSource = "reducer/container-image-base-image"

// containerImageDerivedFromBasisRepositorySingleBase is the attribution basis
// stamped on an edge inferred from Dockerfile evidence alone: the declaring
// repository resolved to exactly one distinct exact-digest runtime base, so
// every exact-digest image built from that repository derives from it.
//
// This is the extension seam for the attribution model. Dockerfile-parsed
// evidence cannot link a specific built image digest to a specific Dockerfile,
// which is why a repository with more than one distinct base projects nothing
// today. When CI or SLSA provenance later supplies that per-image link, it
// lands as an additional basis value on the same edge type and the same row
// shape -- a strictly more precise attribution admitted alongside this one,
// with no edge-type, schema, or query change.
const containerImageDerivedFromBasisRepositorySingleBase = "repository_single_base"

// ContainerImageDerivedFromEdgeWriter persists and retracts canonical
// DERIVED_FROM edges between a built ContainerImage and the base ContainerImage
// its repository's Dockerfile declared. Implementations MUST be idempotent by
// (image digest, DERIVED_FROM, base digest) so reducer retries and re-projected
// generations converge on one edge, and MUST NOT fabricate an endpoint node: a
// row whose image or base node is absent is a no-op.
type ContainerImageDerivedFromEdgeWriter interface {
	WriteDerivedFromEdges(ctx context.Context, rows []map[string]any, scopeID, generationID, evidenceSource string) error
	RetractDerivedFromEdges(ctx context.Context, scopeID, generationID, evidenceSource string) error
}

// containerImageDerivedFromRows builds DERIVED_FROM edge rows from
// container-image-identity decisions.
//
// Both endpoints must be exact_digest (#5472 decision 4). The canonical writer
// matches a ContainerImage by digest, so a non-exact endpoint has no node to
// attach to; and the question this edge exists to answer -- does my image
// inherit CVE-X from its base -- is only answerable when the base resolves to
// the specific digest whose vulnerabilities are known. A tag-only or
// ARG-parameterized base stays ambiguous and projects nothing.
//
// Attribution is deliberately conservative. Dockerfile evidence names a
// repository's runtime base but never says which of that repository's built
// images came from which Dockerfile, so a repository resolving to more than one
// distinct base digest is ambiguous and projects NO edge. The alternative --
// an all-pairs fan-out over every built image and every base -- would assert
// CVE lineage that does not exist in a monorepo that builds several images from
// several Dockerfiles, and a fabricated inheritance claim is a worse failure
// than a missing one.
func containerImageDerivedFromRows(decisions []ContainerImageIdentityDecision) []map[string]any {
	basesByRepository := make(map[string]map[string]struct{})
	for _, decision := range decisions {
		digest := exactContainerImageDigest(decision)
		if digest == "" {
			continue
		}
		for _, repositoryID := range uniqueSortedStrings(decision.BaseImageForRepositoryIDs) {
			if repositoryID == "" {
				continue
			}
			if basesByRepository[repositoryID] == nil {
				basesByRepository[repositoryID] = make(map[string]struct{})
			}
			basesByRepository[repositoryID][digest] = struct{}{}
		}
	}

	// A repository is attributable only when its Dockerfiles agree on one
	// runtime base. Anything else is the ambiguous case above.
	baseForRepository := make(map[string]string, len(basesByRepository))
	for repositoryID, digests := range basesByRepository {
		if len(digests) != 1 {
			continue
		}
		for digest := range digests {
			baseForRepository[repositoryID] = digest
		}
	}
	if len(baseForRepository) == 0 {
		return nil
	}

	rows := make([]map[string]any, 0, len(decisions))
	seen := make(map[string]struct{}, len(decisions))
	for _, decision := range decisions {
		digest := exactContainerImageDigest(decision)
		if digest == "" {
			continue
		}
		for _, repositoryID := range uniqueSortedStrings(decision.SourceRepositoryIDs) {
			baseDigest, ok := baseForRepository[repositoryID]
			if !ok {
				continue
			}
			// An image is not its own ancestor. A repository that declares the
			// same digest it builds (a base image repository rebuilding itself,
			// or a digest reused across both roles) would otherwise emit a
			// self-loop that makes CVE lineage traversal cyclic.
			if baseDigest == digest {
				continue
			}
			key := digest + "\x00" + baseDigest
			if _, duplicate := seen[key]; duplicate {
				continue
			}
			seen[key] = struct{}{}
			rows = append(rows, map[string]any{
				"digest":            digest,
				"base_digest":       baseDigest,
				"attribution_basis": containerImageDerivedFromBasisRepositorySingleBase,
			})
		}
	}
	if len(rows) == 0 {
		return nil
	}
	return rows
}

// exactContainerImageDigest returns a decision's digest when the decision is an
// exact_digest outcome carrying a non-empty digest, and an empty string
// otherwise. It is the single gate both DERIVED_FROM endpoints pass through,
// so the exact-only tiering cannot drift between the child side and the base
// side.
func exactContainerImageDigest(decision ContainerImageIdentityDecision) string {
	if decision.Outcome != ContainerImageIdentityExactDigest {
		return ""
	}
	return strings.TrimSpace(decision.Digest)
}

// projectContainerImageDerivedFromEdges retracts this generation's prior
// DERIVED_FROM edges owned by this evidence_source and re-projects the current
// attributable decisions. It is a no-op when no writer is wired, so the
// container-image-identity profile stays Postgres-only until an adapter is
// configured, and it never fails the identity result for an empty projection;
// only a writer error propagates.
//
// Retract runs unconditionally, ahead of any row check, so a generation that
// drops a previously-attributable base -- a Dockerfile deleted, or a second
// Dockerfile added that makes the repository ambiguous -- still removes the
// stale edge (#5472 retract-first-per-generation).
func (h ContainerImageIdentityHandler) projectContainerImageDerivedFromEdges(
	ctx context.Context,
	intent Intent,
	decisions []ContainerImageIdentityDecision,
) error {
	if h.DerivedFromEdgeWriter == nil {
		return nil
	}

	if err := h.DerivedFromEdgeWriter.RetractDerivedFromEdges(
		ctx, intent.ScopeID, intent.GenerationID, containerImageDerivedFromProvenanceEvidenceSource,
	); err != nil {
		return fmt.Errorf("retract container image derived_from provenance edges: %w", err)
	}

	rows := containerImageDerivedFromRows(decisions)
	h.emitDerivedFromEdgeCounter(ctx, "materialized", len(rows))
	if len(rows) == 0 {
		return nil
	}
	if err := h.DerivedFromEdgeWriter.WriteDerivedFromEdges(
		ctx, rows, intent.ScopeID, intent.GenerationID, containerImageDerivedFromProvenanceEvidenceSource,
	); err != nil {
		return fmt.Errorf("write container image derived_from provenance edges: %w", err)
	}
	return nil
}

// emitDerivedFromEdgeCounter records a ProvenanceEdges counter sample for the
// DERIVED_FROM projection, labeled by outcome. It is a no-op when no
// Instruments are wired or the count is zero.
func (h ContainerImageIdentityHandler) emitDerivedFromEdgeCounter(ctx context.Context, outcome string, count int) {
	if h.Instruments == nil || h.Instruments.ProvenanceEdges == nil || count <= 0 {
		return
	}
	h.Instruments.ProvenanceEdges.Add(
		ctx,
		int64(count),
		metric.WithAttributes(
			telemetry.AttrDomain(containerImageDerivedFromProvenanceEvidenceSource),
			telemetry.AttrOutcome(outcome),
		),
	)
}
