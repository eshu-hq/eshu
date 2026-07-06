// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	codedataflowv1 "github.com/eshu-hq/eshu/sdk/go/factschema/codedataflow/v1"
)

// decodeCodeDataflowScanned decodes one "code_dataflow_scanned" envelope into
// the typed codedataflowv1.DataflowScanned struct through the contracts seam.
// The struct declares zero required fields (RepoID is optional, matching the
// projector's own tolerant read), so this can only fail on a genuine
// type-mismatch payload, never a missing-field input_invalid.
func decodeCodeDataflowScanned(env facts.Envelope) (codedataflowv1.DataflowScanned, error) {
	scanned, err := factschema.DecodeCodeDataflowScanned(factschemaEnvelope(env))
	if err != nil {
		return codedataflowv1.DataflowScanned{}, newFactDecodeError(factschema.FactKindCodeDataflowScanned, err)
	}
	return scanned, nil
}

// decodeCodeDataflowFunction decodes one "code_dataflow_function" envelope
// into the typed codedataflowv1.DataflowFunction struct through the contracts
// seam, returning a self-classifying *factDecodeError when the payload is
// missing a required identity field (repo_id, relative_path, function_name)
// or is otherwise malformed. No reducer materialization handler decodes this
// kind today (query-layer-only consumer); this wrapper exists for the
// family's manifest/schema completeness and for a future reducer consumer.
func decodeCodeDataflowFunction(env facts.Envelope) (codedataflowv1.DataflowFunction, error) {
	function, err := factschema.DecodeCodeDataflowFunction(factschemaEnvelope(env))
	if err != nil {
		return codedataflowv1.DataflowFunction{}, newFactDecodeError(factschema.FactKindCodeDataflowFunction, err)
	}
	return function, nil
}

// decodeCodeFunctionSummary decodes one "code_function_summary" envelope into
// the typed codedataflowv1.FunctionSummary struct through the contracts seam,
// returning a self-classifying *factDecodeError when the payload is missing
// its required function_id identity field or is otherwise malformed. It is
// the single decode site for this kind: the reducer handler's
// ExtractCodeFunctionSummaryEffectsWithQuarantine /
// ExtractCodeFunctionGraphIDsWithQuarantine extractors decode through here
// (over the raw envelopes postgres LoadCodeFunctionSummaryFacts /
// LoadCodeFunctionGraphIDFacts return), so a code_function_summary fact
// missing function_id dead-letters as input_invalid via
// partitionDecodeFailures/recordQuarantinedFacts instead of being silently
// skipped with no operator signal.
func decodeCodeFunctionSummary(env facts.Envelope) (codedataflowv1.FunctionSummary, error) {
	summary, err := factschema.DecodeCodeFunctionSummary(factschemaEnvelope(env))
	if err != nil {
		return codedataflowv1.FunctionSummary{}, newFactDecodeError(factschema.FactKindCodeFunctionSummary, err)
	}
	return summary, nil
}

// decodeCodeFunctionSource decodes one "code_function_source" envelope into
// the typed codedataflowv1.FunctionSource struct through the contracts seam,
// returning a self-classifying *factDecodeError when the payload is missing a
// required field (function_id, kind) or is otherwise malformed. It is the
// single decode site for this kind: the reducer handler's
// ExtractCodeFunctionSourcesWithQuarantine extractor decodes through here
// (over the raw envelopes postgres LoadCodeFunctionSourceFacts returns), so a
// code_function_source fact missing function_id/kind dead-letters as
// input_invalid rather than being silently skipped.
func decodeCodeFunctionSource(env facts.Envelope) (codedataflowv1.FunctionSource, error) {
	source, err := factschema.DecodeCodeFunctionSource(factschemaEnvelope(env))
	if err != nil {
		return codedataflowv1.FunctionSource{}, newFactDecodeError(factschema.FactKindCodeFunctionSource, err)
	}
	return source, nil
}

// decodeCodeTaintEvidence decodes one "code_taint_evidence" envelope into the
// typed codedataflowv1.TaintEvidence struct through the contracts seam,
// returning a self-classifying *factDecodeError when the payload is missing
// its required function_uid identity field or is otherwise malformed. It is
// the single decode site for this kind: the reducer handler's
// ExtractCodeTaintEvidenceRowsWithQuarantine extractor decodes through here
// (over the raw envelopes postgres LoadCodeTaintEvidence returns), so a
// finding missing its attachment identity dead-letters as input_invalid via
// partitionDecodeFailures/recordQuarantinedFacts instead of silently producing
// an evidence row keyed on an empty-string function uid.
func decodeCodeTaintEvidence(env facts.Envelope) (codedataflowv1.TaintEvidence, error) {
	evidence, err := factschema.DecodeCodeTaintEvidence(factschemaEnvelope(env))
	if err != nil {
		return codedataflowv1.TaintEvidence{}, newFactDecodeError(factschema.FactKindCodeTaintEvidence, err)
	}
	return evidence, nil
}

// decodeCodeInterprocEvidence decodes one "code_interproc_evidence" envelope
// into the typed codedataflowv1.InterprocEvidence struct through the contracts
// seam, returning a self-classifying *factDecodeError when the payload is
// missing a required endpoint field (source_function_uid,
// sink_function_uid) or is otherwise malformed. It is the single decode site
// for this kind: the reducer handler's
// ExtractCodeInterprocEvidenceRowsWithQuarantine extractor decodes through
// here (over the raw envelopes postgres LoadCodeInterprocEvidenceFacts
// returns), so a finding missing either edge endpoint dead-letters as
// input_invalid via partitionDecodeFailures/recordQuarantinedFacts instead of
// being silently dropped with no operator signal.
func decodeCodeInterprocEvidence(env facts.Envelope) (codedataflowv1.InterprocEvidence, error) {
	evidence, err := factschema.DecodeCodeInterprocEvidence(factschemaEnvelope(env))
	if err != nil {
		return codedataflowv1.InterprocEvidence{}, newFactDecodeError(factschema.FactKindCodeInterprocEvidence, err)
	}
	return evidence, nil
}
