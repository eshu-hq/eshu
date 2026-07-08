// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	codegraphv1 "github.com/eshu-hq/eshu/sdk/go/factschema/codegraph/v1"
)

// codegraphCanonicalStage is the bounded telemetry stage label the projector's
// source-local codegraph canonical extractor reports on
// eshu_dp_projector_input_invalid_facts_total.
const codegraphCanonicalStage = "codegraph_canonical"

// decodeCodegraphRepository decodes one repository envelope into the typed
// struct through the contracts seam. A missing required field (repo_id) yields
// a self-classifying *projectorDecodeError.
func decodeCodegraphRepository(env facts.Envelope) (codegraphv1.Repository, error) {
	repository, err := factschema.DecodeCodegraphRepository(factschemaEnvelope(env))
	if err != nil {
		return codegraphv1.Repository{}, newProjectorDecodeError(factschema.FactKindCodegraphRepository, err)
	}
	return repository, nil
}

// decodeCodegraphFile decodes one file envelope into the typed struct through
// the contracts seam. A missing required field (repo_id, relative_path, or
// parsed_file_data) yields a self-classifying *projectorDecodeError.
func decodeCodegraphFile(env facts.Envelope) (codegraphv1.File, error) {
	file, err := factschema.DecodeCodegraphFile(factschemaEnvelope(env))
	if err != nil {
		return codegraphv1.File{}, newProjectorDecodeError(factschema.FactKindCodegraphFile, err)
	}
	return file, nil
}

// codegraphDerefString returns the value a *string points at, or "" when it is
// nil. The typed codegraph structs carry optional fields as *string so an
// absent key stays distinct from an observed empty value; the row builders
// substitute "" for an unobserved field, matching the pre-typing payloadString
// behavior.
func codegraphDerefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
