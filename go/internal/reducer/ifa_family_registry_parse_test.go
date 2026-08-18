// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"testing"
)

// ifaFamilyRegistryRowRE pulls every `IFA_FAMILY_BLOCKER_KIND[<family>]="<value>"`
// element assignment out of one row file under
// scripts/lib/ifa_family_registry/rows/. It intentionally matches only this
// one table's assignment syntax, not the several sibling tables
// (IFA_FAMILY_WAIT_STAGE, IFA_FAMILY_ANCHOR, ...) every row file also
// populates. This file is the live-parse counterpart to
// materialized_edge_family_blocker_shape_test.go's checkFamilyBlockerLockstep:
// it exists so that file reads the real declaration
// scripts/lib/ifa_family_registry.sh's own accessor functions return, never
// a Go-side copy of it.
var ifaFamilyRegistryRowRE = regexp.MustCompile(`IFA_FAMILY_BLOCKER_KIND\[(\w+)\]="([^"]*)"`)

// ifaFamilyRegistryRowsDir returns the absolute path to
// scripts/lib/ifa_family_registry/rows/, the directory
// scripts/lib/ifa_family_registry.sh sources every family's row file from (in
// LC_ALL=C order, by ordinal-prefixed filename -- 01_sql_relationships.sh,
// 02_code_calls.sh, and so on). Resolved via runtime.Caller so it works
// regardless of the directory `go test` is invoked from -- this package's
// existing convention for repo-root-relative test fixtures (see e.g.
// cross_repo_source_tool_snapshot_test.go, handles_route_java_test.go).
func ifaFamilyRegistryRowsDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "..")
	return filepath.Join(repoRoot, "scripts", "lib", "ifa_family_registry", "rows")
}

// parseIfaFamilyRegistryBlockerKinds reads every *.sh file directly under
// rowsDir -- the real scripts/lib/ifa_family_registry/rows/ in every
// committed use of this function, a scratch copy only when proving this
// parser is load-bearing -- and returns the family -> declared-blocker-kind
// rows parsed from their IFA_FAMILY_BLOCKER_KIND[...]="..." assignments.
//
// Three distinct failure modes are each a named, loud test failure, never a
// quietly empty map: the directory itself missing or unreadable, the
// directory matching zero *.sh files, and any individual row file matching
// zero recognized IFA_FAMILY_BLOCKER_KIND assignments. This mirrors
// ifa_family_registry.sh's own fail-closed row-loading guard on the Go side
// -- that shell file's own comment calls an empty result here "the single
// most dangerous failure mode this file could ship": silent, total coverage
// loss that looks exactly like success. An empty declared-set here would
// make every covered family look "not declared" and pass vacuously through
// TestMaterializedEdgeFamilyBlockerLockstep's not-yet-declared branch --
// exactly the failure this test suite exists to prevent, just moved one
// level down into how the declaration itself is read.
//
// A family with no row anywhere under rowsDir is simply absent from the
// returned map, the same as before the rows-directory split: callers must
// treat that absence as "no blocker cell declared yet," not a parse
// failure -- see materializedEdgeFamilyNotYetInRegistry.
func parseIfaFamilyRegistryBlockerKinds(t *testing.T, rowsDir string) map[string]string {
	t.Helper()

	entries, err := os.ReadDir(rowsDir)
	if err != nil {
		t.Fatalf("read dir %s: %v -- rows directory missing or unreadable; every gate that iterates this registry would silently drive zero families and still report success, so this parser refuses to treat that as \"no families declared\"", rowsDir, err)
	}

	var rowFiles []string
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".sh" {
			continue
		}
		rowFiles = append(rowFiles, filepath.Join(rowsDir, entry.Name()))
	}
	if len(rowFiles) == 0 {
		t.Fatalf("%s: matched zero *.sh row files -- registry format changed or the rows directory was emptied; this parser found no family declarations at all and refuses to treat that as \"no families declared\"", rowsDir)
	}
	sort.Strings(rowFiles) // mirrors the shell loader's explicit LC_ALL=C sort of ordinal-prefixed filenames

	declared := make(map[string]string, len(rowFiles))
	for _, path := range rowFiles {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		rows := ifaFamilyRegistryRowRE.FindAllSubmatch(raw, -1)
		if len(rows) == 0 {
			t.Fatalf("%s: matched no IFA_FAMILY_BLOCKER_KIND[<family>]=\"<value>\" assignments -- registry row-file format changed, this parser is stale", path)
		}
		for _, row := range rows {
			declared[string(row[1])] = string(row[2])
		}
	}
	return declared
}
