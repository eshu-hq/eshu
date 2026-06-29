// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package replaycoverage

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
// api/mcp golden) are resolved against Snapshot.
type ArtifactResolver struct {
	// RepoRoot is the repository root that repo-relative refs are joined onto.
	RepoRoot string
	// Snapshot is the loaded B-12 golden snapshot for correlation/query-shape refs.
	Snapshot goldengate.Snapshot
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
	default:
		return false, fmt.Sprintf("unknown scenario type %q", entry.Scenario)
	}
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
