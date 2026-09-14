// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

func collectTypedPackageStrings(
	pkg *packages.Package,
	declaration *ast.GenDecl,
	valuesByObject map[types.Object]string,
	unknownObjects map[types.Object]string,
) {
	if declaration.Tok != token.CONST && declaration.Tok != token.VAR {
		return
	}
	for _, spec := range declaration.Specs {
		valueSpec, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}
		for index, name := range valueSpec.Names {
			object := pkg.TypesInfo.Defs[name]
			if object == nil {
				continue
			}
			if constantObject, ok := object.(*types.Const); ok {
				if constantObject.Val().Kind() == constant.String {
					valuesByObject[object] = constant.StringVal(constantObject.Val())
				}
				continue
			}
			if !typeContainsString(object.Type(), map[types.Type]struct{}{}) {
				continue
			}
			expression, exists := valueExpression(valueSpec, index)
			if exists {
				parts, resolved := typedInitializerStrings(pkg.TypesInfo, expression)
				if resolved {
					valuesByObject[object] = strings.Join(parts, "\n")
					continue
				}
			}
			position := pkg.Fset.Position(name.Pos())
			unknownObjects[object] = fmt.Sprintf(
				"%s:%d: unresolved package string reference %s",
				filepath.Base(position.Filename),
				position.Line,
				name.Name,
			)
		}
	}
}

func valueExpression(spec *ast.ValueSpec, index int) (ast.Expr, bool) {
	if index < len(spec.Values) {
		return spec.Values[index], true
	}
	if len(spec.Names) == 1 && len(spec.Values) == 1 {
		return spec.Values[0], true
	}
	return nil, false
}

func typedInitializerStrings(info *types.Info, expression ast.Expr) ([]string, bool) {
	expression = ast.Unparen(expression)
	if typed, ok := info.Types[expression]; ok && typed.Value != nil && typed.Value.Kind() == constant.String {
		if identifier, ok := expression.(*ast.Ident); ok {
			if _, constantObject := info.ObjectOf(identifier).(*types.Const); !constantObject {
				return nil, false
			}
		}
		return []string{constant.StringVal(typed.Value)}, true
	}

	switch typed := expression.(type) {
	case *ast.CompositeLit:
		var parts []string
		for _, element := range typed.Elts {
			if keyed, ok := element.(*ast.KeyValueExpr); ok {
				if !isStructFieldKey(info, keyed.Key) {
					keyParts, resolved := typedInitializerStrings(info, keyed.Key)
					if !resolved {
						return nil, false
					}
					parts = append(parts, keyParts...)
				}
				element = keyed.Value
			}
			elementParts, resolved := typedInitializerStrings(info, element)
			if !resolved {
				return nil, false
			}
			parts = append(parts, elementParts...)
		}
		return parts, true
	case *ast.UnaryExpr:
		if typed.Op == token.AND {
			return typedInitializerStrings(info, typed.X)
		}
	}

	if !expressionContainsString(info.TypeOf(expression), map[types.Type]struct{}{}) {
		return nil, true
	}
	return nil, false
}

func isStructFieldKey(info *types.Info, expression ast.Expr) bool {
	identifier, ok := expression.(*ast.Ident)
	if !ok {
		return false
	}
	field, ok := info.ObjectOf(identifier).(*types.Var)
	return ok && field.IsField()
}

func typeContainsString(valueType types.Type, seen map[types.Type]struct{}) bool {
	return expressionContainsString(valueType, seen)
}

func expressionContainsString(valueType types.Type, seen map[types.Type]struct{}) bool {
	if valueType == nil {
		return false
	}
	valueType = types.Unalias(valueType)
	if _, visited := seen[valueType]; visited {
		return false
	}
	seen[valueType] = struct{}{}
	switch typed := valueType.Underlying().(type) {
	case *types.Basic:
		return typed.Info()&types.IsString != 0
	case *types.Array:
		return expressionContainsString(typed.Elem(), seen)
	case *types.Slice:
		return expressionContainsString(typed.Elem(), seen)
	case *types.Map:
		return expressionContainsString(typed.Key(), seen) ||
			expressionContainsString(typed.Elem(), seen)
	case *types.Pointer:
		return expressionContainsString(typed.Elem(), seen)
	case *types.Struct:
		for index := 0; index < typed.NumFields(); index++ {
			if expressionContainsString(typed.Field(index).Type(), seen) {
				return true
			}
		}
	}
	return false
}

func TestTypedPackageStringReferences(t *testing.T) {
	t.Parallel()

	const source = `package cypher

const relationship = "MERGE (a)-[rel:REVIEW_PROBE_FLOWS_TO]->(b)"
const aliasOne = relationship
const aliasTwo = aliasOne
const concatenated = "MERGE (a)-[rel:REVIEW_" + "PROBE_FLOWS_TO]->(b)"
const harmless = "MATCH (n) RETURN n"

var compositeAlias = map[string]string{"edge": aliasTwo}
var mapLiteral = map[string]string{"edge": relationship}
var sliceLiteral = []string{relationship}
var structLiteral = struct{ Edge string }{Edge: relationship}
var sliceStructLiteral = []struct{ Edge string }{{Edge: relationship}}
var mapKeyLiteral = map[string]int{relationship: 1}
var unrelatedComposite = []string{harmless, "RETURN 1"}
var mutableBase = relationship
var mutableAlias = mutableBase

func ChainedAliasWrite() { _ = aliasTwo }
func ConstantConcatWrite() { _ = concatenated }
func CompositeAliasWrite() { _ = compositeAlias }
func MapLiteralWrite() { _ = mapLiteral }
func SliceLiteralWrite() { _ = sliceLiteral }
func StructLiteralWrite() { _ = structLiteral }
func SliceStructLiteralWrite() { _ = sliceStructLiteral }
func MapKeyLiteralWrite() { _ = mapKeyLiteral }
func LocalShadowNodeOnly() { aliasTwo := harmless; _ = aliasTwo }
func UnrelatedNodeOnly() { _ = unrelatedComposite }
func MutableAliasUnknown() { _ = mutableAlias }
`
	dir := writeCypherScanFixture(t, source)
	classifications := classifyCypherPorts(parseCypherPackage(t, dir), map[string]struct{}{
		"ChainedAliasWrite":       {},
		"CompositeAliasWrite":     {},
		"ConstantConcatWrite":     {},
		"LocalShadowNodeOnly":     {},
		"MapKeyLiteralWrite":      {},
		"MapLiteralWrite":         {},
		"MutableAliasUnknown":     {},
		"SliceLiteralWrite":       {},
		"SliceStructLiteralWrite": {},
		"StructLiteralWrite":      {},
		"UnrelatedNodeOnly":       {},
	})
	byPort := make(map[string]cypherPortClassification, len(classifications))
	for _, classification := range classifications {
		byPort[classification.Port] = classification
	}
	for _, port := range []string{
		"ChainedAliasWrite",
		"CompositeAliasWrite",
		"ConstantConcatWrite",
		"MapKeyLiteralWrite",
		"MapLiteralWrite",
		"SliceLiteralWrite",
		"SliceStructLiteralWrite",
		"StructLiteralWrite",
	} {
		if !byPort[port].WritesEdges {
			t.Errorf("%s did not resolve its constant-derived relationship template", port)
		}
	}
	for _, port := range []string{"LocalShadowNodeOnly", "UnrelatedNodeOnly"} {
		if got := byPort[port]; got.WritesEdges || len(got.UnknownRefs) != 0 {
			t.Errorf("%s classification = %+v, want resolved node-only", port, got)
		}
	}
	if got := byPort["MutableAliasUnknown"]; got.WritesEdges || len(got.UnknownRefs) == 0 {
		t.Errorf("MutableAliasUnknown classification = %+v, want unresolved", got)
	}
}

func writeCypherScanFixture(t *testing.T, source string) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"go.mod":    "module example.com/cypherstrings\n\ngo 1.26.6\n",
		"writer.go": source,
	}
	for name, contents := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return dir
}
