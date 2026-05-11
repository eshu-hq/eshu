package hcl

import (
	"math/big"
	"sort"
	"strconv"

	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
)

// reservedResourceAttributeNames are top-level attribute names the parser
// surfaces as dedicated row fields (count, for_each) or that carry no drift
// signal (depends_on). Skipped at the ROOT of the walk only; nested-block
// attributes with the same name are still emitted under their dotted path.
var reservedResourceAttributeNames = map[string]struct{}{
	"count":      {},
	"for_each":   {},
	"depends_on": {},
}

// reservedResourceBlockTypes are nested block types the parser never descends
// into. These are Terraform meta-blocks with no state-side equivalent — emitting
// their attributes as drift signal would produce noise that the classifier's
// cfgHas/stateHas guard would silently drop anyway.
var reservedResourceBlockTypes = map[string]struct{}{
	"lifecycle":   {},
	"provisioner": {},
	"connection":  {},
	"dynamic":     {},
}

// extractResourceAttributes walks a "resource" block's top-level attributes
// AND singleton nested blocks recursively, emitting a flat dot-path map of
// known literal values and a sorted list of dot-paths whose values are
// non-literal HCL expressions. The dot-path encoding matches the state-side
// flattener in tfstate_drift_evidence_state_row.go so the classifier's
// attribute-drift allowlist (e.g. "versioning.enabled" and
// "server_side_encryption_configuration.rule.apply_server_side_encryption_by_default.sse_algorithm")
// fires consistently against state-side values.
//
// Literal values are extracted via cty-value evaluation (hclsyntax.Value with a
// nil EvalContext) rather than byte-level source reading. This correctly handles
// heredoc strings and escaped-quote strings: heredoc expressions evaluate to the
// unindented, undelimited body content; escaped quotes evaluate to the unescaped
// character. Both match the state-side coerceJSONString output.
//
// Multi-element repeated nested blocks of the same type (e.g. two
// lifecycle_rule blocks) collapse to the FIRST occurrence by deterministic
// block ordering — v1 has no multi-element allowlist entries and the
// state-side flatten applies the same first-wins rule. Block labels are
// NOT part of the dot-path; allowlist entries reference block types only.
//
// Returns nil/nil when the block declares no relevant attributes, so the
// caller can omit the JSON keys and keep existing snapshots byte-stable.
func extractResourceAttributes(block *hclsyntax.Block) (map[string]any, []string) {
	if block == nil {
		return nil, nil
	}
	known := map[string]any{}
	var unknown []string
	walkBlockAttributes(block.Body, "", known, &unknown)
	if len(known) == 0 {
		known = nil
	}
	sort.Strings(unknown)
	return known, unknown
}

// walkBlockAttributes recursively emits dot-path attribute values from a body.
// prefix is empty at the root call; nested calls prefix with the parent block
// type. Reserved-name suppression applies at the root only.
func walkBlockAttributes(
	body *hclsyntax.Body,
	prefix string,
	known map[string]any,
	unknown *[]string,
) {
	if body == nil {
		return
	}
	for _, item := range sortedAttributes(body.Attributes) {
		if prefix == "" {
			if _, reserved := reservedResourceAttributeNames[item.name]; reserved {
				continue
			}
		}
		path := item.name
		if prefix != "" {
			path = prefix + "." + item.name
		}
		if value, ok := literalAttributeValue(item.attribute); ok {
			known[path] = value
		} else {
			*unknown = append(*unknown, path)
		}
	}
	seenBlockTypes := map[string]struct{}{}
	for _, nested := range body.Blocks {
		if _, reserved := reservedResourceBlockTypes[nested.Type]; reserved {
			continue
		}
		if len(nested.Labels) > 0 {
			// Labeled nested blocks (e.g. `provisioner "local-exec" {}`) are
			// not allowlist-eligible — Terraform's repeated-block schemas are
			// unlabeled.
			continue
		}
		if _, seen := seenBlockTypes[nested.Type]; seen {
			// First-wins for multi-element repeated nested blocks; documented
			// v1 limit, matches the state-side flatten policy.
			continue
		}
		seenBlockTypes[nested.Type] = struct{}{}
		childPrefix := nested.Type
		if prefix != "" {
			childPrefix = prefix + "." + nested.Type
		}
		walkBlockAttributes(nested.Body, childPrefix, known, unknown)
	}
}

// literalAttributeValue evaluates one attribute's expression to a deterministic
// string suitable for drift comparison, returning (value, true) when the
// expression is a context-free literal (LiteralValueExpr or a TemplateExpr
// with no variables) and ("", false) otherwise. The classifier treats every
// other shape as "no signal" so it cannot raise false positives against
// concrete state values.
//
// Evaluating via hclsyntax.Value rather than reading raw source bytes is the
// load-bearing correctness step: heredocs and escaped-quote strings would
// otherwise produce wrong values that never match the state-side
// coerceJSONString output. The encoding here MUST stay in lockstep with
// tfstate_drift_evidence_state_row.go (introduced in Task 4); the classifier's
// value-equality check at go/internal/correlation/drift/tfconfigstate/classify.go:171
// silently drops mismatched encodings.
func literalAttributeValue(attribute *hclsyntax.Attribute) (string, bool) {
	if attribute == nil {
		return "", false
	}
	switch expr := attribute.Expr.(type) {
	case *hclsyntax.LiteralValueExpr:
		return ctyValueToDriftString(expr.Val)
	case *hclsyntax.TemplateExpr:
		if len(expr.Variables()) != 0 {
			return "", false
		}
		val, diags := expr.Value(nil)
		if diags.HasErrors() {
			return "", false
		}
		return ctyValueToDriftString(val)
	default:
		return "", false
	}
}

// ctyValueToDriftString formats one cty.Value as the canonical drift-comparison
// string. Strings come back unquoted and HCL-unescaped (so `"foo\"bar"` is
// rendered as `foo"bar` to match the state side). Booleans render as `true`
// or `false`, never `cty.True` / `cty.False`. Numbers render as their decimal
// integer form when exact, otherwise as a minimal decimal. Unknown or null
// values are treated as "no signal" so the caller routes them to
// unknown_attributes.
func ctyValueToDriftString(v cty.Value) (string, bool) {
	if !v.IsKnown() || v.IsNull() {
		return "", false
	}
	switch v.Type() {
	case cty.String:
		return v.AsString(), true
	case cty.Bool:
		if v.True() {
			return "true", true
		}
		return "false", true
	case cty.Number:
		bf := v.AsBigFloat()
		if bf.IsInt() {
			if i, acc := bf.Int64(); acc == big.Exact {
				return strconv.FormatInt(i, 10), true
			}
		}
		f, _ := bf.Float64()
		return strconv.FormatFloat(f, 'f', -1, 64), true
	default:
		return "", false
	}
}
