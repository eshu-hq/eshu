// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// queryReceiverShapePrelude declares two types whose methods share the name
// list: A.list writes a fixed 500, B.list forwards its status parameter. The
// guard once keyed writers by package and name only, so B.list inherited
// A.list's 500 and B.list's callers were flagged (SurfaceInventoryHandler.list
// against SemanticEvidenceHandler.list).
const queryReceiverShapePrelude = `package p

import "net/http"

type A struct{}

type B struct{}

type H struct {
	a *A
	b *B
}

func (a *A) list(w http.ResponseWriter) {
	WriteError(w, http.StatusInternalServerError, "failed")
}

func (b *B) list(w http.ResponseWriter, status int) {
	WriteError(w, status, "failed")
}
`

// TestQueryWriterReceiverShapes pins that writers are keyed by receiver type.
// wantB counts every assertion B site in the file, A.list's own 500 included;
// wantNote is whether a site reports an unresolved receiver.
func TestQueryWriterReceiverShapes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		decls    string
		wantB    int
		wantNote bool
	}{
		{"clean_method_caller_not_flagged", `func (b *B) get(w http.ResponseWriter) { b.list(w, http.StatusBadRequest) }`, 1, false},
		{"dirty_method_caller_flagged", `func (a *A) get(w http.ResponseWriter) { a.list(w) }`, 2, false},
		{"clean_method_through_field", `func (h *H) get(w http.ResponseWriter) { h.b.list(w, http.StatusNotFound) }`, 1, false},
		{"dirty_method_through_field", `func (h *H) get(w http.ResponseWriter) { h.a.list(w) }`, 2, false},
		{"clean_method_on_param", `func get(w http.ResponseWriter, b *B) { b.list(w, http.StatusConflict) }`, 1, false},
		{"clean_method_on_literal_local", `func get(w http.ResponseWriter) { b := &B{}; b.list(w, http.StatusConflict) }`, 1, false},
		{"unresolved_receiver_union", `func get(w http.ResponseWriter) { x := lookup(); x.list(w) }`, 2, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src := queryReceiverShapePrelude + "\n" + tc.decls + "\n"
			tree, file := queryGuardTreeFromSource(t, tc.name+".go", src)
			sites := queryDirectServerStatusSites(tree, file)
			report := formatQueryErrorTextLeakSites(sites)
			if len(sites) != tc.wantB {
				t.Errorf("assertion B found %d sites, want %d:\n%s", len(sites), tc.wantB, report)
			}
			if got := strings.Contains(report, "receiver type unresolved; matched A.list, B.list"); got != tc.wantNote {
				t.Errorf("unresolved-receiver note present = %t, want %t:\n%s", got, tc.wantNote, report)
			}
		})
	}
}

// TestQueryWriterPlainFunctionsArePackageScoped pins that a plain function
// resolves in its own package: a.writeFailure writes a fixed 500, and the
// same-named b.writeFailure forwards its status, so only package a is named.
func TestQueryWriterPlainFunctionsArePackageScoped(t *testing.T) {
	t.Parallel()

	files := map[string]string{
		"a/a.go": `package a

func writeFailure(w http.ResponseWriter) {
	WriteError(w, http.StatusInternalServerError, "failed")
}

func read(w http.ResponseWriter) {
	writeFailure(w)
}
`,
		"b/b.go": `package b

func writeFailure(w http.ResponseWriter, status int) {
	WriteError(w, status, "failed")
}

func read(w http.ResponseWriter) {
	writeFailure(w, http.StatusBadRequest)
}
`,
	}
	root := t.TempDir()
	for rel, src := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", rel, err)
		}
		if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	findings := queryDirectServerStatusFindings(loadQueryGuardTree(t, root, queryGuardSeedImportRoot))
	if got := queryGuardFindingFiles(findings); !slices.Equal(got, []string{"a/a.go"}) {
		t.Fatalf("assertion B findings = %v, want [a/a.go]", got)
	}
	if got := len(findings[0].sites); got != 2 {
		t.Fatalf("a/a.go sites = %d, want 2 (the 500 and its caller):\n%s", got, formatQueryErrorTextLeakSites(findings[0].sites))
	}
}
