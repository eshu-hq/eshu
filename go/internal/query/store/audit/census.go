// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package audit

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// sqlFragment recognizes a string literal that reads like SQL: a FROM or JOIN
// that names a relation and continues like a clause. Prose such as "copied from
// the store" does not match, because no clause keyword follows the noun.
var sqlFragment = regexp.MustCompile(
	`(?i)\b(?:from|join)\s+[a-z_][a-z0-9_.]*\s*(?:\(|\s(?:as\s+[a-z_]|on\b|where\b|join\b|left\b|inner\b|cross\b|group\b|order\b|limit\b|for\b)|;|$)`,
)

// kindLiteral recognizes a string literal shaped like a fact kind; it counts
// only when the generated registry knows it or it is reducer-owned, so an
// ordinary identifier never reads as a kind.
var kindLiteral = regexp.MustCompile(`^[a-z][a-z0-9_.]*$`)

func isFactKindLiteral(text string) bool {
	if !kindLiteral.MatchString(text) {
		return false
	}
	if strings.HasPrefix(text, reducerKindPrefix) {
		return true
	}
	_, ok := facts.FactKindRegistryEntryFor(text)
	return ok
}

var createRelation = regexp.MustCompile(
	`(?i)create\s+(?:or\s+replace\s+)?(?:unlogged\s+)?(?:materialized\s+)?(?:table|view)\s+(?:if\s+not\s+exists\s+)?([a-z_][a-z0-9_.]*)`,
)

// KnownRelations returns the tables and views the schema creates, read from the
// CREATE statements in the SQL migrations and Go schema constants under root
// (the storage/postgres tree). Names are lower-case, without a public prefix.
func KnownRelations(root string) (map[string]struct{}, error) {
	known := map[string]struct{}{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		isSchemaFile := strings.HasSuffix(path, ".sql") || strings.HasSuffix(path, ".go")
		if entry.IsDir() || strings.HasSuffix(path, "_test.go") || !isSchemaFile {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, match := range createRelation.FindAllStringSubmatch(string(raw), -1) {
			known[strings.TrimPrefix(strings.ToLower(match[1]), "public.")] = struct{}{}
		}
		return nil
	})
	return known, err
}

// Census walks the non-test Go files under root and returns, per package
// directory (relative to root, slash-separated), the stores its SQL string
// literals read. It is the static half of the audit: it needs no probe, so a
// package cannot hide a read by not writing one. It over-approximates, because
// a literal cannot say which capability it serves; the audit compares a
// package's census to the union of its capabilities' declarations.
//
// known is the set of tables and views the schema creates (see
// KnownRelations); a relation outside it is a CTE alias or a word in prose and
// is ignored. A fact_records read whose fragment names no kind takes the fact
// kinds the package defines as literals; with none, it lands in Unresolved.
func Census(root string, known map[string]struct{}) (map[string]Used, error) {
	perPackage := map[string][]literal{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		relDir, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return err
		}
		key := filepath.ToSlash(relDir)
		ast.Inspect(parsed, func(node ast.Node) bool {
			lit, ok := node.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			text, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			perPackage[key] = append(perPackage[key], literal{file: filepath.Base(path), text: text})
			return true
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	out := make(map[string]Used, len(perPackage))
	for pkg, literals := range perPackage {
		used := censusPackage(literals, known)
		if len(used.Stores) > 0 || len(used.Unresolved) > 0 {
			out[pkg] = used
		}
	}
	return out, nil
}

type literal struct{ file, text string }

func censusPackage(literals []literal, known map[string]struct{}) Used {
	var kinds []string
	seenKind := map[string]struct{}{}
	ctes := map[string]struct{}{}
	for _, lit := range literals {
		for _, match := range cteName.FindAllStringSubmatch(lit.text, -1) {
			ctes[strings.ToLower(match[1])] = struct{}{}
		}
		if isFactKindLiteral(lit.text) {
			if _, ok := seenKind[lit.text]; !ok {
				seenKind[lit.text] = struct{}{}
				kinds = append(kinds, lit.text)
			}
		}
	}
	sort.Strings(kinds)
	used := Used{Stores: map[string]string{}}
	for _, lit := range literals {
		if !sqlFragment.MatchString(lit.text) {
			continue
		}
		fragment := Used{Stores: map[string]string{}}
		classifyOne(Statement{SQL: lit.text}, censusScope{census: true, fallbackKinds: kinds, ctes: ctes, known: known}, &fragment)
		for store, evidence := range fragment.Stores {
			add(&used, store, lit.file+": "+evidence)
		}
		for _, reason := range fragment.Unresolved {
			used.Unresolved = append(used.Unresolved, lit.file+": "+reason)
		}
	}
	sort.Strings(used.Unresolved)
	return used
}
