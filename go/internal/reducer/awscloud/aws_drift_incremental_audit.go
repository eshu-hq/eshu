// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/correlation/drift/cloudruntime"
	"github.com/eshu-hq/eshu/go/internal/correlation/engine"
	"github.com/eshu-hq/eshu/go/internal/correlation/model"
	"github.com/eshu-hq/eshu/go/internal/correlation/rules"
	"github.com/eshu-hq/eshu/go/internal/telemetry"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// Closed outcomes for the #11032 audits denominator. The set is closed by
// design: only these three values ever label
// eshu_dp_aws_drift_incremental_audits_total, so the PromQL strike rule can
// distinguish a clean audit from no audit and a broken audit from a clean
// one without a new label value ever escaping the bounded-label contract.
const (
	// AWSDriftAuditOutcomeClean marks an audit whose replay agrees with the
	// full run: the delta would have missed nothing.
	AWSDriftAuditOutcomeClean = "clean"
	// AWSDriftAuditOutcomeCorrecting marks an audit whose replay misses keys
	// the full run re-derived: the delta-correctness defect class.
	AWSDriftAuditOutcomeCorrecting = "correcting"
	// AWSDriftAuditOutcomeFailed marks an audit whose reconstruction itself
	// errored before any comparison ran.
	AWSDriftAuditOutcomeFailed = "failed"
)

// RecordAWSDriftAudit records one audit_due reconstruction on the #11032
// denominator counter. Only the closed outcomes above emit; any other value
// (including empty) records nothing, so a caller bug cannot mint a novel
// label value. Nil instruments are a no-op.
func RecordAWSDriftAudit(ctx context.Context, instruments *telemetry.Instruments, outcome string) {
	if instruments == nil {
		return
	}
	switch outcome {
	case AWSDriftAuditOutcomeClean, AWSDriftAuditOutcomeCorrecting, AWSDriftAuditOutcomeFailed:
	default:
		return
	}
	instruments.AWSDriftIncrementalAudits.Add(ctx, 1,
		metric.WithAttributes(attribute.String("outcome", outcome)))
}

// recordAuditCorrections reconstructs what a delta run would have admitted
// on the same loaded rows and counts what it missed (#10878 D6, #11018 P1).
// The reconstruction replays the exact production path — filter the loaded
// rows to the audit plan's diff, build candidates, evaluate, admit — because
// classification is per-ARN independent: equal AddressedRow inputs imply
// equal candidates, so the replay agrees with a real delta by construction.
//
// The comparison is same-evidence by design: the full run and the replay
// read the same loaded rows, so the gap counts exactly the keys a delta
// would have skipped (unchanged identities the audit re-derived anyway —
// the config-miss class the daily audit exists to correct). A cheaper
// row-identity intersection may replace this replay only with an
// equivalence test pinning it to this simulation.
//
// It emits only on audit_due full runs, never on delta or fallback runs,
// and a zero-miss audit records no point. It never fails the run:
// observability must not break classification.
func (h AWSCloudRuntimeDriftHandler) recordAuditCorrections(
	ctx context.Context,
	intent reducercontract.Intent,
	rows []cloudruntime.AddressedRow,
	fullAdmitted []model.Candidate,
	diff reducercontract.ARNRecordDiff,
) {
	deltaRows := FilterAddressedRowsToDiff(rows, diff)
	deltaCandidates := cloudruntime.BuildCandidates(deltaRows, intent.ScopeID)
	deltaEval, err := engine.Evaluate(rules.AWSCloudRuntimeDriftRulePack(), deltaCandidates)
	if err != nil {
		if h.Logger != nil {
			h.Logger.LogAttrs(ctx, slog.LevelWarn, "aws drift audit reconstruction failed",
				slog.String("intent_id", intent.IntentID),
				slog.String("error", err.Error()),
			)
		}
		h.recordAuditOutcome(ctx, 0, err)
		return
	}
	deltaAdmitted := admittedAWSCloudRuntimeDriftCandidates(deltaEval)
	h.recordAuditOutcome(ctx,
		CountAuditCorrections(correlationKeys(deltaAdmitted), correlationKeys(fullAdmitted)), nil)
}

// recordAuditOutcome emits the #11032 denominator point for one finished
// audit_due reconstruction plus the corrections magnitude on success. A
// reconstruction error records failed and no corrections, so a broken audit
// never reads as clean. It never fails the run: observability must not break
// classification.
func (h AWSCloudRuntimeDriftHandler) recordAuditOutcome(ctx context.Context, corrections int, reconstructErr error) {
	if reconstructErr != nil {
		RecordAWSDriftAudit(ctx, h.Instruments, AWSDriftAuditOutcomeFailed)
		return
	}
	if corrections > 0 {
		RecordAWSDriftAudit(ctx, h.Instruments, AWSDriftAuditOutcomeCorrecting)
	} else {
		RecordAWSDriftAudit(ctx, h.Instruments, AWSDriftAuditOutcomeClean)
	}
	RecordAWSDriftAuditCorrections(ctx, h.Instruments, corrections)
}

// correlationKeys renders the candidate correlation keys CountAuditCorrections
// compares: order-independent, duplicates collapsed by the counter.
func correlationKeys(candidates []model.Candidate) []string {
	keys := make([]string, 0, len(candidates))
	for _, c := range candidates {
		keys = append(keys, c.CorrelationKey)
	}
	return keys
}
