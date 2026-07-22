// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// grantPostureStubGraph is an in-memory GraphQuery stub that records every
// Cypher statement and parameter bag the store sends and answers from a
// per-substring canned-row table. It also records whether the store bounded
// the read with a context deadline.
type grantPostureStubGraph struct {
	cyphers     []string
	params      []map[string]any
	hadDeadline bool
	rowsFor     func(cypher string) []map[string]any
	err         error
}

func (g *grantPostureStubGraph) Run(
	ctx context.Context, cypher string, params map[string]any,
) ([]map[string]any, error) {
	_, g.hadDeadline = ctx.Deadline()
	g.cyphers = append(g.cyphers, cypher)
	g.params = append(g.params, params)
	if g.err != nil {
		return nil, g.err
	}
	if g.rowsFor == nil {
		return nil, nil
	}
	return g.rowsFor(cypher), nil
}

func (g *grantPostureStubGraph) RunSingle(
	ctx context.Context, cypher string, params map[string]any,
) (map[string]any, error) {
	rows, err := g.Run(ctx, cypher, params)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}

func TestGraphSecretsIAMGrantPostureStoreSummarize(t *testing.T) {
	t.Parallel()

	graph := &grantPostureStubGraph{
		rowsFor: func(cypher string) []map[string]any {
			switch {
			case strings.Contains(cypher, "rel.grant_outcome"):
				return []map[string]any{
					{"bucket": "allowed", "bucket_count": int64(3)},
					{"bucket": "unknown", "bucket_count": int64(1)},
				}
			case strings.Contains(cypher, "rel.resolution_mode"):
				return []map[string]any{
					{"bucket": "exact_arn", "bucket_count": int64(2)},
					{"bucket": "unknown", "bucket_count": int64(2)},
				}
			case strings.Contains(cypher, "rel.is_public = true"):
				return []map[string]any{{"total": int64(1)}}
			case strings.Contains(cypher, "rel.is_cross_account = true"):
				return []map[string]any{{"total": int64(2)}}
			case strings.Contains(cypher, "rel.is_service_principal = true"):
				return []map[string]any{{"total": int64(1)}}
			}
			return nil
		},
	}
	store := NewGraphSecretsIAMGrantPostureStore(graph)

	posture, err := store.SummarizeS3ExternalPrincipalGrantPosture(context.Background(), "scope-1")
	if err != nil {
		t.Fatalf("SummarizeS3ExternalPrincipalGrantPosture: %v", err)
	}

	// TotalGrants is derived from the outcome grouping: grant_outcome is a
	// required edge property, so every edge lands in exactly one outcome bucket.
	if posture.TotalGrants != 4 {
		t.Fatalf("TotalGrants = %d, want 4", posture.TotalGrants)
	}
	if len(posture.GrantsByOutcome) != 2 || posture.GrantsByOutcome[0].Bucket != "allowed" || posture.GrantsByOutcome[0].Count != 3 {
		t.Fatalf("GrantsByOutcome = %+v", posture.GrantsByOutcome)
	}
	if len(posture.GrantsByResolutionMode) != 2 || posture.GrantsByResolutionMode[0].Bucket != "exact_arn" {
		t.Fatalf("GrantsByResolutionMode = %+v", posture.GrantsByResolutionMode)
	}
	if posture.PublicGrants != 1 || posture.CrossAccountGrants != 2 || posture.ServicePrincipalGrants != 1 {
		t.Fatalf("flag counts = %d/%d/%d, want 1/2/1",
			posture.PublicGrants, posture.CrossAccountGrants, posture.ServicePrincipalGrants)
	}

	if !graph.hadDeadline {
		t.Fatalf("store did not bound the graph read with a context deadline")
	}
	if len(graph.cyphers) != 5 {
		t.Fatalf("expected 5 bounded aggregate reads, got %d", len(graph.cyphers))
	}
	for i, params := range graph.params {
		if params["scope_id"] != "scope-1" {
			t.Fatalf("query %d scope_id = %v, want scope-1", i, params["scope_id"])
		}
	}
}

func TestGraphSecretsIAMGrantPostureStoreEmptyScopeYieldsZeroPosture(t *testing.T) {
	t.Parallel()

	store := NewGraphSecretsIAMGrantPostureStore(&grantPostureStubGraph{})
	posture, err := store.SummarizeS3ExternalPrincipalGrantPosture(context.Background(), "scope-empty")
	if err != nil {
		t.Fatalf("SummarizeS3ExternalPrincipalGrantPosture: %v", err)
	}
	if posture.TotalGrants != 0 || posture.PublicGrants != 0 ||
		len(posture.GrantsByOutcome) != 0 || len(posture.GrantsByResolutionMode) != 0 {
		t.Fatalf("empty scope posture = %+v, want zero posture", posture)
	}
}

func TestGraphSecretsIAMGrantPostureStoreRequiresGraphAndScope(t *testing.T) {
	t.Parallel()

	if _, err := (GraphSecretsIAMGrantPostureStore{}).SummarizeS3ExternalPrincipalGrantPosture(
		context.Background(), "scope-1",
	); err == nil || !strings.Contains(err.Error(), "graph is required") {
		t.Fatalf("nil-graph error = %v", err)
	}
	if _, err := NewGraphSecretsIAMGrantPostureStore(&grantPostureStubGraph{}).SummarizeS3ExternalPrincipalGrantPosture(
		context.Background(), "",
	); err == nil || !strings.Contains(err.Error(), "scope_id is required") {
		t.Fatalf("empty-scope error = %v", err)
	}
}

func TestGraphSecretsIAMGrantPostureStorePropagatesGraphError(t *testing.T) {
	t.Parallel()

	store := NewGraphSecretsIAMGrantPostureStore(&grantPostureStubGraph{err: fmt.Errorf("boom")})
	if _, err := store.SummarizeS3ExternalPrincipalGrantPosture(context.Background(), "scope-1"); err == nil ||
		!strings.Contains(err.Error(), "boom") {
		t.Fatalf("graph error = %v, want wrapped boom", err)
	}
}

func TestSecretsIAMGrantFlagCountRejectsOffAllowlistFlag(t *testing.T) {
	t.Parallel()

	// The flag property name is interpolated into the Cypher, so the allow-list
	// guard is the defense that keeps it injection-safe (mirrors the SQL bucket
	// field allow-list in secrets_iam_summary.go).
	if _, err := secretsIAMGrantFlagCountCypher("scope_id = '' OR true"); err == nil ||
		!strings.Contains(err.Error(), "unsupported grant flag") {
		t.Fatalf("off-allowlist flag error = %v", err)
	}
}

func TestSecretsIAMGrantPostureCyphersAreScopedBoundedAggregates(t *testing.T) {
	t.Parallel()

	shapes := []string{secretsIAMGrantsByOutcomeCypher, secretsIAMGrantsByResolutionModeCypher}
	for _, flag := range []string{"is_public", "is_cross_account", "is_service_principal"} {
		cypher, err := secretsIAMGrantFlagCountCypher(flag)
		if err != nil {
			t.Fatalf("secretsIAMGrantFlagCountCypher(%q): %v", flag, err)
		}
		shapes = append(shapes, cypher)
	}
	for _, cypher := range shapes {
		if !strings.Contains(cypher, "MATCH (:CloudResource)-[rel:GRANTS_ACCESS_TO]->(:ExternalPrincipal)") {
			t.Fatalf("cypher missing canonical edge anchor: %s", cypher)
		}
		if !strings.Contains(cypher, "rel.scope_id = $scope_id") {
			t.Fatalf("cypher missing scope bound: %s", cypher)
		}
		if !strings.Contains(cypher, "count(*)") {
			t.Fatalf("cypher is not an aggregate: %s", cypher)
		}
	}
	for _, grouped := range shapes[:2] {
		if !strings.Contains(grouped, "ORDER BY bucket ASC") {
			t.Fatalf("grouped cypher missing deterministic ordering: %s", grouped)
		}
	}
}
