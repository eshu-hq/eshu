// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// TestReducerClaimDoesNotSupersedeFanOutDebtBehindNewerGeneration is the
// queue-layer half of the #10060 regression. A CVE-keyed holder fan-out that
// exceeds the per-claim cap re-pends its supply_chain_impact item carrying
// the unreached holders in payload fanout_remaining (and the fanout_owed
// marker until the last holder drains). The acknowledgement consumes
// cross_scope_replay_required, so the payload is the only durable record of
// the debt. Before the fix, the claim statement's
// supersedeInactiveReducerGenerationsCTE marked that older-generation row
// 'superseded' — terminal and unreplayable — the first time any worker
// claimed after a newer generation activated, and the holders on the list
// were never reopened. The successor generation's own pass does not cover
// them: its changed-CVE diff is taken against the previous generation's
// stored rows, which already include the committing pass's change.
//
// The spare must stay narrow, mirroring the readiness-gate precedent
// (#4445): manifest-dependency markers keep superseding (their
// completion-stamp baseline diff covers a skipped generation, #9874),
// debt-free rows keep superseding, and the same payload keys on another
// domain do not spare it.
func TestReducerClaimDoesNotSupersedeFanOutDebtBehindNewerGeneration(t *testing.T) {
	dsn := reducerSupersedeReadinessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_SUPERSEDE_READINESS_DSN or ESHU_POSTGRES_DSN to run the fan-out-debt supersede proof")
	}

	debtRemaining := `{"entity_key":"supply_chain_impact:scope-fanout-debt","` + supplychaincore.PayloadKeyFanOutRemaining + `":["scope:holder-a","scope:holder-b"]}`
	debtOwed := `{"entity_key":"supply_chain_impact:scope-fanout-debt","` + supplychaincore.PayloadKeyFanOutOwed + `":true}`
	debtBoth := `{"entity_key":"supply_chain_impact:scope-fanout-debt","` + supplychaincore.PayloadKeyFanOutOwed + `":true,"` + supplychaincore.PayloadKeyFanOutRemaining + `":["scope:holder-a"]}`
	markerDebt := `{"entity_key":"supply_chain_impact:scope-fanout-debt","` + supplychaincore.PayloadKeyTrigger + `":"` + supplychaincore.TriggerManifestDependency + `","` + supplychaincore.PayloadKeyFanOutRemaining + `":["scope:holder-a"]}`

	cases := []struct {
		name        string
		domain      string
		status      string
		payload     string
		wantClaimed bool
		wantStatus  string
	}{
		{name: "continuation list and owed marker are spared and claimed", domain: string(reducer.DomainSupplyChainImpact), status: "pending", payload: debtBoth, wantClaimed: true, wantStatus: "claimed"},
		{name: "continuation list alone is spared and claimed", domain: string(reducer.DomainSupplyChainImpact), status: "pending", payload: debtRemaining, wantClaimed: true, wantStatus: "claimed"},
		{name: "owed marker alone is spared and claimed", domain: string(reducer.DomainSupplyChainImpact), status: "pending", payload: debtOwed, wantClaimed: true, wantStatus: "claimed"},
		{name: "manifest marker continuation still supersedes", domain: string(reducer.DomainSupplyChainImpact), status: "pending", payload: markerDebt, wantClaimed: false, wantStatus: "superseded"},
		{name: "debt-free supply chain row still supersedes", domain: string(reducer.DomainSupplyChainImpact), status: "pending", payload: `{"entity_key":"supply_chain_impact:scope-fanout-debt"}`, wantClaimed: false, wantStatus: "superseded"},
		{name: "debt keys on another domain still supersede", domain: string(reducer.DomainWorkloadMaterialization), status: "pending", payload: debtBoth, wantClaimed: false, wantStatus: "superseded"},
		{name: "dead letter carrying debt stays a replayable dead letter", domain: string(reducer.DomainSupplyChainImpact), status: "dead_letter", payload: debtRemaining, wantClaimed: false, wantStatus: "dead_letter"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			db := openReducerSupersedeReadinessDB(t, ctx, dsn)

			const (
				scopeID    = "scope-fanout-debt"
				genOld     = "gen-fanout-debt-old"
				genNew     = "gen-fanout-debt-new"
				workItemID = "fanout-debt-work-1"
			)
			oldIngested := time.Date(2026, time.July, 2, 8, 0, 0, 0, time.UTC)
			newIngested := oldIngested.Add(time.Hour)

			insertReducerSupersedeReadinessScope(t, ctx, db, scopeID, genOld, oldIngested)
			insertReducerSupersedeReadinessGeneration(t, ctx, db, genOld, scopeID, oldIngested, "active", oldIngested, nil)
			insertFanOutDebtWorkItem(t, ctx, db, workItemID, scopeID, genOld, tc.domain, tc.status, tc.payload, oldIngested.Add(time.Minute))
			activateReducerSupersedeReadinessGeneration(t, ctx, db, scopeID, genOld, genNew, newIngested)

			queue := ReducerQueue{
				database:      SQLDB{DB: db},
				LeaseOwner:    "fanout-debt-test",
				LeaseDuration: time.Minute,
				Now:           func() time.Time { return newIngested.Add(time.Minute) },
			}
			intent, claimed, err := queue.Claim(ctx)
			if err != nil {
				t.Fatalf("Claim() error = %v", err)
			}
			if claimed != tc.wantClaimed {
				t.Fatalf("Claim() claimed = %v (intent %q), want %v", claimed, intent.IntentID, tc.wantClaimed)
			}
			if claimed && intent.IntentID != workItemID {
				t.Fatalf("Claim() intent = %q, want the spared continuation %q", intent.IntentID, workItemID)
			}
			status, failureClass := readReducerSupersedeReadinessWorkItemStatus(t, ctx, db, workItemID)
			if status != tc.wantStatus {
				t.Fatalf("work item %q status = %q (failure_class=%q), want %q: #10060 stranded fan-out continuation", workItemID, status, failureClass, tc.wantStatus)
			}
		})
	}
}

func insertFanOutDebtWorkItem(
	t *testing.T, ctx context.Context, db *sql.DB,
	workItemID, scopeID, generationID, domain, status, payload string, updatedAt time.Time,
) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, conflict_domain,
    conflict_key, status, attempt_count, payload, created_at, updated_at
) VALUES ($1::text, $2, $3, 'reducer', $4, 'scope', $2, $5, 0, $6::jsonb, $7, $7)`,
		workItemID, scopeID, generationID, domain, status, payload, updatedAt); err != nil {
		t.Fatalf("insert fan-out debt work item %q: %v", workItemID, err)
	}
}
