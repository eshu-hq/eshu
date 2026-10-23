// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8780 disposable PostgreSQL 18 proof that the #8750 destructive-successor guard anchors a cumulative chained delta at its union-base commit (the newest activated full), so a chained delta that deletes most of the base's files is refused at commit and a small chained delta stores; reuses the #7978 bootstrapped proof schema; wall time is logged, not gated
//eshu:readiness:env ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE ESHU_AWS_DRIFT_ARN_PROOF_DSN ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE ESHU_DEFERRED_PARTITION_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// openActiveDeltaOverFull projects gen-a (200 files) and the delta gen-b over
// it, so gen-b is the active delta at sha-b and gen-a the newest activated
// full at sha-a.
func openActiveDeltaOverFull(t *testing.T) *reprojectProof {
	t.Helper()
	telemetry.ResetSkippedRefreshCountForTesting()
	t.Cleanup(telemetry.ResetSkippedRefreshCountForTesting)
	p := openReprojectProof(t)
	p.seedLargeFullPrior(200, 300)
	first := guardSizingPaths("f", 0, 5)
	gen := p.deltaGeneration("gen-b", "hint-b", "sha-b", "sha-a")
	gen.DeltaUnionBaseCommitSHA = "sha-a"
	p.commitGuardSizing(gen, guardSizingLive(first), 5, first, nil)
	if got := p.project(); len(got) != 1 || got[0] != "gen-b" {
		t.Fatalf("projected = %v, want [gen-b]", got)
	}
	return p
}

// TestGuardAnchorsChainedDeltaAtUnionBaseLive is the #8780 guard proof: a
// cumulative delta fenced on the active delta gen-b and diffed from gen-a
// anchors at gen-a. Deleting 150 of gen-a's 200 files is a sharp shrink and
// is refused at commit. Before #8780 the anchor resolved at the fence commit
// sha-b, found no full, and stored the delta for the projector to refuse.
func TestGuardAnchorsChainedDeltaAtUnionBaseLive(t *testing.T) {
	p := openActiveDeltaOverFull(t)
	deleted := guardSizingPaths("f", 0, 150)
	gen := p.deltaGeneration("gen-c", "hint-c", "sha-c", "sha-b")
	gen.DeltaUnionBaseCommitSHA = "sha-a"
	p.commitGuardSizing(gen, guardSizingTombstones(deleted), 0, nil, deleted)
	p.requireGuardOutcome("gen-c", false, 1)
}

// TestGuardStoresSmallChainedDeltaLive keeps the chained guard proportional: a
// cumulative delta changing ten files keeps the other 190 of its union base.
func TestGuardStoresSmallChainedDeltaLive(t *testing.T) {
	p := openActiveDeltaOverFull(t)
	changed := guardSizingPaths("f", 0, 10)
	gen := p.deltaGeneration("gen-c", "hint-c", "sha-c", "sha-b")
	gen.DeltaUnionBaseCommitSHA = "sha-a"
	p.commitGuardSizing(gen, guardSizingLive(changed), 10, changed, nil)
	p.requireGuardOutcome("gen-c", true, 0)
}
