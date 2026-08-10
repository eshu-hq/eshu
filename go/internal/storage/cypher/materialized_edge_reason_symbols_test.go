// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// goIdentifierInReason matches the Go identifiers the registries cite inside
// their human-readable reason strings — the template or retract constant that
// writes or reaps each edge type.
var goIdentifierInReason = regexp.MustCompile(`\b(batchCanonical\w+|canonical\w+Cypher|retract\w+Cypher)\b`)

// packageIdentifiers returns every identifier declared in this package's
// non-test sources.
func packageIdentifiers(t *testing.T) map[string]struct{} {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	declared := map[string]struct{}{}
	decl := regexp.MustCompile(`(?m)^(?:func|const|var|type)\s+\(?\s*(\w+)|^\s*(\w+)\s*=\s*` + "`")
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(filepath.Clean(name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for _, m := range decl.FindAllStringSubmatch(string(raw), -1) {
			for _, g := range m[1:] {
				if g != "" {
					declared[g] = struct{}{}
				}
			}
		}
	}
	if len(declared) == 0 {
		t.Fatal("no identifiers parsed from the package; this test would pass vacuously")
	}
	return declared
}

// TestRegistryReasonsCiteRealSymbols catches registry reasons that name a
// function or constant which does not exist.
//
// Every materialized-edge registry maps an edge type to a reason naming what
// writes it, and those names are the first thing a maintainer greps for when a
// family's coverage looks wrong. Three separate reasons in this package have
// already cited invented symbols — two template constants whose real names were
// different, and a dispatch function that had been renamed — because prose is
// not compiled and nothing checked it.
//
// The regex deliberately matches only the repo's own naming conventions for
// these artifacts. A reason mentioning something outside those shapes is not
// checked here; the point is to pin the citations that are load-bearing for
// navigation, not to police English.
func TestRegistryReasonsCiteRealSymbols(t *testing.T) {
	t.Parallel()

	declared := packageIdentifiers(t)

	registries := map[string]map[string]string{
		"code_calls":        CodeCallMaterializedEdgeTypes(),
		"inheritance_edges": InheritanceMaterializedEdgeTypes(),
		"repo_dependency":   RepoDependencyMaterializedEdgeTypes(),
	}
	for _, family := range SingleTypeMaterializedEdgeFamilyNames() {
		reg, ok := SingleTypeMaterializedEdgeTypes(family)
		if !ok {
			t.Fatalf("family %q vanished between listing and lookup", family)
		}
		registries[family] = reg
	}

	var missing []string
	for family, registry := range registries {
		for edgeType, reason := range registry {
			for _, m := range goIdentifierInReason.FindAllString(reason, -1) {
				if _, ok := declared[m]; !ok {
					missing = append(missing, family+"/"+edgeType+" cites "+m)
				}
			}
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("registry reasons cite symbols this package does not declare:\n  %s\nA maintainer grepping for one of these finds nothing and cannot verify the family's derivation.",
			strings.Join(missing, "\n  "))
	}
}
