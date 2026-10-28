// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deployment

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

func TestDeploymentTraceCloudResourcesFromRows(t *testing.T) {
	t.Parallel()

	t.Run("GroupsObservationsByResource", func(t *testing.T) {
		t.Parallel()
		rows := []map[string]any{
			{"id": "res-1", "name": "db", "provider": "aws", "confidence": 0.7, "reason": "config_read"},
			{"id": "res-1", "name": "db", "provider": "aws", "confidence": 0.9, "reason": "materialized"},
			{"id": "res-2", "name": "queue", "provider": "aws", "confidence": 0.5},
		}
		got, err := DeploymentTraceCloudResourcesFromRows(rows, "deployment_config_read_evidence")
		if err != nil {
			t.Fatalf("DeploymentTraceCloudResourcesFromRows() error = %v, want nil", err)
		}
		if len(got) != 2 {
			t.Fatalf("len(resources) = %d, want 2 (two rows share res-1)", len(got))
		}
		// The highest-confidence observation wins the merged row.
		if got[0]["confidence"] != 0.9 || got[0]["reason"] != "materialized" {
			t.Fatalf("res-1 row = %v, want the 0.9 materialized observation selected", got[0])
		}
		if got[0]["relationship_basis"] != "deployment_config_read_evidence" {
			t.Fatalf("relationship_basis = %v, want the caller default", got[0]["relationship_basis"])
		}
	})
	t.Run("PreselectedObservationsPassThrough", func(t *testing.T) {
		t.Parallel()
		rows := []map[string]any{{
			"id": "res-1", "name": "db",
			"observations": []map[string]any{{"environment": "prod", "confidence": 0.8}},
		}}
		got, err := DeploymentTraceCloudResourcesFromRows(rows, "basis")
		if err != nil {
			t.Fatalf("DeploymentTraceCloudResourcesFromRows() error = %v, want nil", err)
		}
		if len(got) != 1 || got[0]["environment"] != "prod" {
			t.Fatalf("resources = %v, want the preselected observation preserved", got)
		}
	})
	t.Run("NonFiniteConfidenceErrors", func(t *testing.T) {
		t.Parallel()
		rows := []map[string]any{{
			"id":           "res-1",
			"observations": []map[string]any{{"confidence": math.NaN()}},
		}}
		if _, err := DeploymentTraceCloudResourcesFromRows(rows, "basis"); err == nil {
			t.Fatal("DeploymentTraceCloudResourcesFromRows() error = nil, want an error for non-finite confidence")
		}
	})
	t.Run("ObservationEnvironmentFallbackChain", func(t *testing.T) {
		t.Parallel()
		rows := []map[string]any{{
			"id":                   "res-1",
			"observation":          map[string]any{"confidence": 0.6},
			"resource_environment": "staging",
		}}
		got, err := DeploymentTraceCloudResourcesFromRows(rows, "basis")
		if err != nil {
			t.Fatalf("DeploymentTraceCloudResourcesFromRows() error = %v, want nil", err)
		}
		if got[0]["environment"] != "staging" {
			t.Fatalf("environment = %v, want the resource_environment fallback", got[0]["environment"])
		}
	})
}

func TestDeploymentTraceCloudCandidates(t *testing.T) {
	t.Parallel()

	rows := []map[string]any{
		{"id": "res-1", "relationship_basis": "materialized_evidence"},
		{"id": "res-2"},
	}
	got := DeploymentTraceCloudCandidates(rows)
	if len(got) != 2 {
		t.Fatalf("len(candidates) = %d, want 2", len(got))
	}
	if got[0]["candidate_status"] != "uncorrelated" || got[0]["missing_relationship"] != "workload_cloud_relationship" {
		t.Fatalf("candidate = %v, want uncorrelated stamps", got[0])
	}
	if got[0]["match_basis"] != "materialized_evidence" {
		t.Fatalf("match_basis = %v, want the row basis preserved", got[0]["match_basis"])
	}
	if got[1]["match_basis"] != "deployment_config_read_evidence" {
		t.Fatalf("match_basis = %v, want the default basis", got[1]["match_basis"])
	}
}

func scopedDeploymentContext() context.Context {
	return auth.ContextWithAuthContext(context.Background(), auth.AuthContext{
		Mode:                 auth.AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "tenant-a",
		AllowedRepositoryIDs: []string{"repo-a"},
	})
}

func TestLoadUncorrelatedCloudResourceCandidatesBounded(t *testing.T) {
	t.Parallel()

	t.Run("NilGraphAndBlankService", func(t *testing.T) {
		t.Parallel()
		if got, truncated, err := LoadUncorrelatedCloudResourceCandidatesBounded(t.Context(), nil, "svc", 10); got != nil || truncated || err != nil {
			t.Fatalf("nil graph = (%v, %v, %v), want (nil, false, nil)", got, truncated, err)
		}
		reader := graph.FakeGraphReader{}
		if got, truncated, err := LoadUncorrelatedCloudResourceCandidatesBounded(t.Context(), reader, "  ", 10); got != nil || truncated || err != nil {
			t.Fatalf("blank service = (%v, %v, %v), want (nil, false, nil)", got, truncated, err)
		}
	})
	t.Run("ScopedTokenReadsNothing", func(t *testing.T) {
		t.Parallel()
		reader := graph.FakeGraphReader{RunFn: func(context.Context, string, map[string]any) ([]map[string]any, error) {
			t.Error("graph must not be queried for a scoped token")
			return nil, nil
		}}
		got, truncated, err := LoadUncorrelatedCloudResourceCandidatesBounded(scopedDeploymentContext(), reader, "svc", 10)
		if err != nil {
			t.Fatalf("error = %v, want nil", err)
		}
		if got != nil || truncated {
			t.Fatalf("scoped read = (%v, %v), want (nil, false)", got, truncated)
		}
	})
	t.Run("ReaderErrorPropagates", func(t *testing.T) {
		t.Parallel()
		reader := graph.FakeGraphReader{RunFn: func(context.Context, string, map[string]any) ([]map[string]any, error) {
			return nil, errors.New("backend down")
		}}
		if _, err := LoadUncorrelatedCloudResourceCandidates(t.Context(), reader, "svc", 10); err == nil {
			t.Fatal("error = nil, want the reader failure propagated")
		}
	})
	t.Run("ShapesRowsAndDetectsTruncation", func(t *testing.T) {
		t.Parallel()
		rows := []map[string]any{
			{"id": "r1", "name": "svc-db", "provider": "", "source_system": "terraform", "service_anchor_status": "stale"},
			{"id": "r2", "name": "svc-queue", "service_anchor_status": "unanchored"},
			{"id": "r3", "name": "svc-cache"},
		}
		reader := graph.FakeGraphReader{RunFn: func(context.Context, string, map[string]any) ([]map[string]any, error) {
			return rows, nil
		}}
		got, err := LoadUncorrelatedCloudResourceCandidates(t.Context(), reader, "svc", 2)
		if err != nil {
			t.Fatalf("error = %v, want nil", err)
		}
		if len(got) != 2 {
			t.Fatalf("len(candidates) = %d, want the trimmed limit", len(got))
		}
		if got[0]["provider"] != "terraform" {
			t.Fatalf("provider = %v, want the source_system fallback", got[0]["provider"])
		}
		if got[0]["candidate_status"] != "stale_anchor" {
			t.Fatalf("candidate_status = %v, want the stale row status mapped", got[0]["candidate_status"])
		}
		if got[1]["candidate_status"] != "uncorrelated" {
			t.Fatalf("candidate_status = %v, want the default for an unmapped status", got[1]["candidate_status"])
		}
		if got[0]["missing_relationship"] != "workload_cloud_relationship" {
			t.Fatalf("missing_relationship = %v", got[0]["missing_relationship"])
		}
	})
	t.Run("ClampsLimit", func(t *testing.T) {
		t.Parallel()
		var fetchLimit any
		reader := graph.FakeGraphReader{RunFn: func(_ context.Context, _ string, params map[string]any) ([]map[string]any, error) {
			fetchLimit = params["limit"]
			return nil, nil
		}}
		if _, err := LoadUncorrelatedCloudResourceCandidates(t.Context(), reader, "svc", 0); err != nil {
			t.Fatalf("error = %v, want nil", err)
		}
		if fetchLimit != UncorrelatedCloudResourceCandidateLimit+1 {
			t.Fatalf("fetch limit = %v, want the clamped cap plus the truncation probe row", fetchLimit)
		}
	})
}

func TestHostnameHelpers(t *testing.T) {
	t.Parallel()

	t.Run("BoundedPrefersServiceOwnedHosts", func(t *testing.T) {
		t.Parallel()
		// Mirrors the root pin: the affinity filter is a drop path, so
		// narrowing five hostnames to two reports truncated.
		got, truncated := BoundedIndirectEvidenceHostnamesForService([]string{
			"api.vendor.example.test",
			"docs.vendor.example.test",
			"checkout.qa.example.test",
			"metrics.vendor.example.test",
			"checkout.prod.example.test",
		}, "sample-checkout-api")
		if len(got) != 2 || got[0] != "checkout.prod.example.test" || got[1] != "checkout.qa.example.test" {
			t.Fatalf("hostnames = %v, want the two checkout hosts sorted", got)
		}
		if !truncated {
			t.Fatal("truncated = false, want true (affinity dropped three hostnames)")
		}
	})
	t.Run("BoundedFallsBackToFirstFour", func(t *testing.T) {
		t.Parallel()
		got, truncated := BoundedIndirectEvidenceHostnamesForService([]string{
			"", "api.qa.example.test", " api.qa.example.test ",
			"api.prod.example.test", "api.stage.example.test",
			"api.dev.example.test", "api.extra.example.test",
		}, "")
		if len(got) != 4 || !truncated {
			t.Fatalf("hostnames = %v (truncated=%v), want four sorted with truncation disclosed", got, truncated)
		}
	})
	t.Run("BoundedEmpty", func(t *testing.T) {
		t.Parallel()
		if got, truncated := BoundedIndirectEvidenceHostnamesForService([]string{"", "  "}, "svc"); got != nil || truncated {
			t.Fatalf("empty = (%v, %v), want (nil, false)", got, truncated)
		}
	})
	t.Run("AffinityTokens", func(t *testing.T) {
		t.Parallel()
		if got := serviceHostnameAffinityTokens("sample-checkout-api"); len(got) != 2 {
			t.Fatalf("tokens = %v, want [sample checkout] (api is generic, short tokens dropped)", got)
		}
		if got := serviceHostnameAffinityTokens("api"); len(got) != 0 {
			t.Fatalf("tokens = %v, want none for a fully generic name", got)
		}
	})
	t.Run("MatchesToken", func(t *testing.T) {
		t.Parallel()
		if !hostnameMatchesServiceToken("CHECKOUT.prod.example.test", []string{"checkout"}) {
			t.Fatal("match = false, want case-insensitive containment")
		}
		if hostnameMatchesServiceToken("api.example.test", []string{"checkout"}) {
			t.Fatal("match = true, want false without the token")
		}
	})
}
