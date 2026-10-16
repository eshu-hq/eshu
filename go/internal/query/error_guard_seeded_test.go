// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// queryGuardSeedFiles is a planted tree for the #7674 guard assertions. Each
// file is real Go source the walkers parse; none of it is compiled.
var queryGuardSeedFiles = map[string]string{
	// Clean in both assertions: the converted shape and a pure forwarder.
	"clean.go": `package p

func read(w http.ResponseWriter, r *http.Request, err error) {
	if WriteGraphReadError(w, r, err, capability) {
		return
	}
	tracing.WriteServerFailure(w, r, err, http.StatusInternalServerError, readFailedMessage)
}

func forward(w http.ResponseWriter, status int, message string) {
	WriteError(w, status, message)
}
`,
	// Violates A and B: a 500 whose body quotes the backend error.
	"leak/leak.go": `package leak

func read(w http.ResponseWriter, err error) {
	WriteError(w, http.StatusInternalServerError, err.Error())
}
`,
	// Violates B only: a fixed-text 502 that skips the span record and the 499.
	"direct.go": `package p

func read(w http.ResponseWriter) {
	http.Error(w, "upstream failed", http.StatusBadGateway)
}
`,
	// Exempt from B: the tracing package owns computed server statuses. Its
	// body is fixed text, so A is clean too.
	"tracing/server_failure.go": `package tracing

func WriteServerFailure(w http.ResponseWriter, status int, message string) {
	status = mark(status)
	querycontract.WriteError(w, status, message)
}
`,
	// Skipped by the walk: test files and testdata.
	"leak/leak_test.go":    "package leak\n\nfunc f() { WriteError(w, 500, err.Error()) }\n",
	"testdata/fixture.go":  "package fixture\n\nfunc f() { WriteError(w, 500, err.Error()) }\n",
	".hidden/generated.go": "package hidden\n\nfunc f() { WriteError(w, 500, err.Error()) }\n",
}

// TestQueryErrorGuardsSeeded is the seeded RED/GREEN pair for both #7674 guard
// assertions, run through the same finding functions the real guards call on
// go/internal/query. RED: on the planted tree, assertion A
// (queryErrorTextLeakFindings) names exactly leak/leak.go and assertion B
// (queryDirectServerStatusFindings) names exactly direct.go and leak/leak.go;
// the tracing file, test file, testdata, and dot directory stay out. GREEN:
// with the violating files removed, both assertions find nothing.
func TestQueryErrorGuardsSeeded(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	for rel, src := range queryGuardSeedFiles {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", rel, err)
		}
		if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	if got, want := queryGuardFindingFiles(queryErrorTextLeakFindings(t, root)), []string{"leak/leak.go"}; !slices.Equal(got, want) {
		t.Errorf("RED: assertion A findings = %v, want %v", got, want)
	}
	if got, want := queryGuardFindingFiles(queryDirectServerStatusFindings(t, root)), []string{"direct.go", "leak/leak.go"}; !slices.Equal(got, want) {
		t.Errorf("RED: assertion B findings = %v, want %v", got, want)
	}

	for _, rel := range []string{"leak/leak.go", "direct.go"} {
		if err := os.Remove(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
			t.Fatalf("remove %s: %v", rel, err)
		}
	}
	if got := queryGuardFindingFiles(queryErrorTextLeakFindings(t, root)); len(got) != 0 {
		t.Errorf("GREEN: assertion A findings = %v, want none", got)
	}
	if got := queryGuardFindingFiles(queryDirectServerStatusFindings(t, root)); len(got) != 0 {
		t.Errorf("GREEN: assertion B findings = %v, want none", got)
	}
}

// queryGuardFindingFiles returns the file of each finding, in walk order.
func queryGuardFindingFiles(findings []queryGuardFinding) []string {
	files := make([]string, 0, len(findings))
	for _, finding := range findings {
		files = append(files, finding.file)
	}
	return files
}
