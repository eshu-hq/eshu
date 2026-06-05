package coordinator

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
	"github.com/eshu-hq/eshu/go/internal/scope"
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

// GrafanaPlanner plans Grafana observability workflow rows from collector
// instance configuration.
type GrafanaPlanner interface {
	PlanGrafanaWork(context.Context, GrafanaPlanRequest) (workflow.Run, []workflow.WorkItem, error)
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
	PagerDutyPlanner                  PagerDutyPlanner
	JiraPlanner                       JiraPlanner
	PrometheusMimirPlanner            PrometheusMimirPlanner
	TempoPlanner                      TempoPlanner
	GrafanaPlanner                    GrafanaPlanner
	OwnedPackageTargetReader          OwnedPackageTargetReader
	OSPackageAdvisoryTargetReader     OSPackageAdvisoryTargetReader
	SBOMComponentAdvisoryTargetReader SBOMComponentAdvisoryTargetReader
	AWSScheduledPlanner               AWSScheduledPlanner
	AWSFreshnessTriggers              AWSFreshnessTriggerStore
	AWSFreshnessPlanner               AWSFreshnessPlanner
	AWSFreshnessEvents                awsFreshnessEventCounter
	IncidentFreshnessTriggers         IncidentFreshnessTriggerStore
	Clock                             func() time.Time
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
	drift := desiredCount - durableCount
	if drift < 0 {
		drift = -drift
	}
	if err := s.scheduleTerraformStateWork(ctx, observedAt, instances); err != nil {
		s.recordReconcile(ctx, ReconcileObservation{
			Outcome:      reconcileOutcomeReconcileError,
			Duration:     time.Since(startedAt),
			DesiredCount: desiredCount,
			DurableCount: durableCount,
		})
		return err
	}
	if err := s.scheduleOCIRegistryWork(ctx, observedAt, instances); err != nil {
		s.recordReconcile(ctx, ReconcileObservation{
			Outcome:      reconcileOutcomeReconcileError,
			Duration:     time.Since(startedAt),
			DesiredCount: desiredCount,
			DurableCount: durableCount,
		})
		return err
	}
	if err := s.schedulePackageRegistryWork(ctx, observedAt, instances); err != nil {
		s.recordReconcile(ctx, ReconcileObservation{
			Outcome:      reconcileOutcomeReconcileError,
			Duration:     time.Since(startedAt),
			DesiredCount: desiredCount,
			DurableCount: durableCount,
		})
		return err
	}
	if err := s.scheduleVulnerabilityIntelligenceWork(ctx, observedAt, instances); err != nil {
		s.recordReconcile(ctx, ReconcileObservation{
			Outcome:      reconcileOutcomeReconcileError,
			Duration:     time.Since(startedAt),
			DesiredCount: desiredCount,
			DurableCount: durableCount,
		})
		return err
	}
	if err := s.scheduleSBOMAttestationWork(ctx, observedAt, instances); err != nil {
		s.recordReconcile(ctx, ReconcileObservation{
			Outcome:      reconcileOutcomeReconcileError,
			Duration:     time.Since(startedAt),
			DesiredCount: desiredCount,
			DurableCount: durableCount,
		})
		return err
	}
	if err := s.scheduleScannerWorkerWork(ctx, observedAt, instances); err != nil {
		s.recordReconcile(ctx, ReconcileObservation{
			Outcome:      reconcileOutcomeReconcileError,
			Duration:     time.Since(startedAt),
			DesiredCount: desiredCount,
			DurableCount: durableCount,
		})
		return err
	}
	if err := s.scheduleSecurityAlertWork(ctx, observedAt, instances); err != nil {
		s.recordReconcile(ctx, ReconcileObservation{
			Outcome:      reconcileOutcomeReconcileError,
			Duration:     time.Since(startedAt),
			DesiredCount: desiredCount,
			DurableCount: durableCount,
		})
		return err
	}
	if err := s.schedulePagerDutyWork(ctx, observedAt, instances); err != nil {
		s.recordReconcile(ctx, ReconcileObservation{
			Outcome:      reconcileOutcomeReconcileError,
			Duration:     time.Since(startedAt),
			DesiredCount: desiredCount,
			DurableCount: durableCount,
		})
		return err
	}
	if err := s.scheduleJiraWork(ctx, observedAt, instances); err != nil {
		s.recordReconcile(ctx, ReconcileObservation{
			Outcome:      reconcileOutcomeReconcileError,
			Duration:     time.Since(startedAt),
			DesiredCount: desiredCount,
			DurableCount: durableCount,
		})
		return err
	}
	if err := s.schedulePrometheusMimirWork(ctx, observedAt, instances); err != nil {
		s.recordReconcile(ctx, ReconcileObservation{
			Outcome:      reconcileOutcomeReconcileError,
			Duration:     time.Since(startedAt),
			DesiredCount: desiredCount,
			DurableCount: durableCount,
		})
		return err
	}
	if err := s.scheduleTempoWork(ctx, observedAt, instances); err != nil {
		s.recordReconcile(ctx, ReconcileObservation{
			Outcome:      reconcileOutcomeReconcileError,
			Duration:     time.Since(startedAt),
			DesiredCount: desiredCount,
			DurableCount: durableCount,
		})
		return err
	}
	if err := s.scheduleGrafanaWork(ctx, observedAt, instances); err != nil {
		s.recordReconcile(ctx, ReconcileObservation{
			Outcome:      reconcileOutcomeReconcileError,
			Duration:     time.Since(startedAt),
			DesiredCount: desiredCount,
			DurableCount: durableCount,
		})
		return err
	}
	if err := s.scheduleAWSScheduledWork(ctx, observedAt, instances); err != nil {
		s.recordReconcile(ctx, ReconcileObservation{
			Outcome:      reconcileOutcomeReconcileError,
			Duration:     time.Since(startedAt),
			DesiredCount: desiredCount,
			DurableCount: durableCount,
		})
		return err
	}
	if err := s.scheduleAWSFreshnessWork(ctx, observedAt, instances); err != nil {
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

func (s Service) runAWSFreshnessHandoff(ctx context.Context) error {
	if s.Config.DeploymentMode != deploymentModeActive || !s.Config.ClaimsEnabled || s.AWSFreshnessTriggers == nil {
		return nil
	}
	observedAt := s.now().UTC()
	instances, err := s.Store.ListCollectorInstances(ctx)
	if err != nil {
		return fmt.Errorf("list durable collector instances for AWS freshness handoff: %w", err)
	}
	return s.scheduleAWSFreshnessWork(ctx, observedAt, instances)
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

func (s Service) schedulePackageRegistryWork(
	ctx context.Context,
	observedAt time.Time,
	instances []workflow.CollectorInstance,
) error {
	if s.Config.DeploymentMode != deploymentModeActive || !s.Config.ClaimsEnabled {
		return nil
	}
	for _, instance := range instances {
		if !shouldSchedulePackageRegistry(instance) {
			continue
		}
		if s.PackageRegistryPlanner == nil {
			return fmt.Errorf("package registry planner is required for active package_registry collectors")
		}
		ownedTargets, err := s.packageRegistryOwnedTargets(ctx, instance, observedAt)
		if err != nil {
			return fmt.Errorf("load package registry derived targets for %q: %w", instance.InstanceID, err)
		}
		run, items, err := s.PackageRegistryPlanner.PlanPackageRegistryWork(ctx, PackageRegistryPlanRequest{
			Instance:            instance,
			ObservedAt:          observedAt,
			PlanKey:             s.packageRegistryPlanKey(instance, observedAt),
			OwnedPackageTargets: ownedTargets,
		})
		if err != nil {
			return fmt.Errorf("plan package registry work for %q: %w", instance.InstanceID, err)
		}
		if len(items) == 0 {
			continue
		}
		if _, err := s.createWorkflowWorkIfNoOpenTargets(ctx, instance, run, items); err != nil {
			return fmt.Errorf("create package registry scheduled work for %q: %w", instance.InstanceID, err)
		}
	}
	return nil
}

func (s Service) packageRegistryOwnedTargets(
	ctx context.Context,
	instance workflow.CollectorInstance,
	observedAt time.Time,
) ([]workflow.OwnedPackageDependencyTarget, error) {
	derivation, err := packageRegistryDerivationFromConfig(instance.Configuration)
	if err != nil {
		return nil, err
	}
	if !derivation.Enabled {
		return nil, nil
	}
	if s.OwnedPackageTargetReader == nil {
		return nil, fmt.Errorf("owned package target reader is required for derived package registry targets")
	}
	targetLimit := packageRegistryDerivedTargetLimit(derivation.TargetLimit)
	return s.OwnedPackageTargetReader.ListOwnedPackageDependencyTargets(ctx, workflow.OwnedPackageDependencyTargetFilter{
		Ecosystems:     sortedStringSetValues(packageRegistryDerivationEcosystems(derivation.Ecosystems)),
		Limit:          derivedTargetReadLimit(targetLimit),
		RotationOffset: derivedTargetRotationOffsetForMode(derivation.PlanningMode, observedAt, s.Config.ReconcileInterval, targetLimit),
	})
}

func shouldSchedulePackageRegistry(instance workflow.CollectorInstance) bool {
	return instance.CollectorKind == scope.CollectorPackageRegistry &&
		instance.Enabled &&
		instance.ClaimsEnabled
}

func (s Service) packageRegistryPlanKey(instance workflow.CollectorInstance, observedAt time.Time) string {
	if instance.Bootstrap {
		return "bootstrap"
	}
	interval := s.Config.ReconcileInterval
	prefix := strings.TrimSpace(string(instance.Mode))
	derivation, _ := packageRegistryDerivationFromConfig(instance.Configuration)
	return derivedTargetPlanKey(prefix, observedAt, interval, derivation.PlanningMode)
}

func (s Service) scheduleVulnerabilityIntelligenceWork(
	ctx context.Context,
	observedAt time.Time,
	instances []workflow.CollectorInstance,
) error {
	if s.Config.DeploymentMode != deploymentModeActive || !s.Config.ClaimsEnabled {
		return nil
	}
	for _, instance := range instances {
		if !shouldScheduleVulnerabilityIntelligence(instance) {
			continue
		}
		if s.VulnerabilityIntelligencePlanner == nil {
			return fmt.Errorf("vulnerability intelligence planner is required for active vulnerability_intelligence collectors")
		}
		ownedTargets, err := s.vulnerabilityOwnedTargets(ctx, instance, observedAt)
		if err != nil {
			return fmt.Errorf("load vulnerability intelligence derived targets for %q: %w", instance.InstanceID, err)
		}
		osTargets, sbomTargets, err := s.vulnerabilityInstalledEvidenceTargets(ctx, instance, observedAt)
		if err != nil {
			return fmt.Errorf("load vulnerability intelligence installed evidence targets for %q: %w", instance.InstanceID, err)
		}
		run, items, err := s.VulnerabilityIntelligencePlanner.PlanVulnerabilityIntelligenceWork(ctx, VulnerabilityIntelligencePlanRequest{
			Instance:             instance,
			ObservedAt:           observedAt,
			PlanKey:              s.vulnerabilityIntelligencePlanKey(instance, observedAt),
			OwnedPackageTargets:  ownedTargets,
			OSPackageTargets:     osTargets,
			SBOMComponentTargets: sbomTargets,
		})
		if err != nil {
			return fmt.Errorf("plan vulnerability intelligence work for %q: %w", instance.InstanceID, err)
		}
		if len(items) == 0 {
			continue
		}
		if _, err := s.createWorkflowWorkIfNoOpenTargets(ctx, instance, run, items); err != nil {
			return fmt.Errorf("create vulnerability intelligence scheduled work for %q: %w", instance.InstanceID, err)
		}
	}
	return nil
}

func (s Service) vulnerabilityOwnedTargets(
	ctx context.Context,
	instance workflow.CollectorInstance,
	observedAt time.Time,
) ([]workflow.OwnedPackageDependencyTarget, error) {
	derivation, err := vulnerabilityDerivationFromConfig(instance.Configuration)
	if err != nil {
		return nil, err
	}
	if !derivation.Enabled {
		return nil, nil
	}
	if s.OwnedPackageTargetReader == nil {
		return nil, fmt.Errorf("owned package target reader is required for derived vulnerability targets")
	}
	targetLimit := vulnerabilityDerivedTargetLimit(derivation.TargetLimit)
	return s.OwnedPackageTargetReader.ListOwnedPackageDependencyTargets(ctx, workflow.OwnedPackageDependencyTargetFilter{
		Ecosystems:      sortedStringSetValues(derivationEcosystems(derivation.Ecosystems, []string{"npm"})),
		Limit:           derivedTargetReadLimit(targetLimit),
		VersionSpecific: true,
		RotationOffset:  derivedTargetRotationOffsetForMode(derivation.PlanningMode, observedAt, s.Config.ReconcileInterval, targetLimit),
	})
}

func shouldScheduleVulnerabilityIntelligence(instance workflow.CollectorInstance) bool {
	return instance.CollectorKind == scope.CollectorVulnerabilityIntelligence &&
		instance.Enabled &&
		instance.ClaimsEnabled
}

func (s Service) vulnerabilityIntelligencePlanKey(instance workflow.CollectorInstance, observedAt time.Time) string {
	if instance.Bootstrap {
		return "bootstrap"
	}
	interval := s.Config.ReconcileInterval
	prefix := strings.TrimSpace(string(instance.Mode))
	derivation, _ := vulnerabilityPlanKeyDerivationFromConfig(instance.Configuration)
	return derivedTargetPlanKey(prefix, observedAt, interval, derivation.PlanningMode)
}

func (s Service) scheduleOCIRegistryWork(
	ctx context.Context,
	observedAt time.Time,
	instances []workflow.CollectorInstance,
) error {
	if s.Config.DeploymentMode != deploymentModeActive || !s.Config.ClaimsEnabled {
		return nil
	}
	for _, instance := range instances {
		if !shouldScheduleOCIRegistry(instance) {
			continue
		}
		if s.OCIRegistryPlanner == nil {
			return fmt.Errorf("OCI registry planner is required for active oci_registry collectors")
		}
		run, items, err := s.OCIRegistryPlanner.PlanOCIRegistryWork(ctx, OCIRegistryPlanRequest{
			Instance:   instance,
			ObservedAt: observedAt,
			PlanKey:    s.ociRegistryPlanKey(instance, observedAt),
		})
		if err != nil {
			return fmt.Errorf("plan OCI registry work for %q: %w", instance.InstanceID, err)
		}
		if len(items) == 0 {
			continue
		}
		if _, err := s.createWorkflowWorkIfNoOpenTargets(ctx, instance, run, items); err != nil {
			return fmt.Errorf("create OCI registry scheduled work for %q: %w", instance.InstanceID, err)
		}
	}
	return nil
}

func shouldScheduleOCIRegistry(instance workflow.CollectorInstance) bool {
	return instance.CollectorKind == scope.CollectorOCIRegistry &&
		instance.Enabled &&
		instance.ClaimsEnabled
}

func (s Service) ociRegistryPlanKey(instance workflow.CollectorInstance, observedAt time.Time) string {
	if instance.Bootstrap {
		return "bootstrap"
	}
	interval := s.Config.ReconcileInterval
	if interval <= 0 {
		interval = defaultReconcileInterval
	}
	prefix := strings.TrimSpace(string(instance.Mode))
	if prefix == "" {
		prefix = "schedule"
	}
	return fmt.Sprintf("%s-%s", prefix, observedAt.UTC().Truncate(interval).Format("20060102T150405Z"))
}

func (s Service) scheduleTerraformStateWork(
	ctx context.Context,
	observedAt time.Time,
	instances []workflow.CollectorInstance,
) error {
	if s.Config.DeploymentMode != deploymentModeActive || !s.Config.ClaimsEnabled {
		return nil
	}
	for _, instance := range instances {
		if !shouldScheduleTerraformState(instance) {
			continue
		}
		if s.TerraformStatePlanner == nil {
			return fmt.Errorf("terraform state planner is required for active terraform_state collectors")
		}
		run, items, err := s.TerraformStatePlanner.PlanTerraformStateWork(ctx, TerraformStatePlanRequest{
			Instance:   instance,
			ObservedAt: observedAt,
			PlanKey:    s.terraformStatePlanKey(instance, observedAt),
		})
		if err != nil {
			if terraformstate.IsWaitingOnGitGeneration(err) {
				s.logTerraformStateWait(instance, err)
				continue
			}
			return fmt.Errorf("plan terraform state work for %q: %w", instance.InstanceID, err)
		}
		if len(items) == 0 {
			continue
		}
		if _, err := s.createWorkflowWorkIfNoOpenTargets(ctx, instance, run, items); err != nil {
			return fmt.Errorf("create terraform state scheduled work for %q: %w", instance.InstanceID, err)
		}
	}
	return nil
}

func shouldScheduleTerraformState(instance workflow.CollectorInstance) bool {
	return instance.CollectorKind == scope.CollectorTerraformState &&
		instance.Enabled &&
		instance.ClaimsEnabled
}

func (s Service) terraformStatePlanKey(instance workflow.CollectorInstance, observedAt time.Time) string {
	if instance.Bootstrap {
		return "bootstrap"
	}
	interval := s.Config.ReconcileInterval
	if interval <= 0 {
		interval = defaultReconcileInterval
	}
	prefix := strings.TrimSpace(string(instance.Mode))
	if prefix == "" {
		prefix = "schedule"
	}
	return fmt.Sprintf("%s-%s", prefix, observedAt.UTC().Truncate(interval).Format("20060102T150405Z"))
}

func (s Service) logTerraformStateWait(instance workflow.CollectorInstance, err error) {
	if s.Logger == nil {
		return
	}
	s.Logger.Info(
		"terraform state workflow planning waiting on git generation",
		"collector_instance_id", instance.InstanceID,
		"collector_kind", instance.CollectorKind,
		"status", "waiting_on_git_generation",
		"error", err.Error(),
	)
}

func (s Service) recordReconcile(ctx context.Context, observation ReconcileObservation) {
	if s.Metrics == nil {
		return
	}
	s.Metrics.RecordReconcile(ctx, observation)
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

func (s Service) recordReap(ctx context.Context, observation ReapObservation) {
	metrics, ok := s.Metrics.(interface {
		RecordReap(context.Context, ReapObservation)
	})
	if !ok || metrics == nil {
		return
	}
	metrics.RecordReap(ctx, observation)
}

func (s Service) recordRunReconciliation(ctx context.Context, observation RunReconciliationObservation) {
	metrics, ok := s.Metrics.(interface {
		RecordRunReconciliation(context.Context, RunReconciliationObservation)
	})
	if !ok || metrics == nil {
		return
	}
	metrics.RecordRunReconciliation(ctx, observation)
}

func tickerChan(ticker *time.Ticker) <-chan time.Time {
	if ticker == nil {
		return nil
	}
	return ticker.C
}
