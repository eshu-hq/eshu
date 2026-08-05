// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
)

const containerCILineageWorkflowImageQuery = "GET /api/v0/ci-cd/run-correlations?image_ref=ghcr.io/acme/container-ci-lineage:1.0.0&limit=10&outcome=derived&provider=github_actions&repository_id=repository:r_19519f37&run_id=9100"

func TestGoldenSnapshotPinsContainerCILineageWorkflowImageCorrelation(t *testing.T) {
	t.Parallel()

	snapshot, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	shape, ok := snapshot.QueryShapes.HTTP[containerCILineageWorkflowImageQuery]
	if !ok {
		t.Fatalf("query_shapes.http missing %q", containerCILineageWorkflowImageQuery)
	}
	if shape.MinimumResults != 1 || shape.MaximumResults != 1 {
		t.Fatalf("result bounds = [%d,%d], want [1,1]", shape.MinimumResults, shape.MaximumResults)
	}
	if shape.ResultsField != "correlations" {
		t.Fatalf("results_field = %q, want correlations", shape.ResultsField)
	}

	valid := []byte(`{
		"correlations":[{
			"artifact_digest":"sha256:c10000000000000000000000000000000000000000000000000000000010c1c1",
			"canonical_writes":1,
			"correlation_kind":"workflow_image",
			"image_ref":"ghcr.io/acme/container-ci-lineage:1.0.0",
			"outcome":"derived",
			"provider":"github_actions",
			"repository_id":"repository:r_19519f37",
			"run_id":"9100"
		}],
		"count":1,
		"limit":10,
		"truncated":false
	}`)
	if finding := EvaluateQueryShape("container-ci-lineage-workflow-image", shape, valid); !finding.OK {
		t.Fatalf("valid workflow-image correlation failed: %s", finding.Detail)
	}

	mutated := []byte(`{
		"correlations":[{
			"artifact_digest":"sha256:c10000000000000000000000000000000000000000000000000000000010c1c1",
			"canonical_writes":1,
			"correlation_kind":"artifact_digest",
			"image_ref":"ghcr.io/acme/container-ci-lineage:1.0.0",
			"outcome":"derived",
			"provider":"github_actions",
			"repository_id":"repository:r_19519f37",
			"run_id":"9100"
		}],
		"count":1,
		"limit":10,
		"truncated":false
	}`)
	if finding := EvaluateQueryShape("container-ci-lineage-workflow-image-mutated", shape, mutated); finding.OK {
		t.Fatal("snapshot accepted a non-workflow correlation for run 9100")
	}

	unresolved := []byte(`{
		"correlations":[{
			"artifact_digest":"",
			"canonical_writes":0,
			"correlation_kind":"workflow_image",
			"image_ref":"ghcr.io/acme/container-ci-lineage:1.0.0",
			"outcome":"derived",
			"provider":"github_actions",
			"repository_id":"repository:r_19519f37",
			"run_id":"9100"
		}],
		"count":1,
		"limit":10,
		"truncated":false
	}`)
	if finding := EvaluateQueryShape("container-ci-lineage-workflow-image-unresolved", shape, unresolved); finding.OK {
		t.Fatal("snapshot accepted workflow evidence that did not resolve to a canonical image")
	}
}

func TestGoldenOCICassetteResolvesContainerCILineageWorkflowTag(t *testing.T) {
	t.Parallel()

	path := filepath.Join("..", "..", "..", "testdata", "cassettes", "ociregistry", "supply-chain-demo.json")
	file, err := cassette.LoadFile(path)
	if err != nil {
		t.Fatalf("cassette.LoadFile() error = %v", err)
	}

	const (
		scopeID = "oci_registry:ghcr.io:acme:container-ci-lineage"
		digest  = "sha256:c10000000000000000000000000000000000000000000000000000000010c1c1"
	)
	matched := 0
	for _, scope := range file.Scopes {
		if scope.ScopeID != scopeID {
			continue
		}
		for _, fact := range scope.Facts {
			if fact.FactKind != "oci_registry.image_tag_observation" {
				continue
			}
			if fact.Payload["tag"] == "1.0.0" && fact.Payload["resolved_digest"] == digest {
				matched++
			}
		}
	}
	if matched != 1 {
		t.Fatalf("matching tag observations = %d, want 1", matched)
	}
}
