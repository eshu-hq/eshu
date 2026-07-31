// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestContainerImageIdentityClaimEpochTriggerCatalogLive(t *testing.T) {
	db := openContainerImageIdentityAckCapabilityProofDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()

	rows, err := db.QueryContext(ctx, `
SELECT tgname, pg_get_triggerdef(oid)
FROM pg_trigger
WHERE tgrelid = 'fact_work_items'::regclass
  AND NOT tgisinternal
ORDER BY tgname
`)
	if err != nil {
		t.Fatalf("read fact work item trigger catalog: %v", err)
	}
	defer func() { _ = rows.Close() }()

	var definitions []string
	for rows.Next() {
		var name, definition string
		if err := rows.Scan(&name, &definition); err != nil {
			t.Fatalf("scan fact work item trigger catalog: %v", err)
		}
		definitions = append(definitions, name+"|"+definition)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate fact work item trigger catalog: %v", err)
	}
	if len(definitions) != 1 {
		t.Fatalf(
			"fact work item user triggers = %v, want only claim epoch trigger",
			definitions,
		)
	}
	for _, want := range []string{
		"fact_work_items_container_image_identity_claim_epoch_advance",
		"BEFORE UPDATE OF container_image_identity_claim_epoch",
		"WHEN ((old.domain = 'container_image_identity'::text))",
		"advance_container_image_identity_claim_epoch()",
	} {
		if !strings.Contains(definitions[0], want) {
			t.Fatalf("claim epoch trigger definition missing %q: %s", want, definitions[0])
		}
	}
}

func TestContainerImageIdentityClaimEpochTriggerSkipsUnrelatedRowsLive(
	t *testing.T,
) {
	db := openContainerImageIdentityAckCapabilityProofDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()

	const (
		scopeID      = "repository:5854-claim-trigger-unrelated"
		generationID = "generation:5854-claim-trigger-unrelated"
		workItemID   = "claim-trigger-5854-unrelated"
		owner        = "reducer-5854-claim-trigger"
	)
	now := time.Date(2026, time.July, 30, 23, 0, 0, 0, time.UTC)
	seedContainerImageIdentityAckScope(t, ctx, db, scopeID)
	seedContainerImageIdentityAckGeneration(t, ctx, db, scopeID, generationID)
	seedContainerImageIdentityAckWorkItem(
		t,
		ctx,
		db,
		workItemID,
		scopeID,
		generationID,
		owner,
		now.Add(-time.Minute),
		now.Add(-2*time.Minute),
	)
	if _, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET domain = 'ownership',
    status = 'pending',
    lease_owner = NULL,
    claim_until = NULL
WHERE work_item_id = $1
`, workItemID); err != nil {
		t.Fatalf("prepare unrelated claim trigger row: %v", err)
	}

	queue := ReducerQueue{
		db:            SQLDB{DB: db},
		LeaseOwner:    owner,
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now },
		ClaimDomain:   reducer.DomainOwnership,
	}
	intent, ok, err := queue.Claim(ctx)
	if err != nil {
		t.Fatalf("claim unrelated row: %v", err)
	}
	if !ok || intent.IntentID != workItemID {
		t.Fatalf("claim unrelated row = %+v ok=%t, want %s", intent, ok, workItemID)
	}

	var claimEpoch int64
	if err := db.QueryRowContext(ctx, `
SELECT container_image_identity_claim_epoch
FROM fact_work_items
WHERE work_item_id = $1
`, workItemID).Scan(&claimEpoch); err != nil {
		t.Fatalf("read unrelated claim epoch: %v", err)
	}
	if claimEpoch != 1 {
		t.Fatalf("unrelated claim epoch = %d, want unchanged 1", claimEpoch)
	}
}

func TestContainerImageIdentityClaimEpochAdvancesOnceUnderCompetingClaimersLive(
	t *testing.T,
) {
	db := openContainerImageIdentityAckCapabilityProofDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()

	const (
		scopeID      = "repository:5854-claim-trigger-contention"
		generationID = "generation:5854-claim-trigger-contention"
		workItemID   = "claim-trigger-5854-contention"
		owner        = "reducer-5854-claim-trigger-contention"
	)
	now := time.Date(2026, time.July, 30, 23, 10, 0, 0, time.UTC)
	seedContainerImageIdentityAckScope(t, ctx, db, scopeID)
	seedContainerImageIdentityAckGeneration(t, ctx, db, scopeID, generationID)
	seedContainerImageIdentityAckWorkItem(
		t,
		ctx,
		db,
		workItemID,
		scopeID,
		generationID,
		owner,
		now.Add(-time.Minute),
		now.Add(-2*time.Minute),
	)
	insertContainerImageIdentityCutoverMarker(t, ctx, db, scopeID, generationID)

	queue := ReducerQueue{
		db:            SQLDB{DB: db},
		LeaseOwner:    owner,
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now },
		ClaimDomain:   reducer.DomainContainerImageIdentity,
	}
	type claimResult struct {
		intent reducer.Intent
		ok     bool
		err    error
	}
	start := make(chan struct{})
	results := make(chan claimResult, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	for range 2 {
		go func() {
			ready.Done()
			<-start
			intent, ok, err := queue.Claim(ctx)
			results <- claimResult{intent: intent, ok: ok, err: err}
		}()
	}
	ready.Wait()
	close(start)

	var claimed []reducer.Intent
	for range 2 {
		result := <-results
		if result.err != nil {
			t.Fatalf("competing claim: %v", result.err)
		}
		if result.ok {
			claimed = append(claimed, result.intent)
		}
	}
	if len(claimed) != 1 {
		t.Fatalf("successful competing claims = %d, want 1", len(claimed))
	}
	if claimed[0].IntentID != workItemID || claimed[0].ClaimEpoch != 2 {
		t.Fatalf("winning claim = %+v, want %s at epoch 2", claimed[0], workItemID)
	}
	assertContainerImageIdentityAckClaimFence(
		t,
		ctx,
		db,
		workItemID,
		"running",
		2,
		2,
	)
}
