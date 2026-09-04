// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

// TestImpactSeamCrossPackageAccess proves the #6060 impact seam is usable
// from outside package query -- the position the deployment-config-influence
// family will be in after the impact move. It constructs a
// TraceEnrichmentConfig through the exported constructor and reads
// DeploymentSourceResult / K8sResourceResult fields through the exported
// aliases and accessors, mirroring exactly what
// deployment_config_influence.go does today with the lowercase originals
// (traceEnrichmentConfig{maxDepth: 4}; .rows reads and writes; .limits,
// .imageRefs, .contentLowerBound, .candidates and
// .selectCandidatePoolTruncated reads). If any accessor stops delegating,
// this fails; if any seam name is removed, it fails to compile.
func TestImpactSeamCrossPackageAccess(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Constructor covers the only cross-set construction site
	// (traceEnrichmentConfig{maxDepth: 4}).
	cfg := query.NewTraceEnrichmentConfig(4)
	var zeroCfg query.TraceEnrichmentConfig
	gotTrace, gotErr := query.FetchServiceTraceContext(ctx, nil, nil, nil, "", cfg)
	wantTrace, wantErr := query.FetchServiceTraceContext(ctx, nil, nil, nil, "", zeroCfg)
	if gotErr != nil || wantErr != nil || !reflect.DeepEqual(gotTrace, wantTrace) {
		t.Fatalf("FetchServiceTraceContext(cfg) = (%v, %v), want same as zero config (%v, %v)",
			gotTrace, gotErr, wantTrace, wantErr)
	}

	// DeploymentSourceResult: rows write then rows/limits reads.
	rows := []map[string]any{{"id": "r:1"}}
	var src query.DeploymentSourceResult
	src.SetRows(rows)
	if !reflect.DeepEqual(src.Rows(), rows) {
		t.Fatalf("DeploymentSourceResult.Rows() = %v, want %v", src.Rows(), rows)
	}
	if src.Limits() != nil {
		t.Fatalf("DeploymentSourceResult.Limits() = %v, want nil", src.Limits())
	}
	filtered := query.FilterRowsByRepoIDForAccess(src.Rows(), query.RepositoryAccessFilter{AllScopes: true})
	src.SetRows(filtered)
	if !reflect.DeepEqual(src.Rows(), filtered) {
		t.Fatalf("DeploymentSourceResult.Rows() after SetRows = %v, want %v", src.Rows(), filtered)
	}

	// K8sResourceResult: every field the cross-set reader touches.
	contentRows := []map[string]any{{"id": "k:1", "container_images": []any{"img:1"}}}
	k8s := query.BoundedK8sResourceResult(contentRows, true, nil, false, true)
	if len(k8s.Rows()) == 0 {
		t.Fatal("K8sResourceResult.Rows() is empty, want merged content rows")
	}
	if k8s.Limits() == nil {
		t.Fatal("K8sResourceResult.Limits() is nil, want limits map")
	}
	if !reflect.DeepEqual(k8s.ImageRefs(), []string{"img:1"}) {
		t.Fatalf("K8sResourceResult.ImageRefs() = %v, want [img:1]", k8s.ImageRefs())
	}
	if len(k8s.Candidates()) == 0 {
		t.Fatal("K8sResourceResult.Candidates() is empty, want merged candidates")
	}
	if !k8s.ContentLowerBound() {
		t.Fatal("K8sResourceResult.ContentLowerBound() = false, want true")
	}
	if !k8s.SelectCandidatePoolTruncated() {
		t.Fatal("K8sResourceResult.SelectCandidatePoolTruncated() = false, want true")
	}
}
