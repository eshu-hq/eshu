// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"fmt"
	"time"
)

// defaultFreshnessClaimReapLimit bounds how many stuck AWS/GCP freshness
// claims one reap pass reclaims (#4576), mirroring
// workflow.DefaultExpiredClaimLimit's order of magnitude.
const defaultFreshnessClaimReapLimit = 100

// awsFreshnessReapLimit returns the configured freshness-claim reap row
// limit, falling back to defaultFreshnessClaimReapLimit when unset.
func (s Service) awsFreshnessReapLimit() int {
	if s.Config.FreshnessClaimReapLimit > 0 {
		return s.Config.FreshnessClaimReapLimit
	}
	return defaultFreshnessClaimReapLimit
}

// gcpFreshnessReapLimit is awsFreshnessReapLimit's GCP counterpart. AWS and
// GCP freshness claims share one reap-limit knob since both trigger tables
// share the identical claim/reclaim shape (#4576).
func (s Service) gcpFreshnessReapLimit() int {
	return s.awsFreshnessReapLimit()
}

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

func (s Service) runGCPFreshnessHandoff(ctx context.Context) error {
	if s.Config.DeploymentMode != deploymentModeActive || !s.Config.ClaimsEnabled || s.GCPFreshnessTriggers == nil {
		return nil
	}
	observedAt := s.now().UTC()
	instances, err := s.Store.ListCollectorInstances(ctx)
	if err != nil {
		return fmt.Errorf("list durable collector instances for GCP freshness handoff: %w", err)
	}
	filtered, err := s.filterCollectorInstancesByEgress(ctx, observedAt, instances)
	if err != nil {
		return err
	}
	return s.scheduleGCPFreshnessWork(ctx, observedAt, filtered)
}

func (s Service) runActiveMaintenance(ctx context.Context) error {
	if err := s.runReapExpiredClaims(ctx); err != nil {
		return fmt.Errorf("reap expired claims: %w", err)
	}
	// Reap stuck AWS/GCP freshness claims before this tick's handoff so a
	// trigger stranded at 'claimed' by a prior mid-batch abort or coordinator
	// crash is back in 'queued' in time for scheduleAWSFreshnessWork/
	// scheduleGCPFreshnessWork to claim it again this same tick (#4576).
	if err := s.runReapExpiredAWSFreshnessClaims(ctx); err != nil {
		return fmt.Errorf("reap expired AWS freshness trigger claims: %w", err)
	}
	if err := s.runReapExpiredGCPFreshnessClaims(ctx); err != nil {
		return fmt.Errorf("reap expired GCP freshness trigger claims: %w", err)
	}
	if err := s.runAWSFreshnessHandoff(ctx); err != nil {
		return fmt.Errorf("handoff AWS freshness triggers: %w", err)
	}
	if err := s.runGCPFreshnessHandoff(ctx); err != nil {
		return fmt.Errorf("handoff GCP freshness triggers: %w", err)
	}
	if err := s.runIncidentFreshnessHandoff(ctx); err != nil {
		return fmt.Errorf("handoff incident freshness triggers: %w", err)
	}
	if err := s.runSemanticProviderClaims(ctx); err != nil {
		return fmt.Errorf("drain semantic provider claims: %w", err)
	}
	if err := s.runWorkflowReconciliation(ctx); err != nil {
		return fmt.Errorf("reconcile workflow runs: %w", err)
	}
	return nil
}

// runReapExpiredAWSFreshnessClaims reclaims AWS freshness triggers stranded
// at 'claimed' past their lease back to 'queued' (#4576). It is a no-op when
// AWS freshness claiming is not configured, mirroring the other freshness
// handoff run* helpers' guard.
func (s Service) runReapExpiredAWSFreshnessClaims(ctx context.Context) error {
	if s.Config.DeploymentMode != deploymentModeActive || !s.Config.ClaimsEnabled || s.AWSFreshnessTriggers == nil {
		return nil
	}
	startedAt := time.Now()
	reclaimed, err := s.AWSFreshnessTriggers.ReapExpiredTriggerClaims(ctx, s.now().UTC(), s.awsFreshnessReapLimit())
	if err != nil {
		s.recordAWSFreshnessReap(ctx, FreshnessReapObservation{
			Outcome:  freshnessReapOutcomeError,
			Duration: time.Since(startedAt),
		})
		return err
	}
	if len(reclaimed) > 0 && s.Logger != nil {
		s.Logger.Warn(
			"reclaimed AWS freshness triggers stranded at 'claimed'",
			"reclaimed_count", len(reclaimed),
		)
	}
	for _, trigger := range reclaimed {
		s.recordAWSFreshnessEvent(ctx, trigger.Kind, awsFreshnessActionReclaimed)
	}
	s.recordAWSFreshnessReap(ctx, FreshnessReapObservation{
		Outcome:        freshnessReapOutcomeSuccess,
		Duration:       time.Since(startedAt),
		ReclaimedCount: len(reclaimed),
	})
	return nil
}

// runReapExpiredGCPFreshnessClaims is runReapExpiredAWSFreshnessClaims's GCP
// counterpart, mirroring the identical AWS/GCP freshness trigger shape
// (#4576).
func (s Service) runReapExpiredGCPFreshnessClaims(ctx context.Context) error {
	if s.Config.DeploymentMode != deploymentModeActive || !s.Config.ClaimsEnabled || s.GCPFreshnessTriggers == nil {
		return nil
	}
	startedAt := time.Now()
	reclaimed, err := s.GCPFreshnessTriggers.ReapExpiredTriggerClaims(ctx, s.now().UTC(), s.gcpFreshnessReapLimit())
	if err != nil {
		s.recordGCPFreshnessReap(ctx, FreshnessReapObservation{
			Outcome:  freshnessReapOutcomeError,
			Duration: time.Since(startedAt),
		})
		return err
	}
	if len(reclaimed) > 0 && s.Logger != nil {
		s.Logger.Warn(
			"reclaimed GCP freshness triggers stranded at 'claimed'",
			"reclaimed_count", len(reclaimed),
		)
	}
	for _, trigger := range reclaimed {
		s.recordGCPFreshnessEvent(ctx, trigger.Kind, gcpFreshnessActionReclaimed)
	}
	s.recordGCPFreshnessReap(ctx, FreshnessReapObservation{
		Outcome:        freshnessReapOutcomeSuccess,
		Duration:       time.Since(startedAt),
		ReclaimedCount: len(reclaimed),
	})
	return nil
}

// runSemanticProviderClaims drains the egress-gated semantic-provider execution
// worker when one is configured and claims are enabled. The worker re-checks
// semantic egress fail-closed before any provider dispatch and, with the default
// disabled client, performs no outbound provider traffic.
func (s Service) runSemanticProviderClaims(ctx context.Context) error {
	if s.Config.DeploymentMode != deploymentModeActive || !s.Config.ClaimsEnabled || s.SemanticProviderWorker == nil {
		return nil
	}
	return s.SemanticProviderWorker.Run(ctx)
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
