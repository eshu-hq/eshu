// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package hcl

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
)

type grafanaDashboardMetadata struct {
	uid              string
	titleFingerprint string
	datasourceRefs   []string
}

func collectGrafanaDashboardMetadata(attribute *hclsyntax.Attribute, source []byte) grafanaDashboardMetadata {
	if attribute == nil {
		return grafanaDashboardMetadata{}
	}
	if object := grafanaJSONEncodeObject(attribute.Expr); object != nil {
		return collectGrafanaDashboardMetadataFromObject(object, source)
	}
	if raw, ok := literalAttributeValue(attribute); ok {
		return collectGrafanaDashboardMetadataFromJSON(raw)
	}
	return grafanaDashboardMetadata{}
}

func grafanaJSONEncodeObject(expr hclsyntax.Expression) *hclsyntax.ObjectConsExpr {
	call, ok := expr.(*hclsyntax.FunctionCallExpr)
	if !ok || !strings.EqualFold(call.Name, "jsonencode") || len(call.Args) != 1 {
		return nil
	}
	object, _ := call.Args[0].(*hclsyntax.ObjectConsExpr)
	return object
}

func collectGrafanaDashboardMetadataFromObject(
	object *hclsyntax.ObjectConsExpr,
	source []byte,
) grafanaDashboardMetadata {
	metadata := grafanaDashboardMetadata{}
	for _, item := range object.Items {
		key := strings.Trim(expressionText(item.KeyExpr, source), `"`)
		switch key {
		case "uid":
			metadata.uid, _ = literalGrafanaExpression(item.ValueExpr)
		case "title":
			if title, ok := literalGrafanaExpression(item.ValueExpr); ok {
				metadata.titleFingerprint = fingerprintHCLValue(title)
			}
		}
	}
	metadata.datasourceRefs = collectGrafanaDatasourceRefsFromExpression(object, source)
	return metadata
}

func collectGrafanaDashboardMetadataFromJSON(raw string) grafanaDashboardMetadata {
	var object map[string]any
	if err := json.Unmarshal([]byte(raw), &object); err != nil {
		return grafanaDashboardMetadata{}
	}
	metadata := grafanaDashboardMetadata{
		uid:              cleanGrafanaJSONString(object["uid"]),
		titleFingerprint: fingerprintHCLValue(cleanGrafanaJSONString(object["title"])),
		datasourceRefs:   collectGrafanaDatasourceRefsFromJSON(object),
	}
	return metadata
}

func collectGrafanaDatasourceRefsFromExpression(expr hclsyntax.Expression, source []byte) []string {
	var refs []string
	var walk func(hclsyntax.Expression)
	walk = func(value hclsyntax.Expression) {
		switch typed := value.(type) {
		case *hclsyntax.ObjectConsExpr:
			for _, item := range typed.Items {
				key := strings.Trim(expressionText(item.KeyExpr, source), `"`)
				if key == "datasource" {
					refs = append(refs, grafanaDatasourceRefsFromExpression(item.ValueExpr, source)...)
				}
				walk(item.ValueExpr)
			}
		case *hclsyntax.TupleConsExpr:
			for _, item := range typed.Exprs {
				walk(item)
			}
		case *hclsyntax.FunctionCallExpr:
			for _, arg := range typed.Args {
				walk(arg)
			}
		}
	}
	walk(expr)
	return sortedHCLUnique(refs)
}

func grafanaDatasourceRefsFromExpression(expr hclsyntax.Expression, source []byte) []string {
	switch typed := expr.(type) {
	case *hclsyntax.ObjectConsExpr:
		var refs []string
		for _, item := range typed.Items {
			key := strings.Trim(expressionText(item.KeyExpr, source), `"`)
			value, ok := literalGrafanaExpression(item.ValueExpr)
			if !ok || value == "" {
				continue
			}
			switch key {
			case "uid":
				refs = append(refs, "uid:"+value)
			case "type":
				refs = append(refs, "type:"+strings.ToLower(value))
			}
		}
		return refs
	default:
		if value, ok := literalGrafanaExpression(expr); ok && value != "" {
			return []string{"name_fingerprint:" + fingerprintHCLValue(value)}
		}
		return nil
	}
}

func collectGrafanaDatasourceRefsFromJSON(value any) []string {
	var refs []string
	var walk func(any)
	walk = func(item any) {
		switch typed := item.(type) {
		case map[string]any:
			if datasource, ok := typed["datasource"]; ok {
				refs = append(refs, grafanaDatasourceRefsFromJSONValue(datasource)...)
			}
			for _, child := range typed {
				walk(child)
			}
		case []any:
			for _, child := range typed {
				walk(child)
			}
		}
	}
	walk(value)
	return sortedHCLUnique(refs)
}

func grafanaDatasourceRefsFromJSONValue(value any) []string {
	switch typed := value.(type) {
	case map[string]any:
		var refs []string
		if uid := cleanGrafanaJSONString(typed["uid"]); uid != "" {
			refs = append(refs, "uid:"+uid)
		}
		if datasourceType := cleanGrafanaJSONString(typed["type"]); datasourceType != "" {
			refs = append(refs, "type:"+strings.ToLower(datasourceType))
		}
		return refs
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil
		}
		return []string{"name_fingerprint:" + fingerprintHCLValue(typed)}
	default:
		return nil
	}
}

func literalGrafanaExpression(expr hclsyntax.Expression) (string, bool) {
	if expr == nil {
		return "", false
	}
	val, diags := expr.Value(nil)
	if diags.HasErrors() || val.IsNull() || !val.IsKnown() {
		return "", false
	}
	if val.Type() == cty.String {
		return strings.TrimSpace(val.AsString()), true
	}
	return strings.TrimSpace(val.GoString()), true
}

func cleanGrafanaJSONString(value any) string {
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "" || text == "<nil>" {
		return ""
	}
	return text
}
