package hcl

import "github.com/hashicorp/hcl/v2/hclsyntax"

func parseTerraformLockProviders(body *hclsyntax.Body, source []byte, path string) []map[string]any {
	rows := make([]map[string]any, 0)
	for _, block := range body.Blocks {
		if block.Type != "provider" || len(block.Labels) == 0 {
			continue
		}
		rows = append(rows, map[string]any{
			"name":        block.Labels[0],
			"line_number": block.TypeRange.Start.Line,
			"source":      block.Labels[0],
			"version":     attributeValue(block.Body.Attributes["version"], source),
			"constraints": attributeValue(block.Body.Attributes["constraints"], source),
			"hash_count":  terraformLockHashCount(block),
			"path":        path,
			"lang":        "hcl",
		})
	}
	return rows
}

func terraformLockHashCount(block *hclsyntax.Block) int {
	hashes := block.Body.Attributes["hashes"]
	if hashes == nil {
		return 0
	}
	tuple, ok := hashes.Expr.(*hclsyntax.TupleConsExpr)
	if !ok {
		return 0
	}
	return len(tuple.Exprs)
}
