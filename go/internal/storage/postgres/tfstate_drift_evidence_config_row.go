package postgres

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/correlation/drift/tfconfigstate"
)

// configRowFromParserEntry maps one parsed_file_data.terraform_resources entry
// to a ResourceRow. The HCL parser (terraform_resource_attributes.go) emits a
// flat dot-path "attributes" map of literal values plus a sorted
// "unknown_attributes" list of dot-paths whose expressions are non-literal.
// This bridge decodes both fields so the classifier's attribute-drift dispatch
// (go/internal/correlation/drift/tfconfigstate/classify.go:154) has both sides
// of the comparison.
//
// The canonical address is the root-module form `<type>.<name>`. Module-nested
// state addresses surface as added_in_state in v1 — issue #169.
//
// Returns (nil, false) on blank type or name so genuinely invalid rows do not
// become drift candidates.
func configRowFromParserEntry(entry map[string]any) (*tfconfigstate.ResourceRow, bool) {
	resourceType := strings.TrimSpace(coerceJSONString(entry["resource_type"]))
	resourceName := strings.TrimSpace(coerceJSONString(entry["resource_name"]))
	if resourceType == "" || resourceName == "" {
		return nil, false
	}
	row := &tfconfigstate.ResourceRow{
		Address:      resourceType + "." + resourceName,
		ResourceType: resourceType,
	}
	if attrs, ok := entry["attributes"].(map[string]any); ok && len(attrs) > 0 {
		flat := make(map[string]string, len(attrs))
		for key, value := range attrs {
			flat[key] = coerceJSONString(value)
		}
		row.Attributes = flat
	}
	if unknown := entry["unknown_attributes"]; unknown != nil {
		m := map[string]bool{}
		switch typed := unknown.(type) {
		case []any:
			for _, name := range typed {
				if s := strings.TrimSpace(coerceJSONString(name)); s != "" {
					m[s] = true
				}
			}
		case []string:
			for _, name := range typed {
				if s := strings.TrimSpace(name); s != "" {
					m[s] = true
				}
			}
		}
		if len(m) > 0 {
			row.UnknownAttributes = m
		}
	}
	return row, true
}
