// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deployment

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

func configReadEvidence() map[string]any {
	return map[string]any{
		"artifacts": []map[string]any{
			{"relationship_type": "READS_CONFIG_FROM", "matched_value": "  app-db* "},
			{"relationship_type": "READS_CONFIG_FROM", "matched_value": "app-db"},
			{"relationship_type": "READS_CONFIG_FROM", "matched_value": "ab"},
			{"relationship_type": "DEPLOYS_FROM", "matched_value": "app-db"},
		},
	}
}

func TestLoadConfigDerivedCloudResourceDependencies(t *testing.T) {
	t.Parallel()

	t.Run("NilGraphAndEmptyEvidence", func(t *testing.T) {
		t.Parallel()
		if got, err := LoadConfigDerivedCloudResourceDependencies(t.Context(), nil, configReadEvidence(), 10); got != nil || err != nil {
			t.Fatalf("nil graph = (%v, %v), want (nil, nil)", got, err)
		}
		reader := graph.FakeGraphReader{}
		if got, err := LoadConfigDerivedCloudResourceDependencies(t.Context(), reader, map[string]any{}, 10); got != nil || err != nil {
			t.Fatalf("empty evidence = (%v, %v), want (nil, nil)", got, err)
		}
	})
	t.Run("ScopedTokenReadsNothing", func(t *testing.T) {
		t.Parallel()
		reader := graph.FakeGraphReader{RunFn: func(context.Context, string, map[string]any) ([]map[string]any, error) {
			t.Error("graph must not be queried for a scoped token")
			return nil, nil
		}}
		got, truncated, err := LoadConfigDerivedCloudResourceDependenciesBounded(scopedDeploymentContext(), reader, configReadEvidence(), 10)
		if err != nil {
			t.Fatalf("error = %v, want nil", err)
		}
		if got != nil || truncated {
			t.Fatalf("scoped read = (%v, %v), want (nil, false)", got, truncated)
		}
	})
	t.Run("MatchesAnchorsAndDedupes", func(t *testing.T) {
		t.Parallel()
		reader := graph.FakeGraphReader{RunFn: func(_ context.Context, _ string, params map[string]any) ([]map[string]any, error) {
			if params["config_anchor_pattern"] != ".*(?:app-db).*" {
				t.Errorf("anchor pattern = %v, want the normalized deduped anchor", params["config_anchor_pattern"])
			}
			return []map[string]any{
				{"id": "db-1", "name": "app-db-primary", "resource_type": "aws_db_instance", "provider": "aws"},
				{"id": "db-1", "name": "app-db-primary", "resource_type": "aws_db_instance", "provider": "aws"},
				{"id": "", "name": "unrelated", "resource_type": "aws_queue"},
				{"id": "", "name": "", "resource_id": "", "arn": ""},
			}, nil
		}}
		got, truncated, err := LoadConfigDerivedCloudResourceDependenciesBounded(t.Context(), reader, configReadEvidence(), 10)
		if err != nil {
			t.Fatalf("error = %v, want nil", err)
		}
		if truncated {
			t.Fatal("truncated = true, want false within the bound")
		}
		if len(got) != 1 {
			t.Fatalf("resources = %v, want one (duplicate, anchor-less, and key-less rows dropped)", got)
		}
		if got[0]["matched_value"] != "app-db" || got[0]["relationship_basis"] != "deployment_config_read_evidence" {
			t.Fatalf("resource = %v, want the anchor and basis stamped", got[0])
		}
	})
}

func TestConfigReadCloudResourceAnchors(t *testing.T) {
	t.Parallel()

	anchors, truncated := ConfigReadCloudResourceAnchors(configReadEvidence())
	if len(anchors) != 1 || anchors[0] != "app-db" {
		t.Fatalf("anchors = %v (truncated=%v), want [app-db] false (dup, short, and wrong-type artifacts skipped)", anchors, truncated)
	}
	if got, _ := ConfigReadCloudResourceAnchors(map[string]any{}); len(got) != 0 {
		t.Fatalf("anchors = %v, want none without artifacts", got)
	}
}

func TestMatchingConfigReadCloudResourceAnchor(t *testing.T) {
	t.Parallel()

	row := map[string]any{"name": "svc", "config_path": "/etc/app-db.conf", "resource_id": "x", "arn": "y"}
	if got := matchingConfigReadCloudResourceAnchor(row, []string{"app-db", "zzz"}); got != "app-db" {
		t.Fatalf("anchor = %q, want the config_path match", got)
	}
	if got := matchingConfigReadCloudResourceAnchor(row, []string{"zzz"}); got != "" {
		t.Fatalf("anchor = %q, want empty without a field match", got)
	}
}

func TestObservationTieBreakIsDeterministic(t *testing.T) {
	t.Parallel()

	rows := []map[string]any{
		{"id": "res-1", "name": "db", "provider": "aws", "confidence": 0.5, "reason": "zebra"},
		{"id": "res-1", "name": "db", "provider": "aws", "confidence": 0.5, "reason": "alpha"},
	}
	got, err := DeploymentTraceCloudResourcesFromRows(rows, "basis")
	if err != nil {
		t.Fatalf("DeploymentTraceCloudResourcesFromRows() error = %v, want nil", err)
	}
	if len(got) != 1 || got[0]["reason"] != "alpha" {
		t.Fatalf("resource = %v, want the winning tiebreak (alpha sorts before zebra)", got)
	}
}

func TestCloudResourceCandidateStatuses(t *testing.T) {
	t.Parallel()

	// The status mapper runs on the loader path (the stamp helper always
	// writes uncorrelated): rows carrying anchor verdicts must surface
	// them as candidate statuses.
	rows := []map[string]any{
		{"id": "r1", "name": "a", "service_anchor_status": "ambiguous"},
		{"id": "r2", "name": "b", "service_anchor_status": "weak"},
	}
	reader := graph.FakeGraphReader{RunFn: func(context.Context, string, map[string]any) ([]map[string]any, error) {
		return rows, nil
	}}
	got, err := LoadUncorrelatedCloudResourceCandidates(t.Context(), reader, "svc", 10)
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if len(got) != 2 {
		t.Fatalf("candidates = %v, want both rows", got)
	}
	if got[0]["candidate_status"] != "ambiguous_anchor" {
		t.Fatalf("status = %v, want ambiguous_candidate", got[0]["candidate_status"])
	}
	if got[1]["candidate_status"] != "weak_anchor" {
		t.Fatalf("status = %v, want weak_candidate", got[1]["candidate_status"])
	}
}
