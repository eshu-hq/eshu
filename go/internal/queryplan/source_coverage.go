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
	"sort"
	"strings"
)

// SourceCoverage records every production Run or RunSingle call in one query
// package source file.
type SourceCoverage struct {
	File  string          `yaml:"file"`
	Calls []QueryCallsite `yaml:"calls"`
}

// QueryCallsite records the enclosing production symbol, its exact number of
// graph query calls, its source digest, and whether it is a registered hot path.
type QueryCallsite struct {
	Symbol       string             `yaml:"symbol"`
	Count        int                `yaml:"count"`
	EntryIDs     []string           `yaml:"entry_ids,omitempty"`
	NonHot       *NonHotDisposition `yaml:"non_hot,omitempty"`
	Reason       string             `yaml:"non_hot_reason,omitempty"`
	SourceDigest string             `yaml:"source_sha256,omitempty"`
}

// Closed non-hot coverage classes. They are intentionally behavioral rather
// than prose so a callsite cannot escape the hot-query gate with a placeholder.
const (
	NonHotClassKeyedSupport    = "keyed_support"
	NonHotClassLabelInventory  = "label_inventory"
	NonHotClassDelegated       = "delegated"
	NonHotClassOperatorQuery   = "operator_query"
	NonHotClassBackendMetadata = "backend_metadata"
)

// Closed key-bound classes for keyed support reads.
const (
	NonHotKeyBoundSingle = "single_key"
	NonHotKeyBoundBatch  = "bounded_key_batch"
)

// NonHotDisposition is machine-checked evidence that a production graph call
// is not an independently hot planner path. SourceDigest freezes the reviewed
// production symbol; any source change forces the disposition to be re-audited.
type NonHotDisposition struct {
	Class        string `yaml:"class"`
	SourceDigest string `yaml:"source_sha256"`
	KeyBound     string `yaml:"key_bound,omitempty"`
	MaxKeys      int    `yaml:"max_keys,omitempty"`
	Label        string `yaml:"label,omitempty"`
	MaxResults   int    `yaml:"max_results,omitempty"`
	Delegate     string `yaml:"delegate,omitempty"`
	Policy       string `yaml:"policy,omitempty"`
	Operation    string `yaml:"operation,omitempty"`
}

// DiscoverQueryCallsites returns every direct Run or RunSingle selector call
// in non-test Go files recursively beneath queryDir. Testdata plus hidden and
// underscore-prefixed directories are excluded from the inventory.
func DiscoverQueryCallsites(queryDir string) ([]SourceCoverage, error) {
	coverage := make([]SourceCoverage, 0)
	err := filepath.WalkDir(queryDir, func(path string, dirEntry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if dirEntry.IsDir() {
			name := dirEntry.Name()
			if path != queryDir && (name == "testdata" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")) {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		calls, err := discoverFileQueryCallsites(path)
		if err != nil {
			return err
		}
		if len(calls) == 0 {
			return nil
		}
		relative, err := filepath.Rel(queryDir, path)
		if err != nil {
			return fmt.Errorf("resolve query source path %s: %w", path, err)
		}
		coverage = append(coverage, SourceCoverage{File: filepath.ToSlash(relative), Calls: calls})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk query source directory: %w", err)
	}
	sort.Slice(coverage, func(i, j int) bool { return coverage[i].File < coverage[j].File })
	return coverage, nil
}

// ValidateSourceCoverage rejects newly added, removed, or rehomed production
// graph query calls and requires each callsite to have an explicit disposition.
func ValidateSourceCoverage(manifest Manifest, discovered []SourceCoverage) error {
	entryIDs := make(map[string]struct{}, len(manifest.Entries))
	for _, entry := range manifest.Entries {
		entryIDs[entry.ID] = struct{}{}
	}

	expected, violations := flattenCoverage(manifest.SourceCoverage, true, entryIDs)
	actual, actualViolations := flattenCoverage(discovered, false, nil)
	violations = append(violations, actualViolations...)
	for key, callsite := range actual {
		expectedCallsite, ok := expected[key]
		if !ok {
			violations = append(violations, fmt.Sprintf(
				"unregistered query callsite %s (count %d)",
				key,
				callsite.Count,
			))
			continue
		}
		if callsite.Count != expectedCallsite.Count {
			violations = append(violations, fmt.Sprintf(
				"%s: discovered call count %d, manifest requires %d",
				key,
				callsite.Count,
				expectedCallsite.Count,
			))
		}
		if len(expectedCallsite.EntryIDs) > 0 &&
			len(expectedCallsite.SourceDigest) == sha256.Size*2 &&
			callsite.SourceDigest != expectedCallsite.SourceDigest {
			violations = append(violations, fmt.Sprintf(
				"%s: hot callsite source_sha256 does not match production symbol (manifest %s, production %s)",
				key,
				expectedCallsite.SourceDigest,
				callsite.SourceDigest,
			))
		}
		if strings.TrimSpace(expectedCallsite.Reason) != "" {
			if manifest.GrandfatheredNonHotBaseline != grandfatheredNonHotBaseline {
				violations = append(violations, fmt.Sprintf(
					"%s: new callsites cannot use legacy non_hot_reason",
					key,
				))
			} else if digest, grandfathered := grandfatheredNonHotSourceDigests[key]; !grandfathered {
				violations = append(violations, fmt.Sprintf(
					"%s: new callsites cannot use legacy non_hot_reason",
					key,
				))
			} else if callsite.SourceDigest != digest {
				violations = append(violations, fmt.Sprintf(
					"%s: grandfathered source_sha256 does not match production symbol (baseline %s, production %s)",
					key,
					digest,
					callsite.SourceDigest,
				))
			}
		}
		if expectedCallsite.NonHot != nil && callsite.SourceDigest != expectedCallsite.NonHot.SourceDigest {
			violations = append(violations, fmt.Sprintf(
				"%s: source evidence mismatch: source_sha256 does not match production symbol (manifest %s, production %s)",
				key,
				expectedCallsite.NonHot.SourceDigest,
				callsite.SourceDigest,
			))
		}
	}
	for key := range expected {
		if _, ok := actual[key]; !ok {
			violations = append(violations, fmt.Sprintf("stale query callsite registration %s", key))
		}
	}
	if len(violations) > 0 {
		sort.Strings(violations)
		return errors.New(strings.Join(violations, "; "))
	}
	return nil
}

func discoverFileQueryCallsites(path string) ([]QueryCallsite, error) {
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, path, nil, 0)
	if err != nil {
		return nil, fmt.Errorf("parse query source %s: %w", path, err)
	}
	counts := make(map[string]int)
	digests := make(map[string]string)
	source, err := os.ReadFile(path) // #nosec G304 -- path is discovered beneath the caller-provided source directory
	if err != nil {
		return nil, fmt.Errorf("read query source %s: %w", path, err)
	}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok {
			count := countQueryCalls(declaration)
			if count > 0 {
				counts["<package-init>"] += count
				digests["<package-init>"] = fmt.Sprintf("%x", sha256.Sum256(source))
			}
			continue
		}
		if function.Body == nil {
			continue
		}
		symbol := functionSymbol(function)
		count := countQueryCalls(function.Body)
		if count == 0 {
			continue
		}
		start := fileSet.Position(function.Pos()).Offset
		end := fileSet.Position(function.End()).Offset
		if start < 0 || end < start {
			return nil, fmt.Errorf("invalid source offsets for %s", symbol)
		}
		if end > len(source) {
			return nil, fmt.Errorf("invalid source end offset for %s", symbol)
		}
		digest := fmt.Sprintf("%x", sha256.Sum256(source[start:end]))
		counts[symbol] += count
		digests[symbol] = digest
	}
	result := make([]QueryCallsite, 0, len(counts))
	for symbol, count := range counts {
		if count == 0 {
			continue
		}
		result = append(result, QueryCallsite{Symbol: symbol, Count: count, SourceDigest: digests[symbol]})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Symbol < result[j].Symbol })
	return result, nil
}

func countQueryCalls(node ast.Node) int {
	count := 0
	ast.Inspect(node, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || (selector.Sel.Name != "Run" && selector.Sel.Name != "RunSingle") {
			return true
		}
		count++
		return true
	})
	return count
}

func functionSymbol(function *ast.FuncDecl) string {
	if function.Recv == nil || len(function.Recv.List) == 0 {
		return function.Name.Name
	}
	receiver := function.Recv.List[0].Type
	return "(" + receiverName(receiver) + ")." + function.Name.Name
}

func receiverName(expression ast.Expr) string {
	switch value := expression.(type) {
	case *ast.Ident:
		return value.Name
	case *ast.StarExpr:
		return "*" + receiverName(value.X)
	case *ast.IndexExpr:
		return receiverName(value.X)
	case *ast.IndexListExpr:
		return receiverName(value.X)
	default:
		return "unknown"
	}
}

func flattenCoverage(
	coverage []SourceCoverage,
	validateDisposition bool,
	entryIDs map[string]struct{},
) (map[string]QueryCallsite, []string) {
	flattened := make(map[string]QueryCallsite)
	var violations []string
	for _, source := range coverage {
		if strings.TrimSpace(source.File) == "" {
			violations = append(violations, "query source coverage missing file")
			continue
		}
		for _, callsite := range source.Calls {
			key := source.File + ":" + callsite.Symbol
			if strings.TrimSpace(callsite.Symbol) == "" || callsite.Count <= 0 {
				violations = append(violations, fmt.Sprintf("%s: invalid symbol or call count", key))
				continue
			}
			if _, duplicate := flattened[key]; duplicate {
				violations = append(violations, fmt.Sprintf("duplicate query callsite registration %s", key))
				continue
			}
			flattened[key] = callsite
			if !validateDisposition {
				continue
			}
			hasLegacyReason := strings.TrimSpace(callsite.Reason) != ""
			if len(callsite.EntryIDs) == 0 && callsite.NonHot == nil && !hasLegacyReason {
				violations = append(violations, fmt.Sprintf("%s: requires entry_ids or a non-hot disposition", key))
			}
			if len(callsite.EntryIDs) > 0 && (callsite.NonHot != nil || hasLegacyReason) {
				violations = append(violations, fmt.Sprintf("%s: cannot declare both entry_ids and a non-hot disposition", key))
			}
			if len(callsite.EntryIDs) > 0 && len(callsite.SourceDigest) != sha256.Size*2 {
				violations = append(violations, fmt.Sprintf(
					"%s: hot callsite requires a SHA-256 source_sha256",
					key,
				))
			}
			if callsite.NonHot != nil && hasLegacyReason {
				violations = append(violations, fmt.Sprintf("%s: cannot declare both typed and legacy non-hot dispositions", key))
			}
			if callsite.NonHot != nil {
				violations = append(violations, validateNonHotDisposition(key, *callsite.NonHot)...)
			}
			for _, entryID := range callsite.EntryIDs {
				if _, ok := entryIDs[entryID]; !ok {
					violations = append(violations, fmt.Sprintf("%s: unknown hot path %s", key, entryID))
				}
			}
		}
	}
	return flattened, violations
}

func validateNonHotDisposition(key string, disposition NonHotDisposition) []string {
	var violations []string
	if len(disposition.SourceDigest) != sha256.Size*2 {
		violations = append(violations, fmt.Sprintf("%s: non_hot requires a SHA-256 source_sha256", key))
	}
	switch disposition.Class {
	case NonHotClassKeyedSupport:
		if disposition.KeyBound != NonHotKeyBoundSingle && disposition.KeyBound != NonHotKeyBoundBatch {
			violations = append(violations, fmt.Sprintf("%s: keyed_support requires key_bound", key))
		}
		if disposition.KeyBound == NonHotKeyBoundBatch && disposition.MaxKeys <= 0 {
			violations = append(violations, fmt.Sprintf("%s: bounded_key_batch requires max_keys", key))
		}
		if disposition.MaxResults <= 0 {
			violations = append(violations, fmt.Sprintf("%s: keyed_support requires max_results", key))
		}
	case NonHotClassLabelInventory:
		if strings.TrimSpace(disposition.Label) == "" {
			violations = append(violations, fmt.Sprintf("%s: label_inventory requires label", key))
		}
		if disposition.MaxResults <= 0 {
			violations = append(violations, fmt.Sprintf("%s: label_inventory requires max_results", key))
		}
	case NonHotClassDelegated:
		if disposition.Delegate != "graph_session" && disposition.Delegate != "profiled_callee" {
			violations = append(violations, fmt.Sprintf("%s: delegated requires delegate", key))
		}
	case NonHotClassOperatorQuery:
		if disposition.Policy != "authenticated_read_endpoint" && disposition.Policy != "validated_query_endpoint" {
			violations = append(violations, fmt.Sprintf("%s: operator_query requires policy", key))
		}
	case NonHotClassBackendMetadata:
		if disposition.Operation != "relationship_types" {
			violations = append(violations, fmt.Sprintf("%s: backend_metadata requires operation", key))
		}
	default:
		violations = append(violations, fmt.Sprintf("%s: unsupported non-hot class %q", key, disposition.Class))
	}
	return violations
}
