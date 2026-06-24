// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

func TestResolveRouteMapsCICDRunCorrelationsToBoundedQuery(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_ci_cd_run_correlations", map[string]any{
		"after_correlation_id": "correlation-1",
		"repository_id":        "repo-api",
		"commit_sha":           "abc123",
		"provider":             "github_actions",
		"artifact_digest":      "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		"image_ref":            "registry.example.com/team/api:prod",
		"environment":          "prod",
		"outcome":              "exact",
		"limit":                float64(25),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/ci-cd/run-correlations"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	for key, want := range map[string]string{
		"after_correlation_id": "correlation-1",
		"repository_id":        "repo-api",
		"commit_sha":           "abc123",
		"provider":             "github_actions",
		"artifact_digest":      "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		"image_ref":            "registry.example.com/team/api:prod",
		"environment":          "prod",
		"outcome":              "exact",
		"limit":                "25",
	} {
		if got := route.query[key]; got != want {
			t.Fatalf("route.query[%s] = %#v, want %#v", key, got, want)
		}
	}
}

func TestResolveRouteMapsCICDRunCorrelationAggregatesToImageRefFilter(t *testing.T) {
	t.Parallel()

	for _, tool := range []string{
		"count_ci_cd_run_correlations",
		"get_ci_cd_run_correlation_inventory",
	} {
		tool := tool
		t.Run(tool, func(t *testing.T) {
			t.Parallel()

			route, err := resolveRoute(tool, map[string]any{
				"image_ref": "registry.example.com/team/api:prod",
				"limit":     float64(10),
			})
			if err != nil {
				t.Fatalf("resolveRoute() error = %v, want nil", err)
			}
			if got, want := route.query["image_ref"], "registry.example.com/team/api:prod"; got != want {
				t.Fatalf("route.query[image_ref] = %#v, want %#v", got, want)
			}
		})
	}
}

func TestDispatchToolCICDRunCorrelationsPreservesArtifactEvidenceSummary(t *testing.T) {
	t.Parallel()

	handler := &query.CICDHandler{
		Correlations: mcpCICDRunCorrelationStore{rows: []query.CICDRunCorrelationRow{{
			CorrelationID:  "correlation-digest",
			RepositoryID:   "repo://example/api",
			Provider:       "github_actions",
			RunID:          "run-1",
			Outcome:        "exact",
			ArtifactDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		}}},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	result, err := dispatchTool(
		context.Background(),
		mux,
		"list_ci_cd_run_correlations",
		map[string]any{
			"repository_id": "repo://example/api",
			"limit":         float64(10),
		},
		"",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("dispatchTool() error = %v, want nil", err)
	}
	if result.Envelope == nil {
		t.Fatal("dispatchTool() envelope is nil, want structured CI/CD envelope")
	}
	data, ok := result.Envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope data type = %T, want map[string]any", result.Envelope.Data)
	}
	summary := data["evidence_summary"].(map[string]any)
	bridge := summary["run_artifact_evidence"].(map[string]any)
	if got, want := bridge["reason"], "artifact_digest_present"; got != want {
		t.Fatalf("run_artifact_evidence.reason = %#v, want %#v", got, want)
	}
}

func TestDispatchToolCICDRunCorrelationsPreservesMissingEvidenceSummary(t *testing.T) {
	t.Parallel()

	handler := &query.CICDHandler{
		Correlations: mcpCICDRunCorrelationStore{rows: []query.CICDRunCorrelationRow{{
			CorrelationID: "correlation-no-artifact",
			RepositoryID:  "repo://example/api",
			Provider:      "github_actions",
			RunID:         "run-1",
			Outcome:       "exact",
		}}},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	result, err := dispatchTool(
		context.Background(),
		mux,
		"list_ci_cd_run_correlations",
		map[string]any{
			"repository_id": "repo://example/api",
			"limit":         float64(10),
		},
		"",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("dispatchTool() error = %v, want nil", err)
	}
	if result.Envelope == nil {
		t.Fatal("dispatchTool() envelope is nil, want structured CI/CD envelope")
	}
	data, ok := result.Envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope data type = %T, want map[string]any", result.Envelope.Data)
	}
	summary, ok := data["evidence_summary"].(map[string]any)
	if !ok {
		t.Fatalf("evidence_summary = %#v, want object", data["evidence_summary"])
	}
	missing, ok := summary["missing_evidence"].([]any)
	if !ok || len(missing) != 1 {
		t.Fatalf("missing_evidence = %#v, want one class", summary["missing_evidence"])
	}
	if got, want := missing[0], "ci_run_to_image_artifact_evidence_missing"; got != want {
		t.Fatalf("missing_evidence[0] = %#v, want %#v", got, want)
	}
}

type mcpCICDRunCorrelationStore struct {
	rows []query.CICDRunCorrelationRow
}

func (s mcpCICDRunCorrelationStore) ListCICDRunCorrelations(
	context.Context,
	query.CICDRunCorrelationFilter,
) ([]query.CICDRunCorrelationRow, error) {
	return append([]query.CICDRunCorrelationRow(nil), s.rows...), nil
}
