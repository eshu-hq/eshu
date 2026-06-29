// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"context"
	"net/http"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

type fixedCloudRuntimeDriftStore struct {
	rows  []query.MultiCloudRuntimeDriftFindingRow
	total int
}

func (s fixedCloudRuntimeDriftStore) ListActiveMultiCloudRuntimeDriftFindings(
	_ context.Context,
	_ query.MultiCloudRuntimeDriftFilter,
) ([]query.MultiCloudRuntimeDriftFindingRow, error) {
	return s.rows, nil
}

func (s fixedCloudRuntimeDriftStore) CountActiveMultiCloudRuntimeDriftFindings(
	_ context.Context,
	_ query.MultiCloudRuntimeDriftFilter,
) (int, error) {
	if s.total != 0 {
		return s.total, nil
	}
	return len(s.rows), nil
}

func TestCloudRuntimeDriftHTTPAndMCPParity(t *testing.T) {
	t.Parallel()

	handler := mountCloudRuntimeDriftHandler([]query.MultiCloudRuntimeDriftFindingRow{{
		FactID:           "fact:gcp-vm",
		ScopeID:          "cloud-scope:gcp:project-synthetic",
		GenerationID:     "gcp-gen-1",
		SourceSystem:     "gcp_cloud_inventory",
		Provider:         "gcp",
		CloudResourceUID: "gcp:project-synthetic:compute.googleapis.com/Instance:vm-1",
		RawIdentity:      "//compute.googleapis.com/projects/project-synthetic/zones/us/instances/vm-1",
		FindingKind:      "orphaned_cloud_resource",
		ManagementStatus: "terraform_state_only",
		Confidence:       0.92,
	}}, 5)
	body := map[string]any{
		"project_id":    "cloud-scope:gcp:project-synthetic",
		"provider":      "gcp",
		"finding_kinds": []any{"orphaned_cloud_resource"},
		"limit":         10,
	}

	httpEnv := httpEnvelope(t, handler, http.MethodPost, "/api/v0/cloud/runtime-drift/findings", body)
	mcpEnv, summary := mcpEnvelope(t, handler, "list_cloud_runtime_drift_findings", body)

	requireCloudRuntimeDriftParity(t, "http", "mcp", httpEnv, mcpEnv)
	requireConvenienceSummary(t, summary, mcpEnv)
}

func mountCloudRuntimeDriftHandler(rows []query.MultiCloudRuntimeDriftFindingRow, total int) http.Handler {
	handler := &query.CloudRuntimeDriftHandler{
		Profile: query.ProfileLocalAuthoritative,
		Store: fixedCloudRuntimeDriftStore{
			rows:  rows,
			total: total,
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	return mux
}

func requireCloudRuntimeDriftParity(
	t *testing.T,
	surfaceA string,
	surfaceB string,
	a *query.ResponseEnvelope,
	b *query.ResponseEnvelope,
) {
	t.Helper()

	aCmp := extractComparable(t, a)
	bCmp := extractComparable(t, b)
	requireParity(t, surfaceA, surfaceB, aCmp, bCmp)
	if !equalJSON(a.Data, b.Data) {
		t.Fatalf("cloud runtime drift data parity: %s=%v, %s=%v", surfaceA, a.Data, surfaceB, b.Data)
	}
}
