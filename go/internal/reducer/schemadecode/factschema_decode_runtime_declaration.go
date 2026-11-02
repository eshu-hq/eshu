// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schemadecode

import (
	"fmt"

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
//
// A negative version part (major, minor, or patch) is also input_invalid and
// quarantines. Version parts are non-negative or absent, and the query's
// undecided test (`coalesce(part, -1) = -1` in
// go/internal/query/runtime/declaration/cypher.go) treats -1 as the
// absent-version sentinel, so a projected -1 would be double-counted as both
// below-bound and undecided (#9147).
func DecodeRuntimeDeclarationVersion(env facts.Envelope) (runtimedeclarationv1.Version, error) {
	version, err := factschema.DecodeRuntimeDeclarationVersion(FactschemaEnvelope(env))
	if err != nil {
		return runtimedeclarationv1.Version{}, factdecode.NewFactDecodeError(factschema.FactKindRuntimeDeclarationVersion, err)
	}
	if field, negative := negativeVersionPart(version); negative {
		return runtimedeclarationv1.Version{}, factdecode.NewFactDecodeError(
			factschema.FactKindRuntimeDeclarationVersion,
			&factschema.DecodeError{
				FactKind:       factschema.FactKindRuntimeDeclarationVersion,
				Classification: factschema.ClassificationInputInvalid,
				Field:          field,
				Err:            fmt.Errorf("negative version part %s", field),
			},
		)
	}
	return version, nil
}

// negativeVersionPart reports the first version part that is present and
// negative. The sentinel -1 is the critical value — it collides with the
// query's absent-version test — but any negative part is rejected because the
// contract admits only non-negative parts or absence.
func negativeVersionPart(version runtimedeclarationv1.Version) (string, bool) {
	parts := []struct {
		name  string
		value *int
	}{
		{"version_major", version.VersionMajor},
		{"version_minor", version.VersionMinor},
		{"version_patch", version.VersionPatch},
	}
	for _, part := range parts {
		if part.value != nil && *part.value < 0 {
			return part.name, true
		}
	}
	return "", false
}
