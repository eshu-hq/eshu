// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package payloadusage

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// StructField is one named, JSON-tagged field declared on a typed factschema
// payload struct.
type StructField struct {
	// GoName is the exported Go field identifier, e.g. "ResourceType".
	GoName string
	// JSONName is the field's JSON payload key, e.g. "resource_type".
	JSONName string
	// Required is true when the field is a schema-required field: a
	// non-pointer, non-slice, non-map type with no "omitempty" tag option.
	// This mirrors the same rule sdk/go/factschema/decode.go's
	// requiredFields registration and internal/schemagen use, so the parsed
	// shape agrees with the generated JSON Schema's "required" array without
	// needing to invoke the schema generator.
	Required bool
}

// StructShape is the parsed field set of one typed factschema payload
// struct, keyed by its package-qualified name (e.g. "awsv1.Resource").
type StructShape struct {
	// Qualified is the package-qualified struct name, e.g. "awsv1.Resource".
	Qualified string
	// Fields lists every named, schema-declared field, sorted by JSONName.
	// A field tagged `json:"-"` (the untyped Attributes pass-through every
	// polymorphic envelope carries) is deliberately excluded: it is not a
	// declared schema property, so a handler reading it is never a
	// reverse-break candidate for this gate.
	Fields []StructField
}

// FieldByGoName returns the field with the given Go identifier, and whether
// it was found.
func (s StructShape) FieldByGoName(name string) (StructField, bool) {
	for _, f := range s.Fields {
		if f.GoName == name {
			return f, true
		}
	}
	return StructField{}, false
}

// ParseStructShapes parses every top-level struct type declared directly in
// the Go files under dir (non-recursive; each factschema family version
// directory such as sdk/go/factschema/aws/v1 is flat) and returns a
// StructShape per exported struct, keyed by pkgAlias.StructName.
//
// pkgAlias is the import alias the reducer package uses for this directory's
// package (e.g. "awsv1" for sdk/go/factschema/aws/v1), supplied by the caller
// because the alias is a reducer-side naming convention, not something this
// directory's own package clause records.
func ParseStructShapes(dir, pkgAlias string) (map[string]StructShape, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("payload-usage-manifest: read struct dir %s: %w", dir, err)
	}

	fset := token.NewFileSet()
	shapes := map[string]StructShape{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		// #nosec G304 -- path is built from a fixed struct dir plus a *.go
		// entry name from os.ReadDir of that same dir; not untrusted input.
		file, parseErr := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if parseErr != nil {
			return nil, fmt.Errorf("payload-usage-manifest: parse %s: %w", path, parseErr)
		}
		for _, decl := range file.Decls {
			genDecl, ok := decl.(*ast.GenDecl)
			if !ok || genDecl.Tok != token.TYPE {
				continue
			}
			for _, spec := range genDecl.Specs {
				typeSpec, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				// Only exported structs can be a decode seam's returned type
				// (a seam's QualifiedStruct is always <alias>.<Exported>).
				// Skip unexported helper structs such as the resourceAlias /
				// relationshipAlias types the custom JSON marshalers use, so
				// they never pollute the shape map.
				if !ast.IsExported(typeSpec.Name.Name) {
					continue
				}
				structType, ok := typeSpec.Type.(*ast.StructType)
				if !ok {
					continue
				}
				shape := structShapeFromAST(pkgAlias, typeSpec.Name.Name, structType)
				shapes[shape.Qualified] = shape
			}
		}
	}
	return shapes, nil
}

// structShapeFromAST converts a parsed struct type into a StructShape,
// reading each field's json tag to determine its JSONName and Required flag.
// A field with no json tag, an anonymous (embedded) field, or a tag of
// `json:"-"` is excluded from Fields — the last case is exactly how the
// polymorphic envelopes (Resource.Attributes, Relationship.Attributes) mark
// their untyped pass-through map as not a declared schema property.
func structShapeFromAST(pkgAlias, structName string, structType *ast.StructType) StructShape {
	shape := StructShape{Qualified: pkgAlias + "." + structName}
	if structType.Fields == nil {
		return shape
	}
	for _, field := range structType.Fields.List {
		if len(field.Names) == 0 {
			continue // anonymous/embedded field, not a named JSON key.
		}
		if field.Tag == nil {
			continue
		}
		jsonName, omitempty, hasJSONTag := parseJSONTag(field.Tag.Value)
		if !hasJSONTag || jsonName == "-" {
			continue
		}
		goName := field.Names[0].Name
		shape.Fields = append(shape.Fields, StructField{
			GoName:   goName,
			JSONName: jsonName,
			Required: !omitempty && !isOptionalGoType(field.Type),
		})
	}
	sort.Slice(shape.Fields, func(i, j int) bool { return shape.Fields[i].JSONName < shape.Fields[j].JSONName })
	return shape
}

// isOptionalGoType reports whether a struct field's Go type is inherently
// optional (a pointer, slice, or map), matching the same rule the decode seam
// and schemagen use: a pointer/slice/map field is optional even without an
// explicit "omitempty" tag, because its zero value (nil) already round-trips
// as "absent".
func isOptionalGoType(expr ast.Expr) bool {
	switch expr.(type) {
	case *ast.StarExpr, *ast.ArrayType, *ast.MapType:
		return true
	default:
		return false
	}
}

// parseJSONTag extracts the json struct-tag's field name and whether it
// carries the "omitempty" option, from the raw backtick-quoted tag literal
// (e.g. "`json:\"account_id\"`" or "`json:\"arn,omitempty\"`"). It returns
// hasJSONTag=false when the tag has no json key at all.
func parseJSONTag(rawTag string) (name string, omitempty bool, hasJSONTag bool) {
	unquoted := strings.Trim(rawTag, "`")
	const jsonKey = `json:"`
	idx := strings.Index(unquoted, jsonKey)
	if idx < 0 {
		return "", false, false
	}
	rest := unquoted[idx+len(jsonKey):]
	end := strings.IndexByte(rest, '"')
	if end < 0 {
		return "", false, false
	}
	parts := strings.Split(rest[:end], ",")
	name = parts[0]
	for _, opt := range parts[1:] {
		if opt == "omitempty" {
			omitempty = true
		}
	}
	return name, omitempty, true
}
