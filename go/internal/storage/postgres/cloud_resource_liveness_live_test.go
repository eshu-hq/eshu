// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/graph/owner"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestCloudResourceLivenessLive proves the #6887 global live-check against a
// live Postgres: admission-uid and EC2-tuple probes across scopes'
// current generations, with superseded generations, tombstones, and pending
// generations correctly reading dead. It also proves the ledger release the
// retract pairs with the delete.
//
// Skipped by default; set ESHU_CLOUD_RETRACT_LIVE=1 and ESHU_POSTGRES_DSN.
// Every seeded id is uniquely prefixed per run so parallel databases never
// collide.
func TestCloudResourceLivenessLive(t *testing.T) {
	if strings.TrimSpace(os.Getenv("ESHU_CLOUD_RETRACT_LIVE")) == "" {
		t.Skip("set ESHU_CLOUD_RETRACT_LIVE=1 and ESHU_POSTGRES_DSN to run the cloud retract liveness proof")
	}
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("ESHU_POSTGRES_DSN not set")
	}
	ctx := context.Background()
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer func() { _ = database.Close() }()

	prefix := fmt.Sprintf("retract-live-%d", time.Now().UnixNano())
	now := time.Now().UTC().Truncate(time.Millisecond)

	seedScope := func(scopeID, activeGen string) {
		t.Helper()
		var activeGenAny any
		if activeGen != "" {
			activeGenAny = activeGen
		}
		if _, err := database.ExecContext(ctx, `
INSERT INTO ingestion_scopes
  (scope_id, scope_kind, source_system, source_key, collector_kind,
   partition_key, observed_at, ingested_at, status, active_generation_id, payload)
VALUES ($1, 'aws', 'aws', $1, 'aws', $1, $2, $2, 'active', $3, '{}'::jsonb)
ON CONFLICT (scope_id) DO UPDATE SET active_generation_id = EXCLUDED.active_generation_id`,
			scopeID, now, activeGenAny,
		); err != nil {
			t.Fatalf("seed scope %s: %v", scopeID, err)
		}
	}
	seedGeneration := func(genID, scopeID, status string) {
		t.Helper()
		var activatedAt any
		if status == "active" {
			activatedAt = now
		}
		if _, err := database.ExecContext(ctx, `
INSERT INTO scope_generations
  (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
VALUES ($1, $2, 'manual', $3, $3, $4, $5)
ON CONFLICT (generation_id) DO UPDATE SET status = EXCLUDED.status, activated_at = EXCLUDED.activated_at`,
			genID, scopeID, now, status, activatedAt,
		); err != nil {
			t.Fatalf("seed generation %s: %v", genID, err)
		}
	}
	seedFact := func(scopeID, genID, kind, key string, tombstone bool, payload string) {
		t.Helper()
		if _, err := database.ExecContext(ctx, `
INSERT INTO fact_records
  (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key,
   observed_at, ingested_at, is_tombstone, payload)
VALUES ($1, $2, $3, $4, $5, 'aws', $5, $6, $6, $7, $8::jsonb)`,
			prefix+"-fact-"+key, scopeID, genID, kind, key, now, tombstone, payload,
		); err != nil {
			t.Fatalf("seed fact %s/%s: %v", genID, key, err)
		}
	}

	// Scope A: active A2, superseded A1. Scope B: active B1 only.
	// Scope C: no active generation (pending C0) — its facts must read dead.
	scopeA, scopeB, scopeC := prefix+"-a", prefix+"-b", prefix+"-c"
	genA1, genA2, genB1, genC0 := prefix+"-a1", prefix+"-a2", prefix+"-b1", prefix+"-c0"
	seedScope(scopeA, genA2)
	seedScope(scopeB, genB1)
	seedScope(scopeC, "")
	seedGeneration(genA1, scopeA, "superseded")
	seedGeneration(genA2, scopeA, "active")
	seedGeneration(genB1, scopeB, "active")
	seedGeneration(genC0, scopeC, "pending")

	uidAliveA := prefix + "-alive-a"
	uidShared := prefix + "-shared"
	uidDead := prefix + "-dead"
	uidTomb := prefix + "-tomb"
	uidPendingScope := prefix + "-pending-scope"
	admission := func(uid string) string {
		return fmt.Sprintf(`{"cloud_resource_uid": %q}`, uid)
	}
	seedFact(scopeA, genA2, cloudRetractAdmissionFactKind, "k-alive-a", false, admission(uidAliveA))
	seedFact(scopeA, genA1, cloudRetractAdmissionFactKind, "k-shared-a1", false, admission(uidShared))
	seedFact(scopeB, genB1, cloudRetractAdmissionFactKind, "k-shared-b1", false, admission(uidShared))
	seedFact(scopeA, genA1, cloudRetractAdmissionFactKind, "k-dead", false, admission(uidDead))
	seedFact(scopeA, genA2, cloudRetractAdmissionFactKind, "k-tomb", true, admission(uidTomb))
	seedFact(scopeA, genA1, cloudRetractAdmissionFactKind, "k-tomb-old", false, admission(uidTomb))
	seedFact(scopeC, genC0, cloudRetractAdmissionFactKind, "k-pending", false, admission(uidPendingScope))

	posture := func(account, region, rtype, instance, arn string) string {
		return fmt.Sprintf(
			`{"account_id": %q, "region": %q, "resource_type": %q, "instance_id": %q, "arn": %q}`,
			account, region, rtype, instance, arn,
		)
	}
	// tuple-live: current-gen posture in scope B.
	// tuple-dead: superseded-gen only.
	// tuple-tomb: superseded-gen live row + current-gen tombstone.
	// tuple-arn-fallback: no instance_id, arn only, blank resource_type
	//   (exercises the reader's COALESCE fallbacks on both axes).
	seedFact(scopeB, genB1, cloudRetractEC2PostureFactKind, "p-live", false,
		posture("111", "us-east-1", "aws_ec2_instance", "i-live", ""))
	seedFact(scopeA, genA1, cloudRetractEC2PostureFactKind, "p-dead", false,
		posture("111", "us-east-1", "aws_ec2_instance", "i-dead", ""))
	seedFact(scopeA, genA1, cloudRetractEC2PostureFactKind, "p-tomb-old", false,
		posture("111", "us-east-1", "aws_ec2_instance", "i-tomb", ""))
	seedFact(scopeA, genA2, cloudRetractEC2PostureFactKind, "p-tomb", true,
		posture("111", "us-east-1", "aws_ec2_instance", "i-tomb", ""))
	seedFact(scopeB, genB1, cloudRetractEC2PostureFactKind, "p-arn", false,
		posture("222", "eu-west-1", "", "", "arn:aws:ec2:eu-west-1:222:instance/i-arn"))
	seedFact(scopeB, genB1, cloudRetractEC2PostureFactKind, "p-inst-only", false,
		posture("333", "us-west-2", "aws_ec2_instance", "i-only", ""))

	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	q := SQLTx{Tx: tx}

	t.Run("admission_uids", func(t *testing.T) {
		alive, err := LiveAdmissionCloudUIDs(ctx, q, []string{
			uidAliveA, uidShared, uidDead, uidTomb, uidPendingScope, prefix + "-never-existed",
		})
		if err != nil {
			t.Fatalf("LiveAdmissionCloudUIDs: %v", err)
		}
		for _, want := range []string{uidAliveA, uidShared} {
			if _, ok := alive[want]; !ok {
				t.Errorf("uid %q must read live", want)
			}
		}
		for _, wantDead := range []string{uidDead, uidTomb, uidPendingScope, prefix + "-never-existed"} {
			if _, ok := alive[wantDead]; ok {
				t.Errorf("uid %q must read dead", wantDead)
			}
		}
	})

	t.Run("ec2_tuples", func(t *testing.T) {
		uidLive, uidDeadT, uidTombT, uidArn := prefix+"-ec2-live", prefix+"-ec2-dead", prefix+"-ec2-tomb", prefix+"-ec2-arn"
		uidInstOnly := prefix + "-ec2-inst-only"
		alive, err := LiveEC2PostureUIDs(ctx, q, []reducercontract.EC2PostureCandidate{
			{UID: uidLive, AccountID: "111", Region: "us-east-1", ResourceType: "aws_ec2_instance", InstanceID: "i-live"},
			{UID: uidDeadT, AccountID: "111", Region: "us-east-1", ResourceType: "aws_ec2_instance", InstanceID: "i-dead"},
			{UID: uidTombT, AccountID: "111", Region: "us-east-1", ResourceType: "aws_ec2_instance", InstanceID: "i-tomb"},
			{UID: uidArn, AccountID: "222", Region: "eu-west-1", InstanceID: "", ARN: "arn:aws:ec2:eu-west-1:222:instance/i-arn"},
			{UID: uidInstOnly, AccountID: "333", Region: "us-west-2", ResourceType: "aws_ec2_instance", InstanceID: "i-only"},
			{UID: prefix + "-ec2-noid", AccountID: "222", Region: "eu-west-1"},
		})
		if err != nil {
			t.Fatalf("LiveEC2PostureUIDs: %v", err)
		}
		for _, want := range []string{uidLive, uidArn, uidInstOnly} {
			if _, ok := alive[want]; !ok {
				t.Errorf("uid %q must read live", want)
			}
		}
		for _, wantDead := range []string{uidDeadT, uidTombT, prefix + "-ec2-noid"} {
			if _, ok := alive[wantDead]; ok {
				t.Errorf("uid %q must read dead", wantDead)
			}
		}
	})

	t.Run("empty_inputs", func(t *testing.T) {
		alive, err := LiveAdmissionCloudUIDs(ctx, q, nil)
		if err != nil || len(alive) != 0 {
			t.Fatalf("empty admission probe = %v, %v; want empty, nil", alive, err)
		}
		aliveEC2, err := LiveEC2PostureUIDs(ctx, q, nil)
		if err != nil || len(aliveEC2) != 0 {
			t.Fatalf("empty ec2 probe = %v, %v; want empty, nil", aliveEC2, err)
		}
	})

	t.Run("ledger_release", func(t *testing.T) {
		store := ownerstore.NewGraphNodeOwnerStore()
		uid := prefix + "-release-me"
		if _, _, err := store.ResolveOwnedUIDs(ctx, q, []ownerstore.GraphNodeOwnerEntry{
			{UID: uid, SourceOrderKey: "9999-z", WinningRow: []byte(`{}`)},
		}, now); err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if err := store.ReleaseOwnedUIDs(ctx, q, []string{uid}); err != nil {
			t.Fatalf("release: %v", err)
		}
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM graph_node_owner WHERE uid = $1`, uid).Scan(&count); err != nil {
			t.Fatalf("count ledger row: %v", err)
		}
		if count != 0 {
			t.Fatalf("ledger rows for %q = %d, want 0 after release", uid, count)
		}
		// Releasing twice is a no-op so retries and replays reconverge.
		if err := store.ReleaseOwnedUIDs(ctx, q, []string{uid}); err != nil {
			t.Fatalf("second release: %v", err)
		}
		if err := store.ReleaseOwnedUIDs(ctx, q, nil); err != nil {
			t.Fatalf("empty release: %v", err)
		}
	})
}

// TestCloudResourceLivenessRefusesUndrainedAdmissionLive pins the #6887
// fence the review of PR #6892 found missing: reducer_cloud_resource_identity
// is reducer output from the separate cloud_inventory_admission work item,
// so a scope whose active generation has not drained that item has no
// admission rows yet and every uid it still holds would read dead. The
// live-check must refuse to prove death (fail closed, so the retract's work
// item retries) while any active-generation admission item is nonterminal,
// must answer normally once it is succeeded, and must ignore nonterminal
// items on non-active generations.
//
// Skipped by default; set ESHU_CLOUD_RETRACT_LIVE=1 and ESHU_POSTGRES_DSN.
func TestCloudResourceLivenessRefusesUndrainedAdmissionLive(t *testing.T) {
	if strings.TrimSpace(os.Getenv("ESHU_CLOUD_RETRACT_LIVE")) == "" {
		t.Skip("set ESHU_CLOUD_RETRACT_LIVE=1 and ESHU_POSTGRES_DSN to run the cloud retract liveness proof")
	}
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("ESHU_POSTGRES_DSN not set")
	}
	ctx := context.Background()
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer func() { _ = database.Close() }()
	prefix := fmt.Sprintf("retract-fence-%d", time.Now().UnixNano())
	now := time.Now().UTC().Truncate(time.Millisecond)

	scopeB := prefix + "-b"
	genB1, genB2 := prefix+"-b1", prefix+"-b2"
	uidHeld := prefix + "-held"
	if _, err := database.ExecContext(ctx, `
INSERT INTO ingestion_scopes
  (scope_id, scope_kind, source_system, source_key, collector_kind,
   partition_key, observed_at, ingested_at, status, active_generation_id, payload)
VALUES ($1, 'aws', 'aws', $1, 'aws', $1, $2, $2, 'active', NULL, '{}'::jsonb)`,
		scopeB, now); err != nil {
		t.Fatalf("seed scope: %v", err)
	}
	for gen, status := range map[string]string{genB1: "superseded", genB2: "active"} {
		if _, err := database.ExecContext(ctx, `
INSERT INTO scope_generations
  (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
VALUES ($1, $2, 'manual', $3, $3, $4, $3)`, gen, scopeB, now, status); err != nil {
			t.Fatalf("seed generation %s: %v", gen, err)
		}
	}
	if _, err := database.ExecContext(ctx,
		`UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1`, scopeB, genB2); err != nil {
		t.Fatalf("activate B2: %v", err)
	}
	// B1 admitted the uid; B2's admission has not run yet, so B2 has no rows.
	if _, err := database.ExecContext(ctx, `
INSERT INTO fact_records
  (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key,
   observed_at, ingested_at, is_tombstone, payload)
VALUES ($1, $2, $3, $4, $5, 'aws', $5, $6, $6, FALSE, $7::jsonb)`,
		prefix+"-fact-held", scopeB, genB1, cloudRetractAdmissionFactKind, "k-held", now,
		fmt.Sprintf(`{"cloud_resource_uid": %q}`, uidHeld)); err != nil {
		t.Fatalf("seed admission: %v", err)
	}
	seedWork := func(id, gen, status string) {
		t.Helper()
		if _, err := database.ExecContext(ctx, `
INSERT INTO fact_work_items
  (work_item_id, scope_id, generation_id, stage, domain, status, created_at, updated_at)
VALUES ($1, $2, $3, 'reducer', $4, $5, $6, $6)
ON CONFLICT (work_item_id) DO UPDATE SET status = EXCLUDED.status, updated_at = EXCLUDED.updated_at`,
			id, scopeB, gen, reducercontract.DomainCloudInventoryAdmission, status, now); err != nil {
			t.Fatalf("seed work item %s=%s: %v", id, status, err)
		}
	}
	defer func() {
		_, _ = database.ExecContext(ctx, `DELETE FROM ingestion_scopes WHERE scope_id = $1`, scopeB)
	}()

	probe := func() (map[string]struct{}, error) {
		t.Helper()
		tx, err := database.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer func() { _ = tx.Rollback() }()
		return LiveAdmissionCloudUIDs(ctx, SQLTx{Tx: tx}, []string{uidHeld})
	}
	itemB2 := prefix + "-work-b2"
	for _, status := range []string{"pending", "claimed", "running", "retrying", "failed", "dead_letter"} {
		seedWork(itemB2, genB2, status)
		alive, err := probe()
		if !errors.Is(err, reducercontract.ErrCloudAdmissionUndrained) {
			t.Fatalf("status %s: err = %v (alive=%v), want ErrCloudAdmissionUndrained", status, err, alive)
		}
		if !strings.Contains(err.Error(), scopeB) {
			t.Fatalf("status %s: error %q does not name the undrained scope", status, err)
		}
	}
	// A nonterminal item on the superseded generation is not the active
	// generation's admission and must not block.
	seedWork(itemB2, genB2, "succeeded")
	seedWork(prefix+"-work-b1", genB1, "pending")
	alive, err := probe()
	if err != nil {
		t.Fatalf("drained active admission: err = %v, want nil", err)
	}
	if _, ok := alive[uidHeld]; ok {
		t.Fatalf("uid admitted only by the superseded generation read alive after B2's admission drained with no rows")
	}
	// Superseded active item counts as drained too (the generation was
	// replaced, its admission will never run).
	seedWork(itemB2, genB2, "superseded")
	if _, err := probe(); err != nil {
		t.Fatalf("superseded active admission: err = %v, want nil", err)
	}
}
