// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package capabilitycatalog

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

type productClaimChecker struct {
	repoRoot    string
	catalog     map[string]Entry
	surfaces    map[SurfaceCategory]map[string]bool
	sourceCache map[string][]string
	pathCache   map[string]bool
	dirCache    map[string]bool
}

func newProductClaimChecker(repoRoot string, catalog Catalog, inventory SurfaceInventory) *productClaimChecker {
	surfaces := map[SurfaceCategory]map[string]bool{}
	for _, record := range inventory.Surfaces {
		if surfaces[record.Category] == nil {
			surfaces[record.Category] = map[string]bool{}
		}
		surfaces[record.Category][record.Name] = true
	}
	return &productClaimChecker{
		repoRoot:    repoRoot,
		catalog:     catalogEntriesByID(catalog),
		surfaces:    surfaces,
		sourceCache: map[string][]string{},
		pathCache:   map[string]bool{},
		dirCache:    map[string]bool{},
	}
}

func (c *productClaimChecker) checkClaimSource(claim ProductClaim) []ProductClaimFinding {
	if claim.Source.Path == "" || claim.Source.Line <= 0 || claim.Source.Quote == "" {
		return []ProductClaimFinding{productClaimFinding(ProductClaimFindingSourceMismatch, claim.ID, claim.Source.Path, claim.Source.Line, "source path, line, and quote are required")}
	}
	lines, err := c.sourceLines(claim.Source.Path)
	if err != nil {
		return []ProductClaimFinding{productClaimFinding(ProductClaimFindingSourceMismatch, claim.ID, claim.Source.Path, claim.Source.Line, err.Error())}
	}
	if claim.Source.Line > len(lines) || normalizedClaimLine(lines[claim.Source.Line-1]) != normalizedClaimLine(claim.Source.Quote) {
		return []ProductClaimFinding{productClaimFinding(ProductClaimFindingSourceMismatch, claim.ID, claim.Source.Path, claim.Source.Line, "source line does not exactly match quote")}
	}
	return nil
}

func (c *productClaimChecker) sourceLines(path string) ([]string, error) {
	clean, ok := cleanRepoPath(path)
	if !ok {
		return nil, fmt.Errorf("source path must stay inside repo: %s", path)
	}
	if lines, ok := c.sourceCache[clean]; ok {
		return lines, nil
	}
	raw, err := os.ReadFile(filepath.Join(c.repoRoot, clean)) // #nosec G304 -- ledger paths are repo-owned and cleaned.
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(raw), "\n")
	c.sourceCache[clean] = lines
	return lines, nil
}

func (c *productClaimChecker) checkClaimOwnership(claim ProductClaim) []ProductClaimFinding {
	if len(claim.OwnerPackages) == 0 {
		return []ProductClaimFinding{productClaimFinding(ProductClaimFindingMissingOwner, claim.ID, claim.Source.Path, claim.Source.Line, "owner_packages is required")}
	}
	var findings []ProductClaimFinding
	for _, cap := range claim.Capabilities {
		if entry, ok := c.catalog[cap.ID]; ok && entry.OwnerPackage != "" && !slices.Contains(claim.OwnerPackages, entry.OwnerPackage) {
			findings = append(findings, productClaimFinding(ProductClaimFindingMissingOwner, claim.ID, claim.Source.Path, claim.Source.Line, fmt.Sprintf("%s owner_package %s not listed", cap.ID, entry.OwnerPackage)))
		}
	}
	for _, owner := range claim.OwnerPackages {
		cleanOwner, ok := cleanRepoPath(owner)
		if !ok || !c.repoDirExists(filepath.Join("go", cleanOwner)) {
			findings = append(findings, productClaimFinding(ProductClaimFindingMissingOwner, claim.ID, claim.Source.Path, claim.Source.Line, fmt.Sprintf("owner package path not found: %s", owner)))
		}
	}
	return findings
}

func (c *productClaimChecker) checkClaimSurfaces(claim ProductClaim) []ProductClaimFinding {
	surfaces := claimSurfaces(claim)
	if len(surfaces) == 0 {
		return []ProductClaimFinding{productClaimFinding(ProductClaimFindingMissingSurface, claim.ID, claim.Source.Path, claim.Source.Line, "at least one API, MCP, or console surface is required")}
	}
	var findings []ProductClaimFinding
	catalogSurfaces := c.catalogSurfacesForClaim(claim)
	for _, surface := range claim.APISurfaces {
		if !c.surfaceExists(SurfaceAPIRoute, surface) {
			findings = append(findings, productClaimFinding(ProductClaimFindingMissingSurface, claim.ID, claim.Source.Path, claim.Source.Line, fmt.Sprintf("API surface %s is not in generated surface inventory", surface)))
			continue
		}
		if len(catalogSurfaces) > 0 && !catalogSurfaces[surface] {
			findings = append(findings, productClaimFinding(ProductClaimFindingMissingSurface, claim.ID, claim.Source.Path, claim.Source.Line, fmt.Sprintf("API surface %s is not on referenced capabilities", surface)))
		}
	}
	for _, surface := range claim.ConsoleSurfaces {
		if !c.surfaceExists(SurfaceConsolePage, surface) {
			findings = append(findings, productClaimFinding(ProductClaimFindingMissingSurface, claim.ID, claim.Source.Path, claim.Source.Line, fmt.Sprintf("console surface %s is not in generated surface inventory", surface)))
			continue
		}
		if len(catalogSurfaces) > 0 && !catalogSurfaces[surface] {
			findings = append(findings, productClaimFinding(ProductClaimFindingMissingSurface, claim.ID, claim.Source.Path, claim.Source.Line, fmt.Sprintf("console surface %s is not on referenced capabilities", surface)))
		}
	}
	for _, surface := range claim.MCPSurfaces {
		if !c.surfaceExists(SurfaceMCPTool, surface) {
			findings = append(findings, productClaimFinding(ProductClaimFindingMissingSurface, claim.ID, claim.Source.Path, claim.Source.Line, fmt.Sprintf("MCP surface %s is not in generated surface inventory", surface)))
			continue
		}
		if len(catalogSurfaces) > 0 && !catalogSurfaces[surface] {
			findings = append(findings, productClaimFinding(ProductClaimFindingMissingSurface, claim.ID, claim.Source.Path, claim.Source.Line, fmt.Sprintf("MCP surface %s is not on referenced capabilities", surface)))
		}
	}
	return findings
}

func (c *productClaimChecker) catalogSurfacesForClaim(claim ProductClaim) map[string]bool {
	catalogSurfaces := map[string]bool{}
	for _, cap := range claim.Capabilities {
		entry, ok := c.catalog[cap.ID]
		if !ok {
			continue
		}
		for _, surface := range entry.Surfaces {
			catalogSurfaces[surface.Tool] = true
		}
	}
	return catalogSurfaces
}

func (c *productClaimChecker) checkClaimPaths(claim ProductClaim) []ProductClaimFinding {
	var findings []ProductClaimFinding
	for _, path := range append(append(append([]string{}, claim.ImplementationPaths...), claim.ReducerPaths...), claim.CollectorPaths...) {
		if !c.repoPathExists(path) {
			findings = append(findings, productClaimFinding(ProductClaimFindingMissingPath, claim.ID, claim.Source.Path, claim.Source.Line, fmt.Sprintf("path not found: %s", path)))
		}
	}
	if len(claim.ImplementationPaths) == 0 {
		findings = append(findings, productClaimFinding(ProductClaimFindingMissingPath, claim.ID, claim.Source.Path, claim.Source.Line, "implementation_paths is required"))
	}
	return findings
}

func (c *productClaimChecker) checkClaimProof(claim ProductClaim) []ProductClaimFinding {
	var findings []ProductClaimFinding
	if strings.TrimSpace(claim.Proof.Command) == "" && strings.TrimSpace(claim.Proof.Artifact) == "" {
		findings = append(findings, productClaimFinding(ProductClaimFindingMissingProof, claim.ID, claim.Source.Path, claim.Source.Line, "proof command or artifact is required"))
	}
	if claim.Proof.Artifact != "" && !c.repoPathExists(claim.Proof.Artifact) {
		findings = append(findings, productClaimFinding(ProductClaimFindingMissingProof, claim.ID, claim.Source.Path, claim.Source.Line, fmt.Sprintf("proof artifact not found: %s", claim.Proof.Artifact)))
	}
	for _, count := range claim.Proof.Counts {
		actual, ok := c.surfaceCount(count.Category)
		if !ok || count.Count <= 0 {
			findings = append(findings, productClaimFinding(ProductClaimFindingMissingProof, claim.ID, claim.Source.Path, claim.Source.Line, fmt.Sprintf("invalid surface count expectation: %s=%d", count.Category, count.Count)))
			continue
		}
		if actual != count.Count {
			findings = append(findings, productClaimFinding(ProductClaimFindingMissingProof, claim.ID, claim.Source.Path, claim.Source.Line, fmt.Sprintf("surface count %s actual=%d expected=%d", count.Category, actual, count.Count)))
		}
	}
	for _, signal := range claim.Proof.Signals {
		if !c.proofSignalExists(signal) {
			findings = append(findings, productClaimFinding(ProductClaimFindingMissingProof, claim.ID, claim.Source.Path, claim.Source.Line, fmt.Sprintf("proof signal not found in catalog: %s %s:%s", signal.Capability, signal.Kind, signal.Ref)))
		}
	}
	for _, cap := range claim.Capabilities {
		if !claimProofCoversCapability(claim.Proof.Signals, cap.ID) {
			findings = append(findings, productClaimFinding(ProductClaimFindingMissingProof, claim.ID, claim.Source.Path, claim.Source.Line, fmt.Sprintf("proof.signals must reference capability %s", cap.ID)))
		}
	}
	return findings
}

func (c *productClaimChecker) proofSignalExists(signal ProductClaimProofSignal) bool {
	entry, ok := c.catalog[signal.Capability]
	if !ok || signal.Kind == "" || signal.Ref == "" {
		return false
	}
	for _, existing := range entry.ProofSignals {
		if existing.Kind == signal.Kind && existing.Ref == signal.Ref {
			return true
		}
	}
	return false
}

func claimProofCoversCapability(signals []ProductClaimProofSignal, capability string) bool {
	for _, signal := range signals {
		if signal.Capability == capability {
			return true
		}
	}
	return false
}

func (c *productClaimChecker) surfaceExists(category SurfaceCategory, name string) bool {
	return c.surfaces[category][name]
}

func (c *productClaimChecker) surfaceCount(category SurfaceCategory) (int, bool) {
	surfaces, ok := c.surfaces[category]
	if !ok {
		return 0, false
	}
	return len(surfaces), true
}

func (c *productClaimChecker) repoPathExists(path string) bool {
	clean, ok := cleanRepoPath(path)
	if !ok {
		return false
	}
	if exists, ok := c.pathCache[clean]; ok {
		return exists
	}
	_, err := os.Stat(filepath.Join(c.repoRoot, clean))
	exists := err == nil
	c.pathCache[clean] = exists
	return exists
}

func (c *productClaimChecker) repoDirExists(path string) bool {
	clean, ok := cleanRepoPath(path)
	if !ok {
		return false
	}
	if exists, ok := c.dirCache[clean]; ok {
		return exists
	}
	stat, err := os.Stat(filepath.Join(c.repoRoot, clean))
	exists := err == nil && stat.IsDir()
	c.dirCache[clean] = exists
	return exists
}

func cleanRepoPath(path string) (string, bool) {
	if strings.TrimSpace(path) == "" || filepath.IsAbs(path) {
		return "", false
	}
	clean := filepath.Clean(path)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", false
	}
	return clean, true
}

func normalizedClaimLine(line string) string {
	return strings.Join(strings.Fields(line), " ")
}
