// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inventory_test

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/infra/inventory"
)

// Live proof for the #6843 fact aggregates. Seeds admission, EC2 posture,
// state resource, and provider binding facts plus adversarial rows
// (tombstones, superseded generations, blank addresses, invalid and duplicate
// bindings, identity-less and duplicate EC2 facts) and asserts CountBuckets
// and DimensionBuckets return exactly the live-truth buckets.
//
// Run with:
//
//	ESHU_POSTGRES_DSN=postgresql://eshu:change-me@localhost:<port>/eshu \
//	  go test ./internal/storage/postgres/infra/inventory -run TestGraphOnlyFactsLive -count=1 -race -v
func TestGraphOnlyFactsLive(t *testing.T) {
	sqlDB, ctx := liveDB(t)
	suffix := fmt.Sprintf("6843-live-%d", time.Now().UnixNano())
	scopeID := "scope-" + suffix
	genActive := "gen-active-" + suffix
	genOld := "gen-old-" + suffix
	now := time.Now().UTC()

	// The Reader has no scope selector: AllCategories reads every scope's
	// active generation, so a shared database (gate corpus, sibling runs)
	// contributes rows outside this fixture. Baseline every read before
	// seeding and assert the seeded delta: on a fresh CI database the
	// baselines are empty and the assertions are absolute.
	db := postgres.SQLDB{DB: sqlDB}
	unscoped := inventory.Filter{
		Labels:        []string{"CloudResource", "TerraformStateResource"},
		AllCategories: true,
	}
	readCounts := func(filter inventory.Filter) map[inventory.CountBucket]int64 {
		t.Helper()
		buckets, err := inventory.CountBuckets(ctx, db, filter)
		if err != nil {
			t.Fatalf("CountBuckets() error = %v", err)
		}
		out := make(map[inventory.CountBucket]int64, len(buckets))
		for _, b := range buckets {
			out[inventory.CountBucket{Label: b.Label, Provider: b.Provider, Environment: b.Environment}] += b.Count
		}
		return out
	}
	readDimension := func(filter inventory.Filter, dimension inventory.Dimension) map[string]int64 {
		t.Helper()
		got, err := inventory.DimensionBuckets(ctx, db, filter, dimension)
		if err != nil {
			t.Fatalf("DimensionBuckets() error = %v", err)
		}
		return got
	}
	baseCounts := readCounts(unscoped)
	baseProvider := readDimension(unscoped, inventory.DimensionProvider)
	baseService := readDimension(unscoped, inventory.DimensionResourceService)
	baseLabel := readDimension(unscoped, inventory.DimensionLabel)
	awsFilter := unscoped
	awsFilter.Provider = "aws"
	baseAWS := readDimension(awsFilter, inventory.DimensionLabel)
	kindFilter := inventory.Filter{Labels: []string{"CloudResource"}, AllCategories: true, Kind: "ec2"}
	baseKind := readDimension(kindFilter, inventory.DimensionLabel)
	envFilter := unscoped
	envFilter.Environment = "prod"
	baseEnv := readDimension(envFilter, inventory.DimensionLabel)

	seedScope(t, ctx, sqlDB, scopeID, genActive, genOld, now)
	seedGraphOnlyFacts(t, ctx, sqlDB, scopeID, genActive, genOld, now)

	deltaCounts := func(got map[inventory.CountBucket]int64) []inventory.CountBucket {
		t.Helper()
		var out []inventory.CountBucket
		for bucket, count := range got {
			delta := count - baseCounts[bucket]
			if delta != 0 {
				out = append(out, inventory.CountBucket{
					Label: bucket.Label, Provider: bucket.Provider,
					Environment: bucket.Environment, Count: delta,
				})
			}
		}
		sort.Slice(out, func(i, j int) bool {
			if out[i].Label != out[j].Label {
				return out[i].Label < out[j].Label
			}
			if out[i].Provider != out[j].Provider {
				return out[i].Provider < out[j].Provider
			}
			return out[i].Environment < out[j].Environment
		})
		return out
	}
	deltaDimension := func(got, base map[string]int64) map[string]int64 {
		t.Helper()
		out := map[string]int64{}
		for bucket, count := range got {
			if delta := count - base[bucket]; delta != 0 {
				out[bucket] = delta
			}
		}
		return out
	}

	t.Run("count", func(t *testing.T) {
		got := deltaCounts(readCounts(unscoped))
		want := []inventory.CountBucket{
			{Label: "CloudResource", Provider: "aws", Environment: "unknown", Count: 2},
			{Label: "CloudResource", Provider: "gcp", Environment: "unknown", Count: 1},
			{Label: "TerraformStateResource", Provider: "aws", Environment: "unknown", Count: 1},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("CountBuckets() delta:\n got %+v\nwant %+v", got, want)
		}
	})

	t.Run("provider dimension", func(t *testing.T) {
		got := deltaDimension(readDimension(unscoped, inventory.DimensionProvider), baseProvider)
		want := map[string]int64{"aws": 3, "gcp": 1}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("provider buckets delta = %v, want %v", got, want)
		}
	})

	t.Run("service dimension", func(t *testing.T) {
		got := deltaDimension(readDimension(unscoped, inventory.DimensionResourceService), baseService)
		want := map[string]int64{"ec2": 2, "gce": 1, "unknown": 1}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("service buckets delta = %v, want %v", got, want)
		}
	})

	t.Run("label dimension", func(t *testing.T) {
		got := deltaDimension(readDimension(unscoped, inventory.DimensionLabel), baseLabel)
		want := map[string]int64{"CloudResource": 3, "TerraformStateResource": 1}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("label buckets delta = %v, want %v", got, want)
		}
	})

	t.Run("provider filter", func(t *testing.T) {
		got := deltaDimension(readDimension(awsFilter, inventory.DimensionLabel), baseAWS)
		want := map[string]int64{"CloudResource": 2, "TerraformStateResource": 1}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("provider-filtered buckets delta = %v, want %v", got, want)
		}
	})

	t.Run("kind filter service arm", func(t *testing.T) {
		got := deltaDimension(readDimension(kindFilter, inventory.DimensionLabel), baseKind)
		want := map[string]int64{"CloudResource": 2}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("kind-filtered buckets delta = %v, want %v", got, want)
		}
	})

	t.Run("environment filter matches nothing", func(t *testing.T) {
		got := deltaDimension(readDimension(envFilter, inventory.DimensionLabel), baseEnv)
		if len(got) != 0 {
			t.Fatalf("environment-filtered buckets delta = %v, want empty (no node carries environment)", got)
		}
	})
}

func seedScope(t *testing.T, ctx context.Context, sqlDB *sql.DB, scopeID, genActive, genOld string, now time.Time) {
	t.Helper()
	if _, err := sqlDB.ExecContext(ctx, `
		INSERT INTO ingestion_scopes
		  (scope_id, scope_kind, source_system, source_key, collector_kind,
		   partition_key, observed_at, ingested_at, status, active_generation_id, payload)
		VALUES ($1::text, 'aws_account', 'aws', $1::text, 'aws', $1::text, $2, $2, 'active', $3::text,
		        jsonb_build_object('scope_id', $1::text))
		ON CONFLICT (scope_id) DO NOTHING`,
		scopeID, now, genActive,
	); err != nil {
		t.Fatalf("seed ingestion_scopes: %v", err)
	}
	gens := []struct {
		id     string
		status string
	}{
		{genActive, "active"},
		{genOld, "superseded"},
	}
	for _, gen := range gens {
		if _, err := sqlDB.ExecContext(ctx, `
			INSERT INTO scope_generations
			  (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
			VALUES ($1, $2, 'manual', $3, $3, $4, $3)
			ON CONFLICT (generation_id) DO NOTHING`,
			gen.id, scopeID, now, gen.status,
		); err != nil {
			t.Fatalf("seed scope_generations: %v", err)
		}
	}
}

func seedFact(t *testing.T, ctx context.Context, sqlDB *sql.DB, factID, scopeID, genID, kind, sourceSystem, payload string, observed time.Time, tombstone bool) {
	t.Helper()
	if _, err := sqlDB.ExecContext(ctx, `
		INSERT INTO fact_records
		  (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
		   source_fact_key, observed_at, ingested_at, is_tombstone, payload)
		VALUES ($1, $2, $3, $4, $1, $5, $1, $6, $6, $7, $8::jsonb)`,
		factID, scopeID, genID, kind, sourceSystem, observed, tombstone, payload,
	); err != nil {
		t.Fatalf("seed fact %s: %v", factID, err)
	}
}

func seedGraphOnlyFacts(t *testing.T, ctx context.Context, sqlDB *sql.DB, scopeID, genActive, genOld string, now time.Time) {
	t.Helper()
	earlier := now.Add(-time.Hour)

	// Identity keys carry the scope suffix: cloud and EC2 winners dedup
	// GLOBALLY by uid/instance (not per scope), so fixed keys from an
	// earlier run on a shared database would collide with this run's rows
	// and contribute zero delta. Rows that must collide within the run
	// (duplicate observations, superseded generations) share the suffixed
	// key deliberately.
	uidA1 := "uid-a1-" + scopeID
	uidA2 := "uid-a2-" + scopeID
	arnA1 := "arn:aws:ec2:us-east-1:111:instance/i-a1-" + scopeID
	frnA2 := "//compute.googleapis.com/projects/p/zones/z/instances/i-a2-" + scopeID
	instE1 := "i-e1-" + scopeID
	addrR1 := "aws_instance.web-" + scopeID

	// Admission rows: uid-a1 (aws instance with a provider fact carrying the
	// service), uid-a2 (gcp with a provider fact).
	seedFact(t, ctx, sqlDB, "adm-a1-"+scopeID, scopeID, genActive,
		"reducer_cloud_resource_identity", "aws",
		`{"cloud_resource_uid":"`+uidA1+`","resource_type":"aws_instance","raw_identity":"`+arnA1+`","provider":"aws"}`,
		now, false)
	seedFact(t, ctx, sqlDB, "adm-a2-"+scopeID, scopeID, genActive,
		"reducer_cloud_resource_identity", "gcp",
		`{"cloud_resource_uid":"`+uidA2+`","resource_type":"gce_instance","raw_identity":"`+frnA2+`","provider":"gcp"}`,
		now, false)
	// Provider facts join per provider identity key.
	seedFact(t, ctx, sqlDB, "prov-a1-"+scopeID, scopeID, genActive,
		"aws_resource", "aws",
		`{"arn":"`+arnA1+`","service_kind":"ec2"}`,
		now, false)
	seedFact(t, ctx, sqlDB, "prov-a2-"+scopeID, scopeID, genActive,
		"gcp_cloud_resource", "gcp",
		`{"full_resource_name":"`+frnA2+`","service_kind":"gce"}`,
		now, false)
	// EC2 posture row: duplicate observations collapse to one node with the
	// later observation winning.
	seedFact(t, ctx, sqlDB, "ec2-e1-old-"+scopeID, scopeID, genActive,
		"ec2_instance_posture", "aws",
		`{"account_id":"111","region":"us-east-1","instance_id":"`+instE1+`","service_kind":"ec2"}`,
		earlier, false)
	seedFact(t, ctx, sqlDB, "ec2-e1-new-"+scopeID, scopeID, genActive,
		"ec2_instance_posture", "aws",
		`{"account_id":"111","region":"us-east-1","instance_id":"`+instE1+`","service_kind":"ec2"}`,
		now, false)
	// State resource with a binding; the duplicate later binding loses
	// (first by fact id wins) and the invalid one never joins.
	seedFact(t, ctx, sqlDB, "tsr-r1-"+scopeID, scopeID, genActive,
		"terraform_state_resource", "tfstate",
		`{"address":"`+addrR1+`","type":"aws_instance","name":"web"}`,
		now, false)
	seedFact(t, ctx, sqlDB, "bind-r1-a-"+scopeID, scopeID, genActive,
		"terraform_state_provider_binding", "tfstate",
		`{"resource_address":"`+addrR1+`","provider_address":"registry.terraform.io/hashicorp/aws","provider_type":"aws"}`,
		now, false)
	seedFact(t, ctx, sqlDB, "bind-r1-b-"+scopeID, scopeID, genActive,
		"terraform_state_provider_binding", "tfstate",
		`{"resource_address":"`+addrR1+`","provider_address":"registry.terraform.io/hashicorp/azurerm","provider_type":"azurerm"}`,
		now.Add(time.Minute), false)
	seedFact(t, ctx, sqlDB, "bind-r1-bad-"+scopeID, scopeID, genActive,
		"terraform_state_provider_binding", "tfstate",
		`{"resource_address":"`+addrR1+`","provider_address":"","provider_type":"bad"}`,
		now, false)

	// Adversarial rows, all excluded.
	seedFact(t, ctx, sqlDB, "adm-tomb-"+scopeID, scopeID, genActive,
		"reducer_cloud_resource_identity", "aws",
		`{"cloud_resource_uid":"uid-tomb-`+scopeID+`","resource_type":"aws_instance","raw_identity":"arn:aws:ec2:us-east-1:111:instance/i-tomb-`+scopeID+`"}`,
		now, true)
	seedFact(t, ctx, sqlDB, "adm-old-"+scopeID, scopeID, genOld,
		"reducer_cloud_resource_identity", "aws",
		`{"cloud_resource_uid":"uid-old-`+scopeID+`","resource_type":"aws_instance","raw_identity":"arn:aws:ec2:us-east-1:111:instance/i-old-`+scopeID+`"}`,
		earlier, false)
	seedFact(t, ctx, sqlDB, "ec2-noid-"+scopeID, scopeID, genActive,
		"ec2_instance_posture", "aws",
		`{"account_id":"111","region":"us-east-1","service_kind":"ec2"}`,
		now, false)
	seedFact(t, ctx, sqlDB, "ec2-tomb-"+scopeID, scopeID, genActive,
		"ec2_instance_posture", "aws",
		`{"account_id":"111","region":"us-east-1","instance_id":"i-tomb-`+scopeID+`","service_kind":"ec2"}`,
		now, true)
	seedFact(t, ctx, sqlDB, "tsr-blank-"+scopeID, scopeID, genActive,
		"terraform_state_resource", "tfstate",
		`{"address":"   ","type":"aws_instance"}`,
		now, false)
	seedFact(t, ctx, sqlDB, "tsr-tomb-"+scopeID, scopeID, genActive,
		"terraform_state_resource", "tfstate",
		`{"address":"aws_instance.gone-`+scopeID+`","type":"aws_instance"}`,
		now, true)
	seedFact(t, ctx, sqlDB, "tsr-old-"+scopeID, scopeID, genOld,
		"terraform_state_resource", "tfstate",
		`{"address":"aws_instance.old-`+scopeID+`","type":"aws_instance"}`,
		earlier, false)
}
