// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/capabilitycatalog"
)

// repoDocsDir resolves the repository docs/public directory from this test file.
func repoDocsDir(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", "..", "docs", "public"))
}

// TestDocsFreshnessAgainstRealDocs is the docs freshness drift gate: every
// capability-state marker in docs must agree with the catalog.
func TestDocsFreshnessAgainstRealDocs(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	err := run([]string{"-mode", "docs", "-specs", repoSpecsDir(t), "-docs", repoDocsDir(t), "-root", repoRootDir(t)}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("docs freshness failed: %v\nstdout:\n%s", err, stdout.String())
	}
	if !strings.Contains(stdout.String(), "no freshness findings") {
		t.Fatalf("docs freshness output unexpected:\n%s", stdout.String())
	}
}

func TestCheckDocsIncludesProductClaimLedger(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	docsDir := filepath.Join(root, "docs", "public")
	specsDir := filepath.Join(root, "specs")
	writeTestFile(t, filepath.Join(docsDir, "reference", "capability-catalog.md"),
		"The capability catalog is one auditable source.\n")
	writeTestFile(t, filepath.Join(specsDir, "product-claims.v1.yaml"), "version: v1\nclaims:\n  - id: docs.bad-capability\n    source:\n      path: docs/public/reference/capability-catalog.md\n      line: 1\n      quote: The capability catalog is one auditable source.\n    claim_text: The capability catalog is one auditable source.\n    capabilities:\n      - id: missing.capability\n        claimed_maturity: general_availability\n    truth_level: exact\n    owner_packages: [internal/capabilitycatalog]\n    implementation_paths: [go/internal/capabilitycatalog]\n    mcp_surfaces: [get_capability_catalog]\n    deterministic_evidence_source: capability catalog generated artifact and verify gate\n    semantic_output: not_used\n    proof:\n      command: cd go && go run ./cmd/capability-inventory -mode verify\n")

	var stdout bytes.Buffer
	err := checkDocs(&stdout, capabilitycatalog.Catalog{}, docsDir, filepath.Join(specsDir, "product-claims.v1.yaml"), root)
	if err == nil {
		t.Fatal("checkDocs error = nil, want product claim ledger failure")
	}
	if !strings.Contains(stdout.String(), "product claim ledger findings") {
		t.Fatalf("checkDocs output missing ledger findings:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "missing.capability") {
		t.Fatalf("checkDocs output missing product claim detail:\n%s", stdout.String())
	}
}

// TestProductClaimsModeCatchesSameBadClaimAsDocsMode is the consolidation
// regression guard for #4073: the narrower `-mode product-claims` gate must
// still fail closed on the exact bad-claim fixture that `-mode docs` (via
// checkDocs) already catches. It reuses the fixture from
// TestCheckDocsIncludesProductClaimLedger (a ledger row naming a capability
// unknown to the catalog) so the two modes are asserted against the same
// input, proving the narrower mode does not weaken the product-claim ledger
// guard even though it skips the capability-state and collector-state marker
// scans that remain exclusive to `-mode docs`.
func TestProductClaimsModeCatchesSameBadClaimAsDocsMode(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	docsDir := filepath.Join(root, "docs", "public")
	specsDir := filepath.Join(root, "specs")
	writeTestFile(t, filepath.Join(docsDir, "reference", "capability-catalog.md"),
		"The capability catalog is one auditable source.\n")
	ledgerPath := filepath.Join(specsDir, "product-claims.v1.yaml")
	writeTestFile(t, ledgerPath, "version: v1\nclaims:\n  - id: docs.bad-capability\n    source:\n      path: docs/public/reference/capability-catalog.md\n      line: 1\n      quote: The capability catalog is one auditable source.\n    claim_text: The capability catalog is one auditable source.\n    capabilities:\n      - id: missing.capability\n        claimed_maturity: general_availability\n    truth_level: exact\n    owner_packages: [internal/capabilitycatalog]\n    implementation_paths: [go/internal/capabilitycatalog]\n    mcp_surfaces: [get_capability_catalog]\n    deterministic_evidence_source: capability catalog generated artifact and verify gate\n    semantic_output: not_used\n    proof:\n      command: cd go && go run ./cmd/capability-inventory -mode verify\n")

	// Baseline: -mode docs (checkDocs) fails closed on this fixture today.
	var docsStdout bytes.Buffer
	docsErr := checkDocs(&docsStdout, capabilitycatalog.Catalog{}, docsDir, ledgerPath, root)
	if docsErr == nil {
		t.Fatal("checkDocs error = nil, want product claim ledger failure (baseline)")
	}
	if !strings.Contains(docsStdout.String(), "missing.capability") {
		t.Fatalf("checkDocs baseline output missing product claim detail:\n%s", docsStdout.String())
	}

	// Consolidated: -mode product-claims (checkProductClaimsMode) must fail
	// closed on the identical fixture.
	var narrowStdout bytes.Buffer
	narrowErr := checkProductClaimsMode(&narrowStdout, capabilitycatalog.Catalog{}, docsDir, ledgerPath, root)
	if narrowErr == nil {
		t.Fatal("checkProductClaimsMode error = nil, want product claim ledger failure")
	}
	if !strings.Contains(narrowStdout.String(), "missing.capability") {
		t.Fatalf("checkProductClaimsMode output missing product claim detail:\n%s", narrowStdout.String())
	}
	if !strings.Contains(narrowStdout.String(), "product claim ledger findings") {
		t.Fatalf("checkProductClaimsMode output missing ledger findings summary:\n%s", narrowStdout.String())
	}
}

// TestProductClaimsModeCLI exercises the `-mode product-claims` flag end to
// end against the real repository docs and specs, mirroring
// TestDocsFreshnessAgainstRealDocs. It is the CLI wiring proof for the new
// mode the product-claim-ledger CI workflow now uses in place of full
// `-mode docs`.
func TestProductClaimsModeCLI(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	err := run([]string{"-mode", "product-claims", "-specs", repoSpecsDir(t), "-docs", repoDocsDir(t), "-root", repoRootDir(t)}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("product-claims mode failed: %v\nstdout:\n%s", err, stdout.String())
	}
	if !strings.Contains(stdout.String(), "no ledger findings") {
		t.Fatalf("product-claims mode output unexpected:\n%s", stdout.String())
	}
}

func TestCheckDocsScansProductClaimMarkersFromDocsFlag(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	docsDir := filepath.Join(root, "docs-copy", "public")
	specsDir := filepath.Join(root, "specs")
	writeTestFile(t, filepath.Join(root, "docs", "public", ".keep"), "")
	writeTestFile(t, filepath.Join(root, "go", "internal", "capabilitycatalog", "doc.go"), "package capabilitycatalog\n")
	writeTestFile(t, filepath.Join(docsDir, "reference", "capability-catalog.md"),
		"The copied docs tree carries the guarded claim. <!-- product-claim: id=docs.copy.claim -->\n")
	writeTestFile(t, filepath.Join(specsDir, "product-claims.v1.yaml"), `version: v1
claims:
  - id: docs.copy.claim
    source:
      path: docs-copy/public/reference/capability-catalog.md
      line: 1
      quote: "The copied docs tree carries the guarded claim. <!-- product-claim: id=docs.copy.claim -->"
    claim_text: "The copied docs tree carries the guarded claim."
    capabilities:
      - id: capability_catalog.list
        claimed_maturity: general_availability
    truth_level: exact
    owner_packages: [internal/capabilitycatalog]
    implementation_paths: [go/internal/capabilitycatalog]
    mcp_surfaces: [get_capability_catalog]
    deterministic_evidence_source: "capability catalog generated artifact and verify gate"
    semantic_output: not_used
    proof:
      command: "cd go && go run ./cmd/capability-inventory -mode verify"
      signals:
        - capability: capability_catalog.list
          kind: go_test
          ref: ./internal/query
`)
	catalog := capabilitycatalog.Catalog{Entries: []capabilitycatalog.Entry{
		{
			Capability:      "capability_catalog.list",
			OwnerPackage:    "internal/capabilitycatalog",
			Maturity:        capabilitycatalog.MaturityGeneralAvailability,
			DerivedMaturity: capabilitycatalog.MaturityGeneralAvailability,
			Surfaces: []capabilitycatalog.Surface{
				{Tool: "get_capability_catalog", Kind: capabilitycatalog.SurfaceMCP},
			},
			Profiles: map[string]capabilitycatalog.EntryProfile{
				string(capabilitycatalog.ProfileProduction): {Status: "supported", MaxTruthLevel: string(capabilitycatalog.ProductClaimTruthExact)},
			},
			ProofSignals: []capabilitycatalog.ProofSignal{
				{Kind: "go_test", Ref: "./internal/query"},
			},
		},
	}}

	var stdout bytes.Buffer
	err := checkDocs(&stdout, catalog, docsDir, filepath.Join(specsDir, "product-claims.v1.yaml"), root)
	if err != nil {
		t.Fatalf("checkDocs failed for copied docs tree: %v\nstdout:\n%s", err, stdout.String())
	}
}
