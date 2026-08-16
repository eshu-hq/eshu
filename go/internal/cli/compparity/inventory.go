// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package compparity

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/eshu-hq/eshu/go/internal/capabilitycatalog"
	"github.com/eshu-hq/eshu/go/internal/competitiveparity"
)

// Inventory assembles the live competitiveparity.Inventory that
// `eshu competitive-parity validate` scores: the CLI command paths handed in
// by the wrapper, the API/MCP/console surfaces from the capability catalog,
// the committed parity docs read from repoRoot, and the exercise results.
//
// commands comes from the caller because walking the cobra command tree needs
// rootCmd, which lives in package main. firstRun is the first-run report
// exercise, injected for the same reason (see ExerciseResults). A missing doc
// file is skipped — the validator reports it as a failed doc check — but any
// other read failure surfaces as an error.
func Inventory(repoRoot string, commands []string, firstRun func() error) (competitiveparity.Inventory, error) {
	surfaces, err := capabilitycatalog.LoadSurfaceInventory()
	if err != nil {
		return competitiveparity.Inventory{}, err
	}
	inv := competitiveparity.Inventory{
		Commands:  commands,
		Docs:      map[string]string{},
		Exercises: ExerciseResults(repoRoot, firstRun),
	}
	for _, surface := range surfaces.Surfaces {
		switch surface.Category {
		case capabilitycatalog.SurfaceAPIRoute:
			inv.APIRoutes = append(inv.APIRoutes, surface.Name)
		case capabilitycatalog.SurfaceMCPTool:
			inv.MCPTools = append(inv.MCPTools, surface.Name)
		case capabilitycatalog.SurfaceConsolePage:
			inv.ConsolePages = append(inv.ConsolePages, surface.Name)
		default:
			// The remaining surface categories are deliberately not part
			// of the parity inventory. CLI command paths (SurfaceCommand)
			// come from the live cobra tree the wrapper walks, not from
			// the catalog. SurfaceCollector and SurfaceReducerDomain are
			// internal pipeline stages, not operator-invocable endpoints,
			// so a parity comparison of operator-facing surfaces has no
			// row for them — the pre-extraction switch skipped them the
			// same way.
		}
	}
	sort.Strings(inv.APIRoutes)
	sort.Strings(inv.MCPTools)
	sort.Strings(inv.ConsolePages)
	for _, path := range DocPaths() {
		raw, err := os.ReadFile(filepath.Join(repoRoot, path)) // #nosec G304 -- path is a static string from DocPaths(); repoRoot is the operator-supplied repo root, not an HTTP request param //nolint:gosec
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return competitiveparity.Inventory{}, fmt.Errorf("read parity doc %s: %w", path, err)
		}
		inv.Docs[path] = string(raw)
	}
	return inv, nil
}

// ExerciseResults runs every parity exercise and returns one result per
// exercise ID, in a fixed order. Failure details are deliberately static
// per-ID strings — the underlying error may carry local paths, and the
// artifact is share-safe output.
//
// firstRun is the first_run_report_artifact exercise. It is injected because
// the first-run evidence helpers still live in package main (go/cmd/eshu) and
// cannot be imported from here; a nil firstRun records that exercise as
// failed rather than panicking or silently passing.
func ExerciseResults(repoRoot string, firstRun func() error) []competitiveparity.ExerciseResult {
	if firstRun == nil {
		firstRun = func() error { return fmt.Errorf("first-run exercise not wired") }
	}
	checks := []struct {
		id string
		fn func() error
	}{
		{id: "first_run_report_artifact", fn: firstRun},
		{id: "operator_digest_artifact", fn: exerciseOperatorDigestArtifact},
		{id: "investigation_evidence_packet_artifact", fn: exerciseInvestigationEvidencePacketArtifact},
		{id: "evidence_packet_dogfood_fixture", fn: func() error { return exerciseEvidencePacketDogfoodFixture(repoRoot) }},
		{id: "capability_catalog_artifacts", fn: exerciseCapabilityCatalogArtifacts},
	}
	results := make([]competitiveparity.ExerciseResult, 0, len(checks))
	for _, check := range checks {
		result := competitiveparity.ExerciseResult{ID: check.id, OK: true, Detail: "exercised"}
		if err := check.fn(); err != nil {
			result.OK = false
			result.Detail = exerciseFailureDetail(check.id)
		}
		results = append(results, result)
	}
	return results
}

// exerciseFailureDetail maps an exercise ID to its share-safe failure detail.
// The strings are part of the artifact contract asserted by the cmd/eshu
// wrapper tests; keep them stable.
func exerciseFailureDetail(id string) string {
	switch id {
	case "first_run_report_artifact":
		return "first-run evidence exercise failed"
	case "operator_digest_artifact":
		return "operator digest artifact exercise failed"
	case "investigation_evidence_packet_artifact":
		return "investigation evidence packet exercise failed"
	case "evidence_packet_dogfood_fixture":
		return "dogfood fixture unavailable"
	case "capability_catalog_artifacts":
		return "capability catalog artifact exercise failed"
	default:
		return "exercise failed"
	}
}

// Artifact renders a validated report as JSON when jsonOut is set and as the
// Markdown gate summary otherwise. Rendering is delegated to
// internal/competitiveparity so the artifact shape has one owner.
func Artifact(report competitiveparity.Report, jsonOut bool) ([]byte, error) {
	if jsonOut {
		return competitiveparity.RenderJSON(report)
	}
	return []byte(competitiveparity.RenderMarkdown(report)), nil
}

// DocPaths returns the sorted, de-duplicated set of repo-relative doc paths
// the default expectations reference. Inventory reads exactly these paths.
func DocPaths() []string {
	seen := map[string]struct{}{}
	for _, expectation := range competitiveparity.DefaultExpectations() {
		for _, doc := range expectation.Docs {
			seen[doc.Path] = struct{}{}
		}
	}
	paths := make([]string, 0, len(seen))
	for path := range seen {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}
