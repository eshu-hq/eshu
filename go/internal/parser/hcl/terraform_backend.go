// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package hcl

import (
	"strings"

	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
)

var discoverySafeBackendAttributes = map[string]struct{}{
	"bucket":               {},
	"dynamodb_table":       {},
	"key":                  {},
	"path":                 {},
	"region":               {},
	"workspace_key_prefix": {},
}

// backendAttributeRowKey returns the row map key used to store one parsed
// backend attribute value. Every row already carries a "path" key set to the
// repo-relative path of the .tf file the backend block was found in (see the
// row literal below); Terraform's local backend also names its state-file
// locator attribute "path" (see
// https://developer.hashicorp.com/terraform/language/backend/local). Storing
// the attribute under the same key would silently overwrite the file path
// with the state-file locator, breaking every consumer that reads row["path"]
// expecting the file path (backendModuleDir's variable/local scoping in
// go/internal/collector/terraformstate/backend_config_resolution.go). The
// attribute is stored under "state_path" instead so both values survive.
// "state_path" was chosen over an earlier "local_path" candidate because the
// durable `repository` fact ALREADY has an established, differently-scoped
// "local_path" payload field meaning the repo checkout root (see
// go/internal/collector/gitrepo/git_content_fact_envelopes.go); reusing that name
// here for a completely different value (the backend's own path attribute)
// would recreate the exact class of collision this function exists to avoid
// (issue #5594).
func backendAttributeRowKey(name string) string {
	if name == "path" {
		return "state_path"
	}
	return name
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
				rowKey := backendAttributeRowKey(item.name)
				row[rowKey] = value
				row[rowKey+"_is_literal"] = isLiteralStringAttribute(item.attribute, source)
				row[rowKey+"_line_number"] = item.attribute.NameRange.Start.Line
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
