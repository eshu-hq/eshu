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

// FieldUsage records that one reducer source file reads a named field off a
// value returned by a decode<Kind> call.
type FieldUsage struct {
	// File is the reducer file's base name, e.g. "aws_resource_materialization.go".
	File string
	// GoFieldName is the struct field's Go identifier, e.g. "ResourceType".
	GoFieldName string
}

// parsedGoFile pairs a parsed reducer source file with its base name, so
// later passes can attribute a finding back to the file it came from without
// re-deriving the name from *ast.File (which does not carry it).
type parsedGoFile struct {
	name string
	file *ast.File
}

// ScanDecodeUsage AST-walks every non-test *.go file directly under
// reducerDir (go/internal/reducer is flat, no subpackages) and returns, for
// each decode function name in seams, the set of FieldUsage entries found.
//
// A field read is attributed to a seam's decode function in two ways:
//
//  1. Direct: `resource, err := decodeAWSResource(env)` followed by
//     `resource.SomeField` anywhere in the SAME function body — the common
//     shape every migrated extractor uses.
//  2. Indirect: a helper function declared with a parameter typed as the
//     seam's qualified struct (for example
//     `func deriveS3InternetExposureDecision(posture awsv1.S3BucketPosture)`)
//     — every `posture.SomeField` read inside THAT function body is
//     attributed to the seam whose struct type matches the parameter,
//     regardless of which file declares the helper or how many call frames
//     separate it from the original decode call. This covers the real
//     handler pattern where a decoded struct is passed by value into one or
//     more derivation helpers (s3_internet_exposure_rows.go is the reference
//     case: deriveS3InternetExposureDecision and deriveS3PublicPolicyDecision
//     both take awsv1.S3BucketPosture as a plain parameter, not a decode call
//     result).
//  3. Wrapper-mediated: a handler stores the decoded struct in a WRAPPER
//     struct field typed as the seam struct (`iamPermissionStatement.permission
//     iamv1.Permission`, `secretsIAMPrincipal.decoded iamv1.Principal`) and
//     reads the seam field two selector levels deep —
//     `statement.permission.Actions`, `principal.decoded.AccountID` — after
//     ranging the wrapper slice inside a helper or taking the wrapper by value.
//     The read of the seam field is attributed to the decode func its wrapper
//     field type came from (see wrapper.go). This is what makes
//     aws_iam_permission / aws_resource_policy_permission report their
//     actions/not_actions/resources reads, and aws_iam_principal report its
//     account_id/region reads, instead of undercounting them (#4668).
//
// ATTRIBUTION BOUNDARY (a documented limitation, not a bug): the wrapper hop in
// case 3 is a SINGLE hop through a bare value field. A read reachable only
// through general multi-hop dataflow or aliasing — a value returned from a
// call and then wrapped, a range over a map-indexed expression
// (`range g.statementsByAction[key]`), or a wrapper whose seam field is a
// pointer/slice — is still not followed, because resolving it soundly needs
// full type information this AST-only scan deliberately avoids. Missing one of
// those only leaves a field unattributed (UsedFields stays a lower bound); it
// never misattributes, because BuildManifest joins each recorded read against
// the attributed struct's declared fields and drops anything that does not
// match.
//
// This is the usage half of the derivation: DecodeSeam/StructShape describe
// what a struct *declares*; ScanDecodeUsage finds what a handler actually
// *reads* off the decoded value, so the gate can flag a field a handler reads
// that the declared schema does not cover — Contract System v1 §6 enforcement
// gate 2's reverse-break check.
func ScanDecodeUsage(reducerDir string, seams []DecodeSeam) (map[string][]FieldUsage, error) {
	decodeFuncs := make(map[string]struct{}, len(seams))
	structToFunc := make(map[string]string, len(seams)) // qualified struct -> decode func name
	for _, s := range seams {
		decodeFuncs[s.FuncName] = struct{}{}
		structToFunc[s.QualifiedStruct()] = s.FuncName
	}

	parsedFiles, err := parseReducerDir(reducerDir)
	if err != nil {
		return nil, err
	}

	// Wrapper structs (a local struct with a field typed as a seam struct) are
	// derived once for the whole directory, since a type declared in one file
	// is used across the package's handler files.
	wrappers := wrapperSeamFields(parsedFiles, structToFunc)

	usage := map[string][]FieldUsage{}
	for _, pf := range parsedFiles {
		for _, decl := range pf.file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			boundTo := boundIdentifiers(fn, decodeFuncs, structToFunc)
			wrapperBound := wrapperBoundIdentifiers(fn, wrappers)
			if len(boundTo) == 0 && len(wrapperBound) == 0 {
				continue
			}
			recordFieldReads(fn.Body, pf.name, boundTo, wrapperBound, wrappers, usage)
		}
	}

	for funcName := range usage {
		sort.Slice(usage[funcName], func(i, j int) bool {
			a, b := usage[funcName][i], usage[funcName][j]
			if a.File != b.File {
				return a.File < b.File
			}
			return a.GoFieldName < b.GoFieldName
		})
	}
	return usage, nil
}

// parseReducerDir parses every non-test *.go file directly under dir and
// returns them paired with their base names.
func parseReducerDir(dir string) ([]parsedGoFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("payloadusage: read reducer dir %s: %w", dir, err)
	}

	fset := token.NewFileSet()
	var parsed []parsedGoFile
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		path := filepath.Join(dir, name)
		// #nosec G304 -- path is built from a fixed reducer dir plus a *.go
		// entry name from os.ReadDir of that same dir; not untrusted input.
		file, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			return nil, fmt.Errorf("payloadusage: parse %s: %w", path, parseErr)
		}
		parsed = append(parsed, parsedGoFile{name: name, file: file})
	}
	return parsed, nil
}

// boundIdentifiers finds every local identifier inside fn that is bound to a
// decoded typed struct: either a direct decodeFuncs() call-result assignment
// (recordDecodeBindings), or a function parameter whose type is one of
// structToFunc's qualified struct names (the cross-function helper-parameter
// case). It returns identifier name -> decode func name.
func boundIdentifiers(fn *ast.FuncDecl, decodeFuncs map[string]struct{}, structToFunc map[string]string) map[string]string {
	boundTo := map[string]string{}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		recordDecodeBindings(assign, decodeFuncs, boundTo)
		return true
	})
	recordParameterBindings(fn, structToFunc, boundTo)
	return boundTo
}

// recordParameterBindings inspects fn's parameter list and records any
// parameter whose declared type is a package-qualified struct name present in
// structToFunc — the `func helper(posture awsv1.S3BucketPosture)` shape a
// decoded struct is passed into by value. Multiple parameter names in one
// field group (`func f(a, b awsv1.Resource)`) are all recorded.
func recordParameterBindings(fn *ast.FuncDecl, structToFunc map[string]string, boundTo map[string]string) {
	if fn.Type.Params == nil {
		return
	}
	for _, field := range fn.Type.Params.List {
		qualified, ok := qualifiedTypeName(field.Type)
		if !ok {
			continue
		}
		decodeFunc, ok := structToFunc[qualified]
		if !ok {
			continue
		}
		for _, name := range field.Names {
			if name.Name == "_" {
				continue
			}
			boundTo[name.Name] = decodeFunc
		}
	}
}

// qualifiedTypeName returns the package-qualified name of a *ast.SelectorExpr
// type expression (e.g. "awsv1.Resource" for a parameter declared
// `awsv1.Resource`), or ok=false for any other type shape (a pointer to the
// struct, a slice, a built-in type, etc. — those are not the direct-value
// parameter shape this scan targets).
func qualifiedTypeName(expr ast.Expr) (string, bool) {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	pkgIdent, ok := sel.X.(*ast.Ident)
	if !ok {
		return "", false
	}
	return pkgIdent.Name + "." + sel.Sel.Name, true
}

// recordFieldReads walks body and records a FieldUsage in two shapes:
//
//  1. `ident.Field` where ident is a key of boundTo (a seam-bound value from a
//     decode call or a seam-typed parameter) — attributed to boundTo[ident].
//  2. `wrapper.<seamField>.<StructField>` where wrapper is a key of
//     wrapperBound (a value of a wrapper struct type) and <seamField> is a
//     field of that wrapper whose type is a seam struct — the read of
//     <StructField> is attributed to the decode func that seam field came
//     from. This follows the one wrapper-mediated hop the migrated
//     IAM/secrets_iam handlers use (statement.permission.Actions,
//     principal.decoded.AccountID); deeper nesting (`a.b.c.d`) is not followed.
//
// A read that matches no declared field of the attributed struct is dropped
// later by BuildManifest (it joins against the struct's declared fields), so a
// wrapper read of a non-schema field never becomes a false violation.
func recordFieldReads(body *ast.BlockStmt, fileName string, boundTo, wrapperBound map[string]string, wrappers map[string]map[string]string, usage map[string][]FieldUsage) {
	ast.Inspect(body, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if ident, isIdent := sel.X.(*ast.Ident); isIdent {
			if funcName, isBound := boundTo[ident.Name]; isBound {
				usage[funcName] = append(usage[funcName], FieldUsage{File: fileName, GoFieldName: sel.Sel.Name})
			}
			return true
		}
		inner, isSel := sel.X.(*ast.SelectorExpr)
		if !isSel {
			return true
		}
		base, isIdent := inner.X.(*ast.Ident)
		if !isIdent {
			return true
		}
		wrapperType, isWrapperBound := wrapperBound[base.Name]
		if !isWrapperBound {
			return true
		}
		funcName, isSeamField := wrappers[wrapperType][inner.Sel.Name]
		if !isSeamField {
			return true
		}
		usage[funcName] = append(usage[funcName], FieldUsage{File: fileName, GoFieldName: sel.Sel.Name})
		return true
	})
}

// recordDecodeBindings inspects one assignment statement and, when its RHS is
// a direct call to a decode<Kind> function (the `resource, err :=
// decodeAWSResource(env)` shape every migrated handler uses), records the LHS
// value identifier as bound to that decode function name.
func recordDecodeBindings(assign *ast.AssignStmt, decodeFuncs map[string]struct{}, boundTo map[string]string) {
	if len(assign.Rhs) != 1 {
		return
	}
	call, ok := assign.Rhs[0].(*ast.CallExpr)
	if !ok {
		return
	}
	callee, ok := call.Fun.(*ast.Ident)
	if !ok {
		return
	}
	if _, isDecodeFunc := decodeFuncs[callee.Name]; !isDecodeFunc {
		return
	}
	if len(assign.Lhs) == 0 {
		return
	}
	valueIdent, ok := assign.Lhs[0].(*ast.Ident)
	if !ok || valueIdent.Name == "_" {
		return
	}
	boundTo[valueIdent.Name] = callee.Name
}
