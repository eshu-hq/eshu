// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "time"

type deployableUnitCorrelationTiming struct {
	loadFactsDuration          time.Duration
	extractCandidatesDuration  time.Duration
	loadResolvedDuration       time.Duration
	applyResolvedDuration      time.Duration
	filterCandidatesDuration   time.Duration
	evaluateCandidatesDuration time.Duration
	edgeMaterializeDuration    time.Duration
	edgeRetractDuration        time.Duration
	edgeWriteDuration          time.Duration
	admissionDecisionDuration  time.Duration
	phasePublishDuration       time.Duration
	totalDuration              time.Duration
}

type deployableUnitCorrelationSignals struct {
	factCount           int
	rawCandidateCount   int
	candidateCount      int
	evaluatedCandidates int
	edgeRows            int
	retractRows         int
	writeRows           int
	canonicalWrites     int
}

func deployableUnitCorrelationSubDurations(t deployableUnitCorrelationTiming) map[string]float64 {
	return map[string]float64{
		"load_facts":          t.loadFactsDuration.Seconds(),
		"extract_candidates":  t.extractCandidatesDuration.Seconds(),
		"load_resolved":       t.loadResolvedDuration.Seconds(),
		"apply_resolved":      t.applyResolvedDuration.Seconds(),
		"filter_candidates":   t.filterCandidatesDuration.Seconds(),
		"evaluate_candidates": t.evaluateCandidatesDuration.Seconds(),
		"edge_materialize":    t.edgeMaterializeDuration.Seconds(),
		"edge_retract":        t.edgeRetractDuration.Seconds(),
		"edge_write":          t.edgeWriteDuration.Seconds(),
		"admission_decisions": t.admissionDecisionDuration.Seconds(),
		"phase_publish":       t.phasePublishDuration.Seconds(),
		"total":               t.totalDuration.Seconds(),
	}
}

func deployableUnitCorrelationSubSignals(s deployableUnitCorrelationSignals) map[string]float64 {
	return map[string]float64{
		"fact_count":           float64(s.factCount),
		"raw_candidate_count":  float64(s.rawCandidateCount),
		"candidate_count":      float64(s.candidateCount),
		"evaluated_candidates": float64(s.evaluatedCandidates),
		"edge_rows":            float64(s.edgeRows),
		"retract_rows":         float64(s.retractRows),
		"write_rows":           float64(s.writeRows),
		"canonical_writes":     float64(s.canonicalWrites),
	}
}
