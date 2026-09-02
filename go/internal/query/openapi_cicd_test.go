// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

func TestOpenAPISpecIncludesCICDRunCorrelations(t *testing.T) {
	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	paths := querytestutil.MustMapField(t, spec, "paths")
	path := querytestutil.MustMapField(t, paths, "/api/v0/ci-cd/run-correlations")
	get := querytestutil.MustMapField(t, path, "get")
	if got, want := get["operationId"], "listCICDRunCorrelations"; got != want {
		t.Fatalf("operationId = %#v, want %#v", got, want)
	}
	responses := querytestutil.MustMapField(t, get, "responses")
	okResponse := querytestutil.MustMapField(t, responses, "200")
	content := querytestutil.MustMapField(t, querytestutil.MustMapField(t, okResponse, "content"), "application/json")
	schema := querytestutil.MustMapField(t, content, "schema")
	properties := querytestutil.MustMapField(t, schema, "properties")
	correlations := querytestutil.MustMapField(t, properties, "correlations")
	items := querytestutil.MustMapField(t, correlations, "items")
	itemProperties := querytestutil.MustMapField(t, items, "properties")
	if got, want := querytestutil.MustMapField(t, itemProperties, "provenance_only")["type"], "boolean"; got != want {
		t.Fatalf("provenance_only type = %#v, want %#v", got, want)
	}
	evidenceSummary := querytestutil.MustMapField(t, properties, "evidence_summary")
	evidenceProperties := querytestutil.MustMapField(t, evidenceSummary, "properties")
	missingEvidence := querytestutil.MustMapField(t, evidenceProperties, "missing_evidence")
	if got, want := missingEvidence["type"], "array"; got != want {
		t.Fatalf("missing_evidence type = %#v, want %#v", got, want)
	}
	staticWorkflow := querytestutil.MustMapField(t, evidenceProperties, "static_workflow_artifacts")
	staticProperties := querytestutil.MustMapField(t, staticWorkflow, "properties")
	if got, want := querytestutil.MustMapField(t, staticProperties, "paths")["type"], "array"; got != want {
		t.Fatalf("static_workflow_artifacts.paths type = %#v, want %#v", got, want)
	}
	liveRuns := querytestutil.MustMapField(t, evidenceProperties, "live_run_correlations")
	liveProperties := querytestutil.MustMapField(t, liveRuns, "properties")
	if got, want := querytestutil.MustMapField(t, liveProperties, "state")["type"], "string"; got != want {
		t.Fatalf("live_run_correlations.state type = %#v, want %#v", got, want)
	}
	runArtifact := querytestutil.MustMapField(t, evidenceProperties, "run_artifact_evidence")
	runArtifactProperties := querytestutil.MustMapField(t, runArtifact, "properties")
	if got, want := querytestutil.MustMapField(t, runArtifactProperties, "artifact_digest_count")["type"], "integer"; got != want {
		t.Fatalf("run_artifact_evidence.artifact_digest_count type = %#v, want %#v", got, want)
	}
}
