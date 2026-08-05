// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
)

func TestGoldenSnapshotRequiresWorkflowImageBuiltFromCorrelation(t *testing.T) {
	t.Parallel()

	snapshot, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	for _, correlation := range snapshot.Graph.RequiredCorrelations {
		if correlation.ID != "rc-173" {
			continue
		}
		if correlation.Relationship != "BUILT_FROM" || correlation.FromLabel != "ContainerImage" || correlation.ToLabel != "Repository" {
			t.Fatalf("rc-173 = %+v, want ContainerImage-[:BUILT_FROM]->Repository", correlation)
		}
		if correlation.MinimumCount != 1 {
			t.Fatalf("rc-173 minimum_count = %d, want 1", correlation.MinimumCount)
		}
		if !slices.Equal(correlation.EvidenceKinds, []string{"CI_CD_RUN_WORKFLOW_IMAGE_CORRELATION"}) {
			t.Fatalf("rc-173 evidence_kinds = %v, want workflow-image-specific token", correlation.EvidenceKinds)
		}
		if !slices.Equal(correlation.RequiredEdgeProperties, []string{"source_tool"}) {
			t.Fatalf("rc-173 required_edge_properties = %v, want [source_tool]", correlation.RequiredEdgeProperties)
		}
		if got := correlation.AllowedEdgePropertyValues["source_tool"]; !slices.Equal(got, []string{"github_actions"}) {
			t.Fatalf("rc-173 source_tool values = %v, want [github_actions]", got)
		}
		return
	}
	t.Fatal("required_correlations missing rc-173 workflow-image BUILT_FROM proof")
}

const (
	containerCILineageWorkflowImageQuery = "GET /api/v0/ci-cd/run-correlations?image_ref=ghcr.io/acme/container-ci-lineage:1.0.0&limit=10&outcome=exact&provider=github_actions&repository_id=repository:r_19519f37&run_id=9100"
	inputOnlyWorkflowImageQuery          = "GET /api/v0/ci-cd/run-correlations?image_ref=ghcr.io/acme/container-ci-lineage:1.0.0&limit=10&outcome=derived&provider=github_actions&repository_id=repository:r_f252e384&run_id=9200"
)

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
			"outcome":"exact",
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
			"outcome":"exact",
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

func TestGoldenSnapshotPinsInputOnlyWorkflowImageCorrelation(t *testing.T) {
	t.Parallel()

	snapshot, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	shape, ok := snapshot.QueryShapes.HTTP[inputOnlyWorkflowImageQuery]
	if !ok {
		t.Fatalf("query_shapes.http missing %q", inputOnlyWorkflowImageQuery)
	}
	if shape.MinimumResults != 1 || shape.MaximumResults != 1 {
		t.Fatalf("result bounds = [%d,%d], want [1,1]", shape.MinimumResults, shape.MaximumResults)
	}

	const inputOnlyReason = "workflow image ref is a reusable-workflow input (consumed by, not produced by, this workflow); one container image identity row matched"
	valid := []byte(`{
		"correlations":[{
			"artifact_digest":"sha256:c10000000000000000000000000000000000000000000000000000000010c1c1",
			"canonical_writes":1,
			"correlation_kind":"workflow_image",
			"image_ref":"ghcr.io/acme/container-ci-lineage:1.0.0",
			"outcome":"derived",
			"provider":"github_actions",
			"reason":"` + inputOnlyReason + `",
			"repository_id":"repository:r_f252e384",
			"run_id":"9200"
		}],
		"count":1,
		"limit":10,
		"truncated":false
	}`)
	if finding := EvaluateQueryShape("input-only-workflow-image", shape, valid); !finding.OK {
		t.Fatalf("valid input-only correlation failed: %s", finding.Detail)
	}

	fallback := []byte(`{
		"correlations":[{
			"artifact_digest":"sha256:c10000000000000000000000000000000000000000000000000000000010c1c1",
			"canonical_writes":1,
			"correlation_kind":"workflow_image",
			"image_ref":"ghcr.io/acme/container-ci-lineage:1.0.0",
			"outcome":"derived",
			"provider":"github_actions",
			"reason":"workflow image ref matches one container image identity row via repository-wide fallback (no commit-matched workflow file)",
			"repository_id":"repository:r_f252e384",
			"run_id":"9200"
		}],
		"count":1,
		"limit":10,
		"truncated":false
	}`)
	if finding := EvaluateQueryShape("input-only-workflow-image-fallback", shape, fallback); finding.OK {
		t.Fatal("snapshot accepted repository-wide fallback in place of the input-only classifier branch")
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
