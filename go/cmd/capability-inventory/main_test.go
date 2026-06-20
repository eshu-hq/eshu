package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/capabilitycatalog"
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
	err := run([]string{"-mode", "docs", "-specs", repoSpecsDir(t), "-docs", repoDocsDir(t)}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("docs freshness failed: %v\nstdout:\n%s", err, stdout.String())
	}
	if !strings.Contains(stdout.String(), "no freshness findings") {
		t.Fatalf("docs freshness output unexpected:\n%s", stdout.String())
	}
}

// cloneLiveSurfaces returns a deep copy of a live-surface set so a test can
// mutate one category without affecting the original.
func cloneLiveSurfaces(live capabilitycatalog.LiveSurfaces) capabilitycatalog.LiveSurfaces {
	out := capabilitycatalog.LiveSurfaces{Surfaces: map[capabilitycatalog.SurfaceCategory][]string{}}
	for cat, names := range live.Surfaces {
		out.Surfaces[cat] = append([]string(nil), names...)
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
