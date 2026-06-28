// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package capabilitycatalog

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

const ProductClaimLedgerFileName = "product-claims.v1.yaml"

// SemanticOutput records whether a public claim depends on LLM output.
type SemanticOutput string

const (
	// SemanticOutputNotUsed means the claim is fully deterministic.
	SemanticOutputNotUsed SemanticOutput = "not_used"
	// SemanticOutputOptionalGated means semantic output may enrich the path but
	// deterministic evidence remains sufficient without a provider key.
	SemanticOutputOptionalGated SemanticOutput = "optional_gated"
	// SemanticOutputRequired is invalid for public product claims.
	SemanticOutputRequired SemanticOutput = "required"
)

// ProductClaimTruthLevel is the closed vocabulary for a public claim's evidence
// authority. A claim cannot exceed the matrix ceiling of any referenced
// capability.
type ProductClaimTruthLevel string

const (
	// ProductClaimTruthUnsupported means the referenced capability has no
	// supported truth in the selected contract.
	ProductClaimTruthUnsupported ProductClaimTruthLevel = "unsupported"
	// ProductClaimTruthDerived means the claim is computed from deterministic
	// evidence but is not exact source or graph truth.
	ProductClaimTruthDerived ProductClaimTruthLevel = "derived"
	// ProductClaimTruthExact means the claim is backed by exact deterministic
	// source, graph, or catalog truth.
	ProductClaimTruthExact ProductClaimTruthLevel = "exact"
)

// IssueState is the ledger's public GitHub issue state vocabulary.
type IssueState string

const (
	IssueStateOpen   IssueState = "open"
	IssueStateClosed IssueState = "closed"
)

// ProductClaimLedger is the committed public claim-to-proof ledger.
type ProductClaimLedger struct {
	Version string         `yaml:"version"`
	Claims  []ProductClaim `yaml:"claims"`
}

// ProductClaim binds one public prose claim to catalog and proof evidence.
type ProductClaim struct {
	ID                          string                   `yaml:"id"`
	Source                      ProductClaimSource       `yaml:"source"`
	ClaimText                   string                   `yaml:"claim_text"`
	Capabilities                []ProductClaimCapability `yaml:"capabilities"`
	TruthLevel                  ProductClaimTruthLevel   `yaml:"truth_level"`
	OwnerPackages               []string                 `yaml:"owner_packages"`
	ImplementationPaths         []string                 `yaml:"implementation_paths"`
	APISurfaces                 []string                 `yaml:"api_surfaces"`
	MCPSurfaces                 []string                 `yaml:"mcp_surfaces"`
	ConsoleSurfaces             []string                 `yaml:"console_surfaces"`
	ReducerPaths                []string                 `yaml:"reducer_paths"`
	CollectorPaths              []string                 `yaml:"collector_paths"`
	DeterministicEvidenceSource string                   `yaml:"deterministic_evidence_source"`
	SemanticOutput              SemanticOutput           `yaml:"semantic_output"`
	Proof                       ProductClaimProof        `yaml:"proof"`
	Issues                      []ProductClaimIssue      `yaml:"issues"`
}

// ProductClaimSource identifies the exact source line backing a claim.
type ProductClaimSource struct {
	Path  string `yaml:"path"`
	Line  int    `yaml:"line"`
	Quote string `yaml:"quote"`
}

// ProductClaimCapability binds a claim to a capability and maturity state.
type ProductClaimCapability struct {
	ID              string   `yaml:"id"`
	ClaimedMaturity Maturity `yaml:"claimed_maturity"`
}

// ProductClaimProof names the durable command or artifact that proves a claim.
type ProductClaimProof struct {
	Command  string                     `yaml:"command"`
	Artifact string                     `yaml:"artifact"`
	Counts   []ProductClaimSurfaceCount `yaml:"surface_counts"`
	Signals  []ProductClaimProofSignal  `yaml:"signals"`
}

// ProductClaimSurfaceCount records a generated surface count asserted by prose.
type ProductClaimSurfaceCount struct {
	Category SurfaceCategory `yaml:"category"`
	Count    int             `yaml:"count"`
}

// ProductClaimProofSignal references one catalog proof signal used by a claim.
type ProductClaimProofSignal struct {
	Capability string `yaml:"capability"`
	Kind       string `yaml:"kind"`
	Ref        string `yaml:"ref"`
}

// ProductClaimIssue records the expected state of a tracking issue.
type ProductClaimIssue struct {
	Number int        `yaml:"number"`
	State  IssueState `yaml:"state"`
}

// ProductClaimFindingKind classifies a product claim ledger failure.
type ProductClaimFindingKind string

const (
	ProductClaimFindingMalformed         ProductClaimFindingKind = "malformed_claim"
	ProductClaimFindingDuplicateID       ProductClaimFindingKind = "duplicate_claim_id"
	ProductClaimFindingSourceMismatch    ProductClaimFindingKind = "source_mismatch"
	ProductClaimFindingUnknownCapability ProductClaimFindingKind = "unknown_capability"
	ProductClaimFindingStaleMaturity     ProductClaimFindingKind = "stale_maturity"
	ProductClaimFindingStaleTruthLevel   ProductClaimFindingKind = "stale_truth_level"
	ProductClaimFindingMissingOwner      ProductClaimFindingKind = "missing_owner"
	ProductClaimFindingMissingSurface    ProductClaimFindingKind = "missing_surface"
	ProductClaimFindingMissingPath       ProductClaimFindingKind = "missing_path"
	ProductClaimFindingMissingProof      ProductClaimFindingKind = "missing_proof"
	ProductClaimFindingMissingMarker     ProductClaimFindingKind = "missing_marker"
	ProductClaimFindingMissingIssue      ProductClaimFindingKind = "missing_issue"
	ProductClaimFindingStaleIssue        ProductClaimFindingKind = "stale_issue"
	ProductClaimFindingSemanticRequired  ProductClaimFindingKind = "semantic_required"
)

// ProductClaimFinding is one ledger contradiction or missing proof edge.
type ProductClaimFinding struct {
	Kind   ProductClaimFindingKind `json:"kind"`
	ID     string                  `json:"id"`
	Path   string                  `json:"path,omitempty"`
	Line   int                     `json:"line,omitempty"`
	Detail string                  `json:"detail"`
}

// LoadProductClaimLedger reads the YAML claim ledger from disk.
func LoadProductClaimLedger(path string) (ProductClaimLedger, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- path is repo-owned command input.
	if err != nil {
		return ProductClaimLedger{}, fmt.Errorf("read product claim ledger %s: %w", path, err)
	}
	var ledger ProductClaimLedger
	if err := yaml.Unmarshal(raw, &ledger); err != nil {
		return ProductClaimLedger{}, fmt.Errorf("parse product claim ledger %s: %w", path, err)
	}
	return ledger, nil
}

// CheckProductClaims validates public product claims against the catalog, repo
// files, deterministic proof, and no-provider-key semantic invariant.
func CheckProductClaims(repoRoot string, catalog Catalog, inventory SurfaceInventory, ledger ProductClaimLedger) []ProductClaimFinding {
	checker := newProductClaimChecker(repoRoot, catalog, inventory)
	var findings []ProductClaimFinding
	seen := map[string]bool{}
	if ledger.Version == "" {
		findings = append(findings, productClaimFinding(ProductClaimFindingMalformed, "", "", 0, "ledger version is required"))
	}
	for _, claim := range ledger.Claims {
		findings = append(findings, checkClaimBasics(claim, seen)...)
		findings = append(findings, checker.checkClaimSource(claim)...)
		findings = append(findings, checkClaimCapabilities(claim, checker.catalog)...)
		findings = append(findings, checkClaimTruthLevel(claim, checker.catalog)...)
		findings = append(findings, checker.checkClaimOwnership(claim)...)
		findings = append(findings, checker.checkClaimSurfaces(claim)...)
		findings = append(findings, checker.checkClaimPaths(claim)...)
		findings = append(findings, checker.checkClaimProof(claim)...)
		findings = append(findings, checkClaimSemanticOutput(claim)...)
		findings = append(findings, checkClaimIssues(claim, checker.catalog)...)
	}
	return findings
}

func catalogEntriesByID(catalog Catalog) map[string]Entry {
	byID := make(map[string]Entry, len(catalog.Entries))
	for _, entry := range catalog.Entries {
		byID[entry.Capability] = entry
	}
	return byID
}

func checkClaimBasics(claim ProductClaim, seen map[string]bool) []ProductClaimFinding {
	var findings []ProductClaimFinding
	if strings.TrimSpace(claim.ID) == "" {
		findings = append(findings, productClaimFinding(ProductClaimFindingMalformed, claim.ID, claim.Source.Path, claim.Source.Line, "claim id is required"))
	}
	if seen[claim.ID] {
		findings = append(findings, productClaimFinding(ProductClaimFindingDuplicateID, claim.ID, claim.Source.Path, claim.Source.Line, "claim id is duplicated"))
	}
	seen[claim.ID] = true
	if strings.TrimSpace(claim.ClaimText) == "" || strings.TrimSpace(string(claim.TruthLevel)) == "" {
		findings = append(findings, productClaimFinding(ProductClaimFindingMalformed, claim.ID, claim.Source.Path, claim.Source.Line, "claim_text and truth_level are required"))
	}
	if strings.TrimSpace(claim.DeterministicEvidenceSource) == "" {
		findings = append(findings, productClaimFinding(ProductClaimFindingMissingProof, claim.ID, claim.Source.Path, claim.Source.Line, "deterministic_evidence_source is required"))
	}
	return findings
}

func checkClaimCapabilities(claim ProductClaim, catalog map[string]Entry) []ProductClaimFinding {
	if len(claim.Capabilities) == 0 {
		return []ProductClaimFinding{productClaimFinding(ProductClaimFindingUnknownCapability, claim.ID, claim.Source.Path, claim.Source.Line, "at least one capability is required")}
	}
	var findings []ProductClaimFinding
	for _, cap := range claim.Capabilities {
		entry, ok := catalog[cap.ID]
		if !ok {
			findings = append(findings, productClaimFinding(ProductClaimFindingUnknownCapability, claim.ID, claim.Source.Path, claim.Source.Line, cap.ID))
			continue
		}
		if cap.ClaimedMaturity == "" {
			findings = append(findings, productClaimFinding(ProductClaimFindingStaleMaturity, claim.ID, claim.Source.Path, claim.Source.Line, fmt.Sprintf("%s missing claimed_maturity", cap.ID)))
			continue
		}
		if cap.ClaimedMaturity != entry.Maturity {
			findings = append(findings, productClaimFinding(ProductClaimFindingStaleMaturity, claim.ID, claim.Source.Path, claim.Source.Line, fmt.Sprintf("%s claimed=%s expected=%s", cap.ID, cap.ClaimedMaturity, entry.Maturity)))
		}
	}
	return findings
}

func checkClaimTruthLevel(claim ProductClaim, catalog map[string]Entry) []ProductClaimFinding {
	claimRank, ok := productClaimTruthRank(claim.TruthLevel)
	if !ok {
		return []ProductClaimFinding{productClaimFinding(ProductClaimFindingMalformed, claim.ID, claim.Source.Path, claim.Source.Line, fmt.Sprintf("truth_level must be exact, derived, or unsupported: %s", claim.TruthLevel))}
	}
	var findings []ProductClaimFinding
	for _, cap := range claim.Capabilities {
		entry, exists := catalog[cap.ID]
		if !exists {
			continue
		}
		ceiling, ceilingRank, hasCeiling := entryTruthCeiling(entry)
		if !hasCeiling {
			findings = append(findings, productClaimFinding(ProductClaimFindingStaleTruthLevel, claim.ID, claim.Source.Path, claim.Source.Line, fmt.Sprintf("%s has no max truth ceiling", cap.ID)))
			continue
		}
		if claimRank > ceilingRank {
			findings = append(findings, productClaimFinding(ProductClaimFindingStaleTruthLevel, claim.ID, claim.Source.Path, claim.Source.Line, fmt.Sprintf("%s truth_level=%s exceeds ceiling=%s", cap.ID, claim.TruthLevel, ceiling)))
		}
	}
	return findings
}

func entryTruthCeiling(entry Entry) (ProductClaimTruthLevel, int, bool) {
	var maxLevel ProductClaimTruthLevel
	maxRank := -1
	for _, profile := range entry.Profiles {
		level := ProductClaimTruthLevel(profile.MaxTruthLevel)
		rank, ok := productClaimTruthRank(level)
		if !ok {
			continue
		}
		if rank > maxRank {
			maxLevel = level
			maxRank = rank
		}
	}
	return maxLevel, maxRank, maxRank >= 0
}

func productClaimTruthRank(level ProductClaimTruthLevel) (int, bool) {
	switch level {
	case ProductClaimTruthUnsupported:
		return 0, true
	case ProductClaimTruthDerived:
		return 1, true
	case ProductClaimTruthExact:
		return 2, true
	default:
		return 0, false
	}
}

func claimSurfaces(claim ProductClaim) []string {
	var surfaces []string
	surfaces = append(surfaces, claim.APISurfaces...)
	surfaces = append(surfaces, claim.MCPSurfaces...)
	surfaces = append(surfaces, claim.ConsoleSurfaces...)
	return surfaces
}

func checkClaimSemanticOutput(claim ProductClaim) []ProductClaimFinding {
	switch claim.SemanticOutput {
	case SemanticOutputNotUsed, SemanticOutputOptionalGated:
		return nil
	case SemanticOutputRequired:
		return []ProductClaimFinding{productClaimFinding(ProductClaimFindingSemanticRequired, claim.ID, claim.Source.Path, claim.Source.Line, "public claims cannot require semantic/LLM output")}
	default:
		return []ProductClaimFinding{productClaimFinding(ProductClaimFindingSemanticRequired, claim.ID, claim.Source.Path, claim.Source.Line, "semantic_output must be not_used or optional_gated")}
	}
}

func checkClaimIssues(claim ProductClaim, catalog map[string]Entry) []ProductClaimFinding {
	var findings []ProductClaimFinding
	for _, issue := range claim.Issues {
		if issue.Number <= 0 || (issue.State != IssueStateOpen && issue.State != IssueStateClosed) {
			findings = append(findings, productClaimFinding(ProductClaimFindingStaleIssue, claim.ID, claim.Source.Path, claim.Source.Line, fmt.Sprintf("invalid issue state: %+v", issue)))
		}
	}
	if claimReferencesNonGA(claim, catalog) && !hasIssue(claim.Issues) {
		findings = append(findings, productClaimFinding(ProductClaimFindingMissingIssue, claim.ID, claim.Source.Path, claim.Source.Line, "non-GA capability claim needs a tracking issue"))
	}
	return findings
}

func claimReferencesNonGA(claim ProductClaim, catalog map[string]Entry) bool {
	for _, cap := range claim.Capabilities {
		if entry, ok := catalog[cap.ID]; ok && entry.Maturity != MaturityGeneralAvailability {
			return true
		}
	}
	return false
}

func hasIssue(issues []ProductClaimIssue) bool {
	for _, issue := range issues {
		if issue.Number > 0 {
			return true
		}
	}
	return false
}

func productClaimFinding(kind ProductClaimFindingKind, id, path string, line int, detail string) ProductClaimFinding {
	return ProductClaimFinding{Kind: kind, ID: id, Path: path, Line: line, Detail: detail}
}
