package postgres

import (
	"encoding/json"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/correlation/drift/tfconfigstate"
)

// stateRowFromCollectorPayload decodes one terraform_state_resource fact
// payload into a ResourceRow. The collector emits classified attributes as a
// nested map[string]any (resources.go:173-181); flattenStateAttributes
// recursively produces a flat dot-path map[string]string so the classifier's
// attribute-drift dispatch can compare against the parser-emitted config-side
// dot-paths (terraform_resource_attributes.go).
//
// Returns (nil, false) when the address is blank or the payload fails to
// decode. The caller surfaces decode failures via logDecodeFailure;
// successful decodes with an empty address are a parser invariant violation
// and intentionally silent (they cannot become drift candidates).
func stateRowFromCollectorPayload(address string, payload []byte, lineageRotation bool) (*tfconfigstate.ResourceRow, bool) {
	address = strings.TrimSpace(address)
	if address == "" {
		return nil, false
	}
	var decoded struct {
		Address    string         `json:"address"`
		Type       string         `json:"type"`
		Attributes map[string]any `json:"attributes"`
	}
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &decoded); err != nil {
			return nil, false
		}
	}
	row := &tfconfigstate.ResourceRow{
		Address:         address,
		ResourceType:    strings.TrimSpace(decoded.Type),
		LineageRotation: lineageRotation,
	}
	if len(decoded.Attributes) > 0 {
		flat := map[string]string{}
		flattenStateAttributes(decoded.Attributes, "", flat)
		if len(flat) > 0 {
			row.Attributes = flat
		}
	}
	return row, true
}

// flattenStateAttributes recursively walks a Terraform-state attributes value
// and emits flat dot-path keys to mirror the parser's config-side encoding
// (terraform_resource_attributes.go). Rules applied in order:
//
//  1. map[string]any: recurse on each child with path "<prefix>.<key>" (or
//     "<key>" at the root).
//  2. []any of length >= 1 whose first element is map[string]any: recurse into
//     the FIRST element only. Singleton repeated blocks (versioning, the SSE
//     chain, logging, …) wrap their object in a length-1 array; multi-element
//     repeated blocks fall under the same first-wins policy applied by the
//     parser's seenBlockTypes guard. The allowlist has no multi-element
//     entries in v1.
//  3. []any of primitives or empty []any: emit out[prefix] =
//     coerceJSONString(value).
//  4. Any other scalar / nil: emit out[prefix] = coerceJSONString(value).
//
// A blank prefix at a non-map case emits nothing — there is no key to attach
// to. The recursive shape preserves the existing top-level behavior for
// scalars at root because case 1 routes top-level scalars through case 4 with
// prefix=key.
func flattenStateAttributes(value any, prefix string, out map[string]string) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			childPath := key
			if prefix != "" {
				childPath = prefix + "." + key
			}
			flattenStateAttributes(child, childPath, out)
		}
	case []any:
		if len(typed) >= 1 {
			if obj, isMap := typed[0].(map[string]any); isMap {
				flattenStateAttributes(obj, prefix, out)
				return
			}
		}
		if prefix != "" {
			out[prefix] = coerceJSONString(typed)
		}
	default:
		if prefix != "" {
			out[prefix] = coerceJSONString(value)
		}
	}
}
