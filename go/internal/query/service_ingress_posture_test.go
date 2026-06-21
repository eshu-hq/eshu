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
		"lb-1": {wafProtected: true, tlsTerminated: true},
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
		"lb-1": {wafProtected: true, tlsTerminated: true},
		// lb-2 has no protection edge -> observed-negative for both.
	}
	posture := buildIngressPosture(edges, protection)
	// One of two edges uncovered rolls the tile to the negative state.
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

func TestLoadServiceIngressPostureRunsBoundedQuery(t *testing.T) {
	t.Parallel()

	cloudResources := []map[string]any{
		{"id": "lb-1", "name": "public-alb", "resource_type": "aws_elbv2_load_balancer"},
		{"id": "db-1", "name": "orders-db", "resource_type": "aws_rds_db_instance"},
	}

	var capturedIDs []string
	graph := fakeGraphReader{
		run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
			if !strings.Contains(cypher, "AWS_wafv2_web_acl_protects_resource") {
				t.Fatalf("unexpected cypher: %s", cypher)
			}
			ids, _ := params["edge_ids"].([]string)
			capturedIDs = ids
			return []map[string]any{
				{"edge_id": "lb-1", "waf_protected": true, "tls_terminated": false},
			}, nil
		},
	}

	posture, err := loadServiceIngressPosture(context.Background(), graph, cloudResources)
	if err != nil {
		t.Fatalf("loadServiceIngressPosture() error = %v", err)
	}
	if len(capturedIDs) != 1 || capturedIDs[0] != "lb-1" {
		t.Fatalf("edge_ids = %#v, want [lb-1] (only the edge resource is queried)", capturedIDs)
	}
	if got := StringVal(posture, "waf_coverage"); got != ingressPostureProtected {
		t.Fatalf("waf_coverage = %q, want %q", got, ingressPostureProtected)
	}
	if got := StringVal(posture, "tls_termination"); got != ingressPostureNotTerminated {
		t.Fatalf("tls_termination = %q, want %q", got, ingressPostureNotTerminated)
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
