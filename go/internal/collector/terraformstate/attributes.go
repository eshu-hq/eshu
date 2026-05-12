package terraformstate

import (
	"encoding/json"
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
		switch key {
		case "tags", "tags_all":
			tags, valid, err := readTagValues(decoder, key)
			if err != nil {
				return nil, err
			}
			attributes = append(attributes, attributeValue{Key: key, TagMap: true, Tags: tags, InvalidTagMap: !valid})
		default:
			value, scalar, err := readScalarOrSkip(decoder)
			if err != nil {
				return nil, fmt.Errorf("decode terraform state resource attribute %q: %w", key, err)
			}
			attributes = append(attributes, attributeValue{Key: key, Value: value, Scalar: scalar})
		}
	}
	if _, err := decoder.Token(); err != nil {
		return nil, fmt.Errorf("close terraform state resource attributes: %w", err)
	}
	return attributes, nil
}

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
		attributes[attribute.Key] = attribute.Value
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
