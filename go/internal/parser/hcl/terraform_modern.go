package hcl

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/hashicorp/hcl/v2/hclsyntax"
)

func parseTerraformImportBlock(block *hclsyntax.Block, source []byte, path string) map[string]any {
	to := attributeValue(block.Body.Attributes["to"], source)
	row := map[string]any{
		"name":        fallbackTerraformName(to, "import", block.TypeRange.Start.Line),
		"line_number": block.TypeRange.Start.Line,
		"to":          to,
		"provider":    attributeValue(block.Body.Attributes["provider"], source),
		"id":          attributeValue(block.Body.Attributes["id"], source),
		"path":        path,
		"lang":        "hcl",
	}
	if idAttr := block.Body.Attributes["id"]; idAttr != nil {
		row["id_is_literal"] = isLiteralStringAttribute(idAttr, source)
	}
	if forEach := attributeSourceText(block.Body.Attributes["for_each"], source); forEach != "" {
		row["for_each"] = forEach
	}
	return row
}

func parseTerraformMovedBlock(block *hclsyntax.Block, source []byte, path string) map[string]any {
	from := attributeValue(block.Body.Attributes["from"], source)
	to := attributeValue(block.Body.Attributes["to"], source)
	name := fallbackTerraformName(strings.TrimSpace(from+" -> "+to), "moved", block.TypeRange.Start.Line)
	return map[string]any{
		"name":        name,
		"line_number": block.TypeRange.Start.Line,
		"from":        from,
		"to":          to,
		"path":        path,
		"lang":        "hcl",
	}
}

func parseTerraformRemovedBlock(block *hclsyntax.Block, source []byte, path string) map[string]any {
	from := attributeValue(block.Body.Attributes["from"], source)
	row := map[string]any{
		"name":        fallbackTerraformName(from, "removed", block.TypeRange.Start.Line),
		"line_number": block.TypeRange.Start.Line,
		"from":        from,
		"path":        path,
		"lang":        "hcl",
	}
	if destroy := terraformRemovedLifecycleDestroy(block, source); destroy != "" {
		row["lifecycle_destroy"] = destroy
	}
	return row
}

func parseTerraformCheckBlock(block *hclsyntax.Block, path string) map[string]any {
	assertCount := 0
	for _, child := range block.Body.Blocks {
		if child.Type == "assert" {
			assertCount++
		}
	}
	return map[string]any{
		"name":         block.Labels[0],
		"line_number":  block.TypeRange.Start.Line,
		"assert_count": assertCount,
		"path":         path,
		"lang":         "hcl",
	}
}

func terraformRemovedLifecycleDestroy(block *hclsyntax.Block, source []byte) string {
	for _, child := range block.Body.Blocks {
		if child.Type != "lifecycle" {
			continue
		}
		return attributeSourceText(child.Body.Attributes["destroy"], source)
	}
	return ""
}

func fallbackTerraformName(value string, prefix string, line int) string {
	name := strings.TrimSpace(value)
	if name != "" {
		return name
	}
	return fmt.Sprintf("%s:%d", prefix, line)
}

func isTerraformLockFile(path string) bool {
	return strings.EqualFold(filepath.Base(path), ".terraform.lock.hcl")
}
