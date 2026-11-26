// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// Live acceptance for #11535 P1-2: the full-production-projection first page
// on NornicDB 1.5.x stays inside the 10s production reader deadline.
// Measured 2026-11-26: 8.646s cold, 61ms warm over 150k nodes (untuned).
// Env-gated with the sibling scale proof; needs a disposable NornicDB
// backend (seed takes ~30m cold) and refuses a graph holding any other
// CloudResource nodes.

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

func TestCloudResourceOwnerBackfillFullProjectionMeetsDeadline11535(t *testing.T) {
	if strings.TrimSpace(os.Getenv(cloudResourceOwnerBackfillScaleLiveEnv)) == "" {
		t.Skip("set " + cloudResourceOwnerBackfillScaleLiveEnv + "=1 to run the live NornicDB full-projection proof")
	}
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		t.Fatal("ESHU_NEO4J_URI is required")
	}
	const total = 150_000

	ctx, cancel := context.WithTimeout(context.Background(), 55*time.Minute)
	defer cancel()
	driver, err := neo4jdriver.NewDriverWithContext(uri, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open NornicDB driver: %v", err)
	}
	defer func() { _ = driver.Close(context.Background()) }()
	driver11535SeedsFullProjection(t, ctx, driver, total)

	reader := NewNeo4jReader(driver, "nornic")

	started := time.Now()
	rows, err := reader.Run(ctx, cloudResourceOwnerBackfillPageQuery, map[string]any{"after_uid": "", "limit": 500})
	coldElapsed := time.Since(started)
	if err != nil {
		t.Fatalf("COLD first page (full projection, 500 rows over %d nodes): ERROR after %s: %v", total, coldElapsed, err)
	}
	t.Logf("COLD first page: %d rows over %d nodes in %s (deadline %s)", len(rows), total, coldElapsed, defaultGraphReadTimeout)
	if coldElapsed > defaultGraphReadTimeout {
		t.Fatalf("COLD first page took %s, over the %s production reader deadline", coldElapsed, defaultGraphReadTimeout)
	}

	warmCypher := fmt.Sprintf("/* warm-%d */ ", time.Now().UnixNano()) + cloudResourceOwnerBackfillPageQuery
	started = time.Now()
	rows, err = reader.Run(ctx, warmCypher, map[string]any{"after_uid": "", "limit": 500})
	warmElapsed := time.Since(started)
	if err != nil {
		t.Fatalf("WARM first page: ERROR after %s: %v", warmElapsed, err)
	}
	t.Logf("WARM first page: %d rows over %d nodes in %s", len(rows), total, warmElapsed)
}

func countRawNodes11535(t *testing.T, ctx context.Context, driver neo4jdriver.DriverWithContext, cypher string, params map[string]any) int {
	t.Helper()
	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeRead, DatabaseName: "nornic"})
	defer func() { _ = session.Close(ctx) }()
	result, err := session.Run(ctx, cypher, params)
	if err != nil {
		t.Fatalf("count nodes: %v", err)
	}
	records, err := result.Collect(ctx)
	if err != nil || len(records) == 0 {
		t.Fatalf("collect count: %v", err)
	}
	n, ok := records[0].Values[0].(int64)
	if !ok {
		t.Fatalf("count is %T, want int64", records[0].Values[0])
	}
	return int(n)
}

func writeFullRows11535(t *testing.T, ctx context.Context, driver neo4jdriver.DriverWithContext, rows []map[string]any) {
	t.Helper()
	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeWrite, DatabaseName: "nornic"})
	defer func() { _ = session.Close(ctx) }()
	_, err := session.ExecuteWrite(ctx, func(tx neo4jdriver.ManagedTransaction) (any, error) {
		result, err := tx.Run(ctx, `
UNWIND $rows AS row
CREATE (n:CloudResource)
SET n = row`, map[string]any{"rows": rows})
		if err != nil {
			return nil, err
		}
		return nil, result.Err()
	})
	if err != nil {
		t.Fatalf("seed full-projection rows: %v", err)
	}
}

func driver11535SeedsFullProjection(t *testing.T, ctx context.Context, driver neo4jdriver.DriverWithContext, total int) {
	t.Helper()
	seed := cloudResourceBackfillScaleSeed{t: t, ctx: ctx, driver: driver, total: total}
	seed.ensureSchema()

	countCypher := `MATCH (n:CloudResource) WHERE n.uid STARTS WITH $prefix RETURN count(n) AS count`
	mine := countRawNodes11535(t, ctx, driver, countCypher, map[string]any{"prefix": cloudResourceOwnerBackfillScalePrefix})
	if mine != total {
		if mine != 0 {
			t.Fatalf("graph holds %d prefixed nodes, want 0 or %d; use a fresh container", mine, total)
		}
		started := time.Now()
		for start := 0; start < total; start += cloudResourceOwnerBackfillScaleSeedBatch {
			end := min(start+cloudResourceOwnerBackfillScaleSeedBatch, total)
			rows := make([]map[string]any, 0, end-start)
			for i := start; i < end; i++ {
				uid := cloudResourceBackfillScaleUID(i)
				rows = append(rows, map[string]any{
					"uid":                         uid,
					"id":                          uid,
					"arn":                         fmt.Sprintf("arn:aws:ec2:us-east-1:123456789012:instance/i-%017d", i),
					"resource_id":                 fmt.Sprintf("i-%017d", i),
					"resource_type":               "aws_ec2_instance",
					"name":                        fmt.Sprintf("instance-%07d", i),
					"state":                       "running",
					"account_id":                  "123456789012",
					"region":                      "us-east-1",
					"service_kind":                "ec2",
					"correlation_anchors":         []any{"anchor-a", "anchor-b"},
					"service_anchor_status":       "anchored",
					"service_anchor_source":       "workload",
					"service_anchor_reason":       "selector",
					"service_anchor_names":        []any{"svc-a"},
					"service_anchor_name_tokens":  []any{"svc", "a"},
					"workload_id":                 fmt.Sprintf("workload-%07d", i),
					"service_name":                "svc-a",
					"imds_v2_required":            true,
					"imds_http_endpoint":          "enabled",
					"imds_http_put_hop_limit":     int64(2),
					"user_data_present":           false,
					"detailed_monitoring_enabled": true,
					"ebs_optimized":               true,
					"public_ip_associated":        false,
					"instance_profile_arn":        "arn:aws:iam::123456789012:instance-profile/p",
					"tenancy":                     "default",
					"nitro_enclave_enabled":       false,
					"source_fact_id":              fmt.Sprintf("fact-%07d", i),
					"stable_fact_key":             fmt.Sprintf("key-%07d", i),
					"source_system":               "aws",
					"source_record_id":            fmt.Sprintf("rec-%07d", i),
					"source_confidence":           "high",
					"collector_kind":              "aws",
					"evidence_source":             "describe-instances",
				})
			}
			writeFullRows11535(t, ctx, driver, rows)
			if end%25_000 == 0 || end == total {
				t.Logf("seeded %d/%d CloudResource nodes in %s", end, total, time.Since(started).Round(time.Second))
			}
		}
		t.Logf("seeded %d nodes in %s", total, time.Since(started).Round(time.Second))
	} else {
		t.Logf("reusing %d seeded CloudResource nodes", mine)
	}
}
