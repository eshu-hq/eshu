// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	codeownersv1 "github.com/eshu-hq/eshu/sdk/go/factschema/codeowners/v1"
)

// decodeCodeownersOwnership decodes one codeowners.ownership envelope into the
// typed codeownersv1.Ownership struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing a required
// field (repo_id, source_path, pattern, owners, order_index) or is otherwise
// malformed. It is the single decode site for the codeowners.ownership kind on
// the reducer side (issue #5419 Phase 3): CodeownersOwnershipEdgeMaterializationHandler
// decodes through here, and a missing required field is routed through
// partitionDecodeFailures so it dead-letters as a per-fact input_invalid
// quarantine rather than a silent empty-identity graph write.
func decodeCodeownersOwnership(env facts.Envelope) (codeownersv1.Ownership, error) {
	ownership, err := factschema.DecodeCodeownersOwnership(factschemaEnvelope(env))
	if err != nil {
		return codeownersv1.Ownership{}, newFactDecodeError(factschema.FactKindCodeownersOwnership, err)
	}
	return ownership, nil
}
