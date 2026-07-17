// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestEdgeResourcesFromCloudResourcesFiltersToEdgeTypes(t *testing.T) {
	t.Parallel()

	cloudResources := []map[string]any{
		{"id": "lb-1", "name": "public-alb", "resource_type": "aws_elbv2_load_balancer"},
		{"id": "cf-1", "name": "cdn", "kind": "aws_cloudfront_distribution"},
		{"id": "db-1", "name": "orders-db", "resource_type": "aws_rds_db_instance"},
		// Duplicate id is de-duplicated.
		{"id": "lb-1", "name": "public-alb", "resource_type": "aws_elbv2_load_balancer"},
		// No identity is dropped.
		{"name": "", "resource_type": "aws_elbv2_load_balancer"},
	}

	edges := edgeResourcesFromCloudResources(cloudResources)
	if got, want := len(edges), 2; got != want {
		t.Fatalf("len(edges) = %d, want %d (%#v)", got, want, edges)
	}
	// Sorted by name: cdn (cf-1), public-alb (lb-1).
	if edges[0].id != "cf-1" || edges[1].id != "lb-1" {
		t.Fatalf("edge order = %q,%q, want cf-1,lb-1", edges[0].id, edges[1].id)
	}
}

func TestBuildIngressPostureUnprovenWithNoEdges(t *testing.T) {
	t.Parallel()

	posture := buildIngressPosture(nil, nil)
	if got := StringVal(posture, "waf_coverage"); got != ingressPostureUnproven {
		t.Fatalf("waf_coverage = %q, want %q", got, ingressPostureUnproven)
	}
	if got := StringVal(posture, "tls_termination"); got != ingressPostureUnproven {
		t.Fatalf("tls_termination = %q, want %q", got, ingressPostureUnproven)
	}
	if got := IntVal(posture, "edge_count"); got != 0 {
		t.Fatalf("edge_count = %d, want 0", got)
	}
}

func TestBuildIngressPostureProtectedAndTerminated(t *testing.T) {
	t.Parallel()

	edges := []ingressEdgeResource{
		{id: "lb-1", name: "public-alb", resourceType: "aws_elbv2_load_balancer"},
	}
	protection := map[string]ingressEdgeProtection{
		// collectorPresent=true: graph returned a row; flags are genuine observations.
		"lb-1": {collectorPresent: true, wafProtected: true, tlsTerminated: true},
	}
	posture := buildIngressPosture(edges, protection)
	if got := StringVal(posture, "waf_coverage"); got != ingressPostureProtected {
		t.Fatalf("waf_coverage = %q, want %q", got, ingressPostureProtected)
	}
	if got := StringVal(posture, "tls_termination"); got != ingressPostureTerminated {
		t.Fatalf("tls_termination = %q, want %q", got, ingressPostureTerminated)
	}
	if got := IntVal(posture, "waf_protected"); got != 1 {
		t.Fatalf("waf_protected = %d, want 1", got)
	}
}

func TestBuildIngressPostureUnprotectedWhenEdgeMissingProtection(t *testing.T) {
	t.Parallel()

	edges := []ingressEdgeResource{
		{id: "lb-1", name: "covered", resourceType: "aws_elbv2_load_balancer"},
		{id: "lb-2", name: "exposed", resourceType: "aws_elbv2_load_balancer"},
	}
	protection := map[string]ingressEdgeProtection{
		// lb-1: collector ran, WAF and TLS present.
		"lb-1": {collectorPresent: true, wafProtected: true, tlsTerminated: true},
		// lb-2: collector ran and returned a row, but no protection edge exists
		// (observed-negative, not collector-absent).
		"lb-2": {collectorPresent: true, wafProtected: false, tlsTerminated: false},
	}
	posture := buildIngressPosture(edges, protection)
	// One of two edges observed-negative rolls the tile to the negative state.
	if got := StringVal(posture, "waf_coverage"); got != ingressPostureUnprotected {
		t.Fatalf("waf_coverage = %q, want %q", got, ingressPostureUnprotected)
	}
	if got := StringVal(posture, "tls_termination"); got != ingressPostureNotTerminated {
		t.Fatalf("tls_termination = %q, want %q", got, ingressPostureNotTerminated)
	}
	if got := IntVal(posture, "waf_protected"); got != 1 {
		t.Fatalf("waf_protected = %d, want 1", got)
	}
	edgeRows := mapSliceValue(posture, "edges")
	if len(edgeRows) != 2 {
		t.Fatalf("len(edges) = %d, want 2", len(edgeRows))
	}
}

// TestBuildIngressPostureUnprovenWhenCollectorAbsent proves that when an
// internet-facing edge resource is materialized but the AWS collector slice has
// not yet been collected (no graph row returned), the tile reports "unproven"
// rather than "unprotected". Absence of collector ≠ absence of protection.
func TestBuildIngressPostureUnprovenWhenCollectorAbsent(t *testing.T) {
	t.Parallel()

	edges := []ingressEdgeResource{
		{id: "lb-1", name: "public-alb", resourceType: "aws_elbv2_load_balancer"},
	}
	// Empty protection map: graph returned no row for lb-1 (collector absent).
	protection := map[string]ingressEdgeProtection{}
	posture := buildIngressPosture(edges, protection)
	if got := StringVal(posture, "waf_coverage"); got != ingressPostureUnproven {
		t.Fatalf("waf_coverage = %q, want %q (collector-absent resource must not report observed-negative)", got, ingressPostureUnproven)
	}
	if got := StringVal(posture, "tls_termination"); got != ingressPostureUnproven {
		t.Fatalf("tls_termination = %q, want %q (collector-absent resource must not report observed-negative)", got, ingressPostureUnproven)
	}
	edgeRows := mapSliceValue(posture, "edges")
	if len(edgeRows) != 1 {
		t.Fatalf("len(edges) = %d, want 1", len(edgeRows))
	}
	if got := StringVal(edgeRows[0], "waf_coverage"); got != ingressPostureUnproven {
		t.Fatalf("edge waf_coverage = %q, want %q", got, ingressPostureUnproven)
	}
}

// TestBuildIngressPostureMixedCollectorPresentAndAbsent proves that when some
// edges have been collected (observed-negative) and others have not (collector
// absent), the rolled-up tile is unproven, not unprotected. An unproven edge
// beats an observed-negative edge so a partially-collected stack stays honest.
func TestBuildIngressPostureMixedCollectorPresentAndAbsent(t *testing.T) {
	t.Parallel()

	edges := []ingressEdgeResource{
		{id: "lb-1", name: "covered", resourceType: "aws_elbv2_load_balancer"},
		{id: "cf-1", name: "cdn", resourceType: "aws_cloudfront_distribution"},
	}
	protection := map[string]ingressEdgeProtection{
		// lb-1: collector ran, no protection found (observed-negative).
		"lb-1": {collectorPresent: true, wafProtected: false, tlsTerminated: false},
		// cf-1: collector has not yet been collected (absent from graph rows).
	}
	posture := buildIngressPosture(edges, protection)
	// Any unproven edge must roll the tile to unproven, not to unprotected.
	if got := StringVal(posture, "waf_coverage"); got != ingressPostureUnproven {
		t.Fatalf("waf_coverage = %q, want %q (collector-absent edge must beat observed-negative)", got, ingressPostureUnproven)
	}
	if got := StringVal(posture, "tls_termination"); got != ingressPostureUnproven {
		t.Fatalf("tls_termination = %q, want %q (collector-absent edge must beat observed-negative)", got, ingressPostureUnproven)
	}
}

func TestLoadServiceIngressPostureRunsBoundedQuery(t *testing.T) {
	t.Parallel()

	cloudResources := []map[string]any{
		{"id": "lb-1", "name": "public-alb", "resource_type": "aws_elbv2_load_balancer"},
		{"id": "db-1", "name": "orders-db", "resource_type": "aws_rds_db_instance"},
	}

	var capturedIDs []string
	graph := fakeGraphReader{
		run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
			ids, _ := params["edge_ids"].([]string)
			capturedIDs = ids
			// #5287: three single-clause set queries — base (edge exists), waf,
			// tls. lb-1 is observed present, WAF-protected, TLS NOT terminated.
			switch {
			case strings.Contains(cypher, "AWS_wafv2_web_acl_protects_resource"):
				return []map[string]any{{"edge_id": "lb-1"}}, nil // waf set
			case strings.Contains(cypher, "AWS_acm_certificate_used_by_resource"):
				return []map[string]any{}, nil // tls set: empty -> observed-negative
			default:
				return []map[string]any{{"edge_id": "lb-1"}}, nil // base set: lb-1 present
			}
		},
	}

	posture, err := loadServiceIngressPosture(context.Background(), graph, cloudResources)
	if err != nil {
		t.Fatalf("loadServiceIngressPosture() error = %v", err)
	}
	if len(capturedIDs) != 1 || capturedIDs[0] != "lb-1" {
		t.Fatalf("edge_ids = %#v, want [lb-1] (only the edge resource is queried)", capturedIDs)
	}
	// Graph returned a row with waf_protected=true: observed-positive.
	if got := StringVal(posture, "waf_coverage"); got != ingressPostureProtected {
		t.Fatalf("waf_coverage = %q, want %q", got, ingressPostureProtected)
	}
	// Graph returned a row with tls_terminated=false: observed-negative (not unproven).
	if got := StringVal(posture, "tls_termination"); got != ingressPostureNotTerminated {
		t.Fatalf("tls_termination = %q, want %q", got, ingressPostureNotTerminated)
	}
}

// TestLoadServiceIngressPostureUnprovenWhenGraphReturnsNoRow proves that when
// the graph query returns no row for an edge resource (AWS collector slice not
// yet collected), loadServiceIngressPosture reports unproven, not unprotected.
func TestLoadServiceIngressPostureUnprovenWhenGraphReturnsNoRow(t *testing.T) {
	t.Parallel()

	cloudResources := []map[string]any{
		{"id": "lb-1", "name": "public-alb", "resource_type": "aws_elbv2_load_balancer"},
	}
	graph := fakeGraphReader{
		run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
			// Graph returns zero rows: lb-1 is a known edge resource but the
			// AWS collector slice for it has not yet been collected.
			return []map[string]any{}, nil
		},
	}
	posture, err := loadServiceIngressPosture(context.Background(), graph, cloudResources)
	if err != nil {
		t.Fatalf("loadServiceIngressPosture() error = %v", err)
	}
	if got := StringVal(posture, "waf_coverage"); got != ingressPostureUnproven {
		t.Fatalf("waf_coverage = %q, want %q (no graph row = collector absent, not observed-negative)", got, ingressPostureUnproven)
	}
	if got := StringVal(posture, "tls_termination"); got != ingressPostureUnproven {
		t.Fatalf("tls_termination = %q, want %q (no graph row = collector absent, not observed-negative)", got, ingressPostureUnproven)
	}
}

func TestLoadServiceIngressPostureSkipsQueryWithoutEdges(t *testing.T) {
	t.Parallel()

	graph := fakeGraphReader{
		run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
			t.Fatal("graph query must not run when there is no edge resource")
			return nil, nil
		},
	}
	posture, err := loadServiceIngressPosture(context.Background(), graph, []map[string]any{
		{"id": "db-1", "name": "orders-db", "resource_type": "aws_rds_db_instance"},
	})
	if err != nil {
		t.Fatalf("loadServiceIngressPosture() error = %v", err)
	}
	if got := StringVal(posture, "waf_coverage"); got != ingressPostureUnproven {
		t.Fatalf("waf_coverage = %q, want %q", got, ingressPostureUnproven)
	}
}

func TestLoadServiceIngressPosturePropagatesGraphError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("graph down")
	graph := fakeGraphReader{
		run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
			return nil, wantErr
		},
	}
	_, err := loadServiceIngressPosture(context.Background(), graph, []map[string]any{
		{"id": "lb-1", "name": "public-alb", "resource_type": "aws_elbv2_load_balancer"},
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("loadServiceIngressPosture() error = %v, want %v", err, wantErr)
	}
}

// TestIngressPostureQueriesAreSingleClause guards the #5287 fix: each ingress
// posture query must be a single-clause set query (no OPTIONAL MATCH / WITH /
// aggregate) — the pinned NornicDB build mis-executes the prior
// aggregate-over-OPTIONAL-MATCH form (null edge_id, both flags true even for an
// unprotected edge).
func TestIngressPostureQueriesAreSingleClause(t *testing.T) {
	t.Parallel()

	for name, q := range map[string]string{
		"base": ingressPostureBaseCypher,
		"waf":  ingressPostureWafCypher,
		"tls":  ingressPostureTLSCypher,
	} {
		if strings.Contains(q, "OPTIONAL MATCH") {
			t.Errorf("%s ingress query must not use OPTIONAL MATCH (multi-clause defect): %s", name, q)
		}
		if strings.Contains(q, "WITH ") || strings.Contains(q, "count(") {
			t.Errorf("%s ingress query must not aggregate in a multi-clause projection: %s", name, q)
		}
		if strings.Count(q, "MATCH ") != 1 {
			t.Errorf("%s ingress query must be single-clause (one MATCH): %s", name, q)
		}
		if !strings.Contains(q, "RETURN DISTINCT") {
			t.Errorf("%s ingress query must RETURN DISTINCT the edge id set: %s", name, q)
		}
	}
}
