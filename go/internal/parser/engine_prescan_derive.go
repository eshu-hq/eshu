// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"path/filepath"
	"strings"
	"sync/atomic"
)

// derivedPreScanLanguages is the exact set of parser-registry languages whose
// PreScan-equivalent ImportsMap contribution can be derived from the same
// parse payload the collector's parse stage already computes, instead of
// running a second dedicated tree-sitter PreScan pass over the file (#4764).
// Scoped to php, javascript, typescript, and tsx: for these languages the
// "functions", "classes", "interfaces", and "traits" parse buckets are a
// proven (shim-verified, 0/0 symmetric set difference) superset-equivalent of
// the names PreScan collects. json and groovy keep their own prescan/parse
// relationship and are intentionally excluded.
var derivedPreScanLanguages = map[string]bool{
	"php":        true,
	"javascript": true,
	"typescript": true,
	"tsx":        true,
}

// IsDerivedPreScanLanguage reports whether language's ImportsMap contribution
// can be derived from a parse payload via DerivePreScanNames instead of
// requiring a dedicated PreScan pass. See derivedPreScanLanguages for scope
// and proof provenance.
func IsDerivedPreScanLanguage(language string) bool {
	return derivedPreScanLanguages[language]
}

// derivePreScanBuckets lists the parse payload buckets whose item names
// reproduce what the legacy tree-sitter PreScan pass collects for the
// languages in derivedPreScanLanguages. This mirrors the equivalence proven
// by the #4764 shim: PHP class/interface/trait/function/method declarations
// land in "classes"/"interfaces"/"traits"/"functions", and PHP anonymous
// classes and JavaScript-family function-valued pair/assignment/variable
// exports all land in "functions" or "classes" via the same synthesized
// names (e.g. phpAnonymousClassName) PreScan uses.
var derivePreScanBuckets = []string{"functions", "classes", "interfaces", "traits"}

// DerivePreScanNames extracts the PreScan-equivalent declaration names from a
// parse payload produced by ParsePath, for use only when the payload's
// language is IsDerivedPreScanLanguage. Names are deduplicated and
// filepath.Clean-normalized identically to the legacy PreScan path so a
// caller can substitute this for a dedicated PreScan call without changing
// ImportsMap output.
func DerivePreScanNames(payload map[string]any) []string {
	seen := make(map[string]struct{})
	var names []string
	for _, bucket := range derivePreScanBuckets {
		items, ok := payload[bucket].([]map[string]any)
		if !ok {
			continue
		}
		for _, item := range items {
			rawName, ok := item["name"].(string)
			if !ok {
				continue
			}
			name := strings.TrimSpace(rawName)
			if name == "" {
				continue
			}
			cleaned := filepath.Clean(name)
			if _, exists := seen[cleaned]; exists {
				continue
			}
			seen[cleaned] = struct{}{}
			names = append(names, cleaned)
		}
	}
	return names
}

// derivedLanguagePreScanDispatchCount counts calls into preScanOnePath's
// dedicated tree-sitter PreScan dispatch for a parser.IsDerivedPreScanLanguage
// language (php, javascript, typescript, tsx). Test-only: production code
// never reads it. It exists so a regression test can assert this count stays
// at zero for a full-ingest collector run, proving the #4764 dedup (deriving
// ImportsMap for these languages from the parse-stage payload instead) has
// not silently regressed back into a second tree-sitter parse per file. An
// atomic keeps this safe to read from a test while collector workers may
// still be dispatching prescan for other, non-derived languages concurrently.
var derivedLanguagePreScanDispatchCount atomic.Int64

// ResetDerivedLanguagePreScanDispatchCountForTest zeroes
// derivedLanguagePreScanDispatchCount and returns its value beforehand.
// Test-only.
func ResetDerivedLanguagePreScanDispatchCountForTest() int64 {
	return derivedLanguagePreScanDispatchCount.Swap(0)
}

// DerivedLanguagePreScanDispatchCountForTest reads
// derivedLanguagePreScanDispatchCount without resetting it. Test-only.
func DerivedLanguagePreScanDispatchCountForTest() int64 {
	return derivedLanguagePreScanDispatchCount.Load()
}
