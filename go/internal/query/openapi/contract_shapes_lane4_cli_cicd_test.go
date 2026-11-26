// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package openapi

import "testing"

func TestContractShapesLane4CliCicd(t *testing.T) {
	t.Parallel()
	paths := lane4SpecPaths(t)

	t.Run("cli support envelopes name owner keys", func(t *testing.T) {
		t.Parallel()
		// Owner: cli/support/handler.go ValidateVerdict {valid, message}: a
		// 200 always carries the verdict whether the bundle is valid or not.
		validate := lane4PropsOf(t, lane4SchemaAt(t, paths, "/api/v0/report/bundle/validate", "post", "200"), "validate")
		lane4RequireExactKeys(t, validate, "validate", "valid", "message")
		lane4RequireScalarTypes(t, validate, "validate", map[string]string{
			"valid": "boolean", "message": "string",
		})
		lane4RequireDescribed(t, validate, "validate", "valid", "message")
		// Owner: reportbundle/types.go Bundle: the captured wrong-answer
		// bundle envelope the server issues without querying a backend.
		capture := lane4PropsOf(t, lane4SchemaAt(t, paths, "/api/v0/report/bundle/capture", "post", "200"), "capture")
		lane4RequireExactKeys(t, capture, "capture",
			"schema_version", "bundle_id", "created_at", "reporter_note",
			"query", "response", "evidence", "redaction", "payloads", "validation")
		lane4RequireDescribed(t, capture, "capture",
			"schema_version", "bundle_id", "created_at", "reporter_note",
			"query", "response", "evidence", "redaction", "payloads", "validation")
		// Owner: serviceintel/report.go Report: the composed service
		// intelligence report sections plus truth and follow-ups.
		compose := lane4PropsOf(t, lane4SchemaAt(t, paths, "/api/v0/service/report/compose", "post", "200"), "compose")
		lane4RequireKeys(t, compose, "compose",
			"schema", "subject", "supported", "partial", "truth_class",
			"truth", "sections", "limitations", "recommended_next_calls",
			"suggested_investigations")
		lane4RequireDescribed(t, compose, "compose",
			"schema", "subject", "supported", "partial", "truth_class",
			"truth", "sections", "limitations", "recommended_next_calls",
			"suggested_investigations")
		// Render answers raw packet bytes: the JSON variant is the packet
		// document itself, an object keyed by the packet layers.
		renderRoute, ok := paths["/api/v0/investigation/packet/render"].(map[string]any)
		if !ok {
			t.Fatal("spec paths missing /api/v0/investigation/packet/render")
		}
		renderOp := lane4Child(t, renderRoute, "post", "render")
		renderResponses := lane4Child(t, renderOp, "responses", "render post")
		renderOK := lane4Child(t, renderResponses, "200", "render post")
		renderContent, ok := renderOK["content"].(map[string]any)
		if !ok {
			t.Fatal("render 200 response has no content")
		}
		renderJSON, ok := renderContent["application/json"].(map[string]any)
		if !ok {
			t.Fatal("render 200 response has no application/json content")
		}
		renderSchema, ok := renderJSON["schema"].(map[string]any)
		if !ok {
			t.Fatal("render JSON content has no schema")
		}
		if renderSchema["type"] != "object" {
			t.Errorf("render JSON schema has type %v, want object", renderSchema["type"])
		}
		if _, ok := renderSchema["additionalProperties"]; !ok {
			if _, ok := renderSchema["properties"]; !ok {
				t.Error("render JSON schema is a bare object with no properties")
			}
		}
		// Owner: answerquality/types.go Verdict: the scorecard verdict over
		// the captured evidence.
		score := lane4PropsOf(t, lane4SchemaAt(t, paths, "/api/v0/answer-quality/score", "post", "200"), "score")
		lane4RequireExactKeys(t, score, "score",
			"version", "run_id", "pass", "score", "criteria", "prompt_scores", "follow_up_issues")
		lane4RequireScalarTypes(t, score, "score", map[string]string{
			"version": "string", "run_id": "string", "pass": "boolean", "score": "integer",
		})
		lane4RequireDescribed(t, score, "score",
			"version", "run_id", "pass", "score", "criteria", "prompt_scores", "follow_up_issues")
	})

	t.Run("cicd run correlations name owner keys", func(t *testing.T) {
		t.Parallel()
		// Owner: cicd/run_correlations.go decodeRunCorrelationRow projects
		// the fact payload into the fixed 17-key correlation row.
		schema := lane4SchemaAt(t, paths, "/api/v0/ci-cd/run-correlations", "get", "200")
		props := lane4PropsOf(t, schema, "correlations")
		rows := lane4PropsOf(t, lane4ItemsOf(t, lane4Child(t, props, "correlations", "correlations"), "rows"), "rows[]")
		lane4RequireExactKeys(t, rows, "correlations[]",
			"correlation_id", "provider", "run_id", "run_attempt",
			"repository_id", "commit_sha", "environment",
			"environment_evidence", "artifact_digest", "image_ref", "outcome",
			"reason", "provenance_only", "canonical_writes",
			"canonical_target", "correlation_kind", "evidence_fact_ids")
		lane4RequireDescribed(t, rows, "correlations[]",
			"correlation_id", "provider", "run_id", "run_attempt",
			"repository_id", "commit_sha", "environment",
			"artifact_digest", "image_ref", "outcome",
			"reason", "provenance_only", "canonical_writes",
			"canonical_target", "correlation_kind", "evidence_fact_ids")
		lane4RequireScalarTypes(t, rows, "correlations[]", map[string]string{
			"correlation_id": "string", "run_attempt": "string",
			"provenance_only": "boolean", "canonical_writes": "integer",
		})
		lane4RequireDescribed(t, props, "correlations",
			"correlations", "count", "limit", "truncated",
			"evidence_summary", "next_cursor", "collector_readiness")
		summary := lane4PropsOf(t, lane4Child(t, props, "evidence_summary", "correlations"), "evidence_summary")
		lane4RequireDescribed(t, summary, "evidence_summary",
			"static_workflow_artifacts", "live_run_correlations",
			"run_artifact_evidence", "missing_evidence", "reason")
		static := lane4PropsOf(t, lane4Child(t, summary, "static_workflow_artifacts", "summary"), "static")
		lane4RequireDescribed(t, static, "static",
			"state", "count", "candidate_pool_status", "paths", "truncated",
			"image_ref_count", "unresolved_count", "ambiguous_count",
			"evidence_class", "reason")
		live := lane4PropsOf(t, lane4Child(t, summary, "live_run_correlations", "summary"), "live")
		lane4RequireDescribed(t, live, "live", "state", "count", "truncated", "reason")
		bridge := lane4PropsOf(t, lane4Child(t, summary, "run_artifact_evidence", "summary"), "bridge")
		lane4RequireDescribed(t, bridge, "bridge",
			"state", "count", "artifact_digest_count", "image_ref_count",
			"ambiguous_count", "reason")
		next := lane4PropsOf(t, lane4Child(t, props, "next_cursor", "correlations"), "next_cursor")
		lane4RequireExactKeys(t, next, "next_cursor", "after_correlation_id")
		// The inventory aggregate keeps the filter-echo scope idiom: a
		// string-valued map of applied filters with the console chips note.
		inventory := lane4PropsOf(t, lane4SchemaAt(t, paths, "/api/v0/ci-cd/run-correlations/inventory", "get", "200"), "inventory")
		lane4RequireFilterEchoScope(t, inventory, "inventory")
	})
}
