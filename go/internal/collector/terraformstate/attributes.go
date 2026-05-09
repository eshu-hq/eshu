package terraformstate

import (
	"encoding/json"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/redact"
)

type attributeValue struct {
	Key    string
	Value  any
	Scalar bool
}

func readAttributeValues(decoder *json.Decoder) ([]attributeValue, error) {
	if err := readOpeningDelim(decoder, '{', "terraform state resource attributes"); err != nil {
		return nil, err
	}
	attributes := []attributeValue{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, fmt.Errorf("read terraform state resource attribute key: %w", err)
		}
		key, ok := token.(string)
		if !ok {
			return nil, fmt.Errorf("terraform state resource attribute key must be a string")
		}
		value, scalar, err := readScalarOrSkip(decoder)
		if err != nil {
			return nil, fmt.Errorf("decode terraform state resource attribute %q: %w", key, err)
		}
		attributes = append(attributes, attributeValue{Key: key, Value: value, Scalar: scalar})
	}
	if _, err := decoder.Token(); err != nil {
		return nil, fmt.Errorf("close terraform state resource attributes: %w", err)
	}
	return attributes, nil
}

func (p *stateParser) classifyAttributes(address string, input []attributeValue) map[string]any {
	attributes := make(map[string]any, len(input))
	for _, attribute := range input {
		p.classifyAttribute(attributes, address, attribute)
	}
	return attributes
}

func (p *stateParser) classifyAttribute(attributes map[string]any, address string, attribute attributeValue) {
	source := "resources." + address + ".attributes." + attribute.Key
	kind := redact.FieldComposite
	if attribute.Scalar {
		kind = redact.FieldScalar
	}
	decision := p.options.RedactionRules.Classify(source, redact.SchemaKnown, kind)
	if decision.Action == redact.ActionPreserve {
		decision = p.options.RedactionRules.Classify(source, redact.SchemaUnknown, kind)
	}

	switch decision.Action {
	case redact.ActionPreserve:
		attributes[attribute.Key] = attribute.Value
	case redact.ActionRedact:
		attributes[attribute.Key] = redactionMap(redact.Scalar(attribute.Value, decision.Reason, decision.Source, p.options.RedactionKey))
	case redact.ActionDrop:
		p.warnings = append(p.warnings, warningPayload{
			WarningKind: "attribute_dropped",
			Reason:      decision.Reason,
			Source:      decision.Source,
		})
	}
}
