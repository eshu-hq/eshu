// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graphdump

import (
	"context"
	"strings"
	"testing"
)

func TestValidateGCPProjectEdgeScopesAcceptsExactFixture(t *testing.T) {
	t.Parallel()

	scopeID := "gcp:project:project-04:seed:4580"
	reader := fakeReader{edges: []Edge{
		gcpScopeEdge("project-04", "project-04", scopeID),
		{Type: "DEPENDS_ON"},
	}}
	checked, err := ValidateGCPProjectEdgeScopes(context.Background(), reader, gcpProjectExpectations(scopeID, "project-04", 1))
	if err != nil {
		t.Fatalf("ValidateGCPProjectEdgeScopes(valid): %v", err)
	}
	if checked != 1 {
		t.Fatalf("checked = %d, want 1 GCP edge", checked)
	}
}

func TestValidateGCPProjectEdgeScopesRejectsUnexpectedFixtureScopes(t *testing.T) {
	t.Parallel()

	for _, scopeID := range []string{
		"gcp:organization:123:compute:resource:global",
		"tenant-defined-gcp-scope",
	} {
		_, err := ValidateGCPProjectEdgeScopes(context.Background(), fakeReader{edges: []Edge{
			gcpScopeEdge("project-02", "project-04", scopeID),
		}}, gcpProjectExpectations("gcp:project:project-04:seed:4580", "project-04", 1))
		if err == nil || !strings.Contains(err.Error(), "unexpected fixture scope_id") {
			t.Fatalf("scope %q error = %v, want unexpected fixture scope", scopeID, err)
		}
	}
}

func TestValidateGCPProjectEdgeScopesRejectsCrossScopeAndMissingMetadata(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		edge Edge
		want string
	}{
		{
			name: "source from another project",
			edge: gcpScopeEdge("project-02", "project-04", "gcp:project:project-04:seed:4580"),
			want: "source account_id",
		},
		{
			name: "target from another project",
			edge: gcpScopeEdge("project-04", "project-02", "gcp:project:project-04:seed:4580"),
			want: "target account_id",
		},
		{
			name: "missing relationship scope",
			edge: gcpScopeEdge("project-04", "project-04", ""),
			want: "scope_id",
		},
		{
			name: "malformed relationship scope",
			edge: gcpScopeEdge("project-04", "project-04", "project-04"),
			want: "unexpected fixture scope_id",
		},
		{
			name: "missing source account",
			edge: gcpScopeEdge("", "project-04", "gcp:project:project-04:seed:4580"),
			want: "source account_id",
		},
		{
			name: "wrong producer",
			edge: func() Edge {
				edge := gcpScopeEdge("project-04", "project-04", "gcp:project:project-04:seed:4580")
				edge.Props["evidence_source"] = "projector/canonical"
				return edge
			}(),
			want: "evidence_source",
		},
		{
			name: "source is not a cloud resource",
			edge: func() Edge {
				edge := gcpScopeEdge("project-04", "project-04", "gcp:project:project-04:seed:4580")
				edge.FromLabels = []string{"Repository"}
				return edge
			}(),
			want: "source labels",
		},
		{
			name: "target is not a cloud resource",
			edge: func() Edge {
				edge := gcpScopeEdge("project-04", "project-04", "gcp:project:project-04:seed:4580")
				edge.ToLabels = []string{"Repository"}
				return edge
			}(),
			want: "target labels",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := ValidateGCPProjectEdgeScopes(
				context.Background(),
				fakeReader{edges: []Edge{test.edge}},
				gcpProjectExpectations("gcp:project:project-04:seed:4580", "project-04", 1),
			)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("ValidateGCPProjectEdgeScopes() error = %v, want text %q", err, test.want)
			}
		})
	}
}

func TestValidateGCPProjectEdgeScopesRejectsCountMismatch(t *testing.T) {
	t.Parallel()

	scopeID := "gcp:project:project-04:seed:4580"
	_, err := ValidateGCPProjectEdgeScopes(
		context.Background(),
		fakeReader{edges: []Edge{gcpScopeEdge("project-04", "project-04", scopeID)}},
		gcpProjectExpectations(scopeID, "project-04", 2),
	)
	if err == nil || !strings.Contains(err.Error(), "edge count = 1, want 2") {
		t.Fatalf("count mismatch error = %v", err)
	}
}

func gcpProjectExpectations(scopeID, projectID string, edgeCount int) map[string]GCPProjectScopeExpectation {
	return map[string]GCPProjectScopeExpectation{
		scopeID: {ProjectID: projectID, EdgeCount: edgeCount},
	}
}

func gcpScopeEdge(sourceAccount, targetAccount, scopeID string) Edge {
	props := map[string]any{}
	if scopeID != "" {
		props["scope_id"] = scopeID
	}
	props["evidence_source"] = "reducer/gcp-relationships"
	return Edge{
		Type:       "GCP_synthetic_contained_in",
		FromLabels: []string{"CloudResource"},
		FromProps:  map[string]any{"account_id": sourceAccount},
		ToLabels:   []string{"CloudResource"},
		ToProps:    map[string]any{"account_id": targetAccount},
		Props:      props,
	}
}
