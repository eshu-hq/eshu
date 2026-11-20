// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"

	"go.opentelemetry.io/otel/metric"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// The re-key operator signal (#10408, criterion 3.3, D6): the genuine
// identity-change outcome of one pass, counted by
// eshu_dp_supply_chain_impact_rekey_total. reportRekeyIdentity compares each
// derived row against the stored row it replaced on the coalesced group plus
// member set, so an EPSS refresh, admission-only, bootstrap, rollover, or
// same-key rewrite stays silent; only a merge, split, member-only, or
// retire-only change counts — a pass with no identity change that dropped
// suppression coverage reports split. The report carries no group, CVE, or
// advisory ids, so the outcome label stays a closed set of four.

// RekeyOutcome is one closed re-key shape a pass can report.
type RekeyOutcome string

const (
	// RekeyOutcomeMerge means old group keys retired while new keys
	// committed: two groups collapsed into a survivor (L1 late-alias
	// merge). Retirements are present, so the wave retired identity.
	RekeyOutcomeMerge RekeyOutcome = "merge"
	// RekeyOutcomeSplit means a fragment landed on a fresh group key whose
	// members were all already stored (L1 alias-removal split), or the
	// pass dropped suppression coverage (the orphaned-by-split tally is
	// nonzero). This beats the merge shape when both hold, because
	// dropped coverage needs a deliberate re-issue while a collapse
	// converges on its own.
	RekeyOutcomeSplit RekeyOutcome = "split"
	// RekeyOutcomeMemberOnly means the pass rewrote the same group key with
	// different members and retired nothing (L1 member-only refresh). It
	// fires only on a genuine same-group member change: the writer compares
	// each row against its replaced stored row, so first-seen groups and
	// ordinary rewrites stay silent.
	RekeyOutcomeMemberOnly RekeyOutcome = "member_only"
	// RekeyOutcomeRetireOnly means the pass wrote nothing and only retired
	// rows (L1 retire-only: a group the new generation no longer derives).
	RekeyOutcomeRetireOnly RekeyOutcome = "retire_only"
)

// Re-key result sub-signals: per-pass magnitudes on the reducer success
// line (log fields sub_signal_<name>), following the fanout_* sub-signals.
const (
	// rekeyGroupsTouchedSubSignal counts the genuine identity-change groups:
	// every changed row's coalesced group id, plus the old and new member
	// ids of rows that replaced a stored row under a different group id or
	// member set, plus retired groups with no successor.
	rekeyGroupsTouchedSubSignal = "rekey_groups_touched"
	// rekeyFindingsRekeyedSubSignal counts the findings the pass derived
	// under the post-change grouping: the commit's new keys.
	rekeyFindingsRekeyedSubSignal = "rekey_findings_rekeyed"
	// rekeyRowsTombstonedSubSignal counts the stored rows the pass
	// tombstoned or superseded under the group change.
	rekeyRowsTombstonedSubSignal = "rekey_rows_tombstoned"
)

// classifyRekeyOutcome maps one pass's genuine identity-change report to its
// counted outcome. The writer reports identity change (reportRekeyIdentity),
// never commit shape: an ordinary pass with the same groups and members —
// an EPSS-only refresh, a new admission, a generation rollover, a same-key
// rewrite — reports Count false and stays silent (false) however many rows
// it wrote or retracted. A pass that wrote nothing only retired. A pass
// that dropped suppression coverage is a split, whatever else it did,
// because dropped coverage needs a deliberate re-issue.
func classifyRekeyOutcome(rep RekeyReport, orphaned int) (RekeyOutcome, bool) {
	if !rep.Count {
		if orphaned > 0 {
			return RekeyOutcomeSplit, true
		}
		return "", false
	}
	if rep.Outcome == RekeyOutcomeRetireOnly {
		return RekeyOutcomeRetireOnly, true
	}
	if orphaned > 0 {
		return RekeyOutcomeSplit, true
	}
	return rep.Outcome, true
}

// emitRekey records one pass's re-key outcome and its suppression carry
// magnitudes, and merges the re-key sub-signals into the pass result. It
// runs at the pass dispatcher, next to the suppression carry tally, and
// follows the emitFanOut pattern: one counter add per re-key pass with the
// closed outcome label, conditional adds for the carried/orphaned
// magnitudes, magnitudes as result sub-signals. A pass with no genuine
// group-identity change counts nothing and reports zero magnitudes,
// unless it dropped suppression coverage (split plus the orphan
// magnitude). Bounded
// labels only: the outcome is the closed four-set; no group, CVE, or
// advisory id ever reaches a metric attribute.
func (h SupplyChainImpactHandler) emitRekey(ctx context.Context, writeResult SupplyChainImpactWriteResult, carry suppressionCarryTally, subSignals map[string]float64) {
	outcome, count := classifyRekeyOutcome(writeResult.Rekey, carry.orphaned)
	subSignals[rekeyGroupsTouchedSubSignal] = float64(len(writeResult.Rekey.Groups))
	subSignals[rekeyFindingsRekeyedSubSignal] = float64(writeResult.Rekey.Findings)
	subSignals[rekeyRowsTombstonedSubSignal] = float64(writeResult.Rekey.Tombstoned)
	if !count {
		return
	}
	if h.Instruments == nil || h.Instruments.SupplyChainImpactRekeyTotal == nil {
		return
	}
	h.Instruments.SupplyChainImpactRekeyTotal.Add(ctx, 1,
		metric.WithAttributes(
			telemetry.AttrDomain(string(reducercontract.DomainSupplyChainImpact)),
			telemetry.AttrOutcome(string(outcome)),
		),
	)
	if carry.carried > 0 && h.Instruments.SupplyChainImpactSuppressionCarriedViaMember != nil {
		h.Instruments.SupplyChainImpactSuppressionCarriedViaMember.Add(ctx, int64(carry.carried),
			metric.WithAttributes(
				telemetry.AttrDomain(string(reducercontract.DomainSupplyChainImpact)),
				telemetry.AttrOutcome(string(outcome)),
			),
		)
	}
	if carry.orphaned > 0 && h.Instruments.SupplyChainImpactSuppressionOrphanedBySplit != nil {
		h.Instruments.SupplyChainImpactSuppressionOrphanedBySplit.Add(ctx, int64(carry.orphaned),
			metric.WithAttributes(
				telemetry.AttrDomain(string(reducercontract.DomainSupplyChainImpact)),
				telemetry.AttrOutcome(string(outcome)),
			),
		)
	}
}

// StaleCopyRepairs counts the stored copies one pass rewrote across the
// retraction boundary (#10765). A finding's copy in one holder scope can read
// active while another scope has re-derived the same finding advisory_retracted;
// winner selection ranks by priority score first, so until the stale copy's own
// scope re-derives it, the default view can show the stale copy. The holder
// fan-out (#9695) reopens that scope,
// and the pass that follows is the repair: it finds the stored copy in the old
// state and writes it in the new one. "Active" here means any state other than
// advisory_retracted, so an operator copy leaving accepted_risk for a retraction
// counts as well.
type StaleCopyRepairs struct {
	// ActiveToRetracted counts copies that were not retracted and now are.
	ActiveToRetracted int
	// RetractedToActive counts copies that were retracted and now are not: a
	// restored record returning its copy to the state it had before.
	RetractedToActive int
}

// staleCopyRepairsOf diffs the rows a pass writes against the stored rows they
// replace. It uses the baseline the change judgement uses: on a rerun a row is
// matched to its own stored row by fact id, and on the first pass of a
// generation it is matched to the previous generation's row by stable key. A
// row with no stored counterpart is new, not repaired, and a stored row the
// pass no longer derives is retired, not repaired.
func staleCopyRepairsOf(rows []changedRow, stored []storedFinding, generationID string) StaleCopyRepairs {
	own := map[string]storedFinding{}
	prior := map[string]storedFinding{}
	for _, row := range stored {
		if row.generationID == generationID {
			own[row.factID] = row
		} else {
			prior[row.stableKey] = row
		}
	}
	rerun := len(own) > 0
	var repairs StaleCopyRepairs
	for _, row := range rows {
		var (
			base storedFinding
			ok   bool
		)
		if rerun {
			base, ok = own[row.factID]
		} else {
			base, ok = prior[row.stableKey]
		}
		if !ok {
			continue
		}
		was, is := isRetractedState(base.suppressionState), isRetractedState(row.suppressionState)
		switch {
		case !was && is:
			repairs.ActiveToRetracted++
		case was && !is:
			repairs.RetractedToActive++
		}
	}
	return repairs
}

func isRetractedState(state string) bool {
	return state == string(SupplyChainSuppressionStateAdvisoryRetracted)
}

// Stale-copy repair result sub-signals: per-pass magnitudes on the reducer
// success line (log fields sub_signal_<name>), beside the counter.
const (
	staleCopyRepairsActiveToRetractedSubSignal = "stale_copy_repairs_active_to_retracted"
	staleCopyRepairsRetractedToActiveSubSignal = "stale_copy_repairs_retracted_to_active"
)

// emitStaleCopyRepairs counts one pass's retraction-state flips by closed
// transition on eshu_dp_supply_chain_impact_stale_copy_repairs_total. It runs
// right after the writer commit, before the projection enqueue and the holder
// fan-out that can still fail the pass: a retry finds the copy already in its
// new state and repairs nothing, so a count deferred past those steps would be
// lost. A pass whose transaction rolled back returns an error first and counts
// nothing. Labels are the two-valued transition; no scope, CVE, finding, or
// fact id ever reaches a metric attribute.
func (h SupplyChainImpactHandler) emitStaleCopyRepairs(ctx context.Context, repairs StaleCopyRepairs) {
	h.Instruments.RecordSupplyChainImpactStaleCopyRepairs(ctx, telemetry.SupplyChainImpactStaleCopyTransitionActiveToRetracted, int64(repairs.ActiveToRetracted))
	h.Instruments.RecordSupplyChainImpactStaleCopyRepairs(ctx, telemetry.SupplyChainImpactStaleCopyTransitionRetractedToActive, int64(repairs.RetractedToActive))
}

// mergeSubSignals reports the pass's repair magnitudes beside the counter.
func (r StaleCopyRepairs) mergeSubSignals(subSignals map[string]float64) {
	subSignals[staleCopyRepairsActiveToRetractedSubSignal] = float64(r.ActiveToRetracted)
	subSignals[staleCopyRepairsRetractedToActiveSubSignal] = float64(r.RetractedToActive)
}
