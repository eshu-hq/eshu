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

// ifaFamilyRegistryBlockerKindRE and ifaFamilyRegistryWaitKeyRE each pull
// every `IFA_FAMILY_BLOCKER_KIND[<family>]="<value>"` /
// `IFA_FAMILY_WAIT_KEY[<family>]="<value>"` element assignment out of one row
// file under scripts/lib/ifa_family_registry/rows/. Each regexp intentionally
// matches only its own table's assignment syntax, not the several sibling
// tables (IFA_FAMILY_WAIT_STAGE, IFA_FAMILY_ANCHOR, ...) every row file also
// populates. This file is the live-parse counterpart to
// materialized_edge_family_blocker_shape_test.go's checkFamilyBlockerLockstep
// and its wait-key domain-membership check: both exist so that file reads
// the real declarations scripts/lib/ifa_family_registry.sh's own accessor
// functions return, never a Go-side copy of them.
var (
	// Anchored to line start (?m). Unanchored, these matched an assignment
	// anywhere in a row file -- including inside a comment -- and a later match
	// overwrote an earlier one, so a commented-out old row left below the live
	// one would be parsed as the declaration and silently drive the lockstep
	// test against a value the shell loader never reads.
	ifaFamilyRegistryBlockerKindRE = regexp.MustCompile(`(?m)^IFA_FAMILY_BLOCKER_KIND\[(\w+)\]="([^"]*)"`)
	ifaFamilyRegistryWaitKeyRE     = regexp.MustCompile(`(?m)^IFA_FAMILY_WAIT_KEY\[(\w+)\]="([^"]*)"`)
)

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

// parseIfaFamilyRegistryBlockerKinds reads every family's declared
// IFA_FAMILY_BLOCKER_KIND row from rowsDir. See parseIfaFamilyRegistryTable
// for the shared fail-closed parsing contract this and
// parseIfaFamilyRegistryWaitKeys both rely on.
func parseIfaFamilyRegistryBlockerKinds(t *testing.T, rowsDir string) map[string]string {
	t.Helper()
	return parseIfaFamilyRegistryTable(t, rowsDir, ifaFamilyRegistryBlockerKindRE, "IFA_FAMILY_BLOCKER_KIND")
}

// parseIfaFamilyRegistryWaitKeys reads every family's declared
// IFA_FAMILY_WAIT_KEY row from rowsDir -- the queue/domain-completion key the
// shell fault-injection wait helper (ifa_fault_wait_for_claimed) polls on. It
// exists so TestIfaFamilyRegistryWaitKeyIsKnownDomain in
// materialized_edge_family_blocker_shape_test.go can bind each declared value
// to a real reducer.Domain constant instead of trusting the registry row and
// the wait helper's own hand-typed pin to keep agreeing with each other after
// a Domain rename. See parseIfaFamilyRegistryTable for the shared parsing
// contract.
func parseIfaFamilyRegistryWaitKeys(t *testing.T, rowsDir string) map[string]string {
	t.Helper()
	return parseIfaFamilyRegistryTable(t, rowsDir, ifaFamilyRegistryWaitKeyRE, "IFA_FAMILY_WAIT_KEY")
}

// parseIfaFamilyRegistryTable reads every *.sh file directly under rowsDir --
// the real scripts/lib/ifa_family_registry/rows/ in every committed use of
// this function, a scratch copy only when proving this parser is
// load-bearing -- and returns the family -> declared-value rows parsed from
// tableRE's `<table>[<family>]="<value>"` assignments. tableName names the
// table in diagnostics only.
//
// Three distinct failure modes are each a named, loud test failure, never a
// quietly empty map: the directory itself missing or unreadable, the
// directory matching zero *.sh files, and any individual row file matching
// zero recognized tableRE assignments. This mirrors ifa_family_registry.sh's
// own fail-closed row-loading guard on the Go side -- that shell file's own
// comment calls an empty result here "the single most dangerous failure mode
// this file could ship": silent, total coverage loss that looks exactly like
// success. An empty declared-set here would make every covered family look
// "not declared" and pass vacuously through whichever caller-side
// not-yet-declared branch consumes it -- exactly the failure this test suite
// exists to prevent, just moved one level down into how the declaration
// itself is read.
//
// A family with no row anywhere under rowsDir is simply absent from the
// returned map, the same as before the rows-directory split: callers must
// treat that absence as "no row declared yet," not a parse failure -- see
// materializedEdgeFamilyNotYetInRegistry for the blocker-kind table's
// caller-side convention.
func parseIfaFamilyRegistryTable(t *testing.T, rowsDir string, tableRE *regexp.Regexp, tableName string) map[string]string {
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
		rows := tableRE.FindAllSubmatch(raw, -1)
		if len(rows) == 0 {
			t.Fatalf("%s: matched no %s[<family>]=\"<value>\" assignments -- registry row-file format changed, this parser is stale", path, tableName)
		}
		for _, row := range rows {
			declared[string(row[1])] = string(row[2])
		}
	}
	return declared
}
