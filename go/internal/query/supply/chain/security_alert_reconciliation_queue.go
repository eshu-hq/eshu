// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// Reducer reconciliation queue verdicts for a collection-healthy
// security-alert empty answer (#8839), split from
// security_alert_collection_state.go to keep both files under the repo's
// 500-line cap.

// securityAlertReconciliationBacklogVerdict is the verdict for a
// collection-healthy empty answer whose security_alert_reconciliation work is
// still outstanding (#8839). The collected facts exist, so the answer is
// derived, not exact; reconciliation has not run, so it is building. It
// carries no readiness envelope and no collector cause: coverage.reasons and
// the truth cause name the reducer backlog.
func securityAlertReconciliationBacklogVerdict() securityAlertCoverageVerdict {
	return securityAlertCoverageVerdict{
		coverageState:  securityAlertCoverageTargetIncomplete,
		reasons:        []string{securityAlertReasonReducerBacklog},
		level:          querycontract.TruthLevelDerived,
		freshness:      querycontract.FreshnessBuilding,
		collectorState: securityAlertCollectorStateReconciling,
		cause:          querycontract.FreshnessCauseReducerBacklog,
	}
}

// securityAlertReconciliationDeadLetterVerdict is the verdict for a
// collection-healthy empty answer whose security_alert_reconciliation work
// is dead-lettered (#8839): reconciliation is parked for repair and will not
// advance the answer, so it is unavailable, not building.
func securityAlertReconciliationDeadLetterVerdict() securityAlertCoverageVerdict {
	return securityAlertCoverageVerdict{
		coverageState:  securityAlertCoverageTargetIncomplete,
		reasons:        []string{securityAlertReasonDeadLetteredDomain},
		level:          querycontract.TruthLevelFallback,
		freshness:      querycontract.FreshnessUnavailable,
		collectorState: securityAlertCollectorStateReconciliationStuck,
		cause:          querycontract.FreshnessCauseDeadLetteredDomain,
	}
}

// classifySecurityAlertReconciliationQueue turns the reducer queue depths of
// the covering scopes into a verdict (#8839). Dead-lettered work outranks
// outstanding work, mirroring the freshness-causality ranking (stale beats
// building). Scopes the store omits have no such work and contribute
// nothing. The second result is false when every covering scope drained.
func classifySecurityAlertReconciliationQueue(
	states []SecurityAlertReconciliationQueueState,
) (securityAlertCoverageVerdict, bool) {
	deadLettered := false
	outstanding := false
	for _, state := range states {
		if state.DeadLetter > 0 {
			deadLettered = true
		}
		if state.Outstanding > 0 {
			outstanding = true
		}
	}
	switch {
	case deadLettered:
		return securityAlertReconciliationDeadLetterVerdict(), true
	case outstanding:
		return securityAlertReconciliationBacklogVerdict(), true
	default:
		return securityAlertCoverageVerdict{}, false
	}
}
