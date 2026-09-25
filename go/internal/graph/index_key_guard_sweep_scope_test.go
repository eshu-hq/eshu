// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graph

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// writeSweepLiteral plants a one-literal Go file at root/rel so a sweep test
// can decide, per directory, whether the sweep reads or skips it.
func writeSweepLiteral(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	src := "package p\n\nconst q = " + strconv.Quote(body) + "\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
}

// sweepFlaggedFiles returns the base names of the files the sweep flags under
// root/dirs.
func sweepFlaggedFiles(t *testing.T, root string, dirs []string) map[string]bool {
	t.Helper()
	findings, _ := sweepIndexedWrites(t, root, dirs)
	got := map[string]bool{}
	for _, f := range findings {
		got[filepath.Base(strings.SplitN(f.file, ":", 2)[0])] = true
	}
	return got
}

// TestSweepClassifiesLabelOnlyWritesClean proves a statement whose only writes
// add or remove labels assigns no property value, so it has no indexed value
// to measure and needs no exception, while a label SET that also assigns an
// indexed property is still read and flagged when the guard cannot read it.
func TestSweepClassifiesLabelOnlyWritesClean(t *testing.T) {
	root := t.TempDir()
	dir := "internal/storage/"
	writeSweepLiteral(t, root, dir+"label_migration.go",
		"MATCH (r:TerraformResource {uid: row.uid}) SET r:TerraformStateResource REMOVE r:TerraformResource")
	writeSweepLiteral(t, root, dir+"label_set_only.go",
		"MATCH (m:Module {name: $name}) SET m:Reviewed")
	writeSweepLiteral(t, root, dir+"label_and_property.go",
		"UNWIND $rows AS row\nUNWIND row.params AS p\nMATCH (x:Parameter {path: row.file_path})\nSET x:Tagged, x.name = p.name")
	writeSweepLiteral(t, root, dir+"property_only.go",
		"UNWIND $rows AS row\nUNWIND row.params AS p\nMATCH (x:Parameter {path: row.file_path})\nSET x.name = p.name")

	got := sweepFlaggedFiles(t, root, []string{"internal/storage"})
	for _, name := range []string{"label_migration.go", "label_set_only.go"} {
		if got[name] {
			t.Errorf("%s flagged, want clean: a label-only write assigns no property value", name)
		}
	}
	for _, name := range []string{"label_and_property.go", "property_only.go"} {
		if !got[name] {
			t.Errorf("%s not flagged: a property SET on an indexed key must still be read", name)
		}
	}
}

// TestSweepFlagsIndexedCreateInProductionDir proves a CREATE that assigns an
// indexed property is flagged in a production tree, including the exact
// literal the latency gate seeder uses, so excluding that tool is a scope
// decision and not a blind spot in the classifier.
func TestSweepFlagsIndexedCreateInProductionDir(t *testing.T) {
	root := t.TempDir()
	writeSweepLiteral(t, root, "internal/storage/seed.go", "UNWIND $rows AS row CREATE (n:%s { %s })")
	writeSweepLiteral(t, root, "cmd/reducer/seed.go", "UNWIND $rows AS row CREATE (n:%s { %s })")

	findings, _ := sweepIndexedWrites(t, root, []string{"internal/storage", "cmd"})
	got := map[string]bool{}
	for _, f := range findings {
		got[strings.SplitN(f.file, ":", 2)[0]] = true
	}
	for _, rel := range []string{"internal/storage/seed.go", "cmd/reducer/seed.go"} {
		if !got[rel] {
			t.Errorf("%s not flagged; findings = %v", rel, findings)
		}
	}
}

// TestSweepSkipsCmdGateTools proves the sweep covers deployed write paths
// only: a cmd tool named as a gate (a test or benchmark harness that seeds a
// throwaway store) is not scanned, while a service directly beside it is.
func TestSweepSkipsCmdGateTools(t *testing.T) {
	root := t.TempDir()
	const create = "UNWIND $rows AS row CREATE (n:%s { %s })"
	writeSweepLiteral(t, root, "cmd/read-api-latency-gate/seed_graph.go", create)
	writeSweepLiteral(t, root, "cmd/golden-corpus-gate/seed.go", create)
	writeSweepLiteral(t, root, "cmd/ci-gates/seed.go", create)
	writeSweepLiteral(t, root, "cmd/reducer/seed.go", create)

	findings, candidates := sweepIndexedWrites(t, root, []string{"cmd"})
	got := map[string]bool{}
	for _, f := range findings {
		got[strings.SplitN(f.file, ":", 2)[0]] = true
	}
	for _, rel := range []string{"cmd/read-api-latency-gate/seed_graph.go", "cmd/golden-corpus-gate/seed.go", "cmd/ci-gates/seed.go"} {
		if got[rel] {
			t.Errorf("%s scanned, want skipped as a gate tool", rel)
		}
	}
	if !got["cmd/reducer/seed.go"] {
		t.Errorf("cmd/reducer/seed.go not flagged; findings = %v", findings)
	}
	if candidates != 1 {
		t.Errorf("candidates = %d, want 1 (only the service literal)", candidates)
	}
}
