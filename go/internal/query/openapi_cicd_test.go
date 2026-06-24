// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"
)

func TestOpenAPISpecIncludesCICDRunCorrelations(t *testing.T) {
	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	paths := mustMapField(t, spec, "paths")
	path := mustMapField(t, paths, "/api/v0/ci-cd/run-correlations")
	get := mustMapField(t, path, "get")
	if got, want := get["operationId"], "listCICDRunCorrelations"; got != want {
		t.Fatalf("operationId = %#v, want %#v", got, want)
	}
	responses := mustMapField(t, get, "responses")
	okResponse := mustMapField(t, responses, "200")
	content := mustMapField(t, mustMapField(t, okResponse, "content"), "application/json")
	schema := mustMapField(t, content, "schema")
	properties := mustMapField(t, schema, "properties")
	correlations := mustMapField(t, properties, "correlations")
	items := mustMapField(t, correlations, "items")
	itemProperties := mustMapField(t, items, "properties")
	if got, want := mustMapField(t, itemProperties, "provenance_only")["type"], "boolean"; got != want {
		t.Fatalf("provenance_only type = %#v, want %#v", got, want)
	}
	evidenceSummary := mustMapField(t, properties, "evidence_summary")
	evidenceProperties := mustMapField(t, evidenceSummary, "properties")
	missingEvidence := mustMapField(t, evidenceProperties, "missing_evidence")
	if got, want := missingEvidence["type"], "array"; got != want {
		t.Fatalf("missing_evidence type = %#v, want %#v", got, want)
	}
	staticWorkflow := mustMapField(t, evidenceProperties, "static_workflow_artifacts")
	staticProperties := mustMapField(t, staticWorkflow, "properties")
	if got, want := mustMapField(t, staticProperties, "paths")["type"], "array"; got != want {
		t.Fatalf("static_workflow_artifacts.paths type = %#v, want %#v", got, want)
	}
	liveRuns := mustMapField(t, evidenceProperties, "live_run_correlations")
	liveProperties := mustMapField(t, liveRuns, "properties")
	if got, want := mustMapField(t, liveProperties, "state")["type"], "string"; got != want {
		t.Fatalf("live_run_correlations.state type = %#v, want %#v", got, want)
	}
	runArtifact := mustMapField(t, evidenceProperties, "run_artifact_evidence")
	runArtifactProperties := mustMapField(t, runArtifact, "properties")
	if got, want := mustMapField(t, runArtifactProperties, "artifact_digest_count")["type"], "integer"; got != want {
		t.Fatalf("run_artifact_evidence.artifact_digest_count type = %#v, want %#v", got, want)
	}
}
