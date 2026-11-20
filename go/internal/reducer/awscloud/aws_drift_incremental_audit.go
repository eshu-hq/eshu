// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

import (
	"context"
	"log/slog"

	"github.com/eshu-hq/eshu/go/internal/correlation/drift/cloudruntime"
	"github.com/eshu-hq/eshu/go/internal/correlation/engine"
	"github.com/eshu-hq/eshu/go/internal/correlation/model"
	"github.com/eshu-hq/eshu/go/internal/correlation/rules"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

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
		return
	}
	deltaAdmitted := admittedAWSCloudRuntimeDriftCandidates(deltaEval)
	RecordAWSDriftAuditCorrections(ctx, h.Instruments,
		CountAuditCorrections(correlationKeys(deltaAdmitted), correlationKeys(fullAdmitted)))
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
