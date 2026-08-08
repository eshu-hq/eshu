// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
)

const containerImageIdentityEvidenceMergeQuery = "GET /api/v0/supply-chain/container-images/identities?digest=sha256:00000000000000000000000000000000000000000000000000000000000000ff&limit=10"

func TestGoldenSnapshotPinsContainerImageIdentityEvidenceMerge(t *testing.T) {
	t.Parallel()

	snapshot, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	shape, ok := snapshot.QueryShapes.HTTP[containerImageIdentityEvidenceMergeQuery]
	if !ok {
		t.Fatalf("query_shapes.http missing %q", containerImageIdentityEvidenceMergeQuery)
	}
	if shape.MinimumResults != 1 || shape.MaximumResults != 1 {
		t.Fatalf("result bounds = [%d,%d], want [1,1]", shape.MinimumResults, shape.MaximumResults)
	}
	if shape.ResultsField != "identities" {
		t.Fatalf("results_field = %q, want identities", shape.ResultsField)
	}

	valid := []byte(`{
		"identities":[{
			"digest":"sha256:00000000000000000000000000000000000000000000000000000000000000ff",
			"evidence_fact_ids":[
				"91516d563e2dd52dbcf527740c02b2130e61a6b4b77ad4be218417ac0057b51f",
				"952cba28492237a7263251dbd73f07c57bcfb929b3541a97f4b5eaf8335aaaf8",
				"b0803c884786f7b08f472376bb5a6139bfdb5770e398823ca47bb3e615cfc01b",
				"d15088277e773fa3cf958a16a7220845763a484b2646168881d06038afbb9774"
			],
			"identity_strength":"explicit_digest",
			"source_repository_ids":["repository:r_19519f37"],
			"source_revision":"3b4c5d6e7f80912a3b4c5d6e7f80912a3b4c5d6e",
			"source_revision_provenance":"ci_run_commit"
		}],
		"count":1,
		"limit":10,
		"truncated":false,
		"next_cursor":{},
		"collector_readiness":{}
	}`)
	if finding := EvaluateQueryShape("container-image-identity-evidence-merge", shape, valid); !finding.OK {
		t.Fatalf("valid merged identity failed: %s", finding.Detail)
	}

	missingRuntime := []byte(strings.Replace(
		string(valid),
		"d15088277e773fa3cf958a16a7220845763a484b2646168881d06038afbb9774",
		"runtime-evidence-dropped",
		1,
	))
	if finding := EvaluateQueryShape("container-image-identity-evidence-merge-missing-runtime", shape, missingRuntime); finding.OK {
		t.Fatal("snapshot accepted an identity that dropped the AWS runtime observation")
	}

	weakStrength := []byte(strings.Replace(string(valid), "explicit_digest", "artifact_digest_with_registry_observation", 1))
	if finding := EvaluateQueryShape("container-image-identity-evidence-merge-weaker-strength", shape, weakStrength); finding.OK {
		t.Fatal("snapshot accepted an identity that hid the runtime-observed strength")
	}

	duplicate := []byte(strings.Replace(string(valid), `}],`, `},{"digest":"duplicate"}],`, 1))
	if finding := EvaluateQueryShape("container-image-identity-evidence-merge-duplicate", shape, duplicate); finding.OK {
		t.Fatal("snapshot accepted duplicate identities for one digest")
	}
}

func TestGoldenCICDCassetteCollidesRuntimeAndArtifactEvidence(t *testing.T) {
	t.Parallel()

	path := filepath.Join("..", "..", "..", "testdata", "cassettes", "cicdrun", "supply-chain-demo.json")
	file, err := cassette.LoadFile(path)
	if err != nil {
		t.Fatalf("cassette.LoadFile() error = %v", err)
	}

	const (
		scopeID      = "ci_cd_run:github_actions:acme:container-ci-lineage"
		runKey       = "ci_cd_run:github_actions:container-ci-lineage:run:9101"
		artifactKey  = runKey + ":artifact:image"
		digest       = "sha256:00000000000000000000000000000000000000000000000000000000000000ff"
		repositoryID = "repository:r_19519f37"
	)
	foundRun := 0
	foundArtifact := 0
	for _, scope := range file.Scopes {
		if scope.ScopeID != scopeID {
			continue
		}
		for _, fact := range scope.Facts {
			switch fact.StableFactKey {
			case runKey:
				foundRun++
				if got := fact.Payload["repository_id"]; got != repositoryID {
					t.Fatalf("run 9101 repository_id = %v, want %s", got, repositoryID)
				}
			case artifactKey:
				foundArtifact++
				if got := fact.Payload["artifact_digest"]; got != digest {
					t.Fatalf("run 9101 artifact digest = %v, want %s", got, digest)
				}
			}
		}
	}
	if foundRun != 1 || foundArtifact != 1 {
		t.Fatalf("run 9101 facts = run:%d artifact:%d, want one same-generation pair", foundRun, foundArtifact)
	}
}
