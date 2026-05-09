package hcl

import (
	"strings"

	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
)

var discoverySafeBackendAttributes = map[string]struct{}{
	"bucket":               {},
	"key":                  {},
	"region":               {},
	"workspace_key_prefix": {},
}

func parseTerraformBackends(body *hclsyntax.Body, source []byte, path string) []map[string]any {
	rows := make([]map[string]any, 0)
	for _, block := range body.Blocks {
		if block.Type != "terraform" {
			continue
		}
		for _, child := range block.Body.Blocks {
			if child.Type != "backend" || len(child.Labels) == 0 {
				continue
			}
			backendKind := strings.TrimSpace(child.Labels[0])
			if backendKind == "" {
				continue
			}
			row := map[string]any{
				"name":         backendKind,
				"backend_kind": backendKind,
				"line_number":  child.TypeRange.Start.Line,
				"path":         path,
				"lang":         "hcl",
			}
			for _, item := range sortedAttributes(child.Body.Attributes) {
				if _, ok := discoverySafeBackendAttributes[item.name]; !ok {
					continue
				}
				value := attributeValue(item.attribute, source)
				if value == "" {
					continue
				}
				row[item.name] = value
				row[item.name+"_is_literal"] = isLiteralStringAttribute(item.attribute, source)
			}
			rows = append(rows, row)
		}
	}
	return rows
}

func isLiteralStringAttribute(attribute *hclsyntax.Attribute, source []byte) bool {
	if attribute == nil {
		return false
	}
	switch expr := attribute.Expr.(type) {
	case *hclsyntax.LiteralValueExpr:
		return expr.Val.Type() == cty.String
	case *hclsyntax.TemplateExpr:
		raw := strings.TrimSpace(sourceRange(source, expr.Range()))
		return len(expr.Variables()) == 0 && strings.HasPrefix(raw, `"`) && strings.HasSuffix(raw, `"`)
	default:
		return false
	}
}
