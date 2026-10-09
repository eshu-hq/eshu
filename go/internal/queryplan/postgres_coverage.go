// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queryplan

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
)

// PostgresSourceCoverage inventories SQL execution selectors in one pilot
// source file. Discovery also reports unscoped files; the pilot matrix marks
// those excluded, not certified as PostgreSQL operations.
type PostgresSourceCoverage struct {
	File  string             `yaml:"file" json:"file"`
	Calls []PostgresCallsite `yaml:"calls" json:"calls"`
}

// PostgresCallsite binds an execution family to its enclosing source symbol.
// A registered family links entry IDs; a nonpilot family states its exclusion.
type PostgresCallsite struct {
	Symbol               string   `yaml:"symbol" json:"symbol"`
	Count                int      `yaml:"count" json:"count"`
	Methods              []string `yaml:"methods" json:"methods"`
	SourceSHA256         string   `yaml:"source_sha256" json:"source_sha256"`
	Dynamic              bool     `yaml:"dynamic" json:"dynamic"`
	EntryIDs             []string `yaml:"entry_ids,omitempty" json:"entry_ids,omitempty"`
	PilotExclusionReason string   `yaml:"pilot_exclusion_reason,omitempty" json:"pilot_exclusion_reason,omitempty"`
}

// PostgresCoverageRow makes the pilot scope explicit for every discovered
// execution candidate file. An excluded row carries no certification claim.
type PostgresCoverageRow struct {
	File   string `json:"file"`
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

// PostgresCoverageMatrix reports scoped and excluded discovered files. A
// scoped file is only certified after ValidatePostgresCoverage succeeds.
func PostgresCoverageMatrix(manifest Manifest, discovered []PostgresSourceCoverage) []PostgresCoverageRow {
	selected := make(map[string]struct{}, len(manifest.PilotPostgresFiles))
	for _, file := range manifest.PilotPostgresFiles {
		selected[file] = struct{}{}
	}
	rows := make([]PostgresCoverageRow, 0, len(discovered))
	for _, source := range discovered {
		row := PostgresCoverageRow{File: source.File, Status: "excluded", Reason: "outside PostgreSQL pilot scope"}
		if _, ok := selected[source.File]; ok {
			row.Status, row.Reason = "pilot", ""
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].File < rows[j].File })
	return rows
}

var postgresExecutionMethods = map[string]struct{}{
	"QueryContext": {}, "QueryRowContext": {}, "ExecContext": {},
	"Query": {}, "QueryRow": {}, "Exec": {},
}

// DiscoverPostgresCallsites walks non-test Go source for SQL/pgx execution
// selector candidates, including wrapper calls. It intentionally does not
// claim type certainty from syntax alone; the coverage disposition records
// exactly which candidates belong to the pilot.
func DiscoverPostgresCallsites(queryDir string) ([]PostgresSourceCoverage, error) {
	var coverage []PostgresSourceCoverage
	err := filepath.WalkDir(queryDir, func(path string, item fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if item.IsDir() {
			if path != queryDir && (item.Name() == "testdata" || strings.HasPrefix(item.Name(), ".") || strings.HasPrefix(item.Name(), "_")) {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		calls, err := discoverPostgresFile(path)
		if err != nil {
			return err
		}
		if len(calls) == 0 {
			return nil
		}
		relative, err := filepath.Rel(queryDir, path)
		if err != nil {
			return err
		}
		coverage = append(coverage, PostgresSourceCoverage{File: filepath.ToSlash(relative), Calls: calls})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("discover PostgreSQL execution candidates: %w", err)
	}
	sort.Slice(coverage, func(i, j int) bool { return coverage[i].File < coverage[j].File })
	return coverage, nil
}

func discoverPostgresFile(path string) ([]PostgresCallsite, error) {
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, path, nil, 0)
	if err != nil {
		return nil, err
	}
	source, err := os.ReadFile(path) // #nosec G304 -- path is walked under caller-provided query directory
	if err != nil {
		return nil, err
	}
	var calls []PostgresCallsite
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Body == nil {
			continue
		}
		methods := make([]string, 0)
		dynamic := false
		ast.Inspect(function.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if _, candidate := postgresExecutionMethods[selector.Sel.Name]; !candidate {
				return true
			}
			methods = append(methods, selector.Sel.Name)
			queryArgument := 0
			if strings.HasSuffix(selector.Sel.Name, "Context") {
				queryArgument = 1
			}
			if len(call.Args) <= queryArgument {
				dynamic = true
			} else if _, literal := call.Args[queryArgument].(*ast.BasicLit); !literal {
				dynamic = true
			}
			return true
		})
		if len(methods) == 0 {
			continue
		}
		start := fileSet.Position(function.Pos()).Offset
		end := fileSet.Position(function.End()).Offset
		if start < 0 || end < start || end > len(source) {
			return nil, fmt.Errorf("invalid source offsets for %s", functionSymbol(function))
		}
		sort.Strings(methods)
		calls = append(calls, PostgresCallsite{
			Symbol: functionSymbol(function), Count: len(methods), Methods: methods,
			Dynamic: dynamic, SourceSHA256: fmt.Sprintf("%x", sha256.Sum256(source[start:end])),
		})
	}
	sort.Slice(calls, func(i, j int) bool { return calls[i].Symbol < calls[j].Symbol })
	return calls, nil
}

// ValidatePostgresCoverage requires exact call counts, methods, dynamic shape,
// and enclosing source digests for every named pilot file. All other files
// remain explicitly outside this pilot's PostgreSQL claim.
func ValidatePostgresCoverage(manifest Manifest, discovered []PostgresSourceCoverage) error {
	selected := make(map[string]struct{}, len(manifest.PilotPostgresFiles))
	var violations []string
	for _, file := range manifest.PilotPostgresFiles {
		if file == "" || filepath.IsAbs(file) || filepath.Clean(file) != file || strings.HasPrefix(file, "..") {
			violations = append(violations, "invalid PostgreSQL pilot file "+file)
		}
		if _, duplicate := selected[file]; duplicate {
			violations = append(violations, "duplicate PostgreSQL pilot file "+file)
		}
		selected[file] = struct{}{}
	}
	registered := make(map[string]PostgresCallsite)
	linked := make(map[string]struct{})
	for _, source := range manifest.PostgresCoverage {
		if _, ok := selected[source.File]; !ok {
			violations = append(violations, fmt.Sprintf("PostgreSQL coverage file %s outside pilot scope", source.File))
		}
		for _, call := range source.Calls {
			key := source.File + ":" + call.Symbol
			if _, duplicate := registered[key]; duplicate {
				violations = append(violations, "duplicate PostgreSQL callsite "+key)
			}
			registered[key] = call
			if !isSHA256(call.SourceSHA256) || call.Count <= 0 || len(call.Methods) != call.Count {
				violations = append(violations, "malformed PostgreSQL callsite "+key)
			}
			if (len(call.EntryIDs) == 0) == (strings.TrimSpace(call.PilotExclusionReason) == "") {
				violations = append(violations, "PostgreSQL callsite needs entry_ids or pilot_exclusion_reason: "+key)
			}
			for _, id := range call.EntryIDs {
				entry, ok := pilotEntry(manifest, id)
				if !ok || entry.Contract == nil || entry.QueryKind != queryKindSQLReadModel {
					violations = append(violations, fmt.Sprintf("%s: unknown registered PostgreSQL pilot %s", key, id))
				} else {
					linked[id] = struct{}{}
				}
			}
		}
	}
	seen := make(map[string]struct{})
	for _, source := range discovered {
		if _, ok := selected[source.File]; !ok {
			continue
		}
		for _, call := range source.Calls {
			key := source.File + ":" + call.Symbol
			want, ok := registered[key]
			if !ok {
				violations = append(violations, "unregistered PostgreSQL callsite "+key)
				continue
			}
			seen[key] = struct{}{}
			if call.Count != want.Count || call.Dynamic != want.Dynamic || call.SourceSHA256 != want.SourceSHA256 || !reflect.DeepEqual(call.Methods, want.Methods) {
				violations = append(violations, "stale PostgreSQL callsite "+key)
			}
		}
	}
	for key := range registered {
		if _, ok := seen[key]; !ok {
			violations = append(violations, "stale PostgreSQL registration "+key)
		}
	}
	for _, id := range manifest.PilotRequiredIDs {
		entry, ok := pilotEntry(manifest, id)
		if ok && entry.QueryKind == queryKindSQLReadModel {
			if _, linkedToExecution := linked[id]; !linkedToExecution {
				violations = append(violations, "SQL pilot has no PostgreSQL execution callsite "+id)
			}
		}
	}
	if len(violations) > 0 {
		sort.Strings(violations)
		return errors.New(strings.Join(violations, "; "))
	}
	return nil
}

func pilotEntry(manifest Manifest, id string) (Entry, bool) {
	for _, entry := range manifest.Entries {
		if entry.ID == id {
			return entry, true
		}
	}
	return Entry{}, false
}
