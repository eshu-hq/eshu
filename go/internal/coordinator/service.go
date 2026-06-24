// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// Store is the narrow durable surface the workflow coordinator needs.
type Store interface {
	ReconcileCollectorInstances(context.Context, time.Time, []workflow.DesiredCollectorInstance) error
	ListCollectorInstances(context.Context) ([]workflow.CollectorInstance, error)
	CreateRun(context.Context, workflow.Run) error
	// CreateRunWithWorkItemsIfNoOpenTargets admits scheduled work only when no
	// non-terminal run already owns the same collector target tuple.
	CreateRunWithWorkItemsIfNoOpenTargets(context.Context, workflow.Run, []workflow.WorkItem) (int, error)
	EnqueueWorkItems(context.Context, []workflow.WorkItem) error
	ReapExpiredClaims(context.Context, time.Time, int, time.Duration) ([]workflow.Claim, error)
	ReconcileWorkflowRuns(context.Context, time.Time) (int, error)
}

// GovernanceAuditAppender records validation-safe hosted governance audit events.
type GovernanceAuditAppender interface {
	Append(context.Context, []governanceaudit.Event) error
}

// TerraformStatePlanner plans Terraform-state workflow rows from collector
// instance configuration.
type TerraformStatePlanner interface {
	PlanTerraformStateWork(context.Context, TerraformStatePlanRequest) (workflow.Run, []workflow.WorkItem, error)
}

// OCIRegistryPlanner plans OCI registry workflow rows from collector instance
// configuration.
type OCIRegistryPlanner interface {
	PlanOCIRegistryWork(context.Context, OCIRegistryPlanRequest) (workflow.Run, []workflow.WorkItem, error)
}

// PackageRegistryPlanner plans package-registry workflow rows from collector
// instance configuration.
type PackageRegistryPlanner interface {
	PlanPackageRegistryWork(context.Context, PackageRegistryPlanRequest) (workflow.Run, []workflow.WorkItem, error)
}

// VulnerabilityIntelligencePlanner plans vulnerability-intelligence workflow
// rows from collector instance configuration.
type VulnerabilityIntelligencePlanner interface {
	PlanVulnerabilityIntelligenceWork(context.Context, VulnerabilityIntelligencePlanRequest) (workflow.Run, []workflow.WorkItem, error)
}

// SBOMAttestationPlanner plans hosted SBOM/attestation workflow rows from
// collector instance configuration.
type SBOMAttestationPlanner interface {
	PlanSBOMAttestationWork(context.Context, SBOMAttestationPlanRequest) (workflow.Run, []workflow.WorkItem, error)
}

// ScannerWorkerPlanner plans scanner-worker workflow rows from collector
// instance configuration.
type ScannerWorkerPlanner interface {
	PlanScannerWorkerWork(context.Context, ScannerWorkerPlanRequest) (workflow.Run, []workflow.WorkItem, error)
}

// SecurityAlertPlanner plans provider security-alert workflow rows from
// collector instance configuration.
type SecurityAlertPlanner interface {
	PlanSecurityAlertWork(context.Context, SecurityAlertPlanRequest) (workflow.Run, []workflow.WorkItem, error)
}

// PagerDutyPlanner plans PagerDuty incident evidence workflow rows from
// collector instance configuration.
type PagerDutyPlanner interface {
	PlanPagerDutyWork(context.Context, PagerDutyPlanRequest) (workflow.Run, []workflow.WorkItem, error)
}

// JiraPlanner plans Jira workflow rows from collector instance configuration.
type JiraPlanner interface {
	PlanJiraWork(context.Context, JiraPlanRequest) (workflow.Run, []workflow.WorkItem, error)
}

// PrometheusMimirPlanner plans Prometheus/Mimir metric-metadata workflow rows
// from collector instance configuration.
type PrometheusMimirPlanner interface {
	PlanPrometheusMimirWork(context.Context, PrometheusMimirPlanRequest) (workflow.Run, []workflow.WorkItem, error)
}

// TempoPlanner plans Tempo trace-signal workflow rows from collector instance
// configuration.
type TempoPlanner interface {
	PlanTempoWork(context.Context, TempoPlanRequest) (workflow.Run, []workflow.WorkItem, error)
}

// GCPPlanner plans GCP Cloud Asset Inventory workflow rows from collector
// instance configuration.
type GCPPlanner interface {
	PlanGCPWork(context.Context, GCPPlanRequest) (workflow.Run, []workflow.WorkItem, error)
}

// GrafanaPlanner plans Grafana observability workflow rows from collector
// instance configuration.
type GrafanaPlanner interface {
	PlanGrafanaWork(context.Context, GrafanaPlanRequest) (workflow.Run, []workflow.WorkItem, error)
}

// LokiPlanner plans Loki observability workflow rows from collector instance
// configuration.
type LokiPlanner interface {
	PlanLokiWork(context.Context, LokiPlanRequest) (workflow.Run, []workflow.WorkItem, error)
}

// VaultLivePlanner plans live Vault metadata workflow rows from collector
// instance configuration.
type VaultLivePlanner interface {
	PlanVaultLiveWork(context.Context, VaultLivePlanRequest) (workflow.Run, []workflow.WorkItem, error)
}

// ComponentExtensionPlanner plans generic component extension workflow rows.
type ComponentExtensionPlanner interface {
	PlanComponentExtensionWork(context.Context, ComponentExtensionPlanRequest) (workflow.Run, []workflow.WorkItem, error)
}

// OwnedPackageTargetReader loads active dependency evidence that can bound
// derived package-registry and vulnerability-intelligence work.
type OwnedPackageTargetReader interface {
	ListOwnedPackageDependencyTargets(
		context.Context,
		workflow.OwnedPackageDependencyTargetFilter,
	) ([]workflow.OwnedPackageDependencyTarget, error)
}

// AWSScheduledPlanner plans scheduled AWS collector work from configuration.
type AWSScheduledPlanner interface {
	PlanAWSScheduledWork(context.Context, AWSScheduledPlanRequest) (workflow.Run, []workflow.WorkItem, error)
}

// Service is the workflow coordinator runner.
type Service struct {
	Config                            Config
	Store                             Store
	Metrics                           Metrics
	Logger                            *slog.Logger
	TerraformStatePlanner             TerraformStatePlanner
	OCIRegistryPlanner                OCIRegistryPlanner
	PackageRegistryPlanner            PackageRegistryPlanner
	VulnerabilityIntelligencePlanner  VulnerabilityIntelligencePlanner
	SBOMAttestationPlanner            SBOMAttestationPlanner
	ScannerWorkerPlanner              ScannerWorkerPlanner
	SecurityAlertPlanner              SecurityAlertPlanner
	CICDRunPlanner                    CICDRunPlanner
	PagerDutyPlanner                  PagerDutyPlanner
	JiraPlanner                       JiraPlanner
	PrometheusMimirPlanner            PrometheusMimirPlanner
	TempoPlanner                      TempoPlanner
	GCPPlanner                        GCPPlanner
	GrafanaPlanner                    GrafanaPlanner
	LokiPlanner                       LokiPlanner
	VaultLivePlanner                  VaultLivePlanner
	ComponentExtensionPlanner         ComponentExtensionPlanner
	OwnedPackageTargetReader          OwnedPackageTargetReader
	TenantGrantReader                 TenantGrantReader
	OSPackageAdvisoryTargetReader     OSPackageAdvisoryTargetReader
	SBOMComponentAdvisoryTargetReader SBOMComponentAdvisoryTargetReader
	AWSScheduledPlanner               AWSScheduledPlanner
	AWSFreshnessTriggers              AWSFreshnessTriggerStore
	AWSFreshnessPlanner               AWSFreshnessPlanner
	AWSFreshnessEvents                awsFreshnessEventCounter
	IncidentFreshnessTriggers         IncidentFreshnessTriggerStore
	GovernanceAudit                   GovernanceAuditAppender
	// SemanticProviderWorker is the optional egress-gated semantic-provider
	// execution worker. It is nil unless explicitly configured, and even when
	// configured it makes no real provider traffic unless its default-OFF
	// execution flag and an enabled provider client are both supplied.
	SemanticProviderWorker *SemanticProviderWorker
	Clock                  func() time.Time
}

// Run periodically reconciles declarative collector instance state and, in
// active mode, advances workflow control-plane truth.
func (s Service) Run(ctx context.Context) error {
	if s.Store == nil {
		return fmt.Errorf("workflow coordinator store is required")
	}
	s.Config = s.Config.withDefaults()
	if err := s.Config.Validate(); err != nil {
		return err
	}

	if err := s.runReconcile(ctx); err != nil {
		return fmt.Errorf("initial collector reconciliation: %w", err)
	}
	if s.Config.DeploymentMode == deploymentModeActive {
		if err := s.runReapExpiredClaims(ctx); err != nil {
			return fmt.Errorf("initial expired-claim reap: %w", err)
		}
		if err := s.runWorkflowReconciliation(ctx); err != nil {
			return fmt.Errorf("initial workflow run reconciliation: %w", err)
		}
	}
	if s.Logger != nil {
		message := "workflow coordinator running in dark mode"
		if s.Config.DeploymentMode == deploymentModeActive {
			message = "workflow coordinator running in active mode"
		}
		s.Logger.Info(
			message,
			"deployment_mode", s.Config.DeploymentMode,
			"claims_enabled", s.Config.ClaimsEnabled,
			"collector_instances", len(s.Config.CollectorInstances),
			"reconcile_interval", s.Config.ReconcileInterval.String(),
			"run_reconcile_interval", s.Config.RunReconcileInterval.String(),
			"reap_interval", s.Config.ReapInterval.String(),
		)
	}

	reconcileTicker := time.NewTicker(s.Config.ReconcileInterval)
	defer reconcileTicker.Stop()
	var reapTicker *time.Ticker
	var runReconcileTicker *time.Ticker
	if s.Config.DeploymentMode == deploymentModeActive {
		reapTicker = time.NewTicker(s.Config.ReapInterval)
		defer reapTicker.Stop()
		runReconcileTicker = time.NewTicker(s.Config.RunReconcileInterval)
		defer runReconcileTicker.Stop()
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-reconcileTicker.C:
			if err := s.runReconcile(ctx); err != nil {
				return fmt.Errorf("reconcile collector instances: %w", err)
			}
		case <-tickerChan(reapTicker):
			if err := s.runActiveMaintenance(ctx); err != nil {
				return err
			}
		case <-tickerChan(runReconcileTicker):
			if err := s.runWorkflowReconciliation(ctx); err != nil {
				return fmt.Errorf("reconcile workflow runs: %w", err)
			}
		}
	}
}

func (s Service) now() time.Time {
	if s.Clock != nil {
		return s.Clock()
	}
	return time.Now()
}

func (s Service) runReconcile(ctx context.Context) error {
	startedAt := time.Now()
	observedAt := s.now().UTC()
	desiredCount := len(s.Config.CollectorInstances)

	if err := s.Store.ReconcileCollectorInstances(ctx, observedAt, s.Config.CollectorInstances); err != nil {
		s.recordReconcile(ctx, ReconcileObservation{
			Outcome:      reconcileOutcomeReconcileError,
			Duration:     time.Since(startedAt),
			DesiredCount: desiredCount,
		})
		return err
	}

	instances, err := s.Store.ListCollectorInstances(ctx)
	if err != nil {
		s.recordReconcile(ctx, ReconcileObservation{
			Outcome:      reconcileOutcomeStateReadError,
			Duration:     time.Since(startedAt),
			DesiredCount: desiredCount,
		})
		return fmt.Errorf("list durable collector instances: %w", err)
	}

	durableCount := len(instances)
	schedulingInstances, err := s.filterCollectorInstancesByEgress(ctx, observedAt, instances)
	if err != nil {
		s.recordReconcile(ctx, ReconcileObservation{
			Outcome:      reconcileOutcomeReconcileError,
			Duration:     time.Since(startedAt),
			DesiredCount: desiredCount,
			DurableCount: durableCount,
		})
		return err
	}
	drift := desiredCount - durableCount
	if drift < 0 {
		drift = -drift
	}
	if err := s.scheduleTerraformStateWork(ctx, observedAt, schedulingInstances); err != nil {
		s.recordReconcile(ctx, ReconcileObservation{
			Outcome:      reconcileOutcomeReconcileError,
			Duration:     time.Since(startedAt),
			DesiredCount: desiredCount,
			DurableCount: durableCount,
		})
		return err
	}
	if err := s.scheduleOCIRegistryWork(ctx, observedAt, schedulingInstances); err != nil {
		s.recordReconcile(ctx, ReconcileObservation{
			Outcome:      reconcileOutcomeReconcileError,
			Duration:     time.Since(startedAt),
			DesiredCount: desiredCount,
			DurableCount: durableCount,
		})
		return err
	}
	if err := s.schedulePackageRegistryWork(ctx, observedAt, schedulingInstances); err != nil {
		s.recordReconcile(ctx, ReconcileObservation{
			Outcome:      reconcileOutcomeReconcileError,
			Duration:     time.Since(startedAt),
			DesiredCount: desiredCount,
			DurableCount: durableCount,
		})
		return err
	}
	if err := s.scheduleVulnerabilityIntelligenceWork(ctx, observedAt, schedulingInstances); err != nil {
		s.recordReconcile(ctx, ReconcileObservation{
			Outcome:      reconcileOutcomeReconcileError,
			Duration:     time.Since(startedAt),
			DesiredCount: desiredCount,
			DurableCount: durableCount,
		})
		return err
	}
	if err := s.scheduleSBOMAttestationWork(ctx, observedAt, schedulingInstances); err != nil {
		s.recordReconcile(ctx, ReconcileObservation{
			Outcome:      reconcileOutcomeReconcileError,
			Duration:     time.Since(startedAt),
			DesiredCount: desiredCount,
			DurableCount: durableCount,
		})
		return err
	}
	if err := s.scheduleScannerWorkerWork(ctx, observedAt, schedulingInstances); err != nil {
		s.recordReconcile(ctx, ReconcileObservation{
			Outcome:      reconcileOutcomeReconcileError,
			Duration:     time.Since(startedAt),
			DesiredCount: desiredCount,
			DurableCount: durableCount,
		})
		return err
	}
	if err := s.scheduleSecurityAlertWork(ctx, observedAt, schedulingInstances); err != nil {
		s.recordReconcile(ctx, ReconcileObservation{
			Outcome:      reconcileOutcomeReconcileError,
			Duration:     time.Since(startedAt),
			DesiredCount: desiredCount,
			DurableCount: durableCount,
		})
		return err
	}
	if err := s.scheduleCICDRunWork(ctx, observedAt, schedulingInstances); err != nil {
		s.recordReconcile(ctx, ReconcileObservation{
			Outcome:      reconcileOutcomeReconcileError,
			Duration:     time.Since(startedAt),
			DesiredCount: desiredCount,
			DurableCount: durableCount,
		})
		return err
	}
	if err := s.schedulePagerDutyWork(ctx, observedAt, schedulingInstances); err != nil {
		s.recordReconcile(ctx, ReconcileObservation{
			Outcome:      reconcileOutcomeReconcileError,
			Duration:     time.Since(startedAt),
			DesiredCount: desiredCount,
			DurableCount: durableCount,
		})
		return err
	}
	if err := s.scheduleJiraWork(ctx, observedAt, schedulingInstances); err != nil {
		s.recordReconcile(ctx, ReconcileObservation{
			Outcome:      reconcileOutcomeReconcileError,
			Duration:     time.Since(startedAt),
			DesiredCount: desiredCount,
			DurableCount: durableCount,
		})
		return err
	}
	if err := s.schedulePrometheusMimirWork(ctx, observedAt, schedulingInstances); err != nil {
		s.recordReconcile(ctx, ReconcileObservation{
			Outcome:      reconcileOutcomeReconcileError,
			Duration:     time.Since(startedAt),
			DesiredCount: desiredCount,
			DurableCount: durableCount,
		})
		return err
	}
	if err := s.scheduleTempoWork(ctx, observedAt, schedulingInstances); err != nil {
		s.recordReconcile(ctx, ReconcileObservation{
			Outcome:      reconcileOutcomeReconcileError,
			Duration:     time.Since(startedAt),
			DesiredCount: desiredCount,
			DurableCount: durableCount,
		})
		return err
	}
	if err := s.scheduleGCPWork(ctx, observedAt, schedulingInstances); err != nil {
		s.recordReconcile(ctx, ReconcileObservation{
			Outcome:      reconcileOutcomeReconcileError,
			Duration:     time.Since(startedAt),
			DesiredCount: desiredCount,
			DurableCount: durableCount,
		})
		return err
	}
	if err := s.scheduleGrafanaWork(ctx, observedAt, schedulingInstances); err != nil {
		s.recordReconcile(ctx, ReconcileObservation{
			Outcome:      reconcileOutcomeReconcileError,
			Duration:     time.Since(startedAt),
			DesiredCount: desiredCount,
			DurableCount: durableCount,
		})
		return err
	}
	if err := s.scheduleLokiWork(ctx, observedAt, schedulingInstances); err != nil {
		s.recordReconcile(ctx, ReconcileObservation{
			Outcome:      reconcileOutcomeReconcileError,
			Duration:     time.Since(startedAt),
			DesiredCount: desiredCount,
			DurableCount: durableCount,
		})
		return err
	}
	if err := s.scheduleVaultLiveWork(ctx, observedAt, schedulingInstances); err != nil {
		s.recordReconcile(ctx, ReconcileObservation{
			Outcome:      reconcileOutcomeReconcileError,
			Duration:     time.Since(startedAt),
			DesiredCount: desiredCount,
			DurableCount: durableCount,
		})
		return err
	}
	if err := s.scheduleAWSScheduledWork(ctx, observedAt, schedulingInstances); err != nil {
		s.recordReconcile(ctx, ReconcileObservation{
			Outcome:      reconcileOutcomeReconcileError,
			Duration:     time.Since(startedAt),
			DesiredCount: desiredCount,
			DurableCount: durableCount,
		})
		return err
	}
	if err := s.scheduleComponentExtensionWork(ctx, observedAt, schedulingInstances); err != nil {
		s.recordReconcile(ctx, ReconcileObservation{
			Outcome:      reconcileOutcomeReconcileError,
			Duration:     time.Since(startedAt),
			DesiredCount: desiredCount,
			DurableCount: durableCount,
		})
		return err
	}
	if err := s.scheduleAWSFreshnessWork(ctx, observedAt, schedulingInstances); err != nil {
		s.recordReconcile(ctx, ReconcileObservation{
			Outcome:      reconcileOutcomeReconcileError,
			Duration:     time.Since(startedAt),
			DesiredCount: desiredCount,
			DurableCount: durableCount,
		})
		return err
	}
	s.recordReconcile(ctx, ReconcileObservation{
		Outcome:      reconcileOutcomeSuccess,
		Duration:     time.Since(startedAt),
		DesiredCount: desiredCount,
		DurableCount: durableCount,
	})
	if drift > 0 && s.Logger != nil {
		s.Logger.Warn(
			"workflow coordinator collector instance drift detected",
			"desired_collector_instances", desiredCount,
			"durable_collector_instances", durableCount,
			"collector_instance_drift", drift,
		)
	}
	return nil
}
