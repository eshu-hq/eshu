// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/testfixtures"
)

// TestProducerRemovalOnlyGenerationReopensConsumersLive is the #7705
// regression: a producer generation that only tombstones manifests owes (its
// removed keys come from the previous generation's live payloads) and the
// settle reopens the consumers that embedded the removed digests. A disjoint
// consumer must stay succeeded: the intersection still bounds the reopen.
func TestProducerRemovalOnlyGenerationReopensConsumersLive(t *testing.T) {
	if os.Getenv("ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE") != "1" {
		t.Skip("set ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE=1 for disposable PostgreSQL proof")
	}
	database := openIsolatedBootstrapSchema(t, testfixtures.DSNForDeferredPartitionMemoProof(t), "producer_removal")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	seedProducerRemovalCorpus(t, ctx, database)

	carries, err := GenerationCarriesProducerEvidence(ctx, SQLDB{DB: database}, "oci:removal", "removal-new")
	if err != nil {
		t.Fatalf("probe removal-only generation: %v", err)
	}
	if !carries {
		t.Fatalf("probe removal-only generation = false, want true (tombstoned manifest with a live predecessor owes)")
	}
	owed, err := listProducerOwedOCIKeys(ctx, SQLDB{DB: database}, "oci:removal", "removal-new")
	if err != nil {
		t.Fatalf("list removal owed OCI keys: %v", err)
	}
	if len(owed.digests) != 1 || owed.digests[0] != removalDigestA {
		t.Fatalf("removal owed digests = %v, want [%s]", owed.digests, removalDigestA)
	}
	counts, err := SettleProducerActivations(ctx, database)
	if err != nil {
		t.Fatalf("settle removal-only generation: %v", err)
	}
	if len(counts) != 1 || counts["kubernetes_correlation_materialization"] != 1 {
		t.Fatalf("settle reopened %v, want exactly one kubernetes_correlation_materialization item", counts)
	}
	assertProducerObligationState(t, ctx, database, "oci:removal", "removal-new", "completed")
	assertProducerWorkItemStatus(t, ctx, database, "removal-linked/kubernetes_correlation_materialization", "pending")
	assertProducerWorkItemStatus(t, ctx, database, "removal-disjoint/kubernetes_correlation_materialization", "succeeded")
	again, err := SettleProducerActivations(ctx, database)
	if err != nil {
		t.Fatalf("second settle: %v", err)
	}
	if len(again) != 0 {
		t.Fatalf("second settle reopened %v, want nothing (exactly-once)", again)
	}
}

// TestProducerRemovalTiedTimestampReopensLive pins the owner-P2 corner: the
// live predecessor shares the owed generation's ingested_at exactly, and
// the (ingested_at, generation_id) tuple bound still admits it, so the
// removal-only generation owes and the linked consumer replays.
func TestProducerRemovalTiedTimestampReopensLive(t *testing.T) {
	if os.Getenv("ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE") != "1" {
		t.Skip("set ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE=1 for disposable PostgreSQL proof")
	}
	database := openIsolatedBootstrapSchema(t, testfixtures.DSNForDeferredPartitionMemoProof(t), "producer_remtie")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	seedProducerRemovalTiedCorpus(t, ctx, database)

	carries, err := GenerationCarriesProducerEvidence(ctx, SQLDB{DB: database}, "oci:removaltie", "removaltie-gen2")
	if err != nil {
		t.Fatalf("probe tied-timestamp removal generation: %v", err)
	}
	if !carries {
		t.Fatalf("probe tied-timestamp removal generation = false, want true (same-timestamp live predecessor owes)")
	}
	owed, err := listProducerOwedOCIKeys(ctx, SQLDB{DB: database}, "oci:removaltie", "removaltie-gen2")
	if err != nil {
		t.Fatalf("list tied removal owed OCI keys: %v", err)
	}
	if len(owed.digests) != 1 || owed.digests[0] != removalDigestA {
		t.Fatalf("tied removal owed digests = %v, want [%s]", owed.digests, removalDigestA)
	}
	counts, err := SettleProducerActivations(ctx, database)
	if err != nil {
		t.Fatalf("settle tied-timestamp removal generation: %v", err)
	}
	if len(counts) != 1 || counts["kubernetes_correlation_materialization"] != 1 {
		t.Fatalf("settle reopened %v, want exactly one kubernetes_correlation_materialization item", counts)
	}
	assertProducerObligationState(t, ctx, database, "oci:removaltie", "removaltie-gen2", "completed")
	assertProducerWorkItemStatus(t, ctx, database, "removaltie-linked/kubernetes_correlation_materialization", "pending")
}

// TestProducerPartialRemovalReopensBothLive covers the partial-removal half
// of #7705: the owed keys are the live keys plus the removed keys, so the
// consumers of surviving and removed digests both replay.
func TestProducerPartialRemovalReopensBothLive(t *testing.T) {
	if os.Getenv("ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE") != "1" {
		t.Skip("set ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE=1 for disposable PostgreSQL proof")
	}
	database := openIsolatedBootstrapSchema(t, testfixtures.DSNForDeferredPartitionMemoProof(t), "producer_partial")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	seedProducerPartialRemovalCorpus(t, ctx, database)

	counts, err := SettleProducerActivations(ctx, database)
	if err != nil {
		t.Fatalf("settle partial-removal generation: %v", err)
	}
	if len(counts) != 1 || counts["kubernetes_correlation_materialization"] != 2 {
		t.Fatalf("settle reopened %v, want two kubernetes_correlation_materialization items (surviving + removed)", counts)
	}
	assertProducerObligationState(t, ctx, database, "oci:partial", "partial-new", "completed")
}

// TestProducerTombstoneWithoutPredecessorStaysInapplicableLive guards the
// over-owe direction: a tombstone for a key that was never live has no
// predecessor payload, owes nothing, and retires inapplicable. Green before
// and after #7705; it fails if the tombstone arm ever drops the predecessor
// requirement.
func TestProducerTombstoneWithoutPredecessorStaysInapplicableLive(t *testing.T) {
	if os.Getenv("ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE") != "1" {
		t.Skip("set ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE=1 for disposable PostgreSQL proof")
	}
	database := openIsolatedBootstrapSchema(t, testfixtures.DSNForDeferredPartitionMemoProof(t), "producer_nopred")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	seedProducerNoPredecessorCorpus(t, ctx, database)

	carries, err := GenerationCarriesProducerEvidence(ctx, SQLDB{DB: database}, "oci:nopred", "nopred-new")
	if err != nil {
		t.Fatalf("probe predecessor-less tombstone generation: %v", err)
	}
	if carries {
		t.Fatalf("probe predecessor-less tombstone generation = true, want false")
	}
	counts, err := SettleProducerActivations(ctx, database)
	if err != nil {
		t.Fatalf("settle predecessor-less tombstone generation: %v", err)
	}
	if len(counts) != 0 {
		t.Fatalf("settle reopened %v, want nothing", counts)
	}
	assertProducerObligationState(t, ctx, database, "oci:nopred", "nopred-new", "inapplicable")
}

// TestProducerRemovalKindPrefilterDifferentialLive proves the probe's kind
// prefilter never changes the outcome on removal shapes: the shipped
// prefiltered query and the unprefiltered query agree on removal-only,
// partial-removal, predecessor-less, and pod-tombstone generations.
func TestProducerRemovalKindPrefilterDifferentialLive(t *testing.T) {
	if os.Getenv("ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE") != "1" {
		t.Skip("set ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE=1 for disposable PostgreSQL proof")
	}
	database := openIsolatedBootstrapSchema(t, testfixtures.DSNForDeferredPartitionMemoProof(t), "producer_remdiff")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	seedProducerRemovalCorpus(t, ctx, database)
	seedProducerPartialRemovalCorpus(t, ctx, database)
	seedProducerNoPredecessorCorpus(t, ctx, database)
	seedProducerPodRemovalCorpus(t, ctx, database)

	run := func(query, scope, generation string) bool {
		t.Helper()
		var exists bool
		if err := database.QueryRowContext(ctx, query, scope, generation).Scan(&exists); err != nil {
			t.Fatalf("run probe variant on %s/%s: %v", scope, generation, err)
		}
		return exists
	}
	for _, generation := range [][2]string{
		{"oci:removal", "removal-new"},
		{"oci:partial", "partial-new"},
		{"oci:nopred", "nopred-new"},
		{"oci:podremoval", "podremoval-new"},
	} {
		filtered := run(producerEvidenceExistsQuery, generation[0], generation[1])
		unfiltered := run(producerEvidenceExistsUnprefilteredQuery, generation[0], generation[1])
		if filtered != unfiltered {
			t.Errorf("probe disagreement on %s/%s: prefiltered=%v unprefiltered=%v",
				generation[0], generation[1], filtered, unfiltered)
		}
	}
}

// TestProducerRemovalOwedKeysPlanLive pins the #7705 predecessor lookup to
// indexed probes: against bulk unrelated rows the owed-keys query must probe
// a (scope_id, generation_id)-anchored index on both the owed and the
// predecessor side, never a fact_records sequence scan.
func TestProducerRemovalOwedKeysPlanLive(t *testing.T) {
	if os.Getenv("ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE") != "1" {
		t.Skip("set ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE=1 for disposable PostgreSQL proof")
	}
	database := openIsolatedBootstrapSchema(t, testfixtures.DSNForDeferredPartitionMemoProof(t), "producer_remplan")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	seedProducerRemovalCorpus(t, ctx, database)
	seedProducerRemovalBulk(t, ctx, database, 20000)

	planRows, err := database.QueryContext(ctx,
		"EXPLAIN (ANALYZE, BUFFERS) "+producerOwedOCIKeysQuery, "oci:removal", "removal-new")
	if err != nil {
		t.Fatalf("explain removal owed keys: %v", err)
	}
	var plan strings.Builder
	for planRows.Next() {
		var line string
		if err := planRows.Scan(&line); err != nil {
			t.Fatalf("scan explain row: %v", err)
		}
		plan.WriteString(line + "\n")
	}
	_ = planRows.Close()
	if err := planRows.Err(); err != nil {
		t.Fatalf("explain removal owed keys rows: %v", err)
	}
	t.Logf("removal owed-keys plan (20k bulk rows):\n%s", plan.String())
	if strings.Contains(plan.String(), "Seq Scan on fact_records") {
		t.Fatalf("removal owed-keys plan sequence-scans fact_records, want indexed probes:\n%s", plan.String())
	}
	// Both the owed generation and the predecessor lookup must ride a
	// (scope_id, generation_id)-anchored index. Either production index
	// qualifies: fact_records_scope_generation_idx or
	// fact_records_scope_generation_keyset_idx (migration 099); the
	// planner legitimately picks either, so pin the shared prefix and
	// require one probe per side.
	if probes := strings.Count(plan.String(), "fact_records_scope_generation"); probes < 2 {
		t.Fatalf("removal owed-keys plan has %d (scope, generation) probes, want >= 2:\n%s", probes, plan.String())
	}
}
