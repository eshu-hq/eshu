// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inventory_test

import (
	"context"
	"database/sql"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/infra/inventory"
)

// readerFixture seeds one isolated repository and returns a filter scope that
// only sees it, so reader assertions are exact in a shared database.
func readerFixture(t *testing.T, ctx context.Context, sqlDB *sql.DB) string {
	t.Helper()
	repo := uniqueRepo(t)
	putContent(t, ctx, sqlDB, repo,
		contentRow{
			"r1", "a.tf", "TerraformResource", "r1",
			`{"provider":"aws","environment":"prod","resource_service":"s3","resource_category":"storage","resource_type":"aws_s3_bucket"}`,
		},
		contentRow{
			"r2", "a.tf", "TerraformResource", "r2",
			`{"provider":"aws","resource_type":"aws_iam_role"}`,
		},
		contentRow{"d1", "a.tf", "TerraformDataSource", "d1", `{"data_type":"aws_iam_role","provider":"aws"}`},
		contentRow{"k1", "k.yaml", "K8sResource", "k1", `{"kind":"Deployment","environment":"prod"}`},
		contentRow{"v1", "v.tf", "TerraformVariable", "v1", `{}`},
		contentRow{"x1", "x.yaml", "CrossplaneXRD", "x1", `{"kind":"XDatabase","service_kind":"rds"}`},
	)
	if _, err := inventory.MirrorPaths(ctx, postgres.SQLDB{DB: sqlDB}, inventory.Target{RepoID: repo},
		[]string{"a.tf", "k.yaml", "v.tf", "x.yaml"}); err != nil {
		t.Fatalf("MirrorPaths() error = %v", err)
	}
	return repo
}

func TestReaderLiveCountBucketsGroupsLabelProviderEnvironment(t *testing.T) {
	sqlDB, ctx := liveDB(t)
	repo := readerFixture(t, ctx, sqlDB)
	database := postgres.SQLDB{DB: sqlDB}

	buckets, err := inventory.CountBuckets(ctx, database,
		inventory.Filter{Labels: inventory.Labels, AllCategories: true}.ForRepositories(repo))
	if err != nil {
		t.Fatalf("CountBuckets() error = %v", err)
	}
	got := map[inventory.CountBucket]int64{}
	for _, b := range buckets {
		got[inventory.CountBucket{Label: b.Label, Provider: b.Provider, Environment: b.Environment}] += b.Count
	}
	want := map[inventory.CountBucket]int64{
		{Label: "TerraformResource", Provider: "aws", Environment: "prod"}:        1,
		{Label: "TerraformResource", Provider: "aws", Environment: "unknown"}:     1,
		{Label: "TerraformDataSource", Provider: "aws", Environment: "unknown"}:   1,
		{Label: "K8sResource", Provider: "unknown", Environment: "prod"}:          1,
		{Label: "TerraformVariable", Provider: "unknown", Environment: "unknown"}: 1,
		{Label: "CrossplaneXRD", Provider: "unknown", Environment: "unknown"}:     1,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("count buckets:\n got %v\nwant %v", got, want)
	}
}

func TestReaderLiveDimensionBucketsAndFiltersMirrorGraphClauses(t *testing.T) {
	sqlDB, ctx := liveDB(t)
	repo := readerFixture(t, ctx, sqlDB)
	database := postgres.SQLDB{DB: sqlDB}
	all := inventory.Filter{Labels: inventory.Labels, AllCategories: true}.ForRepositories(repo)

	cases := []struct {
		name   string
		filter inventory.Filter
		dim    inventory.Dimension
		want   map[string]int64
	}{
		{
			"service all-categories falls back to service_kind", all, inventory.DimensionResourceService,
			map[string]int64{"s3": 1, "rds": 1, "unknown": 4},
		},
		{"category", all, inventory.DimensionResourceCategory, map[string]int64{"storage": 1, "unknown": 5}},
		{"label", all, inventory.DimensionLabel, map[string]int64{
			"TerraformResource": 2, "TerraformDataSource": 1, "K8sResource": 1,
			"TerraformVariable": 1, "CrossplaneXRD": 1,
		}},
		{
			"kind all-categories matches kind, resource_type, data_type, service_kind",
			withKind(all, "aws_iam_role"), inventory.DimensionLabel,
			map[string]int64{"TerraformResource": 1, "TerraformDataSource": 1},
		},
		{
			"kind all-categories matches service_kind", withKind(all, "rds"), inventory.DimensionLabel,
			map[string]int64{"CrossplaneXRD": 1},
		},
		{
			"kind category-specific ignores service_kind",
			withKind(inventory.Filter{Labels: []string{"CrossplaneXRD"}}.ForRepositories(repo), "rds"),
			inventory.DimensionLabel,
			map[string]int64{},
		},
		{
			"resource_type matches data_type", withResourceType(all, "aws_iam_role"), inventory.DimensionProvider,
			map[string]int64{"aws": 2},
		},
		{
			"provider and environment", withProviderEnvironment(all, "aws", "prod"), inventory.DimensionLabel,
			map[string]int64{"TerraformResource": 1},
		},
		{
			"resource_service all-categories matches service_kind", withService(all, "rds"), inventory.DimensionLabel,
			map[string]int64{"CrossplaneXRD": 1},
		},
		{
			"resource_category", withCategory(all, "storage"), inventory.DimensionLabel,
			map[string]int64{"TerraformResource": 1},
		},
		{
			"empty label set reads nothing", inventory.Filter{}.ForRepositories(repo), inventory.DimensionLabel,
			map[string]int64{},
		},
	}
	for _, tc := range cases {
		got, err := inventory.DimensionBuckets(ctx, database, tc.filter, tc.dim)
		if err != nil {
			t.Fatalf("%s: DimensionBuckets() error = %v", tc.name, err)
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%s:\n got %v\nwant %v", tc.name, got, tc.want)
		}
	}
}

func withKind(f inventory.Filter, v string) inventory.Filter {
	f.Kind = v
	return f
}

func withResourceType(f inventory.Filter, v string) inventory.Filter {
	f.ResourceType = v
	return f
}

func withService(f inventory.Filter, v string) inventory.Filter {
	f.ResourceService = v
	return f
}

func withCategory(f inventory.Filter, v string) inventory.Filter {
	f.ResourceCategory = v
	return f
}

func withProviderEnvironment(f inventory.Filter, provider, environment string) inventory.Filter {
	f.Provider = provider
	f.Environment = environment
	return f
}
