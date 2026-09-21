// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/infra/inventory"
)

// TestLiveInfraGraphOnlyLabelsMatchFactReadModel is the #6843 parity
// battery: the graph-only labels (CloudResource, TerraformStateResource)
// that the read model now serves from Postgres fact truth must bucket
// identically to the whole-label graph scan they replace.
//
// Graph side runs the REAL production write path -- CloudResource and EC2
// node writers plus CanonicalNodeWriter.Write for TerraformStateResource --
// against live NornicDB, then reads back with the production per-label
// aggregate query shape (infraResourceAggregatePerLabelCypher with the
// production provider/environment group expressions), restricted to this
// run's uids. PG side seeds the same logical fixture as fact_records rows
// (the shapes the collectors and the reducer emit, as pinned by
// TestGraphOnlyFactsLive) and reads through inventory.CountBuckets, the
// Reader the routes serve.
//
// Cross-side identity is tight where the pipeline joins tightly: the
// admission facts carry the same cloud_resource_uid values the graph rows
// use as node uids (uid-a1/uid-a2). The EC2 posture fact and the state
// resource fact are keyed by instance identity (i-e1) and resource address
// (aws_instance.web) respectively -- the same keys the extractor rows and
// projector rows carry -- while their graph uids are battery-scoped. The
// envelope-to-row decode (binding pre-pass filling Row.Provider) is pure
// projector logic covered by the projector's own tests; the battery sets
// the row fields to the values that decode produces for the seeded
// binding fact.
//
// The battery is intentionally a fixed corpus, not a generation lifecycle:
// retire/delete convergence (stale graph nodes vs current-generation fact
// truth) is a known follow-up documented on #6843 -- there is no cloud
// retract yet, so a deleted resource would diverge by design. The TSR
// stale-scalar REMOVE behavior itself is proven in the cypher package
// (tfstate_canonical_writer_stale_attrs_test.go); this battery proves the
// steady-state cross-backend equality the route rewrite relies on.
//
// No build tag: skips cleanly without DSNs in the default lane. Run with:
//
//	ESHU_POSTGRES_TEST_DSN=postgresql://eshu:change-me@localhost:<port>/eshu \
//	ESHU_CYPHER_BOLT_DSN=bolt://localhost:<bolt-port> \
//	  go test ./internal/query -run TestLiveInfraGraphOnlyLabelsMatchFactReadModel -count=1 -v
func TestLiveInfraGraphOnlyLabelsMatchFactReadModel(t *testing.T) {
	pgDSN := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	boltDSN := strings.TrimSpace(os.Getenv("ESHU_CYPHER_BOLT_DSN"))
	if pgDSN == "" || boltDSN == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN and ESHU_CYPHER_BOLT_DSN to run the #6843 parity battery")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	defer cancel()

	suffix := fmt.Sprintf("par6843-%d", time.Now().UnixNano())
	uidA1 := "uid-a1-" + suffix
	uidA2 := "uid-a2-" + suffix
	uidE1 := "uid-e1-" + suffix
	uidR1 := "uid-r1-" + suffix
	uids := []string{uidA1, uidA2, uidE1, uidR1}
	// The EC2 posture identity dedups globally by instance, so each run
	// needs its own instance or a shared database contributes zero delta.
	instE1 := "i-e1-" + suffix

	driver, err := neo4jdriver.NewDriverWithContext(boltDSN, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open graph driver: %v", err)
	}
	defer func() { _ = driver.Close(context.Background()) }()
	if err := driver.VerifyConnectivity(ctx); err != nil {
		t.Fatalf("verify graph connectivity: %v", err)
	}

	// All graph writes run back-to-back with no interleaved read: the pinned
	// NornicDB image can silently drop a write that follows an interleaved
	// read on the same node within one test process (see
	// infra_scope_grant_matches_state_pipeline_live_test.go).
	exec := &parityBoltExecutor{driver: driver, database: parityDatabase()}
	if err := cypher.NewCloudResourceNodeWriter(exec, 500).WriteCloudResourceNodes(ctx, []map[string]any{
		{"uid": uidA1, "source_system": "aws", "resource_type": "aws_instance"},
		{"uid": uidA2, "source_system": "gcp", "resource_type": "gce_instance"},
	}, "par6843-battery"); err != nil {
		t.Fatalf("write CloudResource nodes: %v", err)
	}
	if err := cypher.NewEC2InstanceNodeWriter(exec, 500).WriteEC2InstanceNodes(ctx, []map[string]any{
		{
			"uid": uidE1, "source_system": "aws", "resource_type": "aws_instance",
			"resource_id": instE1, "account_id": "111", "region": "us-east-1",
		},
	}, "par6843-battery"); err != nil {
		t.Fatalf("write EC2 node: %v", err)
	}
	tsrMat := canonical.CanonicalMaterialization{
		ScopeID:         "scope-" + suffix,
		GenerationID:    "gen-" + suffix,
		FirstGeneration: true,
		TerraformStateResources: []canonical.TerraformStateResourceRow{{
			UID: uidR1, Address: "aws_instance.web", Mode: "managed",
			ResourceType: "aws_instance", Name: "web",
			Provider: "aws", ProviderAddress: "registry.terraform.io/hashicorp/aws",
			SourceSystem: "tfstate", SourceConfidence: facts.SourceConfidenceObserved,
			CollectorKind:    "terraform_state",
			OwnershipOutcome: canonical.TerraformStateOwnershipResolved,
			OwningRepoID:     "repo-" + suffix,
		}},
	}
	if err := cypher.NewCanonicalNodeWriter(exec, 500, nil).Write(ctx, tsrMat); err != nil {
		t.Fatalf("write TerraformStateResource node: %v", err)
	}

	graphBuckets := readParityGraphBuckets(t, ctx, driver, uids)

	sqlDB, err := sql.Open("pgx", pgDSN)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()
	if err := storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: sqlDB}); err != nil {
		t.Fatalf("apply bootstrap schema: %v", err)
	}
	pgFilter := inventory.Filter{
		Labels:        []string{"CloudResource", "TerraformStateResource"},
		AllCategories: true,
	}
	readPGBuckets := func() map[parityBucket]int64 {
		buckets, err := inventory.CountBuckets(ctx, storagepostgres.SQLDB{DB: sqlDB}, pgFilter)
		if err != nil {
			t.Fatalf("CountBuckets: %v", err)
		}
		out := map[parityBucket]int64{}
		for _, b := range buckets {
			out[parityBucket{Label: b.Label, Provider: b.Provider, Environment: b.Environment}] += b.Count
		}
		return out
	}
	// The Reader has no scope selector, so a shared database contributes
	// rows outside this fixture: baseline before seeding and assert the
	// seeded delta. All fixture identity keys are unique per run.
	basePG := readPGBuckets()
	seedParityFacts(t, ctx, sqlDB, suffix, uidA1, uidA2, instE1)
	pgBuckets := map[parityBucket]int64{}
	for bucket, count := range readPGBuckets() {
		if delta := count - basePG[bucket]; delta != 0 {
			pgBuckets[bucket] = delta
		}
	}

	want := map[parityBucket]int64{
		{Label: "CloudResource", Provider: "aws", Environment: "unknown"}:          2,
		{Label: "CloudResource", Provider: "gcp", Environment: "unknown"}:          1,
		{Label: "TerraformStateResource", Provider: "aws", Environment: "unknown"}: 1,
	}
	if !parityBucketsEqual(graphBuckets, want) {
		t.Fatalf("graph buckets:\n got %+v\nwant %+v", graphBuckets, want)
	}
	if !parityBucketsEqual(pgBuckets, want) {
		t.Fatalf("fact-read-model buckets:\n got %+v\nwant %+v", pgBuckets, want)
	}
	if !parityBucketsEqual(graphBuckets, pgBuckets) {
		t.Fatalf("parity: graph %+v != fact read model %+v", graphBuckets, pgBuckets)
	}

	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cleanupCancel()
	parityRun(t, driver, parityDatabase(), neo4jdriver.AccessModeWrite,
		`MATCH (n) WHERE n.uid IN $uids DETACH DELETE n`, map[string]any{"uids": uidsToAny(uids)}, cleanupCtx)
}

type parityBucket struct {
	Label       string
	Provider    string
	Environment string
}

func parityBucketsEqual(a, b map[parityBucket]int64) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if bv, ok := b[k]; !ok || bv != v {
			return false
		}
	}
	return true
}

func parityDatabase() string {
	if database := strings.TrimSpace(os.Getenv("ESHU_CYPHER_BOLT_DATABASE")); database != "" {
		return database
	}
	return "nornic"
}

// parityBoltExecutor adapts the bolt driver to cypher.Executor with
// sequential autocommit statements. Whole-materialization atomicity is not
// needed: each writer call commits before the next runs, so later MATCH
// phases see earlier writes.
type parityBoltExecutor struct {
	driver   neo4jdriver.DriverWithContext
	database string
}

func (e *parityBoltExecutor) Execute(ctx context.Context, stmt cypher.Statement) error {
	session := e.driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeWrite,
		DatabaseName: e.database,
	})
	defer func() { _ = session.Close(ctx) }()
	result, err := session.Run(ctx, stmt.Cypher, stmt.Parameters)
	if err != nil {
		return err
	}
	_, err = result.Consume(ctx)
	return err
}

// readParityGraphBuckets runs the production whole-label aggregate shape per
// graph-only label (the query shape the #6843 rewrite removes from the
// routes), restricted to this run's uids.
func readParityGraphBuckets(t *testing.T, ctx context.Context, driver neo4jdriver.DriverWithContext, uids []string) map[parityBucket]int64 {
	t.Helper()
	providerExpr := infraResourceProviderGroupExpression(InfraResourceAggregateFilter{})
	buckets := map[parityBucket]int64{}
	for _, label := range []string{"CloudResource", "TerraformStateResource"} {
		cy := infraResourceAggregatePerLabelCypher([]string{label},
			" WHERE n.uid IN $uids",
			"RETURN '"+label+"' AS label, "+providerExpr+" AS provider_bucket, "+
				infraResourceEnvironmentGroupExpression+" AS environment_bucket, count(n) AS bucket_count",
			"RETURN label, provider_bucket, environment_bucket, bucket_count")
		for _, row := range parityRun(t, driver, parityDatabase(), neo4jdriver.AccessModeRead, cy, map[string]any{"uids": uidsToAny(uids)}, ctx) {
			label, _ := row["label"].(string)
			provider, _ := row["provider_bucket"].(string)
			env, _ := row["environment_bucket"].(string)
			var count int64
			switch v := row["bucket_count"].(type) {
			case int64:
				count = v
			case int:
				count = int64(v)
			default:
				t.Fatalf("bucket_count has unexpected type %T", row["bucket_count"])
			}
			buckets[parityBucket{Label: label, Provider: provider, Environment: env}] += count
		}
	}
	return buckets
}

func uidsToAny(uids []string) []any {
	out := make([]any, 0, len(uids))
	for _, u := range uids {
		out = append(out, u)
	}
	return out
}

func parityRun(t *testing.T, driver neo4jdriver.DriverWithContext, database string, mode neo4jdriver.AccessMode, cy string, params map[string]any, ctx context.Context) []map[string]any {
	t.Helper()
	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: mode, DatabaseName: database})
	defer func() { _ = session.Close(ctx) }()
	result, err := session.Run(ctx, cy, params)
	if err != nil {
		t.Fatalf("run cypher: %v", err)
	}
	records, err := result.Collect(ctx)
	if err != nil {
		t.Fatalf("collect cypher: %v", err)
	}
	rows := make([]map[string]any, 0, len(records))
	for _, rec := range records {
		row := map[string]any{}
		for _, key := range rec.Keys {
			v, _ := rec.Get(key)
			row[key] = v
		}
		rows = append(rows, row)
	}
	return rows
}

// seedParityFacts seeds the same logical fixture as fact_records rows using
// the fact kinds and payload keys the collectors and the reducer emit (see
// TestGraphOnlyFactsLive for the adversarial matrix this mirrors): admission
// identities plus provider facts for the two cloud resources, one EC2
// posture fact for instance i-e1, and one state resource with its provider
// binding for aws_instance.web. One tombstoned admission row proves
// exclusion on the fact side.
func seedParityFacts(t *testing.T, ctx context.Context, sqlDB *sql.DB, suffix, uidA1, uidA2, instE1 string) {
	t.Helper()
	scopeID := "scope-" + suffix
	genID := "gen-" + suffix
	now := time.Now().UTC()
	if _, err := sqlDB.ExecContext(ctx, `
		INSERT INTO ingestion_scopes
		  (scope_id, scope_kind, source_system, source_key, collector_kind,
		   partition_key, observed_at, ingested_at, status, active_generation_id, payload)
		VALUES ($1::text, 'aws_account', 'aws', $1::text, 'aws', $1::text, $2, $2, 'active', $3::text,
		        jsonb_build_object('scope_id', $1::text))
		ON CONFLICT (scope_id) DO NOTHING`,
		scopeID, now, genID,
	); err != nil {
		t.Fatalf("seed ingestion_scopes: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, `
		INSERT INTO scope_generations
		  (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
		VALUES ($1, $2, 'manual', $3, $3, 'active', $3)
		ON CONFLICT (generation_id) DO NOTHING`,
		genID, scopeID, now,
	); err != nil {
		t.Fatalf("seed scope_generations: %v", err)
	}
	seed := func(factID, kind, sourceSystem, payload string, tombstone bool) {
		t.Helper()
		if _, err := sqlDB.ExecContext(ctx, `
			INSERT INTO fact_records
			  (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
			   source_fact_key, observed_at, ingested_at, is_tombstone, payload)
			VALUES ($1, $2, $3, $4, $1, $5, $1, $6, $6, $7, $8::jsonb)`,
			factID, scopeID, genID, kind, sourceSystem, now, tombstone, payload,
		); err != nil {
			t.Fatalf("seed fact %s: %v", factID, err)
		}
	}
	seed("adm-a1-"+suffix, "reducer_cloud_resource_identity", "aws",
		`{"cloud_resource_uid":"`+uidA1+`","resource_type":"aws_instance","raw_identity":"arn:aws:ec2:us-east-1:111:instance/i-a1","provider":"aws"}`, false)
	seed("adm-a2-"+suffix, "reducer_cloud_resource_identity", "gcp",
		`{"cloud_resource_uid":"`+uidA2+`","resource_type":"gce_instance","raw_identity":"//compute.googleapis.com/projects/p/zones/z/instances/i-a2","provider":"gcp"}`, false)
	seed("prov-a1-"+suffix, "aws_resource", "aws",
		`{"arn":"arn:aws:ec2:us-east-1:111:instance/i-a1","service_kind":"ec2"}`, false)
	seed("prov-a2-"+suffix, "gcp_cloud_resource", "gcp",
		`{"full_resource_name":"//compute.googleapis.com/projects/p/zones/z/instances/i-a2","service_kind":"gce"}`, false)
	seed("ec2-e1-"+suffix, "ec2_instance_posture", "aws",
		`{"account_id":"111","region":"us-east-1","instance_id":"`+instE1+`","service_kind":"ec2"}`, false)
	seed("tsr-r1-"+suffix, "terraform_state_resource", "tfstate",
		`{"address":"aws_instance.web","type":"aws_instance","name":"web"}`, false)
	seed("bind-r1-"+suffix, "terraform_state_provider_binding", "tfstate",
		`{"resource_address":"aws_instance.web","provider_address":"registry.terraform.io/hashicorp/aws","provider_type":"aws"}`, false)
	seed("adm-tomb-"+suffix, "reducer_cloud_resource_identity", "aws",
		`{"cloud_resource_uid":"uid-tomb-`+suffix+`","resource_type":"aws_instance","raw_identity":"arn:aws:ec2:us-east-1:111:instance/i-tomb"}`, true)
}
