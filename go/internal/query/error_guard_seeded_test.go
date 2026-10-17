// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// queryGuardSeedImportRoot is the import path the planted tree stands for.
const queryGuardSeedImportRoot = "example.test/q"

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
	// A renamed wrapper chain: clean forwarders, and a call through them
	// with a client status. These stay for the GREEN half.
	"chain/writers.go": `package chain

func WriteChainJSON(w http.ResponseWriter, status int, value any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeChainError(w http.ResponseWriter, status int, message string) {
	WriteChainJSON(w, status, map[string]any{"detail": message})
}

func rejectChain(w http.ResponseWriter) {
	writeChainError(w, http.StatusBadRequest, "bad request")
}
`,
	// Violates B: a helper that fixes a 500 through the chain.
	"chain/internal.go": `package chain

func writeChainInternal(w http.ResponseWriter) {
	writeChainError(w, http.StatusInternalServerError, "failed")
}
`,
	// Violates A and B through the chain, and B through the fixed helper.
	"chain/handler.go": `package chain

func read(w http.ResponseWriter, err error) {
	writeChainError(w, http.StatusInternalServerError, err.Error())
}

func list(w http.ResponseWriter) {
	writeChainInternal(w)
}
`,
	// Violates B through an exported wrapper of another package.
	"caller/caller.go": `package caller

import "example.test/q/chain"

func read(w http.ResponseWriter) {
	chain.WriteChainJSON(w, http.StatusBadGateway, nil)
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
// (queryErrorTextLeakFindings) names exactly chain/handler.go and
// leak/leak.go, and assertion B (queryDirectServerStatusFindings) names
// exactly the planted violations, the renamed wrapper chain, the fixed-500
// helper, and the cross-package wrapper call included; the clean forwarders,
// the tracing file, the test file, testdata, and the dot directory stay out.
// GREEN: with the violating files removed, both assertions find nothing.
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

	if got, want := queryGuardFindingFiles(queryErrorTextLeakFindings(loadQueryGuardTree(t, root, queryGuardSeedImportRoot))), []string{"chain/handler.go", "leak/leak.go"}; !slices.Equal(got, want) {
		t.Errorf("RED: assertion A findings = %v, want %v", got, want)
	}
	if got, want := queryGuardFindingFiles(queryDirectServerStatusFindings(loadQueryGuardTree(t, root, queryGuardSeedImportRoot))), []string{
		"caller/caller.go", "chain/handler.go", "chain/internal.go", "direct.go", "leak/leak.go",
	}; !slices.Equal(got, want) {
		t.Errorf("RED: assertion B findings = %v, want %v", got, want)
	}

	for _, rel := range []string{"leak/leak.go", "direct.go", "chain/internal.go", "chain/handler.go", "caller/caller.go"} {
		if err := os.Remove(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
			t.Fatalf("remove %s: %v", rel, err)
		}
	}
	if got := queryGuardFindingFiles(queryErrorTextLeakFindings(loadQueryGuardTree(t, root, queryGuardSeedImportRoot))); len(got) != 0 {
		t.Errorf("GREEN: assertion A findings = %v, want none", got)
	}
	if got := queryGuardFindingFiles(queryDirectServerStatusFindings(loadQueryGuardTree(t, root, queryGuardSeedImportRoot))); len(got) != 0 {
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
