// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package replaycoverage

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/capabilitycatalog"
	"github.com/eshu-hq/eshu/go/internal/goldengate"
)

// Resolver reports whether the scenario a coverage entry references actually
// exists. Resolution is deliberately existence-only: this gate proves a scenario
// is authored and wired, not that it passes — its greenness is proven by the
// sibling gate named in the entry's ProofGate (golden-corpus-gate, the parser
// fixture tests, ...). Verifying existence here, and greenness there, keeps the
// coverage gate fast and credential-free while never claiming a green it did not
// observe.
type Resolver interface {
	// Resolve reports whether the entry's scenario artifact exists, with a short
	// human detail for the coverage report.
	Resolve(entry CoverageEntry) (bool, string)
}

// ArtifactResolver resolves scenario references against the repository tree and
// the loaded B-12 golden snapshot. Path-based scenarios (cassette, parser
// fixture) are resolved against RepoRoot; snapshot-based scenarios (correlation,
// api/mcp golden) are resolved against Snapshot; capability-claim scenarios are
// resolved against Matrix; product-claim scenarios are resolved against the
// public product claim ledger.
type ArtifactResolver struct {
	// RepoRoot is the repository root that repo-relative refs are joined onto.
	RepoRoot string
	// Snapshot is the loaded B-12 golden snapshot for correlation/query-shape refs.
	Snapshot goldengate.Snapshot
	// Matrix is the loaded capability matrix for capability claim/refusal refs.
	Matrix capabilitycatalog.Matrix
	// ProductClaims is the loaded public product claim-to-proof ledger.
	ProductClaims capabilitycatalog.ProductClaimLedger
}

// Resolve implements Resolver.
func (r ArtifactResolver) Resolve(entry CoverageEntry) (bool, string) {
	switch entry.Scenario {
	case ScenarioCassette, ScenarioParserFixture:
		return r.resolvePath(entry.Ref)
	case ScenarioCorrelation:
		for _, c := range r.Snapshot.Graph.RequiredCorrelations {
			if c.ID == entry.Ref {
				return true, fmt.Sprintf("snapshot required correlation %q", entry.Ref)
			}
		}
		return false, fmt.Sprintf("snapshot has no required correlation %q", entry.Ref)
	case ScenarioAPIMCPGolden:
		if _, ok := r.Snapshot.QueryShapes.HTTP[entry.Ref]; ok {
			return true, fmt.Sprintf("snapshot HTTP query shape %q", entry.Ref)
		}
		if _, ok := r.Snapshot.QueryShapes.MCP[entry.Ref]; ok {
			return true, fmt.Sprintf("snapshot MCP query shape %q", entry.Ref)
		}
		return false, fmt.Sprintf("snapshot has no query shape %q", entry.Ref)
	case ScenarioCapabilityClaim:
		return r.resolveCapabilityClaim(entry.Ref)
	case ScenarioProductClaim:
		return r.resolveProductClaim(entry.Ref)
	default:
		return false, fmt.Sprintf("unknown scenario type %q", entry.Scenario)
	}
}

func (r ArtifactResolver) resolveCapabilityClaim(ref string) (bool, string) {
	for _, capRow := range r.Matrix.Capabilities {
		if capRow.Capability != ref {
			continue
		}
		supported, refusal, missing := classifyProfileProofs(capRow)
		if len(missing) > 0 {
			return false, fmt.Sprintf("capability %q profile(s) missing verification: %s", ref, strings.Join(missing, ", "))
		}
		if supported == 0 {
			return false, fmt.Sprintf("capability %q has no supported or experimental profile claim", ref)
		}
		return true, fmt.Sprintf("capability %q matrix profile proofs present (supported=%d refusal=%d)", ref, supported, refusal)
	}
	return false, fmt.Sprintf("matrix has no capability %q", ref)
}

func (r ArtifactResolver) resolveProductClaim(ref string) (bool, string) {
	matrixCapabilities := capabilityIDSet(r.Matrix)
	for _, claim := range r.ProductClaims.Claims {
		if claim.ID != ref {
			continue
		}
		if len(claim.Capabilities) == 0 {
			return false, fmt.Sprintf("product claim %q has no referenced capabilities", ref)
		}
		for _, capRef := range claim.Capabilities {
			capID := strings.TrimSpace(capRef.ID)
			if capID == "" {
				return false, fmt.Sprintf("product claim %q has a blank referenced capability", ref)
			}
			if _, ok := matrixCapabilities[capID]; !ok {
				return false, fmt.Sprintf("product claim %q references unknown capability %q", ref, capID)
			}
		}
		if strings.TrimSpace(claim.Proof.Command) == "" && strings.TrimSpace(claim.Proof.Artifact) == "" {
			return false, fmt.Sprintf("product claim %q missing deterministic proof command or artifact", ref)
		}
		proofSignals := len(claim.Proof.Signals)
		proofCounts := len(claim.Proof.Counts)
		if proofSignals == 0 && proofCounts == 0 {
			return false, fmt.Sprintf("product claim %q missing proof signals or surface-count proof", ref)
		}
		return true, fmt.Sprintf("product claim %q deterministic proof present (capabilities=%d signals=%d counts=%d)", ref, len(claim.Capabilities), proofSignals, proofCounts)
	}
	return false, fmt.Sprintf("product claim ledger has no product claim %q", ref)
}

func capabilityIDSet(matrix capabilitycatalog.Matrix) map[string]struct{} {
	out := make(map[string]struct{}, len(matrix.Capabilities))
	for _, capRow := range matrix.Capabilities {
		capID := strings.TrimSpace(capRow.Capability)
		if capID == "" {
			continue
		}
		out[capID] = struct{}{}
	}
	return out
}

func classifyProfileProofs(capRow capabilitycatalog.MatrixCapability) (supported int, refusal int, missing []string) {
	profiles := make([]string, 0, len(capRow.Profiles))
	for profile := range capRow.Profiles {
		profiles = append(profiles, profile)
	}
	sort.Strings(profiles)
	for _, profileName := range profiles {
		profile := capRow.Profiles[profileName]
		if capabilitycatalog.ProfileClaimsSupport(profile) {
			supported++
		} else {
			refusal++
		}
		if len(profile.Verification) == 0 {
			missing = append(missing, profileName)
		}
	}
	return supported, refusal, missing
}

// resolvePath reports whether a repo-relative ref exists under RepoRoot. A ref
// that escapes the root (via .. or an absolute path) never resolves: coverage
// artifacts are committed inside the repo, so an escaping ref is a manifest bug,
// not a coverage hit.
func (r ArtifactResolver) resolvePath(ref string) (bool, string) {
	if filepath.IsAbs(ref) {
		return false, fmt.Sprintf("ref %q is absolute; refs must be repo-relative", ref)
	}
	clean := filepath.Clean(ref)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return false, fmt.Sprintf("ref %q escapes the repository root", ref)
	}
	if _, err := os.Stat(filepath.Join(r.RepoRoot, clean)); err != nil {
		return false, fmt.Sprintf("artifact missing: %s", ref)
	}
	return true, fmt.Sprintf("artifact present: %s", ref)
}
