// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package payloadusage

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// FieldUsage records that one reducer source file reads a named field off a
// value returned by a decode<Kind> call.
type FieldUsage struct {
	// File is the source file's path relative to the scanned root, e.g.
	// "aws_resource_materialization.go" at the top level or
	// "containerimage/identity.go" once a family moves into a subpackage.
	// Relative rather than a bare base name so two files with the same name
	// in different subpackages stay distinguishable.
	File string
	// GoFieldName is the struct field's Go identifier, e.g. "ResourceType".
	GoFieldName string
}

// parsedGoFile pairs a parsed reducer source file with its path relative to
// the scanned root, so later passes can attribute a finding back to the file it
// came from without re-deriving the name from *ast.File (which does not carry
// it).
type parsedGoFile struct {
	name string
	file *ast.File
}

// ScanDecodeUsage AST-walks every non-test *.go file under reducerDir at any
// depth and returns, for each decode function name in seams, the set of
// FieldUsage entries found. The walk is recursive on purpose: an earlier
// version read only the top level on the assumption that go/internal/reducer
// is flat, which is already false (dsl/, tfstate/ and tags/ live under it) and
// which the package restructure (#6053) makes false everywhere. A decode site
// the scan cannot reach is a silent under-report -- the manifest gate stays
// green while covering less -- not an error anyone would notice.
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
//
// PACKAGE ISOLATION: the recursive walk can pull files from more than one Go
// package into parsedFiles (root reducerDir plus any subpackage). Wrapper
// structs (case 3 above) are derived separately per package directory
// (scanPackageGroup/groupByPackageDir), because Go permits two distinct
// packages to declare a type with the identical name, and folding every
// parsed file into one shared, unqualified-type-name-keyed map let one
// package's wrapper silently overwrite another's. A decode function's bare
// name is recognized within a package only: the seams are unexported, so no
// package can call another's, and when a family moves to a subpackage its
// factschema_decode_*.go seam moves with it — which is what the recursive walk
// exists to find — leaving the seam and its call sites in one package. So
// decodeFuncs itself stays a single global set, a conservative baseline rather
// than a claim about cross-package calls, and effectiveDecodeFuncs additionally
// drops a name, per package, when that package ALSO locally declares its own
// validly decode-shaped function of the same name returning a DIFFERENT
// struct: a coincidental, unrelated same-named function must not satisfy the
// real seam's binding for that package's own call sites (review finding on
// #6080).
func ScanDecodeUsage(reducerDir string, seams []DecodeSeam) (map[string][]FieldUsage, error) {
	decodeFuncs := make(map[string]struct{}, len(seams))
	structToFunc := make(map[string]string, len(seams)) // qualified struct -> decode func name
	funcToStruct := make(map[string]string, len(seams)) // decode func name -> qualified struct
	for _, s := range seams {
		decodeFuncs[s.FuncName] = struct{}{}
		structToFunc[s.QualifiedStruct()] = s.FuncName
		funcToStruct[s.FuncName] = s.QualifiedStruct()
	}

	parsedFiles, err := parseReducerDir(reducerDir)
	if err != nil {
		return nil, err
	}

	usage := map[string][]FieldUsage{}
	for _, group := range groupByPackageDir(parsedFiles) {
		scanPackageGroup(group, decodeFuncs, structToFunc, funcToStruct, usage)
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

// groupByPackageDir partitions parsedFiles by the directory portion of each
// file's path relative to the scanned root — the standard Go convention of
// one package per directory. Grouping this way (rather than by the file's
// declared `package` clause, which a malformed or deliberately-mismatched
// fixture could spoof) gives scanPackageGroup an isolation boundary that
// matches the boundary the Go compiler itself enforces: two files under the
// same directory are always the same package, and two files under different
// directories are never accidentally folded together. Group keys are sorted
// for deterministic iteration; the final usage map is sorted again in
// ScanDecodeUsage regardless, so this only makes intermediate processing
// order reproducible for debugging, not a correctness requirement.
func groupByPackageDir(parsedFiles []parsedGoFile) [][]parsedGoFile {
	byDir := map[string][]parsedGoFile{}
	for _, pf := range parsedFiles {
		dir := filepath.Dir(pf.name)
		byDir[dir] = append(byDir[dir], pf)
	}
	dirs := make([]string, 0, len(byDir))
	for dir := range byDir {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	groups := make([][]parsedGoFile, 0, len(dirs))
	for _, dir := range dirs {
		groups = append(groups, byDir[dir])
	}
	return groups
}

// scanPackageGroup runs the wrapper-derivation and field-read passes against
// one package's files only, appending any findings into usage. Confining both
// passes to a single package group is what prevents a same-named wrapper
// struct or decode-shaped function in one package from clobbering or
// satisfying another package's entry (see ScanDecodeUsage's PACKAGE ISOLATION
// doc).
func scanPackageGroup(files []parsedGoFile, decodeFuncs map[string]struct{}, structToFunc, funcToStruct map[string]string, usage map[string][]FieldUsage) {
	localDecodeFuncs := effectiveDecodeFuncs(files, decodeFuncs, funcToStruct)
	wrappers := wrapperSeamFields(files, structToFunc)

	for _, pf := range files {
		for _, decl := range pf.file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			boundTo := boundIdentifiers(fn, localDecodeFuncs, structToFunc)
			wrapperBound := wrapperBoundIdentifiers(fn, wrappers)
			if len(boundTo) == 0 && len(wrapperBound) == 0 {
				continue
			}
			recordFieldReads(fn.Body, pf.name, boundTo, wrapperBound, wrappers, usage)
		}
	}
}

// effectiveDecodeFuncs returns the subset of decodeFuncs safe to bind within
// one package group: a name is dropped when the group ITSELF locally declares
// a same-named function that also has a valid decode-seam shape (a single
// facts.Envelope-shaped parameter and a (<pkg>.<Struct>, error) result, the
// same shape decodeFuncReturnType recognizes for ParseDecodeSeams) but
// returns a DIFFERENT qualified struct than the real seam. Go permits two
// distinct packages to declare a function with the identical name and a
// genuinely different, also-valid decode-shaped signature; without this
// check, that package's own unrelated decode-shaped function would satisfy
// the real seam's binding and misattribute its field reads.
//
// A local declaration that does not parse as a valid decode-seam shape at all
// (an unqualified return type, the wrong parameter count, and so on) is NOT
// treated as a conflict: it is not a "valid" same-named decode helper, and
// excluding it would also break the ordinary cross-package call-site pattern
// the recursive walk exists to support — a handler in one package legitimately
// calling the canonical seam declared in another.
func effectiveDecodeFuncs(files []parsedGoFile, decodeFuncs map[string]struct{}, funcToStruct map[string]string) map[string]struct{} {
	effective := make(map[string]struct{}, len(decodeFuncs))
	for name := range decodeFuncs {
		effective[name] = struct{}{}
	}
	for _, pf := range files {
		for _, decl := range pf.file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil {
				continue
			}
			wantStruct, isSeamName := funcToStruct[fn.Name.Name]
			if !isSeamName {
				continue
			}
			gotPkg, gotName, ok := decodeFuncReturnType(fn)
			if !ok {
				continue
			}
			if gotPkg+"."+gotName != wantStruct {
				delete(effective, fn.Name.Name)
			}
		}
	}
	return effective
}

// parseReducerDir parses every non-test *.go file under dir at any depth and
// returns them paired with their path relative to dir. The name is relative
// rather than a bare base name so two files with the same name in different
// subpackages stay distinguishable in the usage output.
func parseReducerDir(dir string) ([]parsedGoFile, error) {
	if _, err := os.Stat(dir); err != nil {
		return nil, fmt.Errorf("payloadusage: read reducer dir %s: %w", dir, err)
	}

	fset := token.NewFileSet()
	var parsed []parsedGoFile
	// parseFailure carries this package's own error out of the walk so the
	// caller sees it verbatim instead of nested inside a walk wrapper.
	var parseFailure error
	walkErr := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			// testdata holds deliberately broken fixtures that must never be
			// parsed as production source, matching the exclusion every other
			// gate in this repo applies.
			if entry.Name() == "testdata" && path != dir {
				return filepath.SkipDir
			}
			return nil
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		// #nosec G304 -- path comes from WalkDir over a fixed reducer dir, not
		// from untrusted input.
		file, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			parseFailure = fmt.Errorf("payloadusage: parse %s: %w", path, parseErr)
			return filepath.SkipAll
		}
		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			rel = name
		}
		parsed = append(parsed, parsedGoFile{name: rel, file: file})
		return nil
	})
	if parseFailure != nil {
		return nil, parseFailure
	}
	if walkErr != nil {
		return nil, fmt.Errorf("payloadusage: walk reducer dir %s: %w", dir, walkErr)
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
