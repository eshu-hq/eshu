// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// memoHitReopenWorkStatus returns the status of one work item.
func memoHitReopenWorkStatus(t *testing.T, p *targetedDiffPair, database *sql.DB, workItemID string) string {
	t.Helper()
	var status string
	if err := database.QueryRowContext(p.ctx,
		`SELECT status FROM fact_work_items WHERE work_item_id = $1`, workItemID).Scan(&status); err != nil {
		t.Fatalf("status of %s: %v", workItemID, err)
	}
	return status
}

// memoHitReopenPendingCount counts pending/retrying rows for a generation and
// domain set.
func memoHitReopenPendingCount(t *testing.T, p *targetedDiffPair, database *sql.DB, generationID string, domains ...string) int {
	t.Helper()
	var count int
	for _, domain := range domains {
		var c int
		if err := database.QueryRowContext(p.ctx,
			`SELECT count(*) FROM fact_work_items
WHERE generation_id = $1 AND domain = $2 AND status IN ('pending', 'retrying')`,
			generationID, domain).Scan(&c); err != nil {
			t.Fatalf("pending count %s/%s: %v", generationID, domain, err)
		}
		count += c
	}
	return count
}

// memoHitReopenRelationshipSnapshot captures every relationship-domain work
// row's (id, status, attempt_count) outside the excluded generations, for
// exactly-once comparison across passes. Correlation domains are out of scope
// (they reopen unconditionally every pass by design), and so is any listed
// generation: a memo-miss generation reopens its relationship items on every
// pass (pre-existing #4770 behavior), so only memo-hit generations belong in
// a stability snapshot.
func memoHitReopenRelationshipSnapshot(t *testing.T, p *targetedDiffPair, database *sql.DB, excludeGenerationID string) string {
	t.Helper()
	rows, err := database.QueryContext(p.ctx,
		`SELECT work_item_id || '|' || status || '|' || attempt_count::text
FROM fact_work_items
WHERE scope_id <> 'eshu:global'
  AND domain IN ('deployment_mapping', 'code_import_repo_edge')
  AND generation_id <> $1
ORDER BY 1`, excludeGenerationID)
	if err != nil {
		t.Fatalf("snapshot work rows: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var snapshot string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan work snapshot: %v", err)
		}
		snapshot += line + "\n"
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("work snapshot rows: %v", err)
	}
	return snapshot
}

// TestMemoHitPartitionWithNewInboundEvidenceReopensRelationshipItems is the
// TDD regression for issue #7636: a memo-hit source partition that receives
// new inbound evidence this pass (here from a repo-less GCP cloud scope) must
// have its succeeded deployment_mapping and code_import_repo_edge items
// reopened so they re-read the evidence. On main they stay succeeded: the
// same-pass skip set is built at read time and never revised after the write
// phase attaches cross-partition evidence.
//
// The test also pins the fix's boundaries and cost shape:
//   - quiet memo-hit partitions that receive no new evidence keep their
//     relationship items succeeded (0 reopens across 10 quiet scopes: a bulk
//     quiet corpus must not turn into a fleet reopen);
//   - correlation items still reopen without the memo gate (pre-existing
//     contract, pinned by the differential wholeOutside sets);
//   - the reopened items drain exactly once through the real reducer
//     Claim/Ack path, and the following pass leaves every memo-hit
//     generation's relationship rows untouched (memo-miss and correlation
//     reopens every pass are pre-existing behavior, pinned separately).
func TestMemoHitPartitionWithNewInboundEvidenceReopensRelationshipItems(t *testing.T) {
	p := newTargetedDiffPair(t)

	const quietScopes = 10
	// Seed the cloud_scope_gcp_relation_owed shape (the assertions below run
	// against the whole arm only): the corpus reaches steady state in the
	// pre-pass, then the repo-less GCP scope adds a relation fact the pass
	// attaches as outbound evidence (repo-gsrc->repo-tgt) on gsrc-1's
	// generation, while gsrc-1's own facts are unchanged (same-pass memo
	// hit). Ten extra quiet git scopes bound the cost: they receive no new
	// evidence, so the fix must not reopen their relationship items.
	seedTargetedCorpus(p)
	p.gitRepo("git:gsrc", "gsrc-1", "repo-gsrc", "order-gateway")
	p.workItems("git:gsrc", "gsrc-1")
	p.scope(targetedGCPScope)
	p.generation(targetedGCPScope, "gcp-1", 0, true)
	for i := 0; i < quietScopes; i++ {
		scopeID := fmt.Sprintf("git:quiet%d", i)
		generationID := fmt.Sprintf("quiet%d-1", i)
		repoID := fmt.Sprintf("repo-quiet%d", i)
		p.gitRepo(scopeID, generationID, repoID, fmt.Sprintf("quiet-%d", i))
		p.workItems(scopeID, generationID)
	}
	p.prepass()
	p.generation(targetedGCPScope, "gcp-2", 90*time.Minute, true)
	p.gcpRelation("gcp-2-edge", "gcp-2", "order-gateway", "payments-service")
	p.workItems(targetedGCPScope, "gcp-2")

	wholeStore := targetedDiffStore(p.whole, targetedDiffArmsAt)
	if err := wholeStore.RunDeferredRelationshipMaintenance(p.ctx, nil, nil); err != nil {
		t.Fatalf("pass 1 RunDeferredRelationshipMaintenance() error = %v", err)
	}

	// gsrc-1 is a same-pass memo hit whose generation received new inbound
	// evidence: its relationship items must reopen (RED on main: succeeded).
	for _, domain := range []string{"deployment_mapping", "code_import_repo_edge"} {
		if status := memoHitReopenWorkStatus(t, p, p.whole, "gsrc-1/"+domain); status != "pending" {
			t.Fatalf("gsrc-1/%s status = %q, want pending: memo-hit partition with new inbound evidence must reopen its relationship items (#7636)",
				domain, status)
		}
	}
	// The memo-miss generation reopens everything, as before.
	for _, id := range workIDs("gcp-2") {
		if status := memoHitReopenWorkStatus(t, p, p.whole, id); status != "pending" {
			t.Fatalf("%s status = %q, want pending", id, status)
		}
	}
	// Memo-hit partitions with no new inbound evidence keep their
	// relationship items succeeded (tgt-1/dep-1 are the corpus's built-in
	// negative control alongside the quiet scopes below).
	for _, id := range []string{
		"tgt-1/deployment_mapping", "tgt-1/code_import_repo_edge",
		"dep-1/deployment_mapping", "dep-1/code_import_repo_edge",
	} {
		if status := memoHitReopenWorkStatus(t, p, p.whole, id); status != "succeeded" {
			t.Fatalf("%s status = %q, want succeeded: no new evidence, no reopen", id, status)
		}
	}
	// Quiet memo-hit partitions receive no new evidence: their relationship
	// items stay succeeded. Exact cost bound: 0 reopens across all quiet
	// scopes, so the fix adds reopens only where evidence landed.
	quietRelationshipReopened := 0
	for i := 0; i < quietScopes; i++ {
		generationID := fmt.Sprintf("quiet%d-1", i)
		quietRelationshipReopened += memoHitReopenPendingCount(t, p, p.whole, generationID,
			"deployment_mapping", "code_import_repo_edge")
	}
	if quietRelationshipReopened != 0 {
		t.Fatalf("quiet memo-hit relationship items reopened = %d, want 0", quietRelationshipReopened)
	}

	// Drain every reopened item through the real reducer Claim/Ack path: a
	// reopened item that cannot be claimed would trade the #7636 stall for a
	// pending stall, so claimability is part of the regression.
	// The drain queue uses the real clock, not the fixed arm time: Ack's
	// lease fence compares claim_until against the database's
	// clock_timestamp(), so a fixed past Now would expire every claim
	// before it is acked.
	queue := ReducerQueue{
		database:      SQLDB{DB: p.whole},
		LeaseOwner:    "memo-hit-reopen-drain",
		LeaseDuration: time.Minute,
	}
	for i := 0; i < 500; i++ {
		intent, claimed, err := queue.Claim(p.ctx)
		if err != nil {
			t.Fatalf("drain Claim() error = %v", err)
		}
		if !claimed {
			break
		}
		if err := queue.Ack(p.ctx, intent, reducer.Result{}); err != nil {
			t.Fatalf("drain Ack(%s) error = %v", intent.IntentID, err)
		}
	}
	// Every reopened relationship item drained; only the
	// readiness-gated kubernetes correlation items remain pending
	// (pre-existing gate, unrelated to this fix).
	for _, id := range []string{
		"gsrc-1/deployment_mapping", "gsrc-1/code_import_repo_edge",
		"gcp-2/deployment_mapping", "gcp-2/code_import_repo_edge",
	} {
		if status := memoHitReopenWorkStatus(t, p, p.whole, id); status != "succeeded" {
			t.Fatalf("drained %s status = %q, want succeeded: reopened items must drain exactly once", id, status)
		}
	}
	k8sDomain := string(reducer.DomainKubernetesCorrelationMaterialization)
	var k8sPending int
	if err := p.whole.QueryRowContext(p.ctx,
		`SELECT count(*) FROM fact_work_items
WHERE domain = $1 AND status IN ('pending', 'retrying')`, k8sDomain).Scan(&k8sPending); err != nil {
		t.Fatalf("k8s pending count: %v", err)
	}
	var otherPending int
	if err := p.whole.QueryRowContext(p.ctx,
		`SELECT count(*) FROM fact_work_items
WHERE scope_id <> 'eshu:global' AND domain <> $1 AND status IN ('pending', 'retrying')`,
		k8sDomain).Scan(&otherPending); err != nil {
		t.Fatalf("non-k8s pending count: %v", err)
	}
	if otherPending != 0 {
		t.Fatalf("non-k8s pending items after drain = %d, want 0", otherPending)
	}
	t.Logf("7636 drain: k8s-gated still pending = %d (pre-existing readiness gate), all other reopened items drained", k8sPending)

	// Pass 2 with no new evidence: the memo-hit generations' relationship
	// items stay exactly as the drain left them — the fix reopens exactly
	// once (pass 2's revision unskips nothing). gcp-2 is excluded: it is a
	// memo miss, so it reopens every pass by pre-existing design, pinned
	// below.
	beforeSecond := memoHitReopenRelationshipSnapshot(t, p, p.whole, "gcp-2")
	secondStore := targetedDiffStore(p.whole, targetedDiffArmsAt.Add(time.Hour))
	if err := secondStore.RunDeferredRelationshipMaintenance(p.ctx, nil, nil); err != nil {
		t.Fatalf("pass 2 RunDeferredRelationshipMaintenance() error = %v", err)
	}
	if afterSecond := memoHitReopenRelationshipSnapshot(t, p, p.whole, "gcp-2"); afterSecond != beforeSecond {
		t.Fatalf("pass 2 changed memo-hit relationship rows; drained items must not reopen again:\n%s", afterSecond)
	}
	// Pre-existing boundaries, pinned so the exclusion above cannot hide a
	// regression: the memo-miss generation reopens its relationship items
	// every pass, and correlation items reopen unconditionally every pass.
	for _, id := range []string{"gcp-2/deployment_mapping", "gcp-2/code_import_repo_edge"} {
		if status := memoHitReopenWorkStatus(t, p, p.whole, id); status != "pending" {
			t.Fatalf("pass 2 %s status = %q, want pending: memo-miss generations reopen every pass", id, status)
		}
	}
	if status := memoHitReopenWorkStatus(t, p, p.whole, "gsrc-1/deployable_unit_correlation"); status != "pending" {
		t.Fatalf("pass 2 gsrc-1/deployable_unit_correlation status = %q, want pending: correlation items reopen every pass", status)
	}
}
