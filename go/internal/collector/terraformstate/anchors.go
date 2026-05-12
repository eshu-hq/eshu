package terraformstate

import (
	"sort"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

var correlationAnchorFields = map[string]string{
	"account_id": "account_id",
	"arn":        "arn",
	"id":         "id",
	"name":       "name",
	"region":     "region",
}

func (p *stateParser) correlationAnchors(resourceType string, resourceAddress string, attributes []attributeValue) []any {
	anchors := []any{}
	for _, attribute := range attributes {
		if !attribute.Scalar {
			continue
		}
		anchorKind, ok := correlationAnchorFields[attribute.Key]
		if !ok || p.redactsAnchor(resourceType, resourceAddress, attribute.Key) {
			continue
		}
		anchors = append(anchors, map[string]any{
			"anchor_kind": anchorKind,
			"value_hash":  correlationAnchorHash(anchorKind, attribute.Value),
		})
	}
	sort.Slice(anchors, func(i, j int) bool {
		left := anchors[i].(map[string]any)
		right := anchors[j].(map[string]any)
		return left["anchor_kind"].(string) < right["anchor_kind"].(string)
	})
	return anchors
}

// redactsAnchor classifies a correlation-anchor candidate through the same
// schema-trust seam attributes.go uses so an anchor field on a known schema
// is only emitted when the underlying attribute would have been preserved.
func (p *stateParser) redactsAnchor(resourceType string, resourceAddress string, attributeKey string) bool {
	source := "resources." + resourceAddress + ".attributes." + attributeKey
	decision := p.options.RedactionRules.Classify(source, p.schemaTrust(resourceType, attributeKey), redact.FieldScalar)
	return decision.Action != redact.ActionPreserve
}

func correlationAnchorHash(anchorKind string, value any) string {
	return facts.StableID("TerraformStateCorrelationAnchor", map[string]any{
		"anchor_kind": anchorKind,
		"value":       value,
	})
}
