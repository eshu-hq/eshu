// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package capabilitycatalog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func productClaimCatalog() Catalog {
	return Catalog{Entries: []Entry{
		{
			Capability:   "capability_catalog.list",
			OwnerPackage: "internal/capabilitycatalog",
			Maturity:     MaturityGeneralAvailability,
			Surfaces: []Surface{
				{Tool: "get_capability_catalog", Kind: SurfaceMCP},
			},
			Profiles: map[string]EntryProfile{
				string(ProfileProduction): {Status: statusSupported, MaxTruthLevel: string(ProductClaimTruthExact)},
			},
			ProofSignals: []ProofSignal{{Kind: "go_test", Ref: "cmd/capability-inventory"}},
		},
		{
			Capability:   "platform_impact.cloud_resource_list",
			OwnerPackage: "internal/query",
			Maturity:     MaturityGated,
			Surfaces: []Surface{
				{Tool: "list_cloud_resources", Kind: SurfaceMCP},
			},
			Profiles: map[string]EntryProfile{
				string(ProfileProduction): {Status: statusSupported, MaxTruthLevel: string(ProductClaimTruthDerived)},
			},
			ProofSignals: []ProofSignal{{Kind: "integration_test", Ref: "cloud-resource-list"}},
			LinkedIssues: []int{2700},
		},
	}}
}

func productClaimSurfaceInventory() SurfaceInventory {
	return SurfaceInventory{Surfaces: []SurfaceRecord{
		{Category: SurfaceAPIRoute, Name: "GET /api/v0/capabilities"},
		{Category: SurfaceAPIRoute, Name: "GET /api/v0/unrelated"},
		{Category: SurfaceConsolePage, Name: "CapabilityMatrixPage"},
		{Category: SurfaceConsolePage, Name: "UnrelatedPage"},
		{Category: SurfaceMCPTool, Name: "get_capability_catalog"},
		{Category: SurfaceMCPTool, Name: "list_cloud_resources"},
	}}
}

func validProductClaim() ProductClaim {
	return ProductClaim{
		ID: "docs.capability-catalog.summary",
		Source: ProductClaimSource{
			Path:  "docs/public/reference/capability-catalog.md",
			Line:  1,
			Quote: "The capability catalog is one auditable source.",
		},
		ClaimText: "The capability catalog is one auditable source.",
		Capabilities: []ProductClaimCapability{
			{ID: "capability_catalog.list", ClaimedMaturity: MaturityGeneralAvailability},
		},
		TruthLevel:                  "exact",
		OwnerPackages:               []string{"internal/capabilitycatalog"},
		ImplementationPaths:         []string{"go/internal/capabilitycatalog"},
		MCPSurfaces:                 []string{"get_capability_catalog"},
		DeterministicEvidenceSource: "capability catalog generated artifact and verify gate",
		SemanticOutput:              SemanticOutputNotUsed,
		Proof: ProductClaimProof{
			Command:  "cd go && go run ./cmd/capability-inventory -mode verify",
			Artifact: "go/internal/capabilitycatalog/data/catalog.generated.json",
			Counts: []ProductClaimSurfaceCount{
				{Category: SurfaceMCPTool, Count: 2},
			},
			Signals: []ProductClaimProofSignal{
				{Capability: "capability_catalog.list", Kind: "go_test", Ref: "cmd/capability-inventory"},
			},
		},
	}
}

func TestLoadProductClaimLedger(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ledgerPath := filepath.Join(dir, "product-claims.v1.yaml")
	writeFile(t, ledgerPath, `version: v1
claims:
  - id: docs.capability-catalog.summary
    source:
      path: docs/public/reference/capability-catalog.md
      line: 1
      quote: The capability catalog is one auditable source.
    claim_text: The capability catalog is one auditable source.
    capabilities:
      - id: capability_catalog.list
        claimed_maturity: general_availability
    truth_level: exact
    owner_packages: [internal/capabilitycatalog]
    implementation_paths: [go/internal/capabilitycatalog]
    mcp_surfaces: [get_capability_catalog]
    deterministic_evidence_source: capability catalog generated artifact and verify gate
    semantic_output: not_used
    proof:
      command: cd go && go run ./cmd/capability-inventory -mode verify
      artifact: go/internal/capabilitycatalog/data/catalog.generated.json
      surface_counts:
        - category: mcp_tool
          count: 2
      signals:
        - capability: capability_catalog.list
          kind: go_test
          ref: cmd/capability-inventory
`)

	ledger, err := LoadProductClaimLedger(ledgerPath)
	if err != nil {
		t.Fatalf("LoadProductClaimLedger: %v", err)
	}
	if ledger.Version != "v1" || len(ledger.Claims) != 1 {
		t.Fatalf("ledger = %+v, want one v1 claim", ledger)
	}
	if got := ledger.Claims[0].Capabilities[0].ClaimedMaturity; got != MaturityGeneralAvailability {
		t.Fatalf("claimed maturity = %q", got)
	}
	if len(ledger.Claims[0].Proof.Signals) != 1 {
		t.Fatalf("proof signals = %+v, want one signal", ledger.Claims[0].Proof.Signals)
	}
	if len(ledger.Claims[0].Proof.Counts) != 1 || ledger.Claims[0].Proof.Counts[0].Category != SurfaceMCPTool {
		t.Fatalf("surface counts = %+v, want one MCP count", ledger.Claims[0].Proof.Counts)
	}
}

func TestCheckProductClaims(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "docs/public/reference/capability-catalog.md"),
		"The capability catalog is one auditable source.\nThe capability catalog is one auditable source. Extra claim.\n")
	writeFile(t, filepath.Join(root, "go/internal/capabilitycatalog/README.md"),
		"# capabilitycatalog\n")
	writeFile(t, filepath.Join(root, "go/internal/capabilitycatalog/data/catalog.generated.json"),
		"{}\n")
	writeFile(t, filepath.Join(root, "go/internal/query/README.md"),
		"# query\n")

	valid := validProductClaim()
	unknownCapability := valid
	unknownCapability.ID = "bad.unknown-capability"
	unknownCapability.Capabilities = []ProductClaimCapability{
		{ID: "missing.capability", ClaimedMaturity: MaturityGeneralAvailability},
	}

	staleMaturity := valid
	staleMaturity.ID = "bad.stale-maturity"
	staleMaturity.Capabilities = []ProductClaimCapability{
		{ID: "platform_impact.cloud_resource_list", ClaimedMaturity: MaturityGeneralAvailability},
	}
	staleMaturity.OwnerPackages = []string{"internal/query"}
	staleMaturity.MCPSurfaces = []string{"list_cloud_resources"}
	staleMaturity.Issues = []ProductClaimIssue{{Number: 2700, State: IssueStateOpen}}

	missingProof := valid
	missingProof.ID = "bad.missing-proof"
	missingProof.Proof = ProductClaimProof{}

	requiredSemantic := valid
	requiredSemantic.ID = "bad.required-semantic"
	requiredSemantic.SemanticOutput = SemanticOutputRequired

	missingIssue := valid
	missingIssue.ID = "bad.missing-issue"
	missingIssue.Capabilities = []ProductClaimCapability{
		{ID: "platform_impact.cloud_resource_list", ClaimedMaturity: MaturityGated},
	}
	missingIssue.OwnerPackages = []string{"internal/query"}
	missingIssue.MCPSurfaces = []string{"list_cloud_resources"}

	missingSurface := valid
	missingSurface.ID = "bad.missing-surface"
	missingSurface.MCPSurfaces = nil

	badAPISurface := valid
	badAPISurface.ID = "bad.api-surface"
	badAPISurface.APISurfaces = []string{"GET /not-real"}

	badConsoleSurface := valid
	badConsoleSurface.ID = "bad.console-surface"
	badConsoleSurface.ConsoleSurfaces = []string{"Capabilities page"}

	unlinkedAPISurface := valid
	unlinkedAPISurface.ID = "bad.unlinked-api-surface"
	unlinkedAPISurface.APISurfaces = []string{"GET /api/v0/unrelated"}

	unlinkedConsoleSurface := valid
	unlinkedConsoleSurface.ID = "bad.unlinked-console-surface"
	unlinkedConsoleSurface.ConsoleSurfaces = []string{"UnrelatedPage"}

	badRepoPath := valid
	badRepoPath.ID = "bad.path-escape"
	badRepoPath.ImplementationPaths = []string{"../outside"}

	badOwnerPath := valid
	badOwnerPath.ID = "bad.owner-escape"
	badOwnerPath.OwnerPackages = []string{"../docs"}

	missingSignal := valid
	missingSignal.ID = "bad.missing-signal"
	missingSignal.Proof.Signals = nil

	badSurfaceCount := valid
	badSurfaceCount.ID = "bad.surface-count"
	badSurfaceCount.Proof.Counts = []ProductClaimSurfaceCount{{Category: SurfaceMCPTool, Count: 149}}

	wrongSourceLine := valid
	wrongSourceLine.ID = "bad.source-line"
	wrongSourceLine.Source.Line = 99

	wrongSourceText := valid
	wrongSourceText.ID = "bad.source-text"
	wrongSourceText.Source.Line = 2
	wrongSourceText.Source.Quote = "The capability catalog is one auditable source."

	findings := CheckProductClaims(root, productClaimCatalog(), productClaimSurfaceInventory(), ProductClaimLedger{
		Version: "v1",
		Claims: []ProductClaim{
			valid,
			unknownCapability,
			staleMaturity,
			missingProof,
			requiredSemantic,
			missingIssue,
			missingSurface,
			badAPISurface,
			badConsoleSurface,
			unlinkedAPISurface,
			unlinkedConsoleSurface,
			badRepoPath,
			badOwnerPath,
			missingSignal,
			badSurfaceCount,
			wrongSourceLine,
			wrongSourceText,
		},
	})
	if len(findings) == 0 {
		t.Fatalf("findings = 0, want validation failures")
	}
	wantKinds := map[ProductClaimFindingKind]bool{
		ProductClaimFindingUnknownCapability: true,
		ProductClaimFindingStaleMaturity:     true,
		ProductClaimFindingMissingProof:      true,
		ProductClaimFindingSemanticRequired:  true,
		ProductClaimFindingMissingIssue:      true,
		ProductClaimFindingMissingSurface:    true,
		ProductClaimFindingSourceMismatch:    true,
		ProductClaimFindingMissingPath:       true,
		ProductClaimFindingMissingOwner:      true,
	}
	for _, finding := range findings {
		delete(wantKinds, finding.Kind)
	}
	if len(wantKinds) != 0 {
		t.Fatalf("missing finding kinds: %+v", wantKinds)
	}
}

func TestCheckProductClaimsRejectsInvalidTruthLevel(t *testing.T) {
	t.Parallel()

	root := productClaimTestRoot(t)
	claim := validProductClaim()
	claim.TruthLevel = "excat"

	findings := CheckProductClaims(root, productClaimCatalog(), productClaimSurfaceInventory(), ProductClaimLedger{
		Version: "v1",
		Claims:  []ProductClaim{claim},
	})
	if !hasProductClaimFinding(findings, ProductClaimFindingMalformed, claim.ID) {
		t.Fatalf("findings = %+v, want malformed truth_level finding", findings)
	}
}

func TestCheckProductClaimsRejectsOverstatedTruthLevel(t *testing.T) {
	t.Parallel()

	root := productClaimTestRoot(t)
	claim := validProductClaim()
	claim.Capabilities = []ProductClaimCapability{
		{ID: "platform_impact.cloud_resource_list", ClaimedMaturity: MaturityGated},
	}
	claim.OwnerPackages = []string{"internal/query"}
	claim.MCPSurfaces = []string{"list_cloud_resources"}
	claim.Issues = []ProductClaimIssue{{Number: 2700, State: IssueStateOpen}}
	claim.TruthLevel = "exact"
	claim.Proof.Signals = []ProductClaimProofSignal{
		{Capability: "platform_impact.cloud_resource_list", Kind: "integration_test", Ref: "cloud-resource-list"},
	}

	findings := CheckProductClaims(root, productClaimCatalog(), productClaimSurfaceInventory(), ProductClaimLedger{
		Version: "v1",
		Claims:  []ProductClaim{claim},
	})
	if !hasProductClaimFinding(findings, ProductClaimFindingStaleTruthLevel, claim.ID) {
		t.Fatalf("findings = %+v, want stale truth_level finding", findings)
	}
}

func productClaimTestRoot(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "docs/public/reference/capability-catalog.md"),
		"The capability catalog is one auditable source.\n")
	writeFile(t, filepath.Join(root, "go/internal/capabilitycatalog/README.md"),
		"# capabilitycatalog\n")
	writeFile(t, filepath.Join(root, "go/internal/capabilitycatalog/data/catalog.generated.json"),
		"{}\n")
	writeFile(t, filepath.Join(root, "go/internal/query/README.md"),
		"# query\n")
	return root
}

func hasProductClaimFinding(findings []ProductClaimFinding, kind ProductClaimFindingKind, id string) bool {
	for _, finding := range findings {
		if finding.Kind == kind && finding.ID == id {
			return true
		}
	}
	return false
}

func TestCheckProductClaimMarkers(t *testing.T) {
	t.Parallel()

	ledger := ProductClaimLedger{Claims: []ProductClaim{
		{ID: "ok.claim", Source: ProductClaimSource{Path: "README.md", Line: 1}},
		{ID: "missing.marker", Source: ProductClaimSource{Path: "README.md", Line: 2}},
		{ID: "wrong.line", Source: ProductClaimSource{Path: "README.md", Line: 6}},
	}}
	markers := []ProductClaimMarker{
		{Path: "README.md", Line: 1, ID: "ok.claim"},
		{Path: "README.md", Line: 3, ID: "missing.ledger"},
		{Path: "README.md", Line: 4, ID: "unguarded.claim", Unguarded: true},
		{Path: "README.md", Line: 5, Raw: "<!-- product-claim: state=guarded -->", Malformed: true},
		{Path: "README.md", Line: 7, ID: "wrong.line"},
	}

	findings := CheckProductClaimMarkers(ledger, markers)
	if len(findings) != 4 {
		t.Fatalf("findings = %d, want 4: %+v", len(findings), findings)
	}
	want := map[ProductClaimFindingKind]bool{
		ProductClaimFindingMissingMarker: true,
		ProductClaimFindingMalformed:     true,
	}
	for _, finding := range findings {
		delete(want, finding.Kind)
	}
	if len(want) != 0 {
		t.Fatalf("missing finding kinds: %+v", want)
	}
}

func TestParseProductClaimMarkers(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "README.md"),
		"claimed <!-- product-claim: id=readme.claim -->\n")
	writeFile(t, filepath.Join(root, "docs/public/reference/example.md"),
		"not guarded <!-- product-claim: id=docs.claim state=unguarded -->\nunterminated <!-- product-claim: id=broken.claim\n")

	markers, err := ParseProductClaimMarkers(root, filepath.Join(root, "docs/public"))
	if err != nil {
		t.Fatalf("ParseProductClaimMarkers: %v", err)
	}
	if len(markers) != 3 {
		t.Fatalf("markers = %+v, want 3", markers)
	}
	if markers[0].ID != "readme.claim" || markers[0].Path != "README.md" {
		t.Fatalf("first marker = %+v", markers[0])
	}
	if markers[1].ID != "docs.claim" || !markers[1].Unguarded {
		t.Fatalf("second marker = %+v", markers[1])
	}
	if markers[2].ID != "broken.claim" || !markers[2].Malformed {
		t.Fatalf("third marker = %+v, want malformed unterminated marker", markers[2])
	}
}

func TestParseProductClaimMarkersHandlesAbsoluteRootWithRelativeDocs(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go", ".keep"), "")
	writeFile(t, filepath.Join(root, "README.md"),
		"claimed <!-- product-claim: id=readme.claim -->\n")
	writeFile(t, filepath.Join(root, "docs/public/reference/example.md"),
		"claimed <!-- product-claim: id=docs.claim -->\n")
	t.Chdir(filepath.Join(root, "go"))

	markers, err := ParseProductClaimMarkers(root, filepath.Join("..", "docs", "public"))
	if err != nil {
		t.Fatalf("ParseProductClaimMarkers: %v", err)
	}
	if len(markers) != 2 {
		t.Fatalf("markers = %+v, want 2", markers)
	}
	if markers[0].Path != "README.md" || markers[1].Path != "docs/public/reference/example.md" {
		t.Fatalf("marker paths = %+v, want repo-relative paths", markers)
	}
}

func TestCheckProductClaimIssueStates(t *testing.T) {
	t.Parallel()

	var requests int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		if auth := r.Header.Get("Authorization"); auth != "" {
			t.Errorf("Authorization header leaked to custom issue API: %s", auth)
		}
		if r.URL.Path != "/repos/eshu-hq/eshu/issues/2700" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"state":"closed"}`))
	}))
	t.Cleanup(server.Close)

	ledger := ProductClaimLedger{
		Version: "v1",
		Claims: []ProductClaim{
			{
				ID:     "ok.closed-issue",
				Source: ProductClaimSource{Path: "README.md", Line: 1},
				Issues: []ProductClaimIssue{
					{Number: 2700, State: IssueStateClosed},
				},
			},
			{
				ID:     "bad.stale-issue",
				Source: ProductClaimSource{Path: "README.md", Line: 2},
				Issues: []ProductClaimIssue{
					{Number: 2700, State: IssueStateOpen},
				},
			},
		},
	}

	findings := CheckProductClaimIssueStates(context.Background(), server.Client(), server.URL, "eshu-hq/eshu", "token", ledger)
	if len(findings) != 1 {
		t.Fatalf("findings = %d, want 1: %+v", len(findings), findings)
	}
	if findings[0].ID != "bad.stale-issue" || findings[0].Kind != ProductClaimFindingStaleIssue {
		t.Fatalf("finding = %+v, want stale issue for bad.stale-issue", findings[0])
	}
	if got := atomic.LoadInt32(&requests); got != 1 {
		t.Fatalf("requests = %d, want cached single request", got)
	}
}

func TestCheckProductClaimIssueStatesCachesErrors(t *testing.T) {
	t.Parallel()

	var requests int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)

	ledger := ProductClaimLedger{Claims: []ProductClaim{
		{ID: "first", Source: ProductClaimSource{Path: "README.md", Line: 1}, Issues: []ProductClaimIssue{{Number: 2700, State: IssueStateClosed}}},
		{ID: "second", Source: ProductClaimSource{Path: "README.md", Line: 2}, Issues: []ProductClaimIssue{{Number: 2700, State: IssueStateClosed}}},
	}}

	findings := CheckProductClaimIssueStates(context.Background(), server.Client(), server.URL, "eshu-hq/eshu", "token", ledger)
	if len(findings) != 2 {
		t.Fatalf("findings = %d, want two cached-error findings: %+v", len(findings), findings)
	}
	if got := atomic.LoadInt32(&requests); got != 1 {
		t.Fatalf("requests = %d, want cached single request", got)
	}
	for _, finding := range findings {
		if !strings.Contains(finding.Detail, "status 503") {
			t.Fatalf("finding detail = %q, want status 503", finding.Detail)
		}
	}
}
