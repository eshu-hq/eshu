// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log"
	"strings"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Live proofs for #8082: the whole-pass fan-in publishes only the partitions
// whose backward evidence this pass could have changed. A partition that was a
// memo hit at pass start and received no inserted evidence row this pass is
// already published (its phase row and memo row committed together in an
// earlier pass), so republishing it only rewrites two timestamps — and runs the
// ArgoCD-bearing probe, which owned ~97% of fan-in time on the measured shim.

// fanInUnchangedPassAt is the clock of the pass under test. The pre-pass runs at
// targetedDiffBase+1h, so a row still stamped +1h after this pass was not
// rewritten by it.
var fanInUnchangedPassAt = targetedDiffArmsAt

// seedFanInUnchangedCorpus seeds three git partitions and a repo-less GCP
// scope, runs the pre-pass, then sets up pass 2's three cases:
//
//   - A (git:tgt / tgt-1): memo hit at pass start, no new evidence this pass.
//   - B (git:gsrc / gsrc-1): memo hit at pass start that GAINS a new inserted
//     evidence row this pass (the #7636 shape: a GCP relation whose source
//     resolves to repo-gsrc attaches evidence under gsrc-1).
//   - C (git:miss / miss-1): memo miss (its memo row is deleted after the
//     pre-pass, so the fact load reloads it).
//
// The GCP scope has no repository fact, so it is never a fan-in partition.
func seedFanInUnchangedCorpus(t *testing.T) *targetedDiffPair {
	t.Helper()
	p := newTargetedDiffPair(t)
	p.gitRepo("git:tgt", "tgt-1", "repo-tgt", "payments-service")
	p.workItems("git:tgt", "tgt-1")
	p.gitRepo("git:gsrc", "gsrc-1", "repo-gsrc", "order-gateway")
	p.workItems("git:gsrc", "gsrc-1")
	p.gitRepo("git:miss", "miss-1", "repo-miss", "ledger-service")
	p.scope(targetedGCPScope)
	p.generation(targetedGCPScope, "gcp-1", 0, true)
	p.prepass()
	p.generation(targetedGCPScope, "gcp-2", 90*time.Minute, true)
	p.gcpRelation("gcp-2-edge", "gcp-2", "order-gateway", "payments-service")
	p.exec(`DELETE FROM deferred_backfill_partition_memo WHERE scope_id = 'git:miss'`)
	return p
}

// runFanInUnchangedPass runs the real ingester maintenance pass on the whole
// arm with a manual metric reader and returns the captured log text.
func runFanInUnchangedPass(t *testing.T, p *targetedDiffPair) (string, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	original := log.Writer()
	t.Cleanup(func() { log.SetOutput(original) })
	var buf bytes.Buffer
	log.SetOutput(&buf)
	store := targetedDiffStore(p.whole, fanInUnchangedPassAt)
	if err := store.RunDeferredRelationshipMaintenance(p.ctx, nil, instruments); err != nil {
		log.SetOutput(original)
		t.Fatalf("RunDeferredRelationshipMaintenance() error = %v", err)
	}
	log.SetOutput(original)
	return buf.String(), reader
}

// fanInPhaseUpdatedAt returns the cross-repo readiness row's updated_at for one
// partition and whether the row exists.
func fanInPhaseUpdatedAt(t *testing.T, ctx context.Context, p *targetedDiffPair, scopeID, generationID string) (time.Time, bool) {
	t.Helper()
	var updatedAt time.Time
	err := p.whole.QueryRowContext(ctx, `
SELECT updated_at FROM graph_projection_phase_state
WHERE scope_id = $1 AND acceptance_unit_id = $1 AND source_run_id = $2 AND generation_id = $2
  AND keyspace = 'cross_repo_evidence' AND phase = 'backward_evidence_committed'`,
		scopeID, generationID).Scan(&updatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return time.Time{}, false
		}
		t.Fatalf("read phase row %s/%s: %v", scopeID, generationID, err)
	}
	return updatedAt, true
}

// fanInMemoCommittedAt returns the memo row's committed_at for one partition.
func fanInMemoCommittedAt(t *testing.T, ctx context.Context, p *targetedDiffPair, scopeID, generationID string) time.Time {
	t.Helper()
	var committedAt time.Time
	if err := p.whole.QueryRowContext(ctx,
		`SELECT committed_at FROM deferred_backfill_partition_memo WHERE scope_id = $1 AND generation_id = $2`,
		scopeID, generationID).Scan(&committedAt); err != nil {
		t.Fatalf("read memo row %s/%s: %v", scopeID, generationID, err)
	}
	return committedAt
}

// fanInSkippedByReason sums eshu_dp_deferred_backfill_fanin_skipped_total for
// one reason label.
func fanInSkippedByReason(t *testing.T, reader *sdkmetric.ManualReader, reason string) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	var total int64
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, record := range scopeMetrics.Metrics {
			if record.Name != "eshu_dp_deferred_backfill_fanin_skipped_total" {
				continue
			}
			sum, ok := record.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("skipped counter data = %T", record.Data)
			}
			for _, dp := range sum.DataPoints {
				if value, found := dp.Attributes.Value("reason"); found && value.AsString() == reason {
					total += dp.Value
				}
			}
		}
	}
	return total
}

// TestWholePassFanInSkipsUnchangedMemoHitPartitionsLive is the #8082
// RED/GREEN proof. Partition A (memo hit, no new evidence) must not be
// republished: its phase updated_at and memo committed_at keep the pre-pass
// stamp. B (memo hit that gained evidence) and C (memo miss) publish at the
// pass clock. The completion line reports published=2 skipped=1, the
// unchanged-check line names the skip, and the skipped counter carries reason
// unchanged_since_memo.
func TestWholePassFanInSkipsUnchangedMemoHitPartitionsLive(t *testing.T) {
	p := seedFanInUnchangedCorpus(t)
	prepassAt := targetedDiffBase.Add(time.Hour)
	if got, ok := fanInPhaseUpdatedAt(t, p.ctx, p, "git:tgt", "tgt-1"); !ok || !got.Equal(prepassAt) {
		t.Fatalf("precondition: A phase updated_at = %v (present=%v), want %v", got, ok, prepassAt)
	}

	logs, reader := runFanInUnchangedPass(t, p)

	if got, ok := fanInPhaseUpdatedAt(t, p.ctx, p, "git:tgt", "tgt-1"); !ok || !got.Equal(prepassAt) {
		t.Fatalf("A (unchanged memo hit) phase updated_at = %v (present=%v), want %v: the fan-in republished a partition whose evidence this pass did not change", got, ok, prepassAt)
	}
	if got := fanInMemoCommittedAt(t, p.ctx, p, "git:tgt", "tgt-1"); !got.Equal(prepassAt) {
		t.Fatalf("A (unchanged memo hit) memo committed_at = %v, want %v", got, prepassAt)
	}
	for _, partition := range [][2]string{{"git:gsrc", "gsrc-1"}, {"git:miss", "miss-1"}} {
		if got, ok := fanInPhaseUpdatedAt(t, p.ctx, p, partition[0], partition[1]); !ok || !got.Equal(fanInUnchangedPassAt) {
			t.Fatalf("%s phase updated_at = %v (present=%v), want %v (published this pass)", partition[1], got, ok, fanInUnchangedPassAt)
		}
	}
	if got := fanInMemoCommittedAt(t, p.ctx, p, "git:miss", "miss-1"); !got.Equal(fanInUnchangedPassAt) {
		t.Fatalf("C (memo miss) memo committed_at = %v, want %v", got, fanInUnchangedPassAt)
	}
	if !strings.Contains(logs, "deferred_backfill_fanin_unchanged_checked candidates=1 skipped_unchanged=1 republished_missing_phase=0 generation_advanced=0\n") ||
		!strings.Contains(logs, "deferred_backfill_fanin_completed partitions=3 published=2 skipped=1 ") {
		t.Fatalf("fan-in lines missing or wrong; logs:\n%s", logs)
	}
	if got := fanInSkippedByReason(t, reader, "unchanged_since_memo"); got != 1 {
		t.Fatalf("fanin_skipped_total{reason=unchanged_since_memo} = %d, want 1", got)
	}
	if strings.Contains(logs, "deferred_backfill_fanin_partition_skipped=true") {
		t.Fatalf("an unchanged partition is already published, so it must not log a withheld-partition line; logs:\n%s", logs)
	}
	// The reopen gate consumes the same revised set: A's relationship items stay
	// succeeded and B's reopen (the pre-existing #7636 contract).
	if status := memoHitReopenWorkStatus(t, p, p.whole, "tgt-1/deployment_mapping"); status != "succeeded" {
		t.Fatalf("tgt-1/deployment_mapping = %q, want succeeded", status)
	}
	if status := memoHitReopenWorkStatus(t, p, p.whole, "gsrc-1/deployment_mapping"); status != "pending" {
		t.Fatalf("gsrc-1/deployment_mapping = %q, want pending", status)
	}
}

// TestWholePassFanInRepublishesUnchangedPartitionMissingPhaseRowLive
// pins the self-healing check: an unchanged memo-hit partition whose readiness
// row is gone is republished, so skipping unchanged partitions can never leave
// readiness missing.
func TestWholePassFanInRepublishesUnchangedPartitionMissingPhaseRowLive(t *testing.T) {
	p := seedFanInUnchangedCorpus(t)
	p.exec(`DELETE FROM graph_projection_phase_state
WHERE scope_id = 'git:tgt' AND keyspace = 'cross_repo_evidence' AND phase = 'backward_evidence_committed'`)

	logs, reader := runFanInUnchangedPass(t, p)

	if got, ok := fanInPhaseUpdatedAt(t, p.ctx, p, "git:tgt", "tgt-1"); !ok || !got.Equal(fanInUnchangedPassAt) {
		t.Fatalf("A phase updated_at = %v (present=%v), want %v: an unchanged partition missing its phase row must be republished", got, ok, fanInUnchangedPassAt)
	}
	if !strings.Contains(logs, "deferred_backfill_fanin_unchanged_checked candidates=1 skipped_unchanged=0 republished_missing_phase=1 generation_advanced=0\n") ||
		!strings.Contains(logs, "deferred_backfill_fanin_completed partitions=3 published=3 skipped=0 ") {
		t.Fatalf("fan-in lines missing or wrong; logs:\n%s", logs)
	}
	if got := fanInSkippedByReason(t, reader, "unchanged_since_memo"); got != 0 {
		t.Fatalf("fanin_skipped_total{reason=unchanged_since_memo} = %d, want 0", got)
	}
}

// TestWholePassFanInKeepsTargetedBaselineOnActiveMemoLive pins review finding
// F2: targetedCatalogBaselineQuery's owed_latest_memo picks a scope's latest
// memo by committed_at, and the whole pass no longer re-stamps an unchanged
// memo. The active generation's memo must still win over the superseded
// generation's memo after a skipped re-stamp. tgt-1 is published by the
// pre-pass (+1h), tgt-2 by pass 2 (+2h, memo miss), and pass 3 (+3h) skips
// tgt-2 as unchanged. tgt-1's memo is then given a stale fingerprint: if
// owed_latest_memo picked it, the baseline would report a stale catalog.
func TestWholePassFanInKeepsTargetedBaselineOnActiveMemoLive(t *testing.T) {
	p := newTargetedDiffPair(t)
	seedTargetedCorpus(p)
	p.prepass()
	p.quietGeneration("git:tgt", "tgt-2", "repo-tgt", "payments-service", "orders-api")

	pass2At, pass3At := targetedDiffBase.Add(2*time.Hour), targetedDiffBase.Add(3*time.Hour)
	for _, at := range []time.Time{pass2At, pass3At} {
		if err := targetedDiffStore(p.whole, at).RunDeferredRelationshipMaintenance(p.ctx, nil, nil); err != nil {
			t.Fatalf("RunDeferredRelationshipMaintenance(%v) error = %v", at, err)
		}
	}
	if got := fanInMemoCommittedAt(t, p.ctx, p, "git:tgt", "tgt-2"); !got.Equal(pass2At) {
		t.Fatalf("tgt-2 memo committed_at = %v, want %v (pass 3 must skip the unchanged partition)", got, pass2At)
	}
	if got := fanInMemoCommittedAt(t, p.ctx, p, "git:tgt", "tgt-1"); !got.Before(pass2At) {
		t.Fatalf("tgt-1 memo committed_at = %v, want before %v", got, pass2At)
	}

	var fingerprint string
	if err := p.whole.QueryRowContext(p.ctx,
		`SELECT catalog_fingerprint FROM deferred_backfill_partition_memo WHERE scope_id = 'git:tgt' AND generation_id = 'tgt-2'`,
	).Scan(&fingerprint); err != nil {
		t.Fatalf("read tgt-2 fingerprint: %v", err)
	}
	if _, err := p.whole.ExecContext(p.ctx,
		`UPDATE deferred_backfill_partition_memo SET catalog_fingerprint = 'sha256:stale' WHERE scope_id = 'git:tgt' AND generation_id = 'tgt-1'`,
	); err != nil {
		t.Fatalf("stale tgt-1 fingerprint: %v", err)
	}

	var hasBaseline, stale bool
	if err := p.whole.QueryRowContext(p.ctx, targetedCatalogBaselineQuery, fingerprint, array.StringArray([]string{"git:tgt"})).
		Scan(&hasBaseline, &stale); err != nil {
		t.Fatalf("targetedCatalogBaselineQuery: %v", err)
	}
	if !hasBaseline || stale {
		t.Fatalf("baseline = (has=%v, stale=%v), want (true, false): owed_latest_memo must select the active generation's memo", hasBaseline, stale)
	}
}
