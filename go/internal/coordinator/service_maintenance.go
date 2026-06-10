package coordinator

import (
	"context"
	"fmt"
	"time"
)

func (s Service) runAWSFreshnessHandoff(ctx context.Context) error {
	if s.Config.DeploymentMode != deploymentModeActive || !s.Config.ClaimsEnabled || s.AWSFreshnessTriggers == nil {
		return nil
	}
	observedAt := s.now().UTC()
	instances, err := s.Store.ListCollectorInstances(ctx)
	if err != nil {
		return fmt.Errorf("list durable collector instances for AWS freshness handoff: %w", err)
	}
	filtered, err := s.filterCollectorInstancesByEgress(ctx, observedAt, instances)
	if err != nil {
		return err
	}
	return s.scheduleAWSFreshnessWork(ctx, observedAt, filtered)
}

func (s Service) runActiveMaintenance(ctx context.Context) error {
	if err := s.runReapExpiredClaims(ctx); err != nil {
		return fmt.Errorf("reap expired claims: %w", err)
	}
	if err := s.runAWSFreshnessHandoff(ctx); err != nil {
		return fmt.Errorf("handoff AWS freshness triggers: %w", err)
	}
	if err := s.runIncidentFreshnessHandoff(ctx); err != nil {
		return fmt.Errorf("handoff incident freshness triggers: %w", err)
	}
	if err := s.runWorkflowReconciliation(ctx); err != nil {
		return fmt.Errorf("reconcile workflow runs: %w", err)
	}
	return nil
}

func (s Service) runReapExpiredClaims(ctx context.Context) error {
	startedAt := time.Now()
	claims, err := s.Store.ReapExpiredClaims(
		ctx,
		s.now().UTC(),
		s.Config.ExpiredClaimLimit,
		s.Config.ExpiredClaimRequeueDelay,
	)
	if err != nil {
		s.recordReap(ctx, ReapObservation{
			Outcome:  reaperOutcomeError,
			Duration: time.Since(startedAt),
		})
		return err
	}
	s.recordReap(ctx, ReapObservation{
		Outcome:    reaperOutcomeSuccess,
		Duration:   time.Since(startedAt),
		ReapedRows: len(claims),
	})
	return nil
}

func (s Service) runWorkflowReconciliation(ctx context.Context) error {
	startedAt := time.Now()
	reconciledRuns, err := s.Store.ReconcileWorkflowRuns(ctx, s.now().UTC())
	if err != nil {
		s.recordRunReconciliation(ctx, RunReconciliationObservation{
			Outcome:  runReconcileOutcomeError,
			Duration: time.Since(startedAt),
		})
		return err
	}
	s.recordRunReconciliation(ctx, RunReconciliationObservation{
		Outcome:        runReconcileOutcomeSuccess,
		Duration:       time.Since(startedAt),
		ReconciledRuns: reconciledRuns,
	})
	return nil
}
