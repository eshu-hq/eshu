package postgres

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// LoadCodeTaintEvidence implements reducer.CodeTaintEvidenceLoader by scanning
// code_taint_evidence facts for one scope generation and mapping each to a
// reducer-ready input. The collector has already resolved each finding to its
// Function entity uid, so the loader is a straight payload projection.
func (s FactStore) LoadCodeTaintEvidence(
	ctx context.Context,
	scopeID string,
	generationID string,
) ([]reducer.CodeTaintEvidenceInput, error) {
	envelopes, err := s.ListFactsByKind(ctx, scopeID, generationID, []string{facts.CodeTaintEvidenceFactKind})
	if err != nil {
		return nil, err
	}
	inputs := make([]reducer.CodeTaintEvidenceInput, 0, len(envelopes))
	for _, envelope := range envelopes {
		if envelope.IsTombstone {
			continue
		}
		inputs = append(inputs, codeTaintEvidenceFromEnvelope(envelope))
	}
	return inputs, nil
}

// codeTaintEvidenceFromEnvelope maps one code_taint_evidence fact payload to a
// reducer input.
func codeTaintEvidenceFromEnvelope(envelope facts.Envelope) reducer.CodeTaintEvidenceInput {
	payload := envelope.Payload
	return reducer.CodeTaintEvidenceInput{
		FunctionUID:  payloadString(payload, "function_uid"),
		FunctionName: payloadString(payload, "function_name"),
		RelativePath: payloadString(payload, "relative_path"),
		Language:     payloadString(payload, "language"),
		Kind:         payloadString(payload, "kind"),
		SinkKind:     payloadString(payload, "sink_kind"),
		SourceKind:   payloadString(payload, "source_kind"),
		Binding:      payloadString(payload, "binding"),
		SourceLine:   codeTaintPayloadInt(payload, "source_line"),
		SinkLine:     codeTaintPayloadInt(payload, "sink_line"),
		Confidence:   codeTaintPayloadFloat(payload, "confidence"),
		ClassContext: payloadString(payload, "class_context"),
		SinkLabel:    payloadString(payload, "sink_label"),
		SourceLabel:  payloadString(payload, "source_label"),
		GuardReason:  payloadString(payload, "guard_reason"),
	}
}

// codeTaintPayloadInt reads an integer payload field. JSONB scan yields float64
// for numbers, so that case is handled too.
func codeTaintPayloadInt(payload map[string]any, key string) int {
	switch value := payload[key].(type) {
	case float64:
		return int(value)
	case int:
		return value
	case int64:
		return int(value)
	}
	return 0
}

// codeTaintPayloadFloat reads a float payload field.
func codeTaintPayloadFloat(payload map[string]any, key string) float64 {
	switch value := payload[key].(type) {
	case float64:
		return value
	case int:
		return float64(value)
	}
	return 0
}
