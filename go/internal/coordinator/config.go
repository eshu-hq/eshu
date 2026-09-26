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
	// FreshnessClaimReapLimit bounds how many stuck AWS/GCP freshness claims
	// one reap pass reclaims (#4576). Zero uses defaultFreshnessClaimReapLimit.
	FreshnessClaimReapLimit int
	CollectorEgressPolicy   egress.CollectorPolicy
	ExtensionEgressPolicy   egress.ExtensionPolicy
	TenantBoundary          WorkflowTenantBoundary
	CollectorInstances      []workflow.DesiredCollectorInstance
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
	awsFreshnessClaimLeaseDuration, err := environment.Duration(
		getenv,
		"ESHU_WORKFLOW_COORDINATOR_AWS_FRESHNESS_CLAIM_LEASE_DURATION",
		defaultAWSFreshnessClaimLeaseDuration,
	)
	if err != nil {
		return Config{}, err
	}
	gcpFreshnessClaimLeaseDuration, err := environment.Duration(
		getenv,
		"ESHU_WORKFLOW_COORDINATOR_GCP_FRESHNESS_CLAIM_LEASE_DURATION",
		defaultGCPFreshnessClaimLeaseDuration,
	)
	if err != nil {
		return Config{}, err
	}
	freshnessClaimReapLimit, err := environment.Int(
		getenv,
		"ESHU_WORKFLOW_COORDINATOR_FRESHNESS_CLAIM_REAP_LIMIT",
		defaultFreshnessClaimReapLimit,
	)
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
	instances, err := workflow.ParseDesiredCollectorInstancesJSON(getenv("ESHU_COLLECTOR_INSTANCES_JSON"))
	if err != nil {
		return Config{}, fmt.Errorf("parse ESHU_COLLECTOR_INSTANCES_JSON: %w", err)
	}
	componentInstances, err := componentCollectorInstancesFromEnv(getenv, observer)
	if err != nil {
		return Config{}, err
	}
	instances, err = mergeCollectorInstances(instances, componentInstances)
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		DeploymentMode:                 deploymentMode,
		ClaimsEnabled:                  claimsEnabled,
		ReconcileInterval:              reconcileInterval,
		RunReconcileInterval:           runReconcileInterval,
		ReapInterval:                   reapInterval,
		ClaimLeaseTTL:                  claimLeaseTTL,
		HeartbeatInterval:              heartbeatInterval,
		ExpiredClaimLimit:              expiredClaimLimit,
		ExpiredClaimRequeueDelay:       expiredClaimRequeueDelay,
		AWSFreshnessClaimLeaseDuration: awsFreshnessClaimLeaseDuration,
		GCPFreshnessClaimLeaseDuration: gcpFreshnessClaimLeaseDuration,
		FreshnessClaimReapLimit:        freshnessClaimReapLimit,
		CollectorEgressPolicy:          collectorEgressPolicy,
		ExtensionEgressPolicy:          extensionEgressPolicy,
		TenantBoundary:                 tenantBoundary,
		CollectorInstances:             instances,
	}
	cfg = cfg.withDefaults()
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
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
	activeClaimCollectors := 0
	for _, instance := range c.CollectorInstances {
		if err := instance.Validate(); err != nil {
			return fmt.Errorf("workflow coordinator collector instance: %w", err)
		}
		if err := validateScanInterval(instance.Configuration, c.ReconcileInterval); err != nil {
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

func validateCollectorClaimSchedulingSupported(instance workflow.DesiredCollectorInstance) error {
	switch instance.CollectorKind {
	case scope.CollectorGCP:
		return gcp.ValidateClaimSchedulerConfiguration(instance)
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
