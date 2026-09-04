// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package containerimage

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestContainerImageIdentitySupportWriterRetiresPromotedDecisionOnDemotionLive(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the digest-v3 demotion proof")
	}
	ctx := context.Background()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}

	const (
		scopeID            = "repository:v3-live-demotion"
		generation         = "generation:v3-live-demotion"
		promoteID          = "intent:v3-live-demotion-promote"
		demoteID           = "intent:v3-live-demotion-demote"
		digest             = "sha256:5853585358535853585358535853585358535853585358535853585358535853"
		imageRef           = "registry.example.com/team/demotion:prod"
		repository         = "oci-registry://registry.example.com/team/demotion"
		promoteEpoch int64 = 31
		demoteEpoch  int64 = 32
	)
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, cleanupErr := db.ExecContext(
			cleanupCtx,
			`DELETE FROM ingestion_scopes WHERE scope_id = $1`,
			scopeID,
		); cleanupErr != nil {
			t.Errorf("delete demotion proof scope: %v", cleanupErr)
		}
		if closeErr := db.Close(); closeErr != nil {
			t.Errorf("close postgres: %v", closeErr)
		}
	})

	seedContainerImageIdentityLiveScope(t, db, scopeID, generation)
	activationEpoch := containerImageIdentityLiveEpoch(t, db, scopeID, generation)
	writer := PostgresContainerImageIdentitySupportWriter{
		HeldSupportLoader: containerImageIdentityLiveHeldSupportLoader{db: db},
		ClaimedExecer:     containerImageIdentityLiveClaimedExecer{db: db},
	}

	seedContainerImageIdentityLiveWork(t, db, promoteID, scopeID, generation, promoteEpoch)
	promoted := ContainerImageIdentityWrite{
		IntentID:        promoteID,
		ClaimEpoch:      promoteEpoch,
		ActivationEpoch: activationEpoch,
		ScopeID:         scopeID,
		GenerationID:    generation,
		SourceSystem:    "oci_registry",
		Cause:           "golden_demotion_live_proof",
		EvidenceAsOf:    time.Unix(1_700_000_000, 0).UTC(),
		Decisions: []ContainerImageIdentityDecision{{
			ImageRef:         imageRef,
			Digest:           digest,
			RepositoryID:     repository,
			Outcome:          reducercontract.ContainerImageIdentityTagResolved,
			CanonicalWrites:  1,
			IdentityStrength: "tag_observation_with_digest",
		}},
	}
	if _, err := writer.WriteContainerImageIdentityDecisions(ctx, promoted); err != nil {
		t.Fatalf("publish promoted support: %v", err)
	}
	if got := containerImageIdentityLiveVisibleCount(t, db, digest); got != 1 {
		t.Fatalf("promoted current supports = %d, want 1", got)
	}
	completeContainerImageIdentityLiveWork(t, db, promoteID)

	seedContainerImageIdentityLiveWork(t, db, demoteID, scopeID, generation, demoteEpoch)
	demoted := promoted
	demoted.IntentID = demoteID
	demoted.ClaimEpoch = demoteEpoch
	demoted.EvidenceAsOf = promoted.EvidenceAsOf.Add(time.Minute)
	demoted.Decisions = []ContainerImageIdentityDecision{{
		ImageRef:         imageRef,
		Digest:           digest,
		RepositoryID:     repository,
		Outcome:          reducercontract.ContainerImageIdentityStaleTag,
		CanonicalWrites:  0,
		IdentityStrength: "stale_tag_observation",
	}}
	retirement, err := planContainerImageIdentityRetirement(demoted, nil, nil)
	if err != nil {
		t.Fatalf("plan demotion retirement: %v", err)
	}
	demoted.TombstoneDecisions = retirement.Tombstones
	demoted.HeldDecisions = retirement.HeldDecisions
	demoted.LegacyFactIDs = retirement.LegacyFactIDs
	result, err := writer.WriteContainerImageIdentityDecisions(ctx, demoted)
	if err != nil {
		t.Fatalf("publish demoted support set: %v", err)
	}
	if got := containerImageIdentityLiveVisibleCount(t, db, digest); got != 0 {
		t.Fatalf("demoted current supports = %d, want 0", got)
	}

	var activeSupportCount int
	if err := db.QueryRowContext(ctx, `
SELECT support_set.support_count
FROM container_image_identity_scope_state AS state
JOIN container_image_identity_support_sets AS support_set
  ON support_set.set_id = state.active_set_id
WHERE state.scope_id = $1
`, scopeID).Scan(&activeSupportCount); err != nil {
		t.Fatalf("read demoted active support set: %v", err)
	}
	if activeSupportCount != 0 {
		t.Fatalf("demoted active support count = %d, want explicit empty set", activeSupportCount)
	}
	if result.CanonicalWrites != 0 || result.RetirementAttempts != 1 {
		t.Fatalf("demotion result = %#v, want zero current writes and one retirement attempt", result)
	}
}
