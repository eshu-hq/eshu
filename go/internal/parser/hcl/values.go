package hcl

import (
	"sort"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
)

func sortedAttributes(attributes map[string]*hclsyntax.Attribute) []namedAttribute {
	keys := make([]string, 0, len(attributes))
	for key := range attributes {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	rows := make([]namedAttribute, 0, len(keys))
	for _, key := range keys {
		rows = append(rows, namedAttribute{name: key, attribute: attributes[key]})
	}
	return rows
}

func attributeValue(attribute *hclsyntax.Attribute, source []byte) string {
	if attribute == nil {
		return ""
	}
	return expressionText(attribute.Expr, source)
}

func attributeSourceText(attribute *hclsyntax.Attribute, source []byte) string {
	if attribute == nil {
		return ""
	}
	return strings.TrimSpace(sourceRange(source, attribute.Expr.Range()))
}

func objectAttributeMap(attribute *hclsyntax.Attribute, source []byte) map[string]string {
	if attribute == nil {
		return nil
	}
	objectExpr, ok := attribute.Expr.(*hclsyntax.ObjectConsExpr)
	if !ok {
		return nil
	}
	result := make(map[string]string, len(objectExpr.Items))
	for _, item := range objectExpr.Items {
		name := strings.Trim(expressionText(item.KeyExpr, source), `"`)
		if strings.TrimSpace(name) == "" {
			continue
		}
		result[name] = expressionText(item.ValueExpr, source)
	}
	return result
}

func objectAttributeKeys(attribute *hclsyntax.Attribute, source []byte) []string {
	if attribute == nil {
		return nil
	}
	objectExpr, ok := attribute.Expr.(*hclsyntax.ObjectConsExpr)
	if !ok {
		return nil
	}
	keys := make([]string, 0, len(objectExpr.Items))
	for _, item := range objectExpr.Items {
		key := strings.Trim(expressionText(item.KeyExpr, source), `"`)
		if strings.TrimSpace(key) != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

func expressionText(expr hclsyntax.Expression, source []byte) string {
	if expr == nil {
		return ""
	}
	var text string
	switch typed := expr.(type) {
	case *hclsyntax.LiteralValueExpr:
		text = literalValueText(typed)
	case *hclsyntax.TemplateExpr:
		text = strings.TrimSpace(sourceRange(source, typed.Range()))
	case *hclsyntax.ObjectConsExpr:
		text = strings.TrimSpace(sourceRange(source, typed.Range()))
	case *hclsyntax.ScopeTraversalExpr:
		text = strings.TrimSpace(sourceRange(source, typed.Range()))
	default:
		text = strings.TrimSpace(sourceRange(source, expr.Range()))
	}
	return strings.Trim(text, `"`)
}

func literalValueText(expr *hclsyntax.LiteralValueExpr) string {
	if expr == nil {
		return ""
	}
	if expr.Val.Type() == cty.String {
		return expr.Val.AsString()
	}
	return strings.TrimSpace(expr.Val.GoString())
}

func sourceRange(source []byte, valueRange hcl.Range) string {
	start := valueRange.Start.Byte
	end := valueRange.End.Byte
	if start < 0 {
		start = 0
	}
	if end > len(source) {
		end = len(source)
	}
	if start >= end {
		return ""
	}
	return string(source[start:end])
}
