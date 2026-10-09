// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queryplan

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// Live producers feed the same executable schema as static contract tests.
// The runner supplies both paths and checks that this test executes.
func TestPilotArtifactFiles(t *testing.T) {
	postgresPath := os.Getenv("ESHU_QUERY_METHODOLOGY_POSTGRES_ARTIFACT")
	graphPath := os.Getenv("ESHU_QUERY_METHODOLOGY_GRAPH_ARTIFACT")
	if postgresPath == "" && graphPath == "" {
		t.Skip("live runner supplies PostgreSQL and Neo4j artifacts")
	}
	manifest, err := LoadManifestFile("testdata/handler-hot-cypher.yaml")
	if err != nil {
		t.Fatal(err)
	}
	artifacts := make([]PilotEvidenceArtifact, 0, 2)
	commit := func(ref string) string {
		output, err := exec.Command("git", "rev-parse", "--verify", ref+"^{commit}").Output()
		if err != nil {
			t.Fatalf("resolve artifact commit %s: %v", ref, err)
		}
		return strings.TrimSpace(string(output))
	}
	base, candidate := commit("origin/main"), commit("HEAD")
	for _, path := range []string{postgresPath, graphPath} {
		if path == "" {
			t.Fatal("missing required backend artifact path")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var artifact PilotEvidenceArtifact
		if err := json.Unmarshal(data, &artifact); err != nil {
			t.Fatal(err)
		}
		if err := pilotArtifactCommitIdentity(artifact, base, candidate); err != nil {
			t.Fatal(err)
		}
		artifacts = append(artifacts, artifact)
	}
	if err := ValidatePilotEvidenceSet(manifest, artifacts); err != nil {
		t.Fatal(err)
	}
	t.Log("validated both required pilot backend artifacts")
}

func TestPilotArtifactCommitIdentityRejectsStaleBuilds(t *testing.T) {
	base, candidate := strings.Repeat("a", 40), strings.Repeat("b", 40)
	artifact := PilotEvidenceArtifact{Base: PilotBuildIdentity{Commit: base}, Candidate: PilotBuildIdentity{Commit: candidate}}
	if err := pilotArtifactCommitIdentity(artifact, base, candidate); err != nil {
		t.Fatal(err)
	}
	if err := pilotArtifactCommitIdentity(artifact, strings.Repeat("c", 40), candidate); err == nil {
		t.Fatal("accepted stale base commit")
	}
	if err := pilotArtifactCommitIdentity(artifact, base, strings.Repeat("c", 40)); err == nil {
		t.Fatal("accepted stale candidate commit")
	}
}

func pilotArtifactCommitIdentity(artifact PilotEvidenceArtifact, base, candidate string) error {
	if artifact.Base.Commit != base || artifact.Candidate.Commit != candidate {
		return fmt.Errorf("stale artifact commits: base %s/%s candidate %s/%s", artifact.Base.Commit, base, artifact.Candidate.Commit, candidate)
	}
	return nil
}
