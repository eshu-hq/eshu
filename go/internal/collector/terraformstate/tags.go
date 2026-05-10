package terraformstate

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

const nonScalarTagValueReason = "non_scalar_tag_value"

type tagValue struct {
	Key    string
	Value  any
	Scalar bool
}

func readTagValues(decoder *json.Decoder, tagSource string) ([]tagValue, error) {
	if err := readOpeningDelim(decoder, '{', "terraform state resource "+tagSource); err != nil {
		return nil, err
	}
	tags := []tagValue{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, fmt.Errorf("read terraform state %s key: %w", tagSource, err)
		}
		key, ok := token.(string)
		if !ok {
			return nil, fmt.Errorf("terraform state %s key must be a string", tagSource)
		}
		value, scalar, err := readScalarOrSkip(decoder)
		if err != nil {
			return nil, fmt.Errorf("decode terraform state %s value %q: %w", tagSource, key, err)
		}
		tags = append(tags, tagValue{Key: key, Value: value, Scalar: scalar})
	}
	if _, err := decoder.Token(); err != nil {
		return nil, fmt.Errorf("close terraform state resource %s: %w", tagSource, err)
	}
	return tags, nil
}

func (p *stateParser) emitTagObservations(resourceAddress string, attributes []attributeValue) {
	for _, attribute := range attributes {
		if !attribute.TagMap {
			continue
		}
		tags := append([]tagValue(nil), attribute.Tags...)
		sort.Slice(tags, func(i, j int) bool {
			return tagKeyHash(tags[i].Key) < tagKeyHash(tags[j].Key)
		})
		for _, tag := range tags {
			p.emitTagObservation(resourceAddress, attribute.Key, tag)
		}
	}
}

func (p *stateParser) emitTagObservation(resourceAddress string, tagSource string, tag tagValue) {
	tagHash := tagKeyHash(tag.Key)
	safeSource := "resources." + resourceAddress + ".attributes." + tagSource + ".key:" + tagHash
	if !tag.Scalar {
		p.warnings = append(p.warnings, warningPayload{
			WarningKind: "tag_value_dropped",
			Reason:      nonScalarTagValueReason,
			Source:      safeSource + ".value",
		})
		return
	}

	classificationSource := "resources." + resourceAddress + ".attributes." + tagSource + "." + tag.Key
	payload := map[string]any{
		"resource_address": resourceAddress,
		"tag_source":       tagSource,
		"tag_key_hash":     tagHash,
	}
	p.addTagKey(payload, tag.Key, classificationSource+".key", safeSource+".key")
	p.addTagValue(payload, tag.Value, classificationSource+".value", safeSource+".value")

	stableKey := "tag_observation:" + resourceAddress + ":" + tagSource + ":" + tagHash
	p.facts = append(p.facts, p.envelope(facts.TerraformStateTagObservationFactKind, stableKey, payload, stableKey))
}

func (p *stateParser) addTagKey(payload map[string]any, tagKey string, classificationSource string, safeSource string) {
	decision := p.options.RedactionRules.Classify(classificationSource, redact.SchemaKnown, redact.FieldScalar)
	if decision.Action == redact.ActionRedact {
		payload["tag_key"] = redactionMap(redact.Scalar(tagKey, decision.Reason, safeSource, p.options.RedactionKey))
		p.recordRedaction(decision.Reason)
		return
	}
	payload["tag_key"] = tagKey
}

func (p *stateParser) addTagValue(payload map[string]any, tagValue any, classificationSource string, safeSource string) {
	decision := p.options.RedactionRules.Classify(classificationSource, redact.SchemaKnown, redact.FieldScalar)
	if decision.Action == redact.ActionRedact {
		payload["tag_value"] = redactionMap(redact.Scalar(tagValue, decision.Reason, safeSource, p.options.RedactionKey))
		p.recordRedaction(decision.Reason)
		return
	}
	payload["tag_value"] = tagValue
}

func tagKeyHash(tagKey string) string {
	return facts.StableID("TerraformStateTagKey", map[string]any{
		"tag_key": tagKey,
	})
}
