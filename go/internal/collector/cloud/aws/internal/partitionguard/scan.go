// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package partitionguard

import (
	"fmt"
	"go/ast"
	goparser "go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// commercialARNPrefix is the literal prefix that marks a hardcoded commercial
// partition in a synthesized ARN. GovCloud/China ARNs begin with `arn:aws-`, so
// this prefix matches only the commercial partition baked into a literal.
const commercialARNPrefix = "arn:aws:"

// Violation is one hardcoded-partition ARN synthesis found in scanner source.
type Violation struct {
	// File is the source file path, relative to the services dir root.
	File string
	// Line is the 1-based line of the offending literal.
	Line int
	// Literal is the offending string literal value (the synthesized ARN prefix).
	Literal string
	// Context describes why it was flagged ("concatenation" or "format string").
	Context string
}

// String renders a violation for a guard failure message.
func (v Violation) String() string {
	return fmt.Sprintf("%s:%d: hardcoded partition in ARN %s (%q) — derive it with awscloud.PartitionForRegion/PartitionForBoundary/PartitionFromARN", v.File, v.Line, v.Context, v.Literal)
}

// ScanForHardcodedPartitions walks every non-test Go file under servicesDir
// (recursively, including awssdk adapters) and returns the hardcoded-partition
// ARN synthesis violations it finds, sorted by file then line.
func ScanForHardcodedPartitions(servicesDir string) ([]Violation, error) {
	var violations []Violation
	walkErr := filepath.WalkDir(servicesDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fileViolations, fileErr := scanFile(servicesDir, path)
		if fileErr != nil {
			return fileErr
		}
		violations = append(violations, fileViolations...)
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	sort.Slice(violations, func(i, j int) bool {
		if violations[i].File != violations[j].File {
			return violations[i].File < violations[j].File
		}
		return violations[i].Line < violations[j].Line
	})
	return violations, nil
}

// scanFile parses one Go file and flags hardcoded-partition ARN synthesis. It
// also resolves package-level const/var string identifiers so a hardcoded ARN
// prefix stored in a constant and later concatenated is still caught.
func scanFile(servicesDir, path string) ([]Violation, error) {
	fset := token.NewFileSet()
	file, err := goparser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, fmt.Errorf("parse %q: %w", path, err)
	}
	rel, err := filepath.Rel(servicesDir, path)
	if err != nil {
		rel = path
	}

	// Identifiers bound to a commercial-prefixed string literal; a concatenation
	// or format use of one of these is also a synthesis violation.
	commercialConsts := map[string]token.Pos{}
	for _, decl := range file.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || (genDecl.Tok != token.CONST && genDecl.Tok != token.VAR) {
			continue
		}
		for _, spec := range genDecl.Specs {
			valueSpec, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range valueSpec.Names {
				if i >= len(valueSpec.Values) {
					continue
				}
				if v, ok := stringLitValue(valueSpec.Values[i]); ok && strings.HasPrefix(v, commercialARNPrefix) {
					commercialConsts[name.Name] = valueSpec.Values[i].Pos()
				}
			}
		}
	}

	var violations []Violation
	flag := func(pos token.Pos, literal, context string) {
		violations = append(violations, Violation{
			File:    rel,
			Line:    fset.Position(pos).Line,
			Literal: literal,
			Context: context,
		})
	}

	ast.Inspect(file, func(n ast.Node) bool {
		switch e := n.(type) {
		case *ast.BinaryExpr:
			if e.Op != token.ADD {
				return true
			}
			for _, operand := range []ast.Expr{e.X, e.Y} {
				if lit, ok := commercialOperand(operand, commercialConsts); ok {
					flag(operand.Pos(), lit, "concatenation")
				}
			}
		case *ast.CallExpr:
			switch fmtCallKind(e.Fun) {
			case fmtCallFormat:
				// Sprintf/Errorf/Printf/Fprintf: the first argument is the format
				// string, so a commercial ARN prefix there is a synthesis.
				if len(e.Args) == 0 {
					return true
				}
				if v, ok := stringLitValue(e.Args[0]); ok && strings.HasPrefix(v, commercialARNPrefix) {
					flag(e.Args[0].Pos(), v, "format string")
				}
			case fmtCallConcat:
				// Sprint/Sprintln concatenate their operands rather than taking a
				// format string, so a commercial ARN prefix in ANY argument is a
				// synthesis.
				for _, arg := range e.Args {
					if v, ok := stringLitValue(arg); ok && strings.HasPrefix(v, commercialARNPrefix) {
						flag(arg.Pos(), v, "fmt.Sprint argument")
					}
				}
			}
		}
		return true
	})
	return violations, nil
}

// commercialOperand reports whether an operand of a `+` is a commercial-prefixed
// ARN literal, or an identifier bound to one.
func commercialOperand(expr ast.Expr, commercialConsts map[string]token.Pos) (string, bool) {
	if v, ok := stringLitValue(expr); ok && strings.HasPrefix(v, commercialARNPrefix) {
		return v, true
	}
	if ident, ok := expr.(*ast.Ident); ok {
		if _, bound := commercialConsts[ident.Name]; bound {
			return ident.Name, true
		}
	}
	return "", false
}

// fmt call categories. A format call's first argument is a format string; a
// concat call (Sprint/Sprintln) has no format string and concatenates its
// operands. Both can synthesize a hardcoded-partition ARN, but the literal sits
// in a different position, so the guard inspects them differently.
const (
	fmtCallNone   = ""
	fmtCallFormat = "format"
	fmtCallConcat = "concat"
)

// fmtCallKind classifies a fmt call as a format-string call
// (Sprintf/Errorf/Printf/Fprintf), a concatenating call (Sprint/Sprintln), or
// neither. It returns fmtCallNone for non-fmt or other fmt calls.
func fmtCallKind(fun ast.Expr) string {
	sel, ok := fun.(*ast.SelectorExpr)
	if !ok {
		return fmtCallNone
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok || pkg.Name != "fmt" {
		return fmtCallNone
	}
	switch sel.Sel.Name {
	case "Sprintf", "Errorf", "Printf", "Fprintf":
		return fmtCallFormat
	case "Sprint", "Sprintln":
		return fmtCallConcat
	}
	return fmtCallNone
}

// stringLitValue returns the unquoted value of a string-literal expression.
func stringLitValue(expr ast.Expr) (string, bool) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return value, true
}
