// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil"
)

func TestOpenAPISpecIncludesCICDRunCorrelations(t *testing.T) {
	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	paths := testutil.MustMapField(t, spec, "paths")
	path := testutil.MustMapField(t, paths, "/api/v0/ci-cd/run-correlations")
	get := testutil.MustMapField(t, path, "get")
	if got, want := get["operationId"], "listCICDRunCorrelations"; got != want {
		t.Fatalf("operationId = %#v, want %#v", got, want)
	}
	responses := testutil.MustMapField(t, get, "responses")
	okResponse := testutil.MustMapField(t, responses, "200")
	content := testutil.MustMapField(t, testutil.MustMapField(t, okResponse, "content"), "application/json")
	schema := testutil.MustMapField(t, content, "schema")
	properties := testutil.MustMapField(t, schema, "properties")
	correlations := testutil.MustMapField(t, properties, "correlations")
	items := testutil.MustMapField(t, correlations, "items")
	itemProperties := testutil.MustMapField(t, items, "properties")
	if got, want := testutil.MustMapField(t, itemProperties, "provenance_only")["type"], "boolean"; got != want {
		t.Fatalf("provenance_only type = %#v, want %#v", got, want)
	}
	evidenceSummary := testutil.MustMapField(t, properties, "evidence_summary")
	evidenceProperties := testutil.MustMapField(t, evidenceSummary, "properties")
	missingEvidence := testutil.MustMapField(t, evidenceProperties, "missing_evidence")
	if got, want := missingEvidence["type"], "array"; got != want {
		t.Fatalf("missing_evidence type = %#v, want %#v", got, want)
	}
	staticWorkflow := testutil.MustMapField(t, evidenceProperties, "static_workflow_artifacts")
	staticProperties := testutil.MustMapField(t, staticWorkflow, "properties")
	if got, want := testutil.MustMapField(t, staticProperties, "paths")["type"], "array"; got != want {
		t.Fatalf("static_workflow_artifacts.paths type = %#v, want %#v", got, want)
	}
	liveRuns := testutil.MustMapField(t, evidenceProperties, "live_run_correlations")
	liveProperties := testutil.MustMapField(t, liveRuns, "properties")
	if got, want := testutil.MustMapField(t, liveProperties, "state")["type"], "string"; got != want {
		t.Fatalf("live_run_correlations.state type = %#v, want %#v", got, want)
	}
	runArtifact := testutil.MustMapField(t, evidenceProperties, "run_artifact_evidence")
	runArtifactProperties := testutil.MustMapField(t, runArtifact, "properties")
	if got, want := testutil.MustMapField(t, runArtifactProperties, "artifact_digest_count")["type"], "integer"; got != want {
		t.Fatalf("run_artifact_evidence.artifact_digest_count type = %#v, want %#v", got, want)
	}
}
