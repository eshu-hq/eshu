// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9661 disposable PostgreSQL proof that the workload-to-ECS-service binding declaration census reads the filtered resolver closure with verbatim metadata, excludes tombstones and superseded generations and out-of-closure rows, reports incomplete with zero declarations instead of a bindable prefix, and plans index-backed with no sequential scan on fact_records at 25k-row scale; isolated temp tables per test, rolled back
//eshu:readiness:env ESHU_POSTGRES_TEST_DSN

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/cloud"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// txQueryer adapts *sql.Tx to db.Queryer for the isolated fixture.
type txQueryer struct{ tx *sql.Tx }

func (q txQueryer) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	return q.tx.QueryContext(ctx, query, args...)
}

// openECSBindingFixtureTx opens an isolated fixture whose search_path
// resolves scope_generations and fact_records to temp tables holding only
// the columns the census reads. Cleanup rolls the fixture back.
func openECSBindingFixtureTx(t *testing.T) (context.Context, *sql.Tx) {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the ECS binding census live proof")
	}
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open Postgres: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	t.Cleanup(cancel)
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin isolated fixture: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	for _, stmt := range []string{
		`CREATE TEMP TABLE scope_generations (
		    scope_id text NOT NULL,
		    generation_id text NOT NULL,
		    status text NOT NULL
		)`,
		`CREATE TEMP TABLE fact_records (
		    scope_id text NOT NULL,
		    generation_id text NOT NULL,
		    fact_kind text NOT NULL,
		    is_tombstone boolean NOT NULL,
		    payload jsonb NOT NULL
		)`,
		`CREATE INDEX ecs_binding_fixture_gen_idx ON scope_generations (scope_id, generation_id)`,
		`CREATE INDEX ecs_binding_fixture_fact_idx ON fact_records (scope_id, generation_id)`,
		`CREATE INDEX ecs_binding_fixture_iac_idx ON fact_records
		    (scope_id, generation_id, (payload->>'entity_type'), (payload->>'entity_name'), (payload->>'entity_id'))
		    WHERE fact_kind = 'content_entity'
		      AND is_tombstone = FALSE
		      AND payload->>'entity_type' IN (
		          'TerraformResource',
		          'TerraformModule',
		          'TerraformVariable',
		          'TerraformLocal',
		          'TerragruntConfig',
		          'TerragruntInput'
		      )`,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("create fixture table: %v", err)
		}
	}
	return ctx, tx
}

func seedECSBindingFixture(t *testing.T, ctx context.Context, tx *sql.Tx) {
	t.Helper()
	for _, gen := range [][3]string{
		{"repo-scope", "gen-active", "active"},
		{"repo-scope", "gen-old", "superseded"},
		{"aws-scope", "gen-aws", "active"},
	} {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO scope_generations (scope_id, generation_id, status) VALUES ($1, $2, $3)`,
			gen[0], gen[1], gen[2]); err != nil {
			t.Fatalf("seed generation: %v", err)
		}
	}
	seed := []struct {
		scope, gen, kind, payload string
		tombstone                 bool
	}{
		{"repo-scope", "gen-active", "content_entity", `{"entity_type":"TerraformResource","entity_name":"aws_ecs_service.api","entity_id":"e1","repo_id":"repo-app","relative_path":"main.tf","entity_metadata":{"resource_type":"aws_ecs_service","resource_name":"api","attributes":{"name":"\"api\"","cluster":"\"arn:aws:ecs:us-east-1:111122223333:cluster/prod\""}}}`, false},
		{"repo-scope", "gen-active", "content_entity", `{"entity_type":"TerraformVariable","entity_name":"service_name","entity_id":"e2","repo_id":"repo-app","relative_path":"variables.tf","entity_metadata":{"name":"service_name","default":"\"api\""}}`, false},
		{"repo-scope", "gen-active", "content_entity", `{"entity_type":"TerraformInput","entity_name":"other","entity_id":"e3","repo_id":"repo-app","relative_path":"x.tf","entity_metadata":{}}`, false},
		{"repo-scope", "gen-active", "content_entity", `{"entity_type":"TerraformResource","entity_name":"aws_ecs_service.old","entity_id":"e4","repo_id":"repo-app","relative_path":"old.tf","entity_metadata":{"resource_type":"aws_ecs_service"}}`, true},
		{"repo-scope", "gen-old", "content_entity", `{"entity_type":"TerraformResource","entity_name":"aws_ecs_service.stale","entity_id":"e5","repo_id":"repo-app","relative_path":"stale.tf","entity_metadata":{"resource_type":"aws_ecs_service"}}`, false},
		{"aws-scope", "gen-aws", "aws_resource", `{"resource_type":"aws_ecs_service"}`, false},
	}
	for i, row := range seed {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO fact_records (scope_id, generation_id, fact_kind, is_tombstone, payload) VALUES ($1, $2, $3, $4, $5::jsonb)`,
			row.scope, row.gen, row.kind, row.tombstone, row.payload); err != nil {
			t.Fatalf("seed fact %d: %v", i, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `ANALYZE scope_generations; ANALYZE fact_records`); err != nil {
		t.Fatalf("analyze fixture: %v", err)
	}
}

// TestECSBindingCensusReadsActiveDeclarationsLive proves the shipped
// census returns active declarations with verbatim metadata while
// excluding tombstones, superseded generations, other entity types,
// and non-content facts.
func TestECSBindingCensusReadsActiveDeclarationsLive(t *testing.T) {
	ctx, tx := openECSBindingFixtureTx(t)
	seedECSBindingFixture(t, ctx, tx)

	result, err := NewECSBindingDeclarationCensusStore(txQueryer{tx}, 100).ListECSBindingDeclarations(ctx)
	if err != nil {
		t.Fatalf("ListECSBindingDeclarations: %v", err)
	}
	if !result.Complete {
		t.Fatalf("census complete=false unexpectedly")
	}
	if len(result.Declarations) != 2 {
		t.Fatalf("declarations = %d, want 2 (resource + variable)", len(result.Declarations))
	}
	resource := result.Declarations[0]
	if resource.RepoID != "repo-app" || resource.Path != "main.tf" || resource.EntityType != "TerraformResource" {
		t.Fatalf("resource declaration = %#v", resource)
	}
	attrs, _ := resource.Metadata["attributes"].(map[string]any)
	if attrs["name"] != `"api"` || attrs["cluster"] != `"arn:aws:ecs:us-east-1:111122223333:cluster/prod"` {
		t.Fatalf("resource attributes = %#v", attrs)
	}
	variable := result.Declarations[1]
	if variable.EntityType != "TerraformVariable" || variable.Metadata["default"] != `"api"` {
		t.Fatalf("variable declaration = %#v", variable)
	}
}

// TestECSBindingCensusCeilingReportsIncompleteLive proves the safety
// ceiling reports incomplete with zero declarations instead of a
// bindable prefix (#11084): the reducer blocks declaration binds on an
// incomplete census, so the store must not hand one out.
func TestECSBindingCensusCeilingReportsIncompleteLive(t *testing.T) {
	ctx, tx := openECSBindingFixtureTx(t)
	seedECSBindingFixture(t, ctx, tx)

	result, err := NewECSBindingDeclarationCensusStore(txQueryer{tx}, 1).ListECSBindingDeclarations(ctx)
	if err != nil {
		t.Fatalf("ListECSBindingDeclarations: %v", err)
	}
	if result.Complete {
		t.Fatalf("census complete=true under a 1-row ceiling, want incomplete")
	}
	if len(result.Declarations) != 0 {
		t.Fatalf("declarations = %d, want 0: an incomplete census carries no bindable prefix", len(result.Declarations))
	}
}

// seedECSBindingClosureFixture seeds two active repos exercising every
// closure leg: same-directory variables/locals (kept), other-directory
// context (dropped), same-repo bridges (kept), other-repo bridges
// (dropped), non-ECS resources and other entity types (dropped),
// tombstones and superseded generations (dropped).
func seedECSBindingClosureFixture(t *testing.T, ctx context.Context, tx *sql.Tx) {
	t.Helper()
	for _, gen := range [][3]string{
		{"repo-app", "gen-active", "active"},
		{"repo-other", "gen-other", "active"},
		{"repo-app", "gen-old", "superseded"},
	} {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO scope_generations (scope_id, generation_id, status) VALUES ($1, $2, $3)`,
			gen[0], gen[1], gen[2]); err != nil {
			t.Fatalf("seed generation: %v", err)
		}
	}
	seed := []struct {
		scope, gen, kind, payload string
		tombstone                 bool
	}{
		// repo-app ECS declaration + its directory context.
		{"repo-app", "gen-active", "content_entity", `{"entity_type":"TerraformResource","entity_name":"aws_ecs_service.api","entity_id":"c1","repo_id":"repo-app","relative_path":"svc/main.tf","entity_metadata":{"resource_type":"aws_ecs_service","resource_name":"api","attributes":{"name":"\"api\"","cluster":"\"arn:aws:ecs:us-east-1:111122223333:cluster/prod\""}}}`, false},
		{"repo-app", "gen-active", "content_entity", `{"entity_type":"TerraformVariable","entity_name":"service_name","entity_id":"c2","repo_id":"repo-app","relative_path":"svc/vars.tf","entity_metadata":{"name":"service_name","default":"\"api\""}}`, false},
		// Other-directory context: same repo, no ECS declaration there.
		{"repo-app", "gen-active", "content_entity", `{"entity_type":"TerraformVariable","entity_name":"other","entity_id":"c3","repo_id":"repo-app","relative_path":"infra/vars.tf","entity_metadata":{"name":"other","default":"\"x\""}}`, false},
		{"repo-app", "gen-active", "content_entity", `{"entity_type":"TerraformLocal","entity_name":"loc","entity_id":"c4","repo_id":"repo-app","relative_path":"infra/locals.tf","entity_metadata":{"name":"loc","value":"\"y\""}}`, false},
		// Same-repo bridges: kept without SQL path-cleaning.
		{"repo-app", "gen-active", "content_entity", `{"entity_type":"TerraformModule","entity_name":"mod","entity_id":"c5","repo_id":"repo-app","relative_path":"infra/mod.tf","entity_metadata":{"source":"../svc"}}`, false},
		{"repo-app", "gen-active", "content_entity", `{"entity_type":"TerragruntConfig","entity_name":"tg","entity_id":"c6","repo_id":"repo-app","relative_path":"live/app.hcl","entity_metadata":{"terraform_source":"../svc"}}`, false},
		{"repo-app", "gen-active", "content_entity", `{"entity_type":"TerragruntInput","entity_name":"tgin","entity_id":"c7","repo_id":"repo-app","relative_path":"live/app.hcl","entity_metadata":{"name":"environment","value":"\"prod\""}}`, false},
		// Non-ECS resource and other entity type in an ECS directory.
		{"repo-app", "gen-active", "content_entity", `{"entity_type":"TerraformResource","entity_name":"aws_s3_bucket.b","entity_id":"c8","repo_id":"repo-app","relative_path":"svc/bucket.tf","entity_metadata":{"resource_type":"aws_s3_bucket"}}`, false},
		{"repo-app", "gen-active", "content_entity", `{"entity_type":"TerraformOutput","entity_name":"o","entity_id":"c9","repo_id":"repo-app","relative_path":"svc/out.tf","entity_metadata":{}}`, false},
		// repo-other: corpus-wide C1 leg plus its own bridge; a variable
		// in a directory with no ECS declaration stays out.
		{"repo-other", "gen-other", "content_entity", `{"entity_type":"TerraformResource","entity_name":"aws_ecs_service.other","entity_id":"c10","repo_id":"repo-other","relative_path":"main.tf","entity_metadata":{"resource_type":"aws_ecs_service","resource_name":"other","attributes":{"name":"\"other\"","cluster":"\"arn:aws:ecs:us-east-1:111122223333:cluster/prod\""}}}`, false},
		{"repo-other", "gen-other", "content_entity", `{"entity_type":"TerraformModule","entity_name":"mod","entity_id":"c11","repo_id":"repo-other","relative_path":"mod.tf","entity_metadata":{"source":"."}}`, false},
		{"repo-other", "gen-other", "content_entity", `{"entity_type":"TerraformVariable","entity_name":"z","entity_id":"c12","repo_id":"repo-other","relative_path":"other/vars.tf","entity_metadata":{"name":"z","default":"\"z\""}}`, false},
		// Tombstone and superseded rows never surface.
		{"repo-app", "gen-active", "content_entity", `{"entity_type":"TerraformResource","entity_name":"aws_ecs_service.ghost","entity_id":"c13","repo_id":"repo-app","relative_path":"svc/ghost.tf","entity_metadata":{"resource_type":"aws_ecs_service"}}`, true},
		{"repo-app", "gen-old", "content_entity", `{"entity_type":"TerraformResource","entity_name":"aws_ecs_service.stale","entity_id":"c14","repo_id":"repo-app","relative_path":"svc/stale.tf","entity_metadata":{"resource_type":"aws_ecs_service"}}`, false},
	}
	for i, row := range seed {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO fact_records (scope_id, generation_id, fact_kind, is_tombstone, payload) VALUES ($1, $2, $3, $4, $5::jsonb)`,
			row.scope, row.gen, row.kind, row.tombstone, row.payload); err != nil {
			t.Fatalf("seed closure fact %d: %v", i, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `ANALYZE scope_generations; ANALYZE fact_records`); err != nil {
		t.Fatalf("analyze fixture: %v", err)
	}
}

// TestECSBindingCensusReadsFilteredClosureLive proves the shipped census
// returns exactly the resolver's closure: ECS declarations corpus-wide
// plus same-directory variables/locals and same-repo bridges, with
// tombstones, superseded generations, other entity types, non-ECS
// resources, and out-of-closure context excluded.
func TestECSBindingCensusReadsFilteredClosureLive(t *testing.T) {
	ctx, tx := openECSBindingFixtureTx(t)
	seedECSBindingClosureFixture(t, ctx, tx)

	result, err := NewECSBindingDeclarationCensusStore(txQueryer{tx}, 100).ListECSBindingDeclarations(ctx)
	if err != nil {
		t.Fatalf("ListECSBindingDeclarations: %v", err)
	}
	if !result.Complete {
		t.Fatalf("closure census complete=false, want complete")
	}
	got := make(map[string]string, len(result.Declarations))
	for _, decl := range result.Declarations {
		got[decl.RepoID+"/"+decl.Path+" "+decl.EntityType+"/"+decl.EntityName] = decl.EntityType
	}
	want := map[string]struct{}{
		"repo-app/svc/main.tf TerraformResource/aws_ecs_service.api": {},
		"repo-app/svc/vars.tf TerraformVariable/service_name":        {},
		"repo-app/infra/mod.tf TerraformModule/mod":                  {},
		"repo-app/live/app.hcl TerragruntConfig/tg":                  {},
		"repo-app/live/app.hcl TerragruntInput/tgin":                 {},
		"repo-other/main.tf TerraformResource/aws_ecs_service.other": {},
		"repo-other/mod.tf TerraformModule/mod":                      {},
	}
	if len(got) != len(want) {
		t.Fatalf("closure rows = %d, want %d: %#v", len(got), len(want), got)
	}
	for key := range want {
		if _, ok := got[key]; !ok {
			t.Errorf("closure lacks %q (got %#v)", key, got)
		}
	}
}

// TestECSBindingCensusPlanIsIndexBackedLive runs the shipped census SQL
// (derived from the constant, read-only EXPLAIN tail) against prod-shape
// indexes over a 25k-row fact table and requires the fact leg off
// sequential scans: the materialization must not corpus-scan
// fact_records at QA scale.
func TestECSBindingCensusPlanIsIndexBackedLive(t *testing.T) {
	ctx, tx := openECSBindingFixtureTx(t)
	seedECSBindingFixture(t, ctx, tx)
	// Volume distractors: other entity types, other scopes, and other
	// fact kinds, so a sequential scan would be visibly wrong.
	for _, stmt := range []string{
		`INSERT INTO fact_records (scope_id, generation_id, fact_kind, is_tombstone, payload)
		 SELECT 'repo-scope', 'gen-active', 'content_entity', FALSE,
		        jsonb_build_object('entity_type', 'TerraformOutput', 'entity_name', 'o' || g, 'entity_id', 'd' || g, 'repo_id', 'repo-app', 'relative_path', 'o.tf', 'entity_metadata', '{}')
		 FROM generate_series(1, 15000) AS g`,
		`INSERT INTO fact_records (scope_id, generation_id, fact_kind, is_tombstone, payload)
		 SELECT 'other-scope', 'gen-other', 'content_entity', FALSE,
		        jsonb_build_object('entity_type', 'TerraformResource', 'entity_name', 'aws_s3_bucket.b' || g, 'entity_id', 'x' || g, 'repo_id', 'repo-other', 'relative_path', 'b.tf', 'entity_metadata', '{}')
		 FROM generate_series(1, 5000) AS g`,
		`INSERT INTO fact_records (scope_id, generation_id, fact_kind, is_tombstone, payload)
		 SELECT 'aws-scope', 'gen-aws', 'aws_resource', FALSE,
		        jsonb_build_object('resource_type', 'aws_ecs_service', 'resource_id', 'r' || g)
		 FROM generate_series(1, 5000) AS g`,
		`INSERT INTO scope_generations (scope_id, generation_id, status) VALUES ('other-scope', 'gen-other', 'active')`,
		`ANALYZE scope_generations; ANALYZE fact_records`,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("seed plan volume: %v", err)
		}
	}

	rows, err := tx.QueryContext(ctx, "EXPLAIN "+ecsBindingCensusSQL, 100)
	if err != nil {
		t.Fatalf("explain census: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan plan: %v", err)
		}
		plan.WriteString(line + "\n")
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate plan: %v", err)
	}
	text := plan.String()
	if strings.Contains(text, "Seq Scan on fact_records") {
		t.Fatalf("census plan scans fact_records sequentially:\n%s", text)
	}
	if !strings.Contains(text, "Index") {
		t.Fatalf("census plan uses no index:\n%s", text)
	}
}

// ecsBindingLiveServiceEnvelopes builds one corroborated live service:
// an aws_ecs_service fact plus a RUNNING task fact with an observed ECR
// digest, in the shape the ECS scanner emits.
func ecsBindingLiveServiceEnvelopes(svcID, taskID, name, cluster string) []facts.Envelope {
	const account = "111122223333"
	svcARN := "arn:aws:ecs:us-east-1:" + account + ":service/" + cluster + "/" + name
	clusterARN := "arn:aws:ecs:us-east-1:" + account + ":cluster/" + cluster
	return []facts.Envelope{
		{
			FactID: svcID, StableFactKey: "stable-" + svcID,
			FactKind: cloud.AWSResourceFactKind,
			Payload: map[string]any{
				"account_id": account, "region": "us-east-1",
				"resource_type": "aws_ecs_service", "resource_id": svcARN,
				"arn": svcARN, "name": name, "state": "ACTIVE",
				"attributes": map[string]any{
					"cluster_arn": clusterARN, "running_count": 1,
				},
			},
			SourceRef:     facts.Ref{SourceSystem: "aws-cloud", SourceRecordID: svcID},
			CollectorKind: "aws-cloud",
		},
		{
			FactID: taskID, StableFactKey: "stable-" + taskID,
			FactKind: cloud.AWSResourceFactKind,
			Payload: map[string]any{
				"account_id": account, "region": "us-east-1",
				"resource_type": "aws_ecs_task",
				"resource_id":   "arn:aws:ecs:us-east-1:" + account + ":task/" + cluster + "/task-" + name,
				"state":         "RUNNING",
				"attributes": map[string]any{
					"cluster_arn": clusterARN, "group": "service:" + name,
					"containers": []any{map[string]any{
						"name":         name,
						"image":        account + ".dkr.ecr.us-east-1.amazonaws.com/" + name + ":latest",
						"image_digest": "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
						"runtime_id":   "runtime-a",
					}},
				},
			},
			SourceRef:     facts.Ref{SourceSystem: "aws-cloud", SourceRecordID: taskID},
			CollectorKind: "aws-cloud",
		},
	}
}

// seedECSBindingVetoFixture seeds one ECS declaration per repo; dupRepo
// controls whether the second repo competes for the same (name, cluster)
// (duplicate veto) or declares a disjoint service (bindable control).
func seedECSBindingVetoFixture(t *testing.T, ctx context.Context, tx *sql.Tx, dupRepo bool) {
	t.Helper()
	for _, gen := range [][3]string{
		{"repo-a", "gen-a", "active"},
		{"repo-b", "gen-b", "active"},
	} {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO scope_generations (scope_id, generation_id, status) VALUES ($1, $2, $3)`,
			gen[0], gen[1], gen[2]); err != nil {
			t.Fatalf("seed generation: %v", err)
		}
	}
	otherName := "other"
	if dupRepo {
		otherName = "api"
	}
	for i, row := range []struct{ scope, gen, payload string }{
		{"repo-a", "gen-a", `{"entity_type":"TerraformResource","entity_name":"aws_ecs_service.api","entity_id":"v1","repo_id":"repo-a","relative_path":"main.tf","entity_metadata":{"resource_type":"aws_ecs_service","resource_name":"api","attributes":{"name":"\"api\"","cluster":"\"arn:aws:ecs:us-east-1:111122223333:cluster/prod\""}}} `},
		{"repo-b", "gen-b", `{"entity_type":"TerraformResource","entity_name":"aws_ecs_service.` + otherName + `","entity_id":"v2","repo_id":"repo-b","relative_path":"main.tf","entity_metadata":{"resource_type":"aws_ecs_service","resource_name":"` + otherName + `","attributes":{"name":"\"` + otherName + `\"","cluster":"\"arn:aws:ecs:us-east-1:111122223333:cluster/prod\""}}}`},
	} {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO fact_records (scope_id, generation_id, fact_kind, is_tombstone, payload) VALUES ($1, $2, 'content_entity', FALSE, $3::jsonb)`,
			row.scope, row.gen, row.payload); err != nil {
			t.Fatalf("seed veto fact %d: %v", i, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `ANALYZE scope_generations; ANALYZE fact_records`); err != nil {
		t.Fatalf("analyze fixture: %v", err)
	}
}

// TestECSBindingVetoThroughRealStoreLive is the #11084 live re-check:
// declarations read through the real store drive the real resolver, so
// a competing declaration vetoes the bind (duplicate) while a unique
// one binds. Under the retired prefix census the competitor past the
// cap was invisible and the duplicate case bound.
func TestECSBindingVetoThroughRealStoreLive(t *testing.T) {
	for _, tc := range []struct {
		name      string
		duplicate bool
		wantRows  int
	}{
		{"unique binds", false, 1},
		{"duplicate vetoes", true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, tx := openECSBindingFixtureTx(t)
			seedECSBindingVetoFixture(t, ctx, tx, tc.duplicate)

			census, err := NewECSBindingDeclarationCensusStore(txQueryer{tx}, 100).ListECSBindingDeclarations(ctx)
			if err != nil {
				t.Fatalf("ListECSBindingDeclarations: %v", err)
			}
			if !census.Complete {
				t.Fatalf("veto fixture census complete=false, want complete")
			}
			rows, _, _, err := reducer.ExtractWorkloadECSBindingRows(
				ecsBindingLiveServiceEnvelopes("fact-svc-api", "fact-task-api", "api", "prod"),
				census,
				map[string][]string{"api": {"prod"}},
			)
			if err != nil {
				t.Fatalf("ExtractWorkloadECSBindingRows: %v", err)
			}
			if len(rows) != tc.wantRows {
				t.Fatalf("bound rows = %d, want %d", len(rows), tc.wantRows)
			}
		})
	}
}

// TestECSBindingCensusEntityTypesMatchSQL pins the shipped closure legs
// to the census entity-type set: adding a resolver context family
// without extending the census read (or vice versa) fails here, not in
// prod. It also pins the ECS service predicate that keeps the corpus
// leg from re-widening into a full census.
func TestECSBindingCensusEntityTypesMatchSQL(t *testing.T) {
	for _, entityType := range ecsBindingCensusEntityTypes {
		if !strings.Contains(ecsBindingCensusSQL, "'"+entityType+"'") {
			t.Errorf("census SQL lacks entity type %q", entityType)
		}
	}
	if !strings.Contains(ecsBindingCensusSQL, "->>'resource_type' = 'aws_ecs_service'") {
		t.Errorf("census SQL lacks the ECS service predicate on entity_metadata")
	}
	_ = reducer.ECSBindingCensusCap()
}
