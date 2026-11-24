// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The proof seams carry statement text to live proofs in other packages and are not for
// product use (#10880 PR-C1, arbiter ruling 13). A seam that writes (the support sync, in
// the storage package) lives in a _test.go file so no binary carries it; the text-only
// seams here may ship, so this census proves nothing in a non-test file calls one.

// proofSeamIdentifier matches a proof seam by name. It is the set of seams, not the
// prefix "Proof": ProofSignal in the capability catalog is unrelated.
var proofSeamIdentifier = regexp.MustCompile(`\b(ProofStoredFindings\w*|ProofRecovery\w*|ProofSyncSupports\w*|WinnersProof\w*)\b`)

// proofSeamDefinition is the one non-test file that may name them.
const proofSeamDefinition = "internal/reducer/supplychain/core/proof_queries.go"

// proofSeamCallers returns, sorted, every non-test source (keyed by path relative to the
// go module root) other than the definition that names a proof seam.
func proofSeamCallers(sources map[string][]byte) []string {
	var callers []string
	for path, body := range sources {
		if strings.HasSuffix(path, "_test.go") || path == proofSeamDefinition {
			continue
		}
		if proofSeamIdentifier.Match(body) {
			callers = append(callers, path)
		}
	}
	sort.Strings(callers)
	return callers
}

// TestProofSeamsHaveNoNonTestCallers fails when any non-test Go file of the module names a
// proof seam.
func TestProofSeamsHaveNoNonTestCallers(t *testing.T) {
	t.Parallel()

	root := filepath.Join("..", "..", "..", "..")
	sources := map[string][]byte{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case "node_modules", ".git", "vendor", "testdata":
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		sources[filepath.ToSlash(rel)] = body
		return nil
	})
	if err != nil {
		t.Fatalf("walk the module: %v", err)
	}
	if _, ok := sources[proofSeamDefinition]; !ok {
		t.Fatalf("the census did not find %s: the walk root is wrong", proofSeamDefinition)
	}
	if callers := proofSeamCallers(sources); len(callers) != 0 {
		t.Fatalf("non-test files name a proof seam: %v (a proof seam is for live proofs; a write-capable one belongs in a _test.go file)", callers)
	}
}

// TestProofSeamCensusFlagsASeededCaller is the seeded RED/GREEN pair: a product file that
// calls a seam is reported, the definition and test files are not, and an unrelated Proof
// identifier is not a seam.
func TestProofSeamCensusFlagsASeededCaller(t *testing.T) {
	t.Parallel()

	clean := map[string][]byte{
		proofSeamDefinition:                     []byte("func ProofStoredFindingsQuery() string { return q }"),
		"internal/storage/postgres/x_test.go":   []byte("postgres.ProofSyncSupportsFromFacts(ctx, db)"),
		"internal/capabilitycatalog/catalog.go": []byte("seen[ProofSignal(verification)] = struct{}{}"),
		"internal/storage/postgres/writer.go":   []byte("func write() {}"),
	}
	if got := proofSeamCallers(clean); len(got) != 0 {
		t.Fatalf("the clean tree reported callers %v", got)
	}
	for name, body := range map[string]string{
		"a core call":      "q := ProofStoredFindingsQuery()",
		"a recovery call":  "q := core.ProofRecoveryCVEIDsQuery()",
		"a sync call":      "postgres.ProofSyncSupportsFromFacts(ctx, db)",
		"a winners select": "a, b := WinnersProofSelects()",
	} {
		seeded := map[string][]byte{proofSeamDefinition: clean[proofSeamDefinition], "internal/reducer/x/handler.go": []byte(body)}
		if got := proofSeamCallers(seeded); len(got) != 1 || got[0] != "internal/reducer/x/handler.go" {
			t.Errorf("%s: callers = %v, want the seeded product file", name, got)
		}
	}
}
