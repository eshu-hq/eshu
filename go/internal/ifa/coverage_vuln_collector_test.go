// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cigates"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/goldengate"
	"github.com/eshu-hq/eshu/go/internal/replaycoverage"
)

// vulnCollectorKinds are the six fact kinds the vulnerability collector emits.
// The recording carries a real fact of each, so their coverage rows name the
// cassette-backed Odù; the other vulnerability kinds are not collector output
// and keep their compiled pack.
var vulnCollectorKinds = []string{
	"vulnerability.cve",
	"vulnerability.affected_product",
	"vulnerability.reference",
	"vulnerability.source_snapshot",
	"vulnerability.known_exploited",
	"vulnerability.epss_score",
}

var vulnPackOnlyKinds = []string{
	"vulnerability.os_package",
	"vulnerability.affected_package",
	"vulnerability.go_module_evidence",
	"vulnerability.go_call_reachability",
	"vulnerability.suppression",
	"vulnerability.warning",
}

// TestCoverageRepointsCollectorKinds reconciles the committed manifest against
// the cassette-backed catalog and requires each collector kind to be covered
// by the recording, so the claim is checked against real collector output and
// not against a hand-built payload.
func TestCoverageRepointsCollectorKinds(t *testing.T) {
	t.Parallel()

	repoRoot := repoRootDir(t)
	specsDir := filepath.Join(repoRoot, "specs")
	entries := registryByKind()

	snap, err := goldengate.LoadSnapshot(filepath.Join(repoRoot, "testdata", "golden", "e2e-20repo-snapshot.json"))
	if err != nil {
		t.Fatalf("LoadSnapshot(real): %v", err)
	}
	replayManifest, err := replaycoverage.LoadManifest(filepath.Join(specsDir, replaycoverage.ManifestFileName))
	if err != nil {
		t.Fatalf("LoadManifest(replay-coverage-manifest): %v", err)
	}
	ifaManifest, err := replaycoverage.LoadManifest(filepath.Join(specsDir, ManifestFileName))
	if err != nil {
		t.Fatalf("LoadManifest(ifa-coverage-manifest): %v", err)
	}
	proofGates, err := cigates.Load(filepath.Join(specsDir, "ci-gates.v1.yaml"))
	if err != nil {
		t.Fatalf("cigates.Load(real): %v", err)
	}
	exp, err := Derive(facts.FactKindRegistry(), snap, replayManifest)
	if err != nil {
		t.Fatalf("Derive: %v", err)
	}
	catalog, err := CatalogByNameAt(repoRoot)
	if err != nil {
		t.Fatalf("CatalogByNameAt: %v", err)
	}

	cov, _, _ := RunCoverage(CoverageInputs{
		Expectations: exp, Manifest: ifaManifest, Catalog: catalog, Registry: entries, ProofGates: proofGates,
	})
	check := func(kind, wantRef string) {
		t.Helper()
		sc := findSurfaceCoverage(t, cov, FactKindSurfacePrefix+kind)
		if sc.Status != replaycoverage.StatusCovered || sc.Scenario == nil {
			t.Errorf("fact_kind:%s status = %q, detail = %q, want covered", kind, sc.Status, sc.Detail)
			return
		}
		if sc.Scenario.Ref != wantRef {
			t.Errorf("fact_kind:%s is covered by %q, want %q", kind, sc.Scenario.Ref, wantRef)
		}
	}
	for _, kind := range vulnCollectorKinds {
		check(kind, VulnCollectorOduName)
	}
	for _, kind := range vulnPackOnlyKinds {
		check(kind, "odu:vuln-pack")
	}
}
