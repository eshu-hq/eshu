// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// streamArmTiming is one timed arm execution: wall-time per stage (build,
// operator, select) plus the stage-1 temp size, the select's shared buffers,
// and the winner count.
type streamArmTiming struct {
	build, operator, selectWall time.Duration
	tempBytes, buffers          int64
	winners                     int
}

// total is the arm's keyed-tick wall-time: build plus operator plus select.
func (m streamArmTiming) total() time.Duration {
	return m.build + m.operator + m.selectWall
}

// measureStreamArmTimed builds one arm's temp and times each stage with the
// wall clock: the stage-1 build, the operator insert, and the winner select
// executed for real (CREATE TEMP TABLE AS, not EXPLAIN). It runs in its own
// rolled-back transaction so back-to-back arms read the same committed
// snapshot. The probe temp it builds over is identical scaffolding for both
// arms and stays outside the timed total.
func measureStreamArmTimed(t *testing.T, h *keyedHarness, name, build, operator, selectSQL string) streamArmTiming {
	t.Helper()
	tx, err := h.db.BeginTx(h.ctx, nil)
	if err != nil {
		t.Fatalf("begin %s timed arm: %v", name, err)
	}
	defer func() { _ = tx.Rollback() }()
	streamExec(t, h, tx, name, `CREATE TEMP TABLE `+postgres.WinnersKeyedNarrowTableForTest+
		` AS SELECT fact_id, scope_id, generation_id FROM supply_chain_impact_finding_supports`)
	streamExec(t, h, tx, name, `ANALYZE `+postgres.WinnersKeyedNarrowTableForTest)

	start := time.Now()
	streamExec(t, h, tx, name, build)
	buildWall := time.Since(start)

	start = time.Now()
	streamExec(t, h, tx, name, operator)
	operatorWall := time.Since(start)

	winnersTable := `timed_` + name + `_winners`
	start = time.Now()
	streamExec(t, h, tx, name, `CREATE TEMP TABLE `+winnersTable+` AS `+selectSQL)
	selectWall := time.Since(start)

	var winners int
	if err := tx.QueryRowContext(h.ctx, `SELECT count(*) FROM `+winnersTable).Scan(&winners); err != nil {
		t.Fatalf("%s count timed winners: %v", name, err)
	}
	m := streamArmTiming{
		build:      buildWall,
		operator:   operatorWall,
		selectWall: selectWall,
		tempBytes:  streamTempBytes(t, h, tx, postgres.WinnersKeyedActiveFactsTableForTest),
		buffers:    streamExplainSharedBuffers(t, h, tx, selectSQL),
		winners:    winners,
	}
	t.Logf("%s arm: build %v operator %v select %v total %v, temp %d bytes, select shared buffers %d, winners %d",
		name, m.build, m.operator, m.selectWall, m.total(), m.tempBytes, m.buffers, m.winners)
	return m
}

// TestSupplyChainImpactWinnersKeyedStreamWallTimeLive records keyed-tick
// wall-time per arm (build plus operator plus select) on one QA-ratio corpus
// (8 identifiers at ~200 copies each, the Leg-B QA fanout shape), seeded
// once, both arms back-to-back in rolled-back transactions over the same
// committed snapshot. Round 1 per arm is cold; rounds 2 and 3 are warm; every
// round is labeled in the log. It asserts only what the arms prove:
// identical winner counts and a far smaller narrow temp. It pins no
// wall-time win: whether the stream is faster is reported, not asserted.
func TestSupplyChainImpactWinnersKeyedStreamWallTimeLive(t *testing.T) {
	h := newKeyedHarness(t, "#9805 C5 / #9975 C4 streaming body wall-time")
	seedStreamQARatioCorpus(t, h, 8, 200)
	streamAnalyzeBaseTables(t, h)

	legacyBuild := postgres.WinnersKeyedActiveFactsSQLForTest
	legacyOperator := postgres.WinnersKeyedOperatorFactsSQLForTest
	legacySelect := streamLegacySelect(postgres.WinnersKeyedActiveFactsTableForTest)
	streamBuild := postgres.WinnersKeyedActiveFactsNarrowBuildFromSupportsSQLForTest
	streamOperator := postgres.WinnersKeyedOperatorFactsNarrowBuildFromSupportsSQLForTest
	streamSelect := streamNewSelect(postgres.WinnersKeyedActiveFactsTableForTest)

	// The streamed cold follows the legacy cold over the same base tables,
	// so its cache starts warmer; the log and the evidence note record this
	// order effect instead of hiding it.
	for round := 1; round <= 3; round++ {
		label := "cold"
		if round > 1 {
			label = "warm"
		}
		legacy := measureStreamArmTimed(t, h, "legacy", legacyBuild, legacyOperator, legacySelect)
		streamed := measureStreamArmTimed(t, h, "streamed", streamBuild, streamOperator, streamSelect)
		t.Logf("round %d (%s): legacy total %v (build %v operator %v select %v, temp %d bytes, buffers %d) vs streamed total %v (build %v operator %v select %v, temp %d bytes, buffers %d), winners %d",
			round, label, legacy.total(), legacy.build, legacy.operator, legacy.selectWall, legacy.tempBytes, legacy.buffers,
			streamed.total(), streamed.build, streamed.operator, streamed.selectWall, streamed.tempBytes, streamed.buffers, legacy.winners)
		if legacy.winners == 0 {
			t.Fatalf("round %d: legacy arm produced no winners: the corpus is empty", round)
		}
		if streamed.winners != legacy.winners {
			t.Fatalf("round %d: streamed winners %d != legacy winners %d", round, streamed.winners, legacy.winners)
		}
		for _, arm := range []struct {
			name string
			m    streamArmTiming
		}{{"legacy", legacy}, {"streamed", streamed}} {
			if arm.m.selectWall <= 0 {
				t.Fatalf("round %d (%s): unrecorded select wall-time: the arm must time the select", round, arm.name)
			}
		}
		if round == 1 && streamed.tempBytes*2 > legacy.tempBytes {
			t.Errorf("narrow temp %d bytes > wide temp %d bytes / 2: the payload-free temp must write far less",
				streamed.tempBytes, legacy.tempBytes)
		}
	}
}
