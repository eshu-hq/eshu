// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// LoadCodeInterprocEvidence implements reducer.CodeInterprocEvidenceLoader by
// scanning code_interproc_evidence facts for one scope generation and mapping
// each to a reducer-ready input. The collector has already resolved both the
// source and sink endpoints to their Function entity uids, so the loader is a
// straight payload projection.
func (s FactStore) LoadCodeInterprocEvidence(
	ctx context.Context,
	scopeID string,
	generationID string,
) ([]reducer.CodeInterprocEvidenceInput, error) {
	envelopes, err := s.ListFactsByKind(ctx, scopeID, generationID, []string{facts.CodeInterprocEvidenceFactKind})
	if err != nil {
		return nil, err
	}
	inputs := make([]reducer.CodeInterprocEvidenceInput, 0, len(envelopes))
	for _, envelope := range envelopes {
		if envelope.IsTombstone {
			continue
		}
		inputs = append(inputs, codeInterprocEvidenceFromEnvelope(envelope))
	}
	return inputs, nil
}

// codeInterprocEvidenceFromEnvelope maps one code_interproc_evidence fact payload
// to a reducer input. confidence reuses the taint loader's float coercion
// (JSONB scan yields float64) and cloud the generic payload bool reader; the cloud
// flag is only present in the payload when true.
func codeInterprocEvidenceFromEnvelope(envelope facts.Envelope) reducer.CodeInterprocEvidenceInput {
	payload := envelope.Payload
	return reducer.CodeInterprocEvidenceInput{
		SourceFunctionUID:  payloadString(payload, "source_function_uid"),
		SinkFunctionUID:    payloadString(payload, "sink_function_uid"),
		RelativePath:       payloadString(payload, "relative_path"),
		SourceFunctionName: payloadString(payload, "source_function_name"),
		SinkFunctionName:   payloadString(payload, "sink_function_name"),
		Language:           payloadString(payload, "language"),
		SinkKind:           payloadString(payload, "sink_kind"),
		SourceKind:         payloadString(payload, "source_kind"),
		Confidence:         codeTaintPayloadFloat(payload, "confidence"),
		Cloud:              payloadBool(payload, "cloud"),
		WhyTrail:           payloadMapSlice(payload, "why_trail"),
		WhyTrailTruncated:  payloadBool(payload, "why_trail_truncated"),
	}
}
