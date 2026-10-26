// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package hcl

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
)

// isTFVarsJSONPath reports whether path is a *.tfvars.json file. The registry
// dispatches the match case-insensitively, so the engine must too.
func isTFVarsJSONPath(path string) bool {
	return strings.HasSuffix(strings.ToLower(filepath.Base(path)), ".tfvars.json")
}

// parseTFVarsJSONBody parses a *.tfvars.json body with HCL's JSON syntax
// (#9048) and projects its top-level variables onto an hclsyntax body, so the
// block, declaration, and backend walks see the same shape as the equivalent
// *.tfvars body: no blocks, one attribute per variable. Scalar values land as
// literal expressions, which the value readers consume without source text;
// objects and lists land as object and tuple constructors, which the tfvars
// declaration walks traverse like their native-syntax twins.
func parseTFVarsJSONBody(parser *hclparse.Parser, source []byte, path string) (*hclsyntax.Body, error) {
	file, diags := parser.ParseJSON(source, path)
	if diags.HasErrors() {
		return nil, fmt.Errorf("parse hcl file %q: %s", path, diags.Error())
	}
	attributes, attrDiags := file.Body.JustAttributes()
	if attrDiags.HasErrors() {
		return nil, fmt.Errorf("parse hcl file %q: %s", path, attrDiags.Error())
	}
	body := &hclsyntax.Body{
		Attributes: make(hclsyntax.Attributes, len(attributes)),
	}
	names := make([]string, 0, len(attributes))
	for name := range attributes {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		value, valueDiags := attributes[name].Expr.Value(nil)
		if valueDiags.HasErrors() {
			return nil, fmt.Errorf("parse hcl file %q: %s", path, valueDiags.Error())
		}
		body.Attributes[name] = &hclsyntax.Attribute{
			Name: name,
			Expr: tfVarsJSONExpression(value),
		}
	}
	return body, nil
}

// tfVarsJSONExpression projects one evaluated JSON value onto the expression
// shape the native-syntax parser would have produced for the same variable:
// literals stay literal, objects become object constructors with literal keys,
// and lists become tuple constructors. The constructors carry no source ranges
// (sourceRange reads a zero range as empty), so rows derived from them keep
// their names, values, and outcomes, with only line numbers unset.
func tfVarsJSONExpression(value cty.Value) hclsyntax.Expression {
	if !value.IsKnown() {
		return &hclsyntax.LiteralValueExpr{Val: cty.NullVal(cty.DynamicPseudoType)}
	}
	if value.Type().IsObjectType() || value.Type().IsMapType() {
		items := make([]hclsyntax.ObjectConsItem, 0, value.LengthInt())
		for iterator := value.ElementIterator(); iterator.Next(); {
			key, element := iterator.Element()
			items = append(items, hclsyntax.ObjectConsItem{
				KeyExpr:   &hclsyntax.LiteralValueExpr{Val: key},
				ValueExpr: tfVarsJSONExpression(element),
			})
		}
		sort.Slice(items, func(left, right int) bool {
			return tfVarsJSONKey(items[left].KeyExpr) < tfVarsJSONKey(items[right].KeyExpr)
		})
		return &hclsyntax.ObjectConsExpr{Items: items}
	}
	if value.Type().IsTupleType() || value.Type().IsListType() || value.Type().IsSetType() {
		expressions := make([]hclsyntax.Expression, 0, value.LengthInt())
		for iterator := value.ElementIterator(); iterator.Next(); {
			_, element := iterator.Element()
			expressions = append(expressions, tfVarsJSONExpression(element))
		}
		return &hclsyntax.TupleConsExpr{Exprs: expressions}
	}
	return &hclsyntax.LiteralValueExpr{Val: value}
}

// tfVarsJSONKey renders a synthesized object key for ordering. Keys are always
// string literals by construction; anything else sorts first and never panics.
func tfVarsJSONKey(expression hclsyntax.Expression) string {
	literal, ok := expression.(*hclsyntax.LiteralValueExpr)
	if !ok || literal.Val.Type() != cty.String || !literal.Val.IsKnown() || literal.Val.IsNull() {
		return ""
	}
	return literal.Val.AsString()
}
