// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schemadecode

import (
	"github.com/eshu-hq/eshu-sdk/factschema"
	runtimedeclarationv1 "github.com/eshu-hq/eshu-sdk/factschema/runtimedeclaration/v1"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/factdecode"
)

// DecodeRuntimeDeclarationVersion decodes one runtime_declaration.version
// envelope into the typed runtimedeclarationv1.Version through the contracts
// seam. A payload missing a required field (repo_id, relative_path,
// scope_dir, line_number, order_index, source_kind, runtime, raw_spec,
// spec_kind) returns a self-classifying fact-decode error, so the reducer
// quarantines that fact as input_invalid instead of projecting an empty
// identity. It is the single reducer-side decode site for the kind (#8816).
func DecodeRuntimeDeclarationVersion(env facts.Envelope) (runtimedeclarationv1.Version, error) {
	version, err := factschema.DecodeRuntimeDeclarationVersion(FactschemaEnvelope(env))
	if err != nil {
		return runtimedeclarationv1.Version{}, factdecode.NewFactDecodeError(factschema.FactKindRuntimeDeclarationVersion, err)
	}
	return version, nil
}
