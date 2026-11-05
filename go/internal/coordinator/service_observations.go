// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/coordinator/vulnerability"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func (s Service) recordReconcile(ctx context.Context, observation ReconcileObservation) {
	if s.Metrics == nil {
		return
	}
	s.Metrics.RecordReconcile(ctx, observation)
}

func (s Service) recordReap(ctx context.Context, observation ReapObservation) {
	if s.Metrics == nil {
		return
	}
	s.Metrics.RecordReap(ctx, observation)
}

func (s Service) recordRunReconciliation(ctx context.Context, observation RunReconciliationObservation) {
	if s.Metrics == nil {
		return
	}
	s.Metrics.RecordRunReconciliation(ctx, observation)
}

func (s Service) recordAWSFreshnessReap(ctx context.Context, observation FreshnessReapObservation) {
	if s.Metrics == nil {
		return
	}
	s.Metrics.RecordAWSFreshnessReap(ctx, observation)
}

func (s Service) recordGCPFreshnessReap(ctx context.Context, observation FreshnessReapObservation) {
	if s.Metrics == nil {
		return
	}
	s.Metrics.RecordGCPFreshnessReap(ctx, observation)
}

func (s Service) recordIncidentFreshnessReap(ctx context.Context, observation FreshnessReapObservation) {
	if s.Metrics == nil {
		return
	}
	s.Metrics.RecordIncidentFreshnessReap(ctx, observation)
}

func (s Service) recordScannerDerivedPage(ctx context.Context, observation ScannerDerivedPageObservation) {
	if s.Metrics == nil {
		return
	}
	s.Metrics.RecordScannerDerivedPage(ctx, observation)
}

func (s Service) recordEPSSDerived(ctx context.Context, observation vulnerability.IntelligenceDerivationObservation) {
	if s.Metrics == nil {
		return
	}
	s.Metrics.RecordEPSSDerivedPage(ctx, EPSSDerivedObservation{
		PlannedTargets: observation.Targets,
		Skipped:        observation.Skipped,
	})
}

// recordOwnedDerivedPage records the owned-package derivation of one plan
// (eshu-hq/eshu#9287 criterion 4): the read's selected targets per ecosystem
// plus the owned-package skip evidence of the same plan. Counts only, never
// package names.
func (s Service) recordOwnedDerivedPage(
	ctx context.Context,
	instance workflow.CollectorInstance,
	page workflow.OwnedPackageDependencyTargetPage,
	observation vulnerability.IntelligenceDerivationObservation,
) {
	if s.Metrics == nil {
		return
	}
	ecosystems, selected := page.SelectedByEcosystem()
	counts := make(map[string]int, len(ecosystems))
	for i, ecosystem := range ecosystems {
		counts[ecosystem] = selected[i]
	}
	s.Metrics.RecordOwnedDerivedPage(ctx, OwnedDerivedObservation{
		CollectorInstanceID: instance.InstanceID,
		Selected:            counts,
		Skipped:             observation.Skipped,
	})
}

func tickerChan(ticker *time.Ticker) <-chan time.Time {
	if ticker == nil {
		return nil
	}
	return ticker.C
}
