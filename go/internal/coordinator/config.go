// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/coordinator/egress"
	"github.com/eshu-hq/eshu/go/internal/coordinator/environment"
	"github.com/eshu-hq/eshu/go/internal/coordinator/planner/gcp"
	"github.com/eshu-hq/eshu/go/internal/coordinator/schedule"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const (
	defaultRunReconcileInterval = 30 * time.Second
	defaultClaimsEnabled        = false
	deploymentModeDark          = "dark"
	deploymentModeActive        = "active"
)

// Config captures workflow-coordinator runtime settings.
type Config struct {
	DeploymentMode           string
	ClaimsEnabled            bool
	ReconcileInterval        time.Duration
	RunReconcileInterval     time.Duration
	ReapInterval             time.Duration
	ClaimLeaseTTL            time.Duration
	HeartbeatInterval        time.Duration
	ExpiredClaimLimit        int
	ExpiredClaimRequeueDelay time.Duration
	// AWSFreshnessClaimLeaseDuration bounds how long a claimed AWS freshness
	// trigger can sit unresolved before the reap pass reclaims it back to
	// 'queued' (#4576). Zero uses defaultAWSFreshnessClaimLeaseDuration.
	AWSFreshnessClaimLeaseDuration time.Duration
	// GCPFreshnessClaimLeaseDuration is AWSFreshnessClaimLeaseDuration's GCP
	// counterpart (#4576). Zero uses defaultGCPFreshnessClaimLeaseDuration.
	GCPFreshnessClaimLeaseDuration time.Duration
	// IncidentFreshnessClaimLeaseDuration bounds how long a claimed incident
	// freshness trigger can sit unresolved before the reap pass reclaims it
	// back to 'queued' (#9379). Zero uses
	// defaultIncidentFreshnessClaimLeaseDuration.
	IncidentFreshnessClaimLeaseDuration time.Duration
	// FreshnessClaimReapLimit bounds how many stuck AWS/GCP freshness claims
	// one reap pass reclaims (#4576). Zero uses defaultFreshnessClaimReapLimit.
	FreshnessClaimReapLimit int
	CollectorEgressPolicy   egress.CollectorPolicy
	ExtensionEgressPolicy   egress.ExtensionPolicy
	TenantBoundary          WorkflowTenantBoundary
	ScannerImageDiscovery   ScannerImageDiscoveryPolicy
	PublicAdvisoryPolicy    PublicAdvisoryPolicy
	CollectorInstances      []workflow.DesiredCollectorInstance
	// StaleComponentActivationSkips lists claims-enabled registry activations
	// dropped from startup planning because their mounted config files are
	// absent (#9634). Informational: Validate ignores it, and the process
	// startup path reports and reconciles each entry.
	StaleComponentActivationSkips []StaleActivationSkip
	// QuotaLookup resolves the enforced per-component generation quota for
	// a collector kind (#8471, #8561). It is built from the admitted
	// manifests of components planning at least one claims-enabled
	// instance (#8582) with the operator override applied per field,
	// and run() passes it straight into the WorkflowControlStore. It is
	// always non-nil; with nothing to enforce every kind reports
	// unthrottled and claims keep the plain statement.
	QuotaLookup workflow.QuotaLookup
	// WorkItemRetention controls the workflow_work_items retention loop
	// (#8829). LoadConfig turns it on at a 720h age unless
	// ESHU_WORKFLOW_COORDINATOR_WORK_ITEM_RETENTION_AGE is 0; the zero value,
	// for a Config built by hand, is off.
	WorkItemRetention WorkItemRetentionConfig
}

// LoadConfig parses the workflow coordinator config from environment without
// reporting producer-grant decisions.
func LoadConfig(getenv func(string) string) (Config, error) {
	return LoadConfigObserved(getenv, nil)
}

// LoadConfigObserved is LoadConfig that also reports the producer-grant
// decisions the coordinator makes while planning installed component
// activations to observer (readback and activation stages). A nil observer
// disables reporting.
func LoadConfigObserved(getenv func(string) string, observer component.GrantObserver) (Config, error) {
	if getenv == nil {
		getenv = os.Getenv
	}

	deploymentMode := strings.TrimSpace(getenv("ESHU_WORKFLOW_COORDINATOR_DEPLOYMENT_MODE"))
	if deploymentMode == "" {
		deploymentMode = deploymentModeDark
	}

	claimsEnabled, err := environment.Bool(getenv, "ESHU_WORKFLOW_COORDINATOR_CLAIMS_ENABLED", defaultClaimsEnabled)
	if err != nil {
		return Config{}, err
	}
	if !claimsEnabled {
		claimsEnabled, err = environment.Bool(getenv, "ESHU_WORKFLOW_COORDINATOR_ENABLE_CLAIMS", defaultClaimsEnabled)
	}
	if err != nil {
		return Config{}, err
	}
	reconcileInterval, err := environment.Duration(getenv, "ESHU_WORKFLOW_COORDINATOR_RECONCILE_INTERVAL", schedule.DefaultReconcileInterval)
	if err != nil {
		return Config{}, err
	}
	runReconcileInterval, err := environment.Duration(
		getenv,
		"ESHU_WORKFLOW_COORDINATOR_RUN_RECONCILE_INTERVAL",
		defaultRunReconcileInterval,
	)
	if err != nil {
		return Config{}, err
	}
	reapInterval, err := environment.Duration(getenv, "ESHU_WORKFLOW_COORDINATOR_REAP_INTERVAL", workflow.DefaultReapInterval())
	if err != nil {
		return Config{}, err
	}
	claimLeaseTTL, err := environment.Duration(getenv, "ESHU_WORKFLOW_COORDINATOR_CLAIM_LEASE_TTL", workflow.DefaultClaimLeaseTTL())
	if err != nil {
		return Config{}, err
	}
	heartbeatInterval, err := environment.Duration(getenv, "ESHU_WORKFLOW_COORDINATOR_HEARTBEAT_INTERVAL", workflow.DefaultHeartbeatInterval())
	if err != nil {
		return Config{}, err
	}
	expiredClaimLimit, err := environment.Int(getenv, "ESHU_WORKFLOW_COORDINATOR_EXPIRED_CLAIM_LIMIT", workflow.DefaultExpiredClaimLimit())
	if err != nil {
		return Config{}, err
	}
	expiredClaimRequeueDelay, err := environment.Duration(getenv, "ESHU_WORKFLOW_COORDINATOR_EXPIRED_CLAIM_REQUEUE_DELAY", workflow.DefaultExpiredClaimRequeueDelay())
	if err != nil {
		return Config{}, err
	}
	awsFreshnessClaimLeaseDuration, gcpFreshnessClaimLeaseDuration, incidentFreshnessClaimLeaseDuration, freshnessClaimReapLimit, err := loadFreshnessClaimTimings(getenv)
	if err != nil {
		return Config{}, err
	}
	collectorEgressPolicy, err := egress.ParseCollectorPolicyJSON(getenv("ESHU_HOSTED_COLLECTOR_EGRESS_POLICY_JSON"))
	if err != nil {
		return Config{}, fmt.Errorf("parse ESHU_HOSTED_COLLECTOR_EGRESS_POLICY_JSON: %w", err)
	}
	extensionEgressPolicy, err := egress.ParseExtensionPolicyJSON(getenv("ESHU_HOSTED_EXTENSION_EGRESS_POLICY_JSON"))
	if err != nil {
		return Config{}, fmt.Errorf("parse ESHU_HOSTED_EXTENSION_EGRESS_POLICY_JSON: %w", err)
	}
	tenantBoundary, err := parseWorkflowTenantBoundaryJSON(getenv("ESHU_WORKFLOW_COORDINATOR_TENANT_BOUNDARY_JSON"))
	if err != nil {
		return Config{}, fmt.Errorf("parse ESHU_WORKFLOW_COORDINATOR_TENANT_BOUNDARY_JSON: %w", err)
	}
	scannerImageDiscovery, err := parseScannerImageDiscoveryPolicy(
		getenv("ESHU_WORKFLOW_COORDINATOR_SCANNER_IMAGE_DISCOVERY_POLICY_JSON"), tenantBoundary)
	if err != nil {
		return Config{}, fmt.Errorf("parse ESHU_WORKFLOW_COORDINATOR_SCANNER_IMAGE_DISCOVERY_POLICY_JSON: %w", err)
	}
	instances, err := workflow.ParseDesiredCollectorInstancesJSON(getenv("ESHU_COLLECTOR_INSTANCES_JSON"))
	if err != nil {
		return Config{}, fmt.Errorf("parse ESHU_COLLECTOR_INSTANCES_JSON: %w", err)
	}
	componentInstances, quotaManifests, staleSkips, err := componentCollectorInstancesFromEnv(getenv, observer)
	if err != nil {
		return Config{}, err
	}
	instances, err = mergeCollectorInstances(instances, componentInstances)
	if err != nil {
		return Config{}, err
	}
	publicAdvisoryPolicy, err := parsePublicAdvisoryPolicy(
		getenv("ESHU_WORKFLOW_COORDINATOR_PUBLIC_ADVISORY_POLICY_JSON"), instances)
	if err != nil {
		return Config{}, fmt.Errorf("parse ESHU_WORKFLOW_COORDINATOR_PUBLIC_ADVISORY_POLICY_JSON: %w", err)
	}
	quotaOverride, err := componentQuotaOverrideFromEnv(getenv)
	if err != nil {
		return Config{}, err
	}
	quotaLookup, err := buildComponentQuotaLookup(quotaManifests, quotaOverride)
	if err != nil {
		return Config{}, err
	}
	workItemRetention, err := loadWorkItemRetentionConfig(getenv)
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		DeploymentMode:                      deploymentMode,
		ClaimsEnabled:                       claimsEnabled,
		ReconcileInterval:                   reconcileInterval,
		RunReconcileInterval:                runReconcileInterval,
		ReapInterval:                        reapInterval,
		ClaimLeaseTTL:                       claimLeaseTTL,
		HeartbeatInterval:                   heartbeatInterval,
		ExpiredClaimLimit:                   expiredClaimLimit,
		ExpiredClaimRequeueDelay:            expiredClaimRequeueDelay,
		AWSFreshnessClaimLeaseDuration:      awsFreshnessClaimLeaseDuration,
		IncidentFreshnessClaimLeaseDuration: incidentFreshnessClaimLeaseDuration,
		GCPFreshnessClaimLeaseDuration:      gcpFreshnessClaimLeaseDuration,
		FreshnessClaimReapLimit:             freshnessClaimReapLimit,
		CollectorEgressPolicy:               collectorEgressPolicy,
		ExtensionEgressPolicy:               extensionEgressPolicy,
		TenantBoundary:                      tenantBoundary,
		ScannerImageDiscovery:               scannerImageDiscovery,
		PublicAdvisoryPolicy:                publicAdvisoryPolicy,
		CollectorInstances:                  instances,
		QuotaLookup:                         quotaLookup,
		StaleComponentActivationSkips:       staleSkips,
		WorkItemRetention:                   workItemRetention,
	}
	cfg = cfg.withDefaults()
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// loadFreshnessClaimTimings parses the AWS/GCP/incident freshness claim lease
// durations and the per-pass stuck-claim reap limit (#4576, #9379). Each
// parse failure returns the bare environment error, matching the inline
// behavior this helper replaces.
func loadFreshnessClaimTimings(getenv func(string) string) (time.Duration, time.Duration, time.Duration, int, error) {
	awsLease, err := environment.Duration(
		getenv,
		"ESHU_WORKFLOW_COORDINATOR_AWS_FRESHNESS_CLAIM_LEASE_DURATION",
		defaultAWSFreshnessClaimLeaseDuration,
	)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	gcpLease, err := environment.Duration(
		getenv,
		"ESHU_WORKFLOW_COORDINATOR_GCP_FRESHNESS_CLAIM_LEASE_DURATION",
		defaultGCPFreshnessClaimLeaseDuration,
	)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	incidentLease, err := environment.Duration(
		getenv,
		"ESHU_WORKFLOW_COORDINATOR_INCIDENT_FRESHNESS_CLAIM_LEASE_DURATION",
		defaultIncidentFreshnessClaimLeaseDuration,
	)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	reapLimit, err := environment.Int(
		getenv,
		"ESHU_WORKFLOW_COORDINATOR_FRESHNESS_CLAIM_REAP_LIMIT",
		defaultFreshnessClaimReapLimit,
	)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	return awsLease, gcpLease, incidentLease, reapLimit, nil
}

// Validate checks the coordinator config invariants.
func (c Config) Validate() error {
	c = c.withDefaults()
	switch c.DeploymentMode {
	case deploymentModeDark, deploymentModeActive:
	default:
		return fmt.Errorf("workflow coordinator deployment mode %q is not supported", c.DeploymentMode)
	}
	if c.ReconcileInterval <= 0 {
		return fmt.Errorf("workflow coordinator reconcile interval must be positive")
	}
	if c.RunReconcileInterval <= 0 {
		return fmt.Errorf("workflow coordinator run reconcile interval must be positive")
	}
	if c.ReapInterval <= 0 {
		return fmt.Errorf("workflow coordinator reap interval must be positive")
	}
	if c.ClaimLeaseTTL <= 0 {
		return fmt.Errorf("workflow coordinator claim lease TTL must be positive")
	}
	if c.HeartbeatInterval <= 0 {
		return fmt.Errorf("workflow coordinator heartbeat interval must be positive")
	}
	if c.HeartbeatInterval >= c.ClaimLeaseTTL {
		return fmt.Errorf("workflow coordinator heartbeat interval must be less than lease TTL")
	}
	if c.ExpiredClaimLimit <= 0 {
		return fmt.Errorf("workflow coordinator expired claim limit must be positive")
	}
	if c.ExpiredClaimRequeueDelay < 0 {
		return fmt.Errorf("workflow coordinator expired claim requeue delay must not be negative")
	}
	if c.AWSFreshnessClaimLeaseDuration <= 0 {
		return fmt.Errorf("workflow coordinator AWS freshness claim lease duration must be positive")
	}
	if c.GCPFreshnessClaimLeaseDuration <= 0 {
		return fmt.Errorf("workflow coordinator GCP freshness claim lease duration must be positive")
	}
	if c.FreshnessClaimReapLimit <= 0 {
		return fmt.Errorf("workflow coordinator freshness claim reap limit must be positive")
	}
	if err := c.TenantBoundary.validate(); err != nil {
		return err
	}
	if err := c.ScannerImageDiscovery.validate(c.TenantBoundary); err != nil {
		return err
	}
	if _, err := c.PublicAdvisoryPolicy.selectInstances(c.CollectorInstances); err != nil {
		return err
	}
	activeClaimCollectors := 0
	for _, instance := range c.CollectorInstances {
		if err := instance.Validate(); err != nil {
			return fmt.Errorf("workflow coordinator collector instance: %w", err)
		}
		derivesImages, err := scannerInstanceDerivesFromRunningImages(instance)
		if err != nil {
			return fmt.Errorf("collector instance %q: %w", instance.InstanceID, err)
		}
		if derivesImages && c.ScannerImageDiscovery.Mode == ScannerImageDiscoveryDisabled {
			return fmt.Errorf("collector instance %q derives running images without an enabled scanner image discovery policy", instance.InstanceID)
		}
		if err := validateScanInterval(instance.Configuration, c.ReconcileInterval); err != nil {
			return fmt.Errorf("collector instance %q: %w", instance.InstanceID, err)
		}
		// instance.Validate skips the derive rules for an activation
		// document, so a static one is checked here (#9311).
		if err := checkActivationDocumentDerivation(instance.CollectorKind, instance.Configuration); err != nil {
			return fmt.Errorf("collector instance %q: %w", instance.InstanceID, err)
		}
		if instance.Enabled && instance.ClaimsEnabled && !c.ClaimsEnabled {
			return fmt.Errorf("collector instance %q enables claims while coordinator claims are disabled", instance.InstanceID)
		}
		if instance.Enabled && instance.ClaimsEnabled {
			if err := validateCollectorClaimSchedulingSupported(instance); err != nil {
				return err
			}
			activeClaimCollectors++
		}
	}
	if c.DeploymentMode == deploymentModeActive {
		if !c.ClaimsEnabled {
			return fmt.Errorf("workflow coordinator active mode requires claims enabled")
		}
		if activeClaimCollectors == 0 {
			return fmt.Errorf("workflow coordinator active mode requires at least one enabled claim-capable collector instance")
		}
	}
	return nil
}

// validateCollectorClaimSchedulingSupported runs the kind-specific startup
// claim check for an in-tree instance. An instance configured as a component
// activation document belongs to the generic component scheduler for every
// collector kind, so no kind validator may read the document as its own
// configuration; the generic planner reports a malformed one at reconcile.
func validateCollectorClaimSchedulingSupported(instance workflow.DesiredCollectorInstance) error {
	if isComponentActivationConfiguration(instance.Configuration) {
		return nil
	}
	switch instance.CollectorKind {
	case scope.CollectorGCP:
		return gcp.ValidateClaimSchedulerConfiguration(instance)
	case scope.CollectorVaultLive:
		// The in-tree vault/live planner is removed (#8869): an enabled
		// vault_live instance would be admitted and then never planned.
		// The lane now ships as eshu-hq/eshu-collector-vault-live, so
		// reject loudly at startup instead of stalling silently.
		return fmt.Errorf("collector instance %q uses collector_kind %q: vault_live planning moved out of tree to eshu-hq/eshu-collector-vault-live; disable the in-tree instance or convert it to a component activation document", instance.InstanceID, instance.CollectorKind)
	default:
		return nil
	}
}

type workflowTenantBoundaryConfig struct {
	TenantID           string `json:"tenant_id"`
	WorkspaceID        string `json:"workspace_id"`
	SubjectClass       string `json:"subject_class"`
	PolicyRevisionHash string `json:"policy_revision_hash"`
}

func parseWorkflowTenantBoundaryJSON(raw string) (WorkflowTenantBoundary, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return WorkflowTenantBoundary{}, nil
	}
	var decoded workflowTenantBoundaryConfig
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return WorkflowTenantBoundary{}, err
	}
	boundary := WorkflowTenantBoundary(decoded).normalize()
	if err := boundary.validate(); err != nil {
		return WorkflowTenantBoundary{}, err
	}
	return boundary, nil
}

func (c Config) withDefaults() Config {
	if strings.TrimSpace(c.DeploymentMode) == "" {
		c.DeploymentMode = deploymentModeDark
	}
	if c.ReconcileInterval <= 0 {
		c.ReconcileInterval = schedule.DefaultReconcileInterval
	}
	if c.RunReconcileInterval <= 0 {
		c.RunReconcileInterval = defaultRunReconcileInterval
	}
	if c.ReapInterval <= 0 {
		c.ReapInterval = workflow.DefaultReapInterval()
	}
	if c.ClaimLeaseTTL <= 0 {
		c.ClaimLeaseTTL = workflow.DefaultClaimLeaseTTL()
	}
	if c.HeartbeatInterval <= 0 {
		c.HeartbeatInterval = workflow.DefaultHeartbeatInterval()
	}
	if c.ExpiredClaimLimit <= 0 {
		c.ExpiredClaimLimit = workflow.DefaultExpiredClaimLimit()
	}
	if c.ExpiredClaimRequeueDelay == 0 {
		c.ExpiredClaimRequeueDelay = workflow.DefaultExpiredClaimRequeueDelay()
	}
	if c.AWSFreshnessClaimLeaseDuration <= 0 {
		c.AWSFreshnessClaimLeaseDuration = defaultAWSFreshnessClaimLeaseDuration
	}
	if c.GCPFreshnessClaimLeaseDuration <= 0 {
		c.GCPFreshnessClaimLeaseDuration = defaultGCPFreshnessClaimLeaseDuration
	}
	if c.FreshnessClaimReapLimit <= 0 {
		c.FreshnessClaimReapLimit = defaultFreshnessClaimReapLimit
	}
	return c
}
