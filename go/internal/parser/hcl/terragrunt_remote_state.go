package hcl

import (
	"strings"

	"github.com/hashicorp/hcl/v2/hclsyntax"
)

// discoverySafeRemoteStateAttributes lists the Terragrunt remote_state config
// attributes the discovery resolver is allowed to consume. Other attributes are
// ignored by extraction so dynamic or sensitive values do not leak into the
// fact payload.
var discoverySafeRemoteStateAttributes = map[string]struct{}{
	"bucket":               {},
	"dynamodb_table":       {},
	"key":                  {},
	"path":                 {},
	"region":               {},
	"workspace_key_prefix": {},
}

// parseTerragruntRemoteStates extracts top-level terragrunt remote_state blocks
// from the given file body. Each block is mapped to a deterministic row carrying
// the backend kind and a curated set of safe attributes with literal-string
// flags, mirroring the shape used by parseTerraformBackends so downstream
// resolvers can treat both inputs uniformly. The walker that resolves include
// chains lives in include_chain.go; this function only sees the local body.
func parseTerragruntRemoteStates(body *hclsyntax.Body, source []byte, path string) []map[string]any {
	rows := make([]map[string]any, 0)
	for _, block := range body.Blocks {
		if block.Type != "remote_state" {
			continue
		}
		row := remoteStateRow(block, source, path)
		if row == nil {
			continue
		}
		row["resolved_from"] = "self"
		rows = append(rows, row)
	}
	return rows
}

// remoteStateRow builds a deterministic row for one Terragrunt remote_state
// block, returning nil when the block does not name a backend kind. The
// backend attribute and the nested config object are read separately so the
// walker and self-extractor share one shape.
func remoteStateRow(block *hclsyntax.Block, source []byte, path string) map[string]any {
	backendAttr := block.Body.Attributes["backend"]
	backendKind := strings.TrimSpace(attributeValue(backendAttr, source))
	if backendKind == "" {
		return nil
	}
	// row["source_path"] carries the parser-side provenance (the .hcl file
	// the row was extracted from). It is intentionally distinct from
	// row["path"], which is reserved for the local backend's `path` config
	// attribute; the two values would collide if the parser used a single key.
	row := map[string]any{
		"name":         backendKind,
		"backend_kind": backendKind,
		"line_number":  block.TypeRange.Start.Line,
		"source_path":  path,
		"lang":         "hcl",
	}

	configAttr := block.Body.Attributes["config"]
	if configAttr == nil {
		return row
	}
	objectExpr, ok := configAttr.Expr.(*hclsyntax.ObjectConsExpr)
	if !ok {
		return row
	}
	for _, item := range objectExpr.Items {
		name := strings.TrimSpace(strings.Trim(expressionText(item.KeyExpr, source), `"`))
		if name == "" {
			continue
		}
		if _, allowed := discoverySafeRemoteStateAttributes[name]; !allowed {
			continue
		}
		value := expressionText(item.ValueExpr, source)
		if strings.TrimSpace(value) == "" {
			continue
		}
		row[name] = value
		row[name+"_is_literal"] = isLiteralRemoteStateConfigValue(item.ValueExpr, source)
	}
	return row
}

// resolveTerragruntRemoteStateFromIncludes returns extra remote_state rows
// inherited from a parent terragrunt.hcl through the local include chain. The
// walker is implemented in include_chain.go; this entry point keeps the
// dispatch site in parser.go free of include-traversal details.
func resolveTerragruntRemoteStateFromIncludes(body *hclsyntax.Body, source []byte, path string) []map[string]any {
	for _, block := range body.Blocks {
		if block.Type == "remote_state" {
			return nil
		}
	}
	resolved, _ := resolveTerragruntRemoteState(path)
	if resolved == nil {
		return nil
	}
	if resolved.resolvedFrom == "self" {
		// Self resolution already emitted by parseTerragruntRemoteStates above.
		return nil
	}
	row := resolved.row
	row["resolved_from"] = resolved.resolvedFrom
	if origin, ok := row["source_path"].(string); ok && origin != "" {
		row["resolved_source"] = origin
	}
	// Re-anchor the row's source_path provenance to the file the parser was
	// invoked on so downstream fact persistence keys the row by the same path
	// it was parsed under. The local backend's own `path` attribute (if any)
	// stays untouched in row["path"].
	row["source_path"] = path
	return []map[string]any{row}
}

// isLiteralRemoteStateConfigValue reports whether the supplied expression is a
// literal string value safe to treat as exact backend evidence. The check
// mirrors isLiteralStringAttribute on terraform backends so the same
// `_is_literal` flag carries the same meaning across both sources. Template
// interpolations (`${...}`) — including embedded function calls that do not
// register as `Variables()` — disqualify the value because the resolver cannot
// evaluate Terragrunt runtime helpers safely.
func isLiteralRemoteStateConfigValue(expr hclsyntax.Expression, source []byte) bool {
	switch typed := expr.(type) {
	case *hclsyntax.LiteralValueExpr:
		return typed.Val.Type().FriendlyName() == "string"
	case *hclsyntax.TemplateExpr:
		raw := strings.TrimSpace(sourceRange(source, typed.Range()))
		if !strings.HasPrefix(raw, `"`) || !strings.HasSuffix(raw, `"`) {
			return false
		}
		if strings.Contains(raw, "${") {
			return false
		}
		return len(typed.Variables()) == 0
	default:
		return false
	}
}
