// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #7666 disposable PostgreSQL 18 row differential: the fenced ARN-keyed state join returns exactly the rows, in order, of the pre-#7666 direct join derived from the shipped constant, over ambiguous ownership, superseded and older AWS generations, a stale caller allowlist, blank and padded ARNs, an inactive scope, and duplicate addresses; enrolled in the live-postgres-readiness runner; about 1-2.8 s locally on PostgreSQL 18.6 including the bootstrap
//eshu:readiness:env ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE ESHU_AWS_DRIFT_ARN_PROOF_DSN ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE ESHU_DEFERRED_PARTITION_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// awsDriftDifferentialFact is one fact_records row of the #7666 differential
// fixture.
type awsDriftDifferentialFact struct {
	factID     string
	scopeID    string
	generation string
	kind       string
	payload    string
}

// TestAWSCloudRuntimeDriftStateARNJoinMatchesReferenceLive is the #7666 row
// differential: the fenced state join must return exactly the rows of the
// pre-#7666 direct join (derived from the shipped constant), in the same
// order, over the edge cases the drift loader depends on. Order matters: the
// loader keeps the first state row per ARN as the row's State.
func TestAWSCloudRuntimeDriftStateARNJoinMatchesReferenceLive(t *testing.T) {
	database, ctx := openAWSDriftARNProofDB(t, "aws_drift_arn_diff",
		"set ESHU_AWS_DRIFT_ARN_PROOF_DSN (or ESHU_POSTGRES_DSN) to run the #7666 aws_cloud_runtime_drift ARN-join row differential")
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

	const (
		awsScope   = "aws:7666:diff"
		awsSealed  = "aws:7666:diff:g2"
		awsOlder   = "aws:7666:diff:g1"
		stateA     = "state_snapshot:s3:a"
		stateB     = "state_snapshot:s3:b"
		stateIdle  = "state_snapshot:s3:idle"
		shared     = "arn:aws:s3:::shared"
		dupAddress = "arn:aws:s3:::dup-address"
		onlyOld    = "arn:aws:s3:::only-superseded"
		olderAWS   = "arn:aws:s3:::older-aws-generation"
		notInAWS   = "arn:aws:s3:::caller-only"
		padded     = "arn:aws:s3:::padded"
		idleScope  = "arn:aws:s3:::idle-scope"
	)
	seedAWSCloudRuntimeDriftScope(t, ctx, database, awsScope, "aws_account_region", awsSealed, now)
	seedAWSCloudRuntimeDriftGeneration(t, ctx, database, awsOlder, awsScope, "superseded", now)
	seedAWSCloudRuntimeDriftGeneration(t, ctx, database, awsSealed, awsScope, "active", now)
	for _, scope := range []struct{ id, active string }{
		{stateA, stateA + ":g2"}, {stateB, stateB + ":g1"}, {stateIdle, ""},
	} {
		seedAWSCloudRuntimeDriftScope(t, ctx, database, scope.id, "state_snapshot", scope.active, now)
	}
	seedAWSCloudRuntimeDriftGeneration(t, ctx, database, stateA+":g1", stateA, "superseded", now)
	seedAWSCloudRuntimeDriftGeneration(t, ctx, database, stateA+":g2", stateA, "active", now)
	seedAWSCloudRuntimeDriftGeneration(t, ctx, database, stateB+":g1", stateB, "active", now)
	seedAWSCloudRuntimeDriftGeneration(t, ctx, database, stateIdle+":g1", stateIdle, "pending", now)

	aws := func(id, generation, arn string) awsDriftDifferentialFact {
		return awsDriftDifferentialFact{id, awsScope, generation, "aws_resource", fmt.Sprintf(`{"arn": %q}`, arn)}
	}
	state := func(id, scope, generation, address, arn string) awsDriftDifferentialFact {
		return awsDriftDifferentialFact{
			id, scope, generation, "terraform_state_resource",
			fmt.Sprintf(`{"address": %q, "attributes": {"arn": %q, "id": %q}}`, address, arn, id),
		}
	}
	facts := []awsDriftDifferentialFact{
		aws("aws-1", awsSealed, shared),
		aws("aws-2", awsSealed, dupAddress),
		aws("aws-3", awsSealed, onlyOld),
		aws("aws-4", awsSealed, padded),
		aws("aws-5", awsSealed, idleScope),
		aws("aws-6", awsSealed, "   "),
		aws("aws-7", awsSealed, shared), // duplicate AWS row for one ARN
		aws("aws-8", awsOlder, olderAWS),
		// shared: declared by two active scopes (ambiguous ownership), plus a
		// superseded generation that must not appear.
		state("tf-b-shared", stateB, stateB+":g1", "aws_s3_bucket.shared", shared),
		state("tf-a-shared", stateA, stateA+":g2", "aws_s3_bucket.shared", shared),
		state("tf-a-shared-old", stateA, stateA+":g1", "aws_s3_bucket.shared", shared),
		// dupAddress: two resources in one active generation, ordered by
		// address, then by fact_id for the identical-address pair. The
		// fact_id of the later address sorts first, so address order and
		// fact_id order disagree.
		state("tf-a-dup-a0", stateA, stateA+":g2", "module.z.aws_s3_bucket.x", dupAddress),
		state("tf-a-dup-b2", stateA, stateA+":g2", "module.a.aws_s3_bucket.x", dupAddress),
		state("tf-a-dup-b1", stateA, stateA+":g2", "module.a.aws_s3_bucket.x", dupAddress),
		state("tf-a-only-old", stateA, stateA+":g1", "aws_s3_bucket.old", onlyOld),
		state("tf-b-older-aws", stateB, stateB+":g1", "aws_s3_bucket.older", olderAWS),
		state("tf-b-caller-only", stateB, stateB+":g1", "aws_s3_bucket.caller", notInAWS),
		state("tf-b-padded", stateB, stateB+":g1", "aws_s3_bucket.padded", " "+padded+" "),
		state("tf-idle", stateIdle, stateIdle+":g1", "aws_s3_bucket.idle", idleScope),
		state("tf-b-blank", stateB, stateB+":g1", "aws_s3_bucket.blank", "   "),
	}
	for _, fact := range facts {
		if _, err := database.ExecContext(ctx, `
INSERT INTO fact_records
  (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
   source_fact_key, observed_at, ingested_at, payload)
VALUES ($1, $2, $3, $4, $1, 'fixture', $1, $5, $5, $6::jsonb)`,
			fact.factID, fact.scopeID, fact.generation, fact.kind, now, fact.payload); err != nil {
			t.Fatalf("seed fact %s: %v", fact.factID, err)
		}
	}
	if _, err := database.ExecContext(ctx, `ANALYZE fact_records`); err != nil {
		t.Fatalf("analyze: %v", err)
	}

	// The caller's allowlist is stale on purpose: it names ARNs the sealed
	// generation does not hold, plus blank values.
	requested, err := json.Marshal([]string{shared, dupAddress, onlyOld, padded, idleScope, olderAWS, notInAWS, "", "  "})
	if err != nil {
		t.Fatalf("marshal requested arns: %v", err)
	}
	args := []any{awsScope, awsSealed, string(requested)}
	got := queryAWSDriftStateARNRows(t, ctx, database, listActiveStateResourcesForAWSARNsQuery, args)
	want := queryAWSDriftStateARNRows(t, ctx, database, awsDriftStateARNReferenceQuery(t), args)

	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("fenced state join rows differ from the reference\n got:\n%s\nwant:\n%s",
			strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	wantIDs := []string{"tf-a-dup-b1", "tf-a-dup-b2", "tf-a-dup-a0", "tf-a-shared", "tf-b-shared"}
	if len(got) != len(wantIDs) {
		t.Fatalf("state join returned %d rows, want %d:\n%s", len(got), len(wantIDs), strings.Join(got, "\n"))
	}
	for i, id := range wantIDs {
		if !strings.Contains(got[i], fmt.Sprintf(`"id": %q`, id)) {
			t.Fatalf("row %d = %s, want fact %s", i, got[i], id)
		}
	}
}

// queryAWSDriftStateARNRows runs one state-join statement and renders each
// row as scope|generation|address|payload, in result order.
func queryAWSDriftStateARNRows(t *testing.T, ctx context.Context, database *sql.DB, query string, args []any) []string {
	t.Helper()
	rows, err := database.QueryContext(ctx, query, args...)
	if err != nil {
		t.Fatalf("query state join: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var scopeID, generationID, address, payload string
		if err := rows.Scan(&scopeID, &generationID, &address, &payload); err != nil {
			t.Fatalf("scan state join row: %v", err)
		}
		out = append(out, strings.Join([]string{scopeID, generationID, address, payload}, "|"))
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate state join rows: %v", err)
	}
	return out
}
