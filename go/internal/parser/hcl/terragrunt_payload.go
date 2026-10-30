// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package hcl

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/hashicorp/hcl/v2/hclsyntax"
)

func parseTerragruntConfig(body *hclsyntax.Body, source []byte, path string) map[string]any {
	row := map[string]any{
		"name":        strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)),
		"line_number": 1,
		"path":        path,
		"lang":        "hcl",
	}

	includeNames := make([]string, 0)
	localNames := make([]string, 0)
	for _, block := range body.Blocks {
		switch block.Type {
		case "terraform":
			row["terraform_source"] = attributeValue(block.Body.Attributes["source"], source)
		case "include":
			includeNames = append(includeNames, block.Labels...)
		case "locals":
			for name := range block.Body.Attributes {
				localNames = append(localNames, name)
			}
		}
	}
	sort.Strings(includeNames)
	sort.Strings(localNames)
	row["includes"] = strings.Join(includeNames, ",")
	row["locals"] = strings.Join(localNames, ",")
	row["inputs"] = strings.Join(objectAttributeKeys(body.Attributes["inputs"], source), ",")
	helperPaths := parseTerragruntHelperPaths(source, path)
	if len(helperPaths.includePaths) > 0 {
		row["include_paths"] = strings.Join(helperPaths.includePaths, ",")
	}
	if len(helperPaths.readConfigPaths) > 0 {
		row["read_config_paths"] = strings.Join(helperPaths.readConfigPaths, ",")
	}
	if len(helperPaths.findInParentFoldersPaths) > 0 {
		row["find_in_parent_folders_paths"] = strings.Join(helperPaths.findInParentFoldersPaths, ",")
	}
	if len(helperPaths.localConfigAssetPaths) > 0 {
		row["local_config_asset_paths"] = strings.Join(helperPaths.localConfigAssetPaths, ",")
	}
	return row
}

func parseTerragruntModuleSources(body *hclsyntax.Body, source []byte, path string) []map[string]any {
	rows := make([]map[string]any, 0, 1)
	for _, block := range body.Blocks {
		if block.Type != "terraform" {
			continue
		}
		moduleSource := attributeValue(block.Body.Attributes["source"], source)
		if moduleSource == "" {
			continue
		}
		rows = append(rows, map[string]any{
			"name":        strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)),
			"line_number": block.TypeRange.Start.Line,
			"source":      moduleSource,
			"path":        path,
			"lang":        "hcl",
		})
	}
	return rows
}

func parseTerragruntDependencies(body *hclsyntax.Body, source []byte, path string) []map[string]any {
	rows := make([]map[string]any, 0)
	for _, block := range body.Blocks {
		if block.Type != "dependency" || len(block.Labels) == 0 {
			continue
		}
		row := map[string]any{
			"name":        block.Labels[0],
			"line_number": block.TypeRange.Start.Line,
			"path":        path,
			"lang":        "hcl",
		}
		if configPath := attributeValue(block.Body.Attributes["config_path"], source); configPath != "" {
			row["config_path"] = configPath
		}
		rows = append(rows, row)
	}
	return rows
}

func parseTerragruntLocals(body *hclsyntax.Body, source []byte, path string) []map[string]any {
	rows := make([]map[string]any, 0)
	for _, block := range body.Blocks {
		if block.Type != "locals" {
			continue
		}
		for _, item := range sortedAttributes(block.Body.Attributes) {
			rows = append(rows, map[string]any{
				"name":        item.name,
				"line_number": item.attribute.NameRange.Start.Line,
				"value":       expressionText(item.attribute.Expr, source),
				"path":        path,
				"lang":        "hcl",
			})
		}
	}
	return rows
}

func parseTerragruntInputs(body *hclsyntax.Body, source []byte, path string) []map[string]any {
	inputs := body.Attributes["inputs"]
	if inputs == nil {
		return nil
	}

	objectExpr, ok := inputs.Expr.(*hclsyntax.ObjectConsExpr)
	if !ok {
		return nil
	}

	rows := make([]map[string]any, 0, len(objectExpr.Items))
	for _, item := range objectExpr.Items {
		name := strings.Trim(expressionText(item.KeyExpr, source), `"`)
		if strings.TrimSpace(name) == "" {
			continue
		}
		rows = append(rows, map[string]any{
			"name":        name,
			"line_number": item.KeyExpr.Range().Start.Line,
			"value":       expressionText(item.ValueExpr, source),
			"path":        path,
			"lang":        "hcl",
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		left, _ := rows[i]["name"].(string)
		right, _ := rows[j]["name"].(string)
		return left < right
	})
	return rows
}

type namedAttribute struct {
	name      string
	attribute *hclsyntax.Attribute
}
