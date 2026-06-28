// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/capabilitycatalog"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// repoRootDir resolves the repository root from this test file.
func repoRootDir(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", ".."))
}

// repoSpecsDir resolves the repository specs directory from this test file.
func repoSpecsDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(repoRootDir(t), "specs")
}

// TestVerifyAgainstRealSpecs is the drift gate: the committed, embedded catalog
// artifact must reconcile with zero findings and match a fresh regeneration from
// the real specs and live MCP registry.
func TestVerifyAgainstRealSpecs(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	err := run([]string{"-mode", "verify", "-specs", repoSpecsDir(t), "-root", repoRootDir(t)}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("verify failed: %v\nstdout:\n%s", err, stdout.String())
	}
	if !strings.Contains(stdout.String(), "capability catalog and surface inventory verified") {
		t.Fatalf("verify output missing confirmation:\n%s", stdout.String())
	}
}

// TestReportListsEntries exercises report mode.
func TestReportListsEntries(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	if err := run([]string{"-mode", "report", "-specs", repoSpecsDir(t), "-root", repoRootDir(t)}, &stdout, &stderr); err != nil {
		t.Fatalf("report failed: %v", err)
	}
	if !strings.Contains(stdout.String(), "no reconciliation findings") {
		t.Fatalf("report findings not clean:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "no surface reconciliation findings") {
		t.Fatalf("report surface findings not clean:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "catalog entries:") {
		t.Fatalf("report missing entry count:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "surface records:") {
		t.Fatalf("report missing surface count:\n%s", stdout.String())
	}
}

// TestUnsupportedMode rejects unknown modes.
func TestUnsupportedMode(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	if err := run([]string{"-mode", "bogus", "-specs", repoSpecsDir(t), "-root", repoRootDir(t)}, &stdout, &stderr); err == nil {
		t.Fatal("run() error = nil, want unsupported mode error")
	}
}

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

// cloneLiveSurfaces returns a deep copy of a live-surface set so a test can
// mutate one category without affecting the original.
func cloneLiveSurfaces(live capabilitycatalog.LiveSurfaces) capabilitycatalog.LiveSurfaces {
	out := capabilitycatalog.LiveSurfaces{
		Surfaces:           map[capabilitycatalog.SurfaceCategory][]string{},
		CollectorFactKinds: map[string][]string{},
	}
	for cat, names := range live.Surfaces {
		out.Surfaces[cat] = append([]string(nil), names...)
	}
	for name, factKinds := range live.CollectorFactKinds {
		out.CollectorFactKinds[name] = append([]string(nil), factKinds...)
	}
	return out
}

// TestSurfaceInventoryDriftAgainstRealCode is the surface drift gate: the
// committed surface inventory artifact must match a fresh build from live code
// and the real overlay, with zero reconciliation findings.
func TestSurfaceInventoryDriftAgainstRealCode(t *testing.T) {
	t.Parallel()
	inv, findings, err := buildSurfaceInventory(repoSpecsDir(t), repoRootDir(t))
	if err != nil {
		t.Fatalf("buildSurfaceInventory: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("surface findings present: %+v", findings)
	}
	payload, err := capabilitycatalog.MarshalSurfaceInventory(inv)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(payload, capabilitycatalog.RawSurfaceArtifact()) {
		t.Fatal("committed surface inventory artifact is stale; run: go run ./cmd/capability-inventory -mode generate")
	}
}

func TestCollectorFactKindsCoversFactEmittingCollectors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		collector scope.CollectorKind
		factKinds []string
	}{
		{collector: scope.CollectorSBOMAttestation, factKinds: facts.SBOMAttestationFactKinds()},
		{collector: scope.CollectorSecurityAlert, factKinds: facts.SecurityAlertFactKinds()},
		{collector: scope.CollectorCICDRun, factKinds: facts.CICDRunFactKinds()},
		{collector: scope.CollectorScannerWorker, factKinds: facts.ScannerWorkerFactKinds()},
	}

	got := collectorFactKinds()
	for _, tt := range tests {
		t.Run(string(tt.collector), func(t *testing.T) {
			t.Parallel()
			gotKinds := got[string(tt.collector)]
			if strings.Join(gotKinds, "\x00") != strings.Join(tt.factKinds, "\x00") {
				t.Fatalf("collectorFactKinds()[%q] = %#v, want %#v", tt.collector, gotKinds, tt.factKinds)
			}
		})
	}
}

func TestEnumerateConsolePagesIncludesOnlyRoutedPages(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	pagesDir := filepath.Join(root, "apps", "console", "src", "pages")
	if err := os.MkdirAll(pagesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(root, "apps", "console", "src", "App.tsx"), `
		import { lazy, Suspense } from "react";
		import { Route, Routes } from "react-router-dom";
		import { DashboardPage } from "./pages/DashboardPage";
		import { SurfaceInventoryPage } from "./pages/SurfaceInventoryPage";

		const WorkspacePage = lazy(() =>
			import("./pages/WorkspacePage").then((module) => ({ default: module.WorkspacePage }))
		);

		export function App(): React.JSX.Element {
			return (
				<Routes>
					<Route path="/dashboard" element={<DashboardPage />} />
					<Route path="/surface-inventory" element={<SurfaceInventoryPage />} />
					<Route
						path="/workspace/:entityKind/:entityId"
						element={
							<Suspense fallback={<h1>Loading workspace</h1>}>
								<WorkspacePage />
							</Suspense>
						}
					/>
				</Routes>
			);
		}
	`)
	writeTestFile(t, filepath.Join(pagesDir, "DashboardPage.tsx"), "export function DashboardPage(): React.JSX.Element { return <div />; }")
	writeTestFile(t, filepath.Join(pagesDir, "HomePage.tsx"), "export function HomePage(): React.JSX.Element { return <div />; }")
	writeTestFile(t, filepath.Join(pagesDir, "SurfaceInventoryPage.tsx"), "export function SurfaceInventoryPage(): React.JSX.Element { return <div />; }")
	writeTestFile(t, filepath.Join(pagesDir, "WorkspacePage.tsx"), "export function WorkspacePage(): React.JSX.Element { return <div />; }")

	pages, err := enumerateConsolePages(root)
	if err != nil {
		t.Fatalf("enumerateConsolePages: %v", err)
	}
	if got, want := strings.Join(pages, ","), "DashboardPage,SurfaceInventoryPage,WorkspacePage"; got != want {
		t.Fatalf("pages = %q, want %q", got, want)
	}
}

// TestEnumerateConsolePagesAcrossSplitRouter verifies that page imports and
// <Route> elements moved out of App.tsx into the extracted appRoutes.tsx table
// (done to honor the file-size limit) are still detected. Regression guard for
// the surface-inventory drift caused by splitting the console router.
func TestEnumerateConsolePagesAcrossSplitRouter(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	pagesDir := filepath.Join(root, "apps", "console", "src", "pages")
	if err := os.MkdirAll(pagesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// App.tsx keeps the login/shell page; the routes table lives in appRoutes.tsx.
	writeTestFile(t, filepath.Join(root, "apps", "console", "src", "App.tsx"), `
		import { LoginPage } from "./pages/LoginPage";
		export function App(): React.JSX.Element {
			return <Routes><Route path="/login" element={<LoginPage />} /></Routes>;
		}
	`)
	writeTestFile(t, filepath.Join(root, "apps", "console", "src", "appRoutes.tsx"), `
		import { DashboardPage } from "./pages/DashboardPage";
		import { SurfaceInventoryPage } from "./pages/SurfaceInventoryPage";
		export function AppRoutes(): React.JSX.Element {
			return (
				<Routes>
					<Route path="/dashboard" element={<DashboardPage />} />
					<Route path="/surface-inventory" element={<SurfaceInventoryPage />} />
				</Routes>
			);
		}
	`)
	writeTestFile(t, filepath.Join(pagesDir, "LoginPage.tsx"), "export function LoginPage(): React.JSX.Element { return <div />; }")
	writeTestFile(t, filepath.Join(pagesDir, "DashboardPage.tsx"), "export function DashboardPage(): React.JSX.Element { return <div />; }")
	writeTestFile(t, filepath.Join(pagesDir, "SurfaceInventoryPage.tsx"), "export function SurfaceInventoryPage(): React.JSX.Element { return <div />; }")

	pages, err := enumerateConsolePages(root)
	if err != nil {
		t.Fatalf("enumerateConsolePages: %v", err)
	}
	if got, want := strings.Join(pages, ","), "DashboardPage,LoginPage,SurfaceInventoryPage"; got != want {
		t.Fatalf("pages = %q, want %q", got, want)
	}
}

// TestSurfaceInventoryGateCatchesSilentDrift is the CI fixture required by
// #3145: a command/collector/API/MCP surface added or removed in code without
// regenerating the committed artifact must fail the gate. It exercises both the
// byte-freshness arm (a silently ADDED surface changes the artifact) and the
// reconciliation arm (a silently REMOVED surface leaves a stale overlay row).
func TestSurfaceInventoryGateCatchesSilentDrift(t *testing.T) {
	t.Parallel()
	root, specs := repoRootDir(t), repoSpecsDir(t)
	live, err := liveSurfaces(root)
	if err != nil {
		t.Fatalf("liveSurfaces: %v", err)
	}
	overlay, err := capabilitycatalog.LoadSurfaceOverlay(filepath.Join(specs, capabilitycatalog.SurfaceOverlayFileName))
	if err != nil {
		t.Fatalf("LoadSurfaceOverlay: %v", err)
	}

	// A silently added MCP tool must change the artifact away from committed.
	added := cloneLiveSurfaces(live)
	added.Surfaces[capabilitycatalog.SurfaceMCPTool] = append(added.Surfaces[capabilitycatalog.SurfaceMCPTool], "ghost_silently_added_tool")
	addedInv, _ := capabilitycatalog.BuildSurfaceInventory(added, overlay)
	addedPayload, err := capabilitycatalog.MarshalSurfaceInventory(addedInv)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(addedPayload, capabilitycatalog.RawSurfaceArtifact()) {
		t.Fatal("a silently added surface did not change the artifact: drift gate would miss it")
	}

	// A silently removed collector must leave a stale overlay finding. Drop a
	// collector that is known to have an overlay row so the assertion targets the
	// stale-overlay arm specifically (an unclassified-collector drop would be a
	// different, also-failing finding).
	const overlaidCollector = "aws"
	removed := cloneLiveSurfaces(live)
	collectors := removed.Surfaces[capabilitycatalog.SurfaceCollector]
	kept := make([]string, 0, len(collectors))
	found := false
	for _, name := range collectors {
		if name == overlaidCollector {
			found = true
			continue
		}
		kept = append(kept, name)
	}
	if !found {
		t.Fatalf("collector %q not in live set; update this test constant", overlaidCollector)
	}
	removed.Surfaces[capabilitycatalog.SurfaceCollector] = kept
	_, removedFindings := capabilitycatalog.BuildSurfaceInventory(removed, overlay)
	if !hasSurfaceFinding(removedFindings, capabilitycatalog.FindingStaleSurfaceOverlay, overlaidCollector) {
		t.Fatalf("removing collector %q produced no stale_surface_overlay finding: %+v", overlaidCollector, removedFindings)
	}
}

func hasSurfaceFinding(findings []capabilitycatalog.Finding, kind capabilitycatalog.FindingKind, subject string) bool {
	for _, f := range findings {
		if f.Kind == kind && f.Subject == subject {
			return true
		}
	}
	return false
}

func writeTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
