package terraformstate

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/redact"
)

type attributeValue struct {
	Key           string
	Value         any
	Scalar        bool
	TagMap        bool
	Tags          []tagValue
	InvalidTagMap bool
}

// readAttributeValues consumes the JSON object after "attributes" for one
// Terraform-state resource instance. Scalar values flow through verbatim;
// the "tags" / "tags_all" maps are walked into the legacy tag observation
// path; composite values whose (resourceType, key) pair is recognized by
// the ProviderSchemaResolver are captured via the streaming nested walker;
// every other composite still skips through skipNested to preserve the
// memory contract enforced by TestParseStream_PeakMemoryGate.
//
// The walker emits the nested-singleton-array shape the loader's
// flattenStateAttributes (storage/postgres/tfstate_drift_evidence.go)
// expects so the drift handler can compare config-side and state-side dot
// paths.
func (p *stateParser) readAttributeValues(resourceType string) ([]attributeValue, error) {
	if err := readOpeningDelim(p.decoder, '{', "terraform state resource attributes"); err != nil {
		return nil, err
	}
	attributes := []attributeValue{}
	for p.decoder.More() {
		token, err := p.decoder.Token()
		if err != nil {
			return nil, fmt.Errorf("read terraform state resource attribute key: %w", err)
		}
		key, ok := token.(string)
		if !ok {
			return nil, fmt.Errorf("terraform state resource attribute key must be a string")
		}
		switch key {
		case "tags", "tags_all":
			tags, valid, err := readTagValues(p.decoder, key)
			if err != nil {
				return nil, err
			}
			attributes = append(attributes, attributeValue{Key: key, TagMap: true, Tags: tags, InvalidTagMap: !valid})
		default:
			value, scalar, err := p.readAttributeBody(resourceType, key)
			if err != nil {
				return nil, fmt.Errorf("decode terraform state resource attribute %q: %w", key, err)
			}
			attributes = append(attributes, attributeValue{Key: key, Value: value, Scalar: scalar})
		}
	}
	if _, err := p.decoder.Token(); err != nil {
		return nil, fmt.Errorf("close terraform state resource attributes: %w", err)
	}
	return attributes, nil
}

// readAttributeBody decides at the attribute boundary whether the next JSON
// token belongs in a SchemaKnown composite-capture path or in the existing
// skip-on-composite fail-closed path. Scalars always pass through unchanged.
func (p *stateParser) readAttributeBody(resourceType string, key string) (any, bool, error) {
	token, err := p.decoder.Token()
	if err != nil {
		return nil, false, err
	}
	delim, isDelim := token.(json.Delim)
	if !isDelim {
		return token, true, nil
	}
	if delim != '{' && delim != '[' {
		return nil, false, fmt.Errorf("unsupported json delimiter %q at terraform state attribute %q", delim, key)
	}
	if !p.schemaKnownCompositeCapture(resourceType, key) {
		p.recordCompositeShapeMismatch(resourceType, key, errCompositeSchemaUnknown)
		return nil, false, skipNested(p.decoder, delim)
	}
	value, err := p.readCompositeValue(delim, resourceType, key)
	if err != nil {
		if errors.Is(err, errCompositeShapeMismatch) {
			// Walker already balanced the open delimiter and recorded
			// telemetry; surface the attribute as a nil composite so the
			// downstream classifier treats it identically to the previous
			// skipNested behavior.
			return nil, false, nil
		}
		return nil, false, err
	}
	return value, false, nil
}

// schemaKnownCompositeCapture returns true when the streaming nested walker
// should capture the composite at (resourceType, key) instead of skipping it.
// A SchemaKnown composite whose top-level source path matches a sensitive
// key (e.g., resources.<addr>.attributes.password_history) still skips so
// the redact policy's per-source segment match never sees the raw value.
func (p *stateParser) schemaKnownCompositeCapture(resourceType string, key string) bool {
	if p.schemaTrust(resourceType, key) != redact.SchemaKnown {
		return false
	}
	if p.options.RedactionRules.Version() == "" {
		return false
	}
	return true
}

// errCompositeShapeMismatch is returned by the streaming nested walker when
// the state JSON shape disagrees with the provider schema's expectation. The
// walker drains the malformed sub-document to keep the outer decoder in a
// consumable state, records the skip via CompositeCaptureRecorder, and lets
// the caller treat the attribute as absent (matching the pre-walker
// fail-closed default at this boundary).
var errCompositeShapeMismatch = errors.New("terraform state composite shape mismatch")

// errCompositeSchemaUnknown is the diagnostic class recorded on the
// CompositeCaptureRecorder when the parser drops a composite attribute
// because the loaded ProviderSchemaResolver does not cover it. Operators
// reading the eshu_dp_drift_schema_unknown_composite_total counter use this
// signal to detect provider-schema drift: real state JSON has shipped a
// nested block (or composite-typed attribute) the bundled schema does not
// know about, and drift detection for that attribute will silently regress
// until the bundle is refreshed.
var errCompositeSchemaUnknown = errors.New("terraform state composite is not covered by provider schema")

func (p *stateParser) classifyAttributes(resourceType string, address string, input []attributeValue) (map[string]any, error) {
	attributes := make(map[string]any, len(input))
	for _, attribute := range input {
		if attribute.TagMap {
			continue
		}
		if err := p.classifyAttribute(attributes, resourceType, address, attribute); err != nil {
			return nil, err
		}
	}
	return attributes, nil
}

func (p *stateParser) classifyAttribute(attributes map[string]any, resourceType string, address string, attribute attributeValue) error {
	source := "resources." + address + ".attributes." + attribute.Key
	kind := redact.FieldComposite
	if attribute.Scalar {
		kind = redact.FieldScalar
	}
	decision := p.options.RedactionRules.Classify(source, p.schemaTrust(resourceType, attribute.Key), kind)

	switch decision.Action {
	case redact.ActionPreserve:
		if attribute.Scalar || attribute.Value == nil {
			attributes[attribute.Key] = attribute.Value
		} else {
			attributes[attribute.Key] = p.applyLeafClassification(attribute.Value, source)
		}
	case redact.ActionRedact:
		attributes[attribute.Key] = redactionMap(redact.Scalar(attribute.Value, decision.Reason, decision.Source, p.options.RedactionKey))
		p.recordRedaction(decision.Reason)
	case redact.ActionDrop:
		p.recordRedaction(decision.Reason)
		if err := p.emitWarning(warningPayload{
			WarningKind: "attribute_dropped",
			Reason:      decision.Reason,
			Source:      decision.Source,
		}); err != nil {
			return err
		}
	}
	return nil
}

// applyLeafClassification descends a captured composite value and applies the
// redact policy to every scalar leaf. The walker output shape stays the same;
// only scalar leaves whose source segment matches a sensitive key (per
// redact.RuleSet.isSensitiveSource) are swapped for a redaction marker map.
// This preserves the nested-singleton-array shape the loader's flattener
// expects while keeping the per-leaf sensitive-key guarantee from
// CLAUDE.md §"Correlation Truth Gates".
func (p *stateParser) applyLeafClassification(value any, sourcePath string) any {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			typed[key] = p.applyLeafClassification(child, sourcePath+"."+key)
		}
		return typed
	case []any:
		for index, child := range typed {
			typed[index] = p.applyLeafClassification(child, sourcePath)
		}
		return typed
	case nil:
		return nil
	default:
		decision := p.options.RedactionRules.Classify(sourcePath, redact.SchemaKnown, redact.FieldScalar)
		switch decision.Action {
		case redact.ActionRedact:
			p.recordRedaction(decision.Reason)
			return redactionMap(redact.Scalar(typed, decision.Reason, decision.Source, p.options.RedactionKey))
		case redact.ActionDrop:
			p.recordRedaction(decision.Reason)
			return nil
		default:
			return typed
		}
	}
}

// schemaTrust returns redact.SchemaKnown when the parser has a
// ProviderSchemaResolver that recognizes the (resourceType, attributeKey)
// pair. Every other case — nil resolver, unknown resource type, unknown
// attribute key, blank inputs — returns redact.SchemaUnknown so the
// RedactionRules policy fails closed.
//
// This is the load-bearing seam that lets non-sensitive Terraform-state
// attributes (e.g. aws_s3_bucket.acl) flow through to downstream drift
// detection while keeping the fail-closed default for unmapped attributes.
func (p *stateParser) schemaTrust(resourceType string, attributeKey string) redact.SchemaTrust {
	if p.options.SchemaResolver == nil {
		return redact.SchemaUnknown
	}
	if resourceType == "" || attributeKey == "" {
		return redact.SchemaUnknown
	}
	if p.options.SchemaResolver.HasAttribute(resourceType, attributeKey) {
		return redact.SchemaKnown
	}
	return redact.SchemaUnknown
}
