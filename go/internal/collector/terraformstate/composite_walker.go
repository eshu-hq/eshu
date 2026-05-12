package terraformstate

import (
	"encoding/json"
	"fmt"
)

// readCompositeValue is the streaming nested walker the parser invokes when a
// SchemaKnown composite attribute opens with '{' or '['. The walker reuses the
// same json.Decoder the rest of the parser drives, so it never calls
// json.Unmarshal or decoder.Decode(&v any) on a whole subtree. Memory growth
// is bounded by schema depth (the AWS provider 5.100.0 schema bundle peaks at
// six levels) and per-leaf scalar size, not by the size of the surrounding
// state file.
//
// Caller hands in the opening delimiter (already consumed from the decoder),
// the resource type, and the top-level attribute key. The walker returns the
// captured value or errCompositeShapeMismatch — at which point the walker has
// drained the malformed sub-document to keep the outer decoder in a consumable
// state.
func (p *stateParser) readCompositeValue(opening json.Delim, resourceType string, attributeKey string) (any, error) {
	value, walkErr := p.walkComposite(opening, attributeKey)
	if walkErr == nil {
		return value, nil
	}
	// Drain whatever the walker left open so the outer parser keeps
	// consuming valid JSON after the malformed sub-document. The opening
	// delimiter has already been consumed by readAttributeBody, so depth
	// starts at one regardless of how deep the walker bailed.
	_ = drainBalancedScope(p.decoder)
	p.recordCompositeSkip(resourceType, attributeKey, CompositeCaptureSkipReasonWalkerError, walkErr)
	return nil, errCompositeShapeMismatch
}

func (p *stateParser) walkComposite(opening json.Delim, path string) (any, error) {
	switch opening {
	case '{':
		return p.walkObject(path)
	case '[':
		return p.walkArray(path)
	default:
		return nil, fmt.Errorf("unsupported json delimiter %q for composite at %q", opening, path)
	}
}

// walkObject reads a JSON object whose opening '{' was already consumed.
func (p *stateParser) walkObject(path string) (map[string]any, error) {
	out := map[string]any{}
	for p.decoder.More() {
		token, err := p.decoder.Token()
		if err != nil {
			return nil, fmt.Errorf("walk %q object key: %w", path, err)
		}
		key, ok := token.(string)
		if !ok {
			return nil, fmt.Errorf("walk %q expected object key, got %T", path, token)
		}
		child, err := p.walkValue(path + "." + key)
		if err != nil {
			return nil, err
		}
		out[key] = child
	}
	if _, err := p.decoder.Token(); err != nil {
		return nil, fmt.Errorf("walk %q close object: %w", path, err)
	}
	return out, nil
}

// walkArray reads a JSON array whose opening '[' was already consumed. The
// returned slice preserves length so multi-element repeated blocks (e.g.,
// aws_security_group.ingress) flow through unchanged; PR #198's first-wins
// truncation runs downstream in
// storage/postgres/tfstate_drift_evidence_state_row.go.
func (p *stateParser) walkArray(path string) ([]any, error) {
	out := []any{}
	for p.decoder.More() {
		child, err := p.walkValue(path)
		if err != nil {
			return nil, err
		}
		out = append(out, child)
	}
	if _, err := p.decoder.Token(); err != nil {
		return nil, fmt.Errorf("walk %q close array: %w", path, err)
	}
	return out, nil
}

func (p *stateParser) walkValue(path string) (any, error) {
	token, err := p.decoder.Token()
	if err != nil {
		return nil, fmt.Errorf("walk %q value: %w", path, err)
	}
	delim, isDelim := token.(json.Delim)
	if !isDelim {
		return token, nil
	}
	return p.walkComposite(delim, path)
}

// drainBalancedScope advances the decoder until the still-open container the
// walker started has been fully consumed. Used on shape-mismatch paths so the
// outer parser keeps reading valid state JSON after the walker bails on a
// malformed sub-document. Depth begins at one because readAttributeBody
// already consumed the outer opening delimiter before invoking the walker.
func drainBalancedScope(decoder *json.Decoder) error {
	depth := 1
	for depth > 0 {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		nested, isDelim := token.(json.Delim)
		if !isDelim {
			continue
		}
		switch nested {
		case '{', '[':
			depth++
		case '}', ']':
			depth--
		}
	}
	return nil
}

// recordCompositeSkip emits a CompositeCaptureSkip with the closed-enum
// Reason label so the recorder can disambiguate the operator-visible cases
// (schema_unknown vs. shape_mismatch) on the
// eshu_dp_drift_schema_unknown_composite_total counter. The high-cardinality
// attribute_key, source path, and walker error string stay in the
// structured-log companion attached to the same Record call.
func (p *stateParser) recordCompositeSkip(resourceType string, attributeKey string, reason string, err error) {
	if p.options.CompositeCaptureMetrics == nil {
		return
	}
	p.options.CompositeCaptureMetrics.Record(p.ctx, CompositeCaptureSkip{
		ResourceType: resourceType,
		AttributeKey: attributeKey,
		Path:         "resources.*.attributes." + attributeKey,
		Reason:       reason,
		Err:          err,
	})
}
