// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"

	"go.opentelemetry.io/otel/metric"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// The re-key operator signal (#10408, criterion 3.3, D6): the outcome of one
// pass that touched an advisory group, counted by
// eshu_dp_supply_chain_impact_rekey_total. The classifier sees only the
// writer commit's shape — the touched group set, the derived-row and
// tombstone counts, and the suppression carry tally — never group, CVE, or
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
// group-identity change counts nothing and reports zero magnitudes. Bounded
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
