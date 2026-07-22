// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	submodulev1 "github.com/eshu-hq/eshu/sdk/go/factschema/submodule/v1"
)

// decodeSubmodulePin decodes one submodule.pin envelope into the typed
// submodulev1.Pin struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing a required
// field (parent_repo_id, submodule_path) or is otherwise malformed. It is the
// single decode site for the submodule.pin kind on the reducer side (issue
// #5420 Phase 3): SubmodulePinEdgeMaterializationHandler decodes through
// here, and a missing required field is routed through
// partitionDecodeFailures so it dead-letters as a per-fact input_invalid
// quarantine rather than a silent empty-identity graph write.
func decodeSubmodulePin(env facts.Envelope) (submodulev1.Pin, error) {
	pin, err := factschema.DecodeSubmodulePin(factschemaEnvelope(env))
	if err != nil {
		return submodulev1.Pin{}, newFactDecodeError(factschema.FactKindSubmodulePin, err)
	}
	return pin, nil
}
