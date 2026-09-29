// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

// #7388: the two projector_stale_scope_reclaim UPDATEs moved an expired row back
// to retrying and replaced its failure_details with scope and work ids, so the
// cause the last attempt failed with was gone: the retry that follows may
// succeed or be superseded, and nothing kept what the reclaim erased. These
// proofs run both reclaim writers through Claim against real PostgreSQL (set
// ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN). The class and message stay the
// reclaim marker, which other tests and the status views read.

const (
	reclaimClass   = "projector_stale_scope_reclaim"
	reclaimMessage = "expired duplicate projector lease reclaimed"
)

// reclaimEvidenceSeed is an expired claimed row that carries the failure its
// previous attempt wrote.
func reclaimEvidenceSeed() pfSeed {
	return pfWithLease(pfSeed{
		attempts: 1,
		class:    pfStr("projection_retryable"), message: pfStr("boom"), details: pfStr("attempt-1"),
	}, "claimed", "dead-worker", -time.Minute)
}

// reclaimPriorWant is what the fold must have recorded for seed.
func reclaimPriorWant(seed pfSeed, status string) map[string]any {
	nullable := func(p *string) any {
		if p == nil {
			return nil
		}
		return *p
	}
	return map[string]any{
		"status":          status,
		"failure_class":   nullable(seed.class),
		"failure_message": nullable(seed.message),
		"failure_details": nullable(seed.details),
	}
}

// requirePrior asserts details carries prior_failure equal to want, with the
// seeded updated_at, and returns the row's other keys.
func requirePrior(t *testing.T, label string, details map[string]any, want map[string]any) map[string]any {
	t.Helper()
	prior, ok := details["prior_failure"].(map[string]any)
	if !ok {
		t.Fatalf("%s: prior_failure missing from failure_details %v", label, details)
	}
	updated, _ := prior["updated_at"].(string)
	got := map[string]any{}
	for k, v := range prior {
		if k != "updated_at" {
			got[k] = v
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s: prior_failure = %v, want %v", label, got, want)
	}
	if parsed, err := time.Parse(time.RFC3339Nano, updated); err != nil || !parsed.Equal(pfUpdatedAt) {
		t.Fatalf("%s: prior_failure.updated_at = %q (%v), want %v", label, updated, err, pfUpdatedAt)
	}
	rest := map[string]any{}
	for k, v := range details {
		if k != "prior_failure" {
			rest[k] = v
		}
	}
	return rest
}

// TestProjectorClaimReclaimKeepsPriorFailureDuplicate covers the first reclaim
// UPDATE: an expired duplicate lease beside a live one.
func TestProjectorClaimReclaimKeepsPriorFailureDuplicate(t *testing.T) {
	dsn := claimMaintenanceProofDSN(t)
	database := openClaimDeadlockProofDB(t, dsn, 2)
	const scopeID = "reclaim-dup"
	pfSeedScope(t, database, scopeID, "pending", "active")
	seed := reclaimEvidenceSeed()
	pfInsertWork(t, database, "old-"+scopeID, scopeID, scopeID+"-gen-old", "projector", seed)
	live := pfWithLease(pfSeed{attempts: 1}, "running", "live-worker", time.Minute)
	pfInsertWork(t, database, "new-"+scopeID, scopeID, scopeID+"-gen-new", "projector", live)

	queue := NewProjectorQueue(SQLDB{DB: database}, "claimer", time.Minute)
	if _, ok, err := queue.Claim(context.Background()); err != nil || ok {
		t.Fatalf("Claim() ok=%v err=%v, want no claim while the scope has a live lease", ok, err)
	}

	row := pfRead(t, database, "old-"+scopeID)
	if row.status != "retrying" || row.class.String != reclaimClass || row.message.String != reclaimMessage {
		t.Fatalf("row = (%s, %q, %q), want (retrying, %q, %q)", row.status, row.class.String, row.message.String, reclaimClass, reclaimMessage)
	}
	rest := requirePrior(t, "duplicate reclaim", row.details, reclaimPriorWant(seed, "claimed"))
	if want := map[string]any{"scope_id": scopeID, "work_item_id": "old-" + scopeID}; !reflect.DeepEqual(rest, want) {
		t.Fatalf("reclaim's own detail keys = %v, want %v", rest, want)
	}
}

// TestProjectorClaimReclaimKeepsPriorFailureSibling covers the second reclaim
// UPDATE: an expired sibling of the row this claim takes. The claimed row's id
// stays in the details.
func TestProjectorClaimReclaimKeepsPriorFailureSibling(t *testing.T) {
	dsn := claimMaintenanceProofDSN(t)
	database := openClaimDeadlockProofDB(t, dsn, 2)
	const scopeID = "reclaim-sib"
	pfSeedScope(t, database, scopeID, "active", "pending")
	seed := reclaimEvidenceSeed()
	pfInsertWork(t, database, "old-"+scopeID, scopeID, scopeID+"-gen-old", "projector", seed)
	pfInsertWork(t, database, "new-"+scopeID, scopeID, scopeID+"-gen-new", "projector", seed)

	queue := NewProjectorQueue(SQLDB{DB: database}, "claimer", time.Minute)
	work, ok, err := queue.Claim(context.Background())
	if err != nil || !ok {
		t.Fatalf("Claim() ok=%v err=%v, want the expired lease reclaimed and claimed", ok, err)
	}
	claimedID, siblingID := "old-"+scopeID, "new-"+scopeID
	if work.Generation.GenerationID == scopeID+"-gen-new" {
		claimedID, siblingID = siblingID, claimedID
	}

	row := pfRead(t, database, siblingID)
	if row.status != "retrying" || row.class.String != reclaimClass || row.message.String != reclaimMessage {
		t.Fatalf("sibling = (%s, %q, %q), want (retrying, %q, %q)", row.status, row.class.String, row.message.String, reclaimClass, reclaimMessage)
	}
	rest := requirePrior(t, "sibling reclaim", row.details, reclaimPriorWant(seed, "claimed"))
	want := map[string]any{"scope_id": scopeID, "work_item_id": siblingID, "claimed_work_item_id": claimedID}
	if !reflect.DeepEqual(rest, want) {
		t.Fatalf("reclaim's own detail keys = %v, want %v", rest, want)
	}
}

// TestProjectorClaimReclaimDoesNotNestAnAlreadyReclaimedRow: a row that already
// carries the marker keeps its details byte for byte. Folding it again would
// nest the reclaim's own bookkeeping one level deeper on every reclaim and bury
// the failure the first fold kept.
func TestProjectorClaimReclaimDoesNotNestAnAlreadyReclaimedRow(t *testing.T) {
	dsn := claimMaintenanceProofDSN(t)
	database := openClaimDeadlockProofDB(t, dsn, 2)
	const scopeID = "reclaim-nest"
	pfSeedScope(t, database, scopeID, "pending", "active")
	kept := `{"scope_id":"reclaim-nest","work_item_id":"old-reclaim-nest","prior_failure":{"status":"claimed","failure_class":"projection_retryable","failure_message":"boom","failure_details":"attempt-1","updated_at":"2026-01-01T00:00:00Z"}}`
	seed := pfWithLease(pfSeed{
		attempts: 2,
		class:    pfStr(reclaimClass), message: pfStr(reclaimMessage), details: pfStr(kept),
	}, "claimed", "dead-worker", -time.Minute)
	pfInsertWork(t, database, "old-"+scopeID, scopeID, scopeID+"-gen-old", "projector", seed)
	pfInsertWork(t, database, "new-"+scopeID, scopeID, scopeID+"-gen-new", "projector",
		pfWithLease(pfSeed{attempts: 1}, "running", "live-worker", time.Minute))

	queue := NewProjectorQueue(SQLDB{DB: database}, "claimer", time.Minute)
	if _, ok, err := queue.Claim(context.Background()); err != nil || ok {
		t.Fatalf("Claim() ok=%v err=%v, want no claim while the scope has a live lease", ok, err)
	}
	row := pfRead(t, database, "old-"+scopeID)
	if row.status != "retrying" {
		t.Fatalf("status = %q, want retrying", row.status)
	}
	if row.raw != kept {
		t.Fatalf("failure_details changed on a second reclaim:\n got  %s\n want %s", row.raw, kept)
	}
}

// TestProjectorClaimReclaimThenSupersedeKeepsTheRealCause is the chain: the
// reclaim folds the real cause, then a claim sweep supersedes the reclaimed row
// and folds the reclaim's own row in turn. The superseded row's prior_failure is
// the reclaim marker, and the details it carries hold the real cause one level
// down, so the cause survives both writers.
func TestProjectorClaimReclaimThenSupersedeKeepsTheRealCause(t *testing.T) {
	dsn := claimMaintenanceProofDSN(t)
	database := openClaimDeadlockProofDB(t, dsn, 2)
	const scopeID = "reclaim-chain"
	pfSeedScope(t, database, scopeID, "pending", "pending")
	seed := reclaimEvidenceSeed()
	pfInsertWork(t, database, "old-"+scopeID, scopeID, scopeID+"-gen-old", "projector", seed)
	// The newer generation's row holds a live lease, so the first claim only
	// reclaims the expired duplicate.
	pfInsertWork(t, database, "new-"+scopeID, scopeID, scopeID+"-gen-new", "projector",
		pfWithLease(pfSeed{attempts: 1}, "running", "live-worker", time.Minute))

	queue := NewProjectorQueue(SQLDB{DB: database}, "claimer", time.Minute)
	ctx := context.Background()
	if _, ok, err := queue.Claim(ctx); err != nil || ok {
		t.Fatalf("first Claim() ok=%v err=%v, want the duplicate reclaimed and nothing claimed", ok, err)
	}
	if status := pfRead(t, database, "old-"+scopeID).status; status != "retrying" {
		t.Fatalf("after reclaim status = %q, want retrying", status)
	}

	// The live lease ends; the newer generation's row becomes claimable and the
	// stale generation's retrying row is now sweepable.
	if _, err := database.Exec(`
UPDATE fact_work_items SET status = 'pending', lease_owner = NULL, claim_until = NULL
WHERE work_item_id = $1`, "new-"+scopeID); err != nil {
		t.Fatalf("release live lease: %v", err)
	}
	if _, ok, err := queue.Claim(ctx); err != nil || !ok {
		t.Fatalf("second Claim() ok=%v err=%v, want the newer generation claimed", ok, err)
	}

	row := pfRead(t, database, "old-"+scopeID)
	if row.status != "superseded" || row.class.String != pfProjectorClass {
		t.Fatalf("row = (%s, %q), want (superseded, %q)", row.status, row.class.String, pfProjectorClass)
	}
	prior, ok := row.details["prior_failure"].(map[string]any)
	if !ok {
		t.Fatalf("prior_failure missing: %v", row.details)
	}
	if prior["failure_class"] != reclaimClass || prior["status"] != "retrying" {
		t.Fatalf("prior_failure = %v, want the reclaim row (retrying, %s)", prior, reclaimClass)
	}
	nested, _ := prior["failure_details"].(string)
	var inner map[string]any
	if err := json.Unmarshal([]byte(nested), &inner); err != nil {
		t.Fatalf("prior_failure.failure_details %q is not the reclaim's JSON: %v", nested, err)
	}
	requirePrior(t, "reclaim row inside the supersede's prior_failure", inner, reclaimPriorWant(seed, "claimed"))
}
