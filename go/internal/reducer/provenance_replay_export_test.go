// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"strings"
)

// PackageOwnershipPublishesRowsForReplayTest exposes the package-private row
// mapper only to the external replay test compiled with this package's tests.
func PackageOwnershipPublishesRowsForReplayTest(
	decisions []PackageSourceCorrelationDecision,
) []map[string]any {
	return packageOwnershipPublishesRows(decisions)
}

// PackagePublicationPublishesRowsForReplayTest exposes the package-private row
// mapper only to the external replay test compiled with this package's tests.
func PackagePublicationPublishesRowsForReplayTest(
	decisions []PackagePublicationDecision,
) []map[string]any {
	return packagePublicationPublishesRows(decisions)
}

// ContainerImageBuiltFromRowsForReplayTest exposes the package-private row
// mapper only to the external replay test compiled with this package's tests.
func ContainerImageBuiltFromRowsForReplayTest(
	decisions []ContainerImageIdentityDecision,
) []map[string]any {
	return containerImageBuiltFromRows(decisions)
}

// ContainerImageEffectiveRowsForReplayTest normalizes decisions through the
// digest-v3 support set used by production and exposes both graph row families
// to the external replay test compiled with this package's tests.
func ContainerImageEffectiveRowsForReplayTest(
	decisions []ContainerImageIdentityDecision,
	repositoryID string,
) ([]map[string]any, []map[string]any, error) {
	supports, err := containerImageIdentitySupportsForReplayTest("replay-test", decisions)
	if err != nil {
		return nil, nil, err
	}
	return containerImageBuiltFromSupportRows(supports),
		containerImageDerivedFromSupportRows(supports, repositoryID), nil
}

// ProjectPackageProvenanceEdgesForReplayTest drives the package-private
// retract-first projection through the real writer supplied by the replay test.
func ProjectPackageProvenanceEdgesForReplayTest(
	ctx context.Context,
	writer PackageProvenanceEdgeWriter,
	scopeID string,
	generationID string,
	ownershipDecisions []PackageSourceCorrelationDecision,
	publicationDecisions []PackagePublicationDecision,
) error {
	handler := PackageSourceCorrelationHandler{ProvenanceEdgeWriter: writer}
	return handler.projectPackageProvenanceEdges(
		ctx,
		Intent{ScopeID: scopeID, GenerationID: generationID},
		ownershipDecisions,
		publicationDecisions,
	)
}

// ProjectEffectiveContainerImageIdentityEdgesForReplayTest normalizes cassette
// decisions through the production digest-v3 support representation, then
// drives the production effective-support projector through the real writers.
func ProjectEffectiveContainerImageIdentityEdgesForReplayTest(
	ctx context.Context,
	provenanceWriter ContainerImageProvenanceEdgeWriter,
	derivedFromWriter ContainerImageDerivedFromEdgeWriter,
	scopeID string,
	generationID string,
	decisions []ContainerImageIdentityDecision,
) error {
	supports, err := containerImageIdentitySupportsForReplayTest(scopeID, decisions)
	if err != nil {
		return err
	}
	handler := ContainerImageIdentityHandler{
		ProvenanceEdgeWriter:  provenanceWriter,
		DerivedFromEdgeWriter: derivedFromWriter,
	}
	return handler.projectEffectiveContainerImageIdentityEdges(
		ctx,
		Intent{ScopeID: scopeID, GenerationID: generationID},
		ContainerImageIdentityWriteResult{
			effectiveSupports:          supports,
			effectiveProjectionPresent: true,
		},
	)
}

func containerImageIdentitySupportsForReplayTest(
	scopeID string,
	decisions []ContainerImageIdentityDecision,
) ([]containerImageIdentitySupport, error) {
	supportSet, err := buildContainerImageIdentitySupportSet(ContainerImageIdentityWrite{
		ScopeID:   strings.TrimSpace(scopeID),
		Decisions: decisions,
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("normalize replay container image supports: %w", err)
	}
	return supportSet.Supports, nil
}
