// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package containerimage

import (
	"context"
	"fmt"
	"strings"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// This file exposes package-private container-image-identity internals to
// two callers outside this package that a _test.go-only export cannot reach:
// internal/reducer/provenance_replay_tombstone_live_test.go drives this
// family together with the still-in-root package-source-correlation family
// in one cross-family cassette replay, and this package's own
// container_image_identity_cicdrun_cassette_test.go (an external
// containerimage_test package, so it too cannot see an internal test file's
// exports) calls ContainerImageBuiltFromRowsForReplayTest directly. A _test.go
// file's exported symbols are visible only to the package's own test binary,
// never to another package's normal (non-test) import, so these have to be
// ordinary exported functions rather than the root's pre-#6061
// provenance_replay_export_test.go pattern. None of them are called by
// production code.

// ContainerImageBuiltFromRowsForReplayTest exposes the package-private
// BUILT_FROM row mapper to the reducer root's cross-family provenance replay
// test.
func ContainerImageBuiltFromRowsForReplayTest(
	decisions []ContainerImageIdentityDecision,
) []map[string]any {
	return containerImageBuiltFromRows(decisions)
}

// ContainerImageEffectiveRowsForReplayTest normalizes decisions through the
// digest-v3 support set used by production and exposes both graph row
// families to the reducer root's cross-family provenance replay test.
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

// ProjectEffectiveContainerImageIdentityEdgesForReplayTest normalizes
// cassette decisions through the production digest-v3 support
// representation, then drives the production effective-support projector
// through the real writers the reducer root's cross-family provenance replay
// test supplies.
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
		reducercontract.Intent{ScopeID: scopeID, GenerationID: generationID},
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
