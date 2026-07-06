// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"errors"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	codegraphv1 "github.com/eshu-hq/eshu/sdk/go/factschema/codegraph/v1"
)

// decodeCodegraphFile decodes one "file" envelope into the typed
// codegraphv1.File struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing a required
// identity field (repo_id, relative_path, parsed_file_data) or is otherwise
// malformed. It is the single decode site for the "file" kind's outer
// envelope on the code-graph-core reducer side: code-call extraction and the
// code-import repo-dependency edge builders decode through here for their
// join identity, and a missing required field is routed through
// partitionDecodeFailures so it dead-letters as a per-fact input_invalid
// quarantine rather than a silent empty-string graph identity (issue #4749).
//
// The returned struct's ParsedFileData field stays an OPEN map[string]any
// container. Specific inner keys are typed incrementally through the factschema
// DecodeParsedFileData* accessors, wrapped reducer-side in
// parsed_file_data_typed.go (issue #4750): S1 routes gomod_state and
// dead_code_file_root_kinds through typed accessors, while the wide per-language
// AST buckets (imports, functions, function_calls, ...) are still read raw until
// their own #4750 increment. The container itself is never narrowed, so an
// untyped key is read exactly as before this contract.
func decodeCodegraphFile(env facts.Envelope) (codegraphv1.File, error) {
	file, err := factschema.DecodeCodegraphFile(factschemaEnvelope(env))
	if err != nil {
		return codegraphv1.File{}, newFactDecodeError(factschema.FactKindCodegraphFile, err)
	}
	return file, nil
}

// decodeCodegraphRepository decodes one "repository" envelope into the typed
// codegraphv1.Repository struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing its required
// repo_id identity field or is otherwise malformed. It is the single decode
// site for the "repository" kind's outer envelope on the code-graph-core
// reducer side. repo_id is the only required field: the reducer read sites
// consume repo_id (required) plus source_run_id, local_path, and the delta
// path slices (all optional).
func decodeCodegraphRepository(env facts.Envelope) (codegraphv1.Repository, error) {
	repository, err := factschema.DecodeCodegraphRepository(factschemaEnvelope(env))
	if err != nil {
		return codegraphv1.Repository{}, newFactDecodeError(factschema.FactKindCodegraphRepository, err)
	}
	return repository, nil
}

// codegraphDecodeQuarantine builds a visible quarantinedFact from a codegraph
// decode error that partitionDecodeFailures did NOT classify as a per-fact
// input_invalid (the residual fatal branch — a payload type mismatch, or, only
// if a future change registers these kinds as versioned, an unsupported schema
// major). It carries the decode error's own classification and field so the
// malformed fact still surfaces on the existing input_invalid counter and
// structured error log through recordQuarantinedFacts, rather than being
// silently dropped by the batch extractor (which has no error return to
// propagate a fatal decode failure). The field is empty when the error is not
// attributable to a single field.
func codegraphDecodeQuarantine(env facts.Envelope, err error) quarantinedFact {
	q := quarantinedFact{
		factID:         env.FactID,
		factKind:       env.FactKind,
		classification: factschema.ClassificationInputInvalid,
	}
	var decodeErr *factschema.DecodeError
	if errors.As(err, &decodeErr) {
		if decodeErr.Classification != "" {
			q.classification = decodeErr.Classification
		}
		q.field = decodeErr.Field
	}
	return q
}
