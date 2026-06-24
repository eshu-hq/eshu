// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const (
	defaultReconcileInterval    = 30 * time.Second
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
	CollectorEgressPolicy    CollectorEgressPolicy
	ExtensionEgressPolicy    ExtensionEgressPolicy
	TenantBoundary           WorkflowTenantBoundary
	CollectorInstances       []workflow.DesiredCollectorInstance
}

// LoadConfig parses the workflow coordinator config from environment.
func LoadConfig(getenv func(string) string) (Config, error) {
	if getenv == nil {
		getenv = os.Getenv
	}

	deploymentMode := strings.TrimSpace(getenv("ESHU_WORKFLOW_COORDINATOR_DEPLOYMENT_MODE"))
	if deploymentMode == "" {
		deploymentMode = deploymentModeDark
	}

	claimsEnabled, err := envBool(getenv, "ESHU_WORKFLOW_COORDINATOR_CLAIMS_ENABLED", defaultClaimsEnabled)
	if err != nil {
		return Config{}, err
	}
	if !claimsEnabled {
		claimsEnabled, err = envBool(getenv, "ESHU_WORKFLOW_COORDINATOR_ENABLE_CLAIMS", defaultClaimsEnabled)
	}
	if err != nil {
		return Config{}, err
	}
	reconcileInterval, err := envDuration(getenv, "ESHU_WORKFLOW_COORDINATOR_RECONCILE_INTERVAL", defaultReconcileInterval)
	if err != nil {
		return Config{}, err
	}
	runReconcileInterval, err := envDuration(
		getenv,
		"ESHU_WORKFLOW_COORDINATOR_RUN_RECONCILE_INTERVAL",
		defaultRunReconcileInterval,
	)
	if err != nil {
		return Config{}, err
	}
	reapInterval, err := envDuration(getenv, "ESHU_WORKFLOW_COORDINATOR_REAP_INTERVAL", workflow.DefaultReapInterval())
	if err != nil {
		return Config{}, err
	}
	claimLeaseTTL, err := envDuration(getenv, "ESHU_WORKFLOW_COORDINATOR_CLAIM_LEASE_TTL", workflow.DefaultClaimLeaseTTL())
	if err != nil {
		return Config{}, err
	}
	heartbeatInterval, err := envDuration(getenv, "ESHU_WORKFLOW_COORDINATOR_HEARTBEAT_INTERVAL", workflow.DefaultHeartbeatInterval())
	if err != nil {
		return Config{}, err
	}
	expiredClaimLimit, err := envInt(getenv, "ESHU_WORKFLOW_COORDINATOR_EXPIRED_CLAIM_LIMIT", workflow.DefaultExpiredClaimLimit())
	if err != nil {
		return Config{}, err
	}
	expiredClaimRequeueDelay, err := envDuration(getenv, "ESHU_WORKFLOW_COORDINATOR_EXPIRED_CLAIM_REQUEUE_DELAY", workflow.DefaultExpiredClaimRequeueDelay())
	if err != nil {
		return Config{}, err
	}
	collectorEgressPolicy, err := ParseCollectorEgressPolicyJSON(getenv("ESHU_HOSTED_COLLECTOR_EGRESS_POLICY_JSON"))
	if err != nil {
		return Config{}, fmt.Errorf("parse ESHU_HOSTED_COLLECTOR_EGRESS_POLICY_JSON: %w", err)
	}
	extensionEgressPolicy, err := ParseExtensionEgressPolicyJSON(getenv("ESHU_HOSTED_EXTENSION_EGRESS_POLICY_JSON"))
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
	componentInstances, err := componentCollectorInstancesFromEnv(getenv)
	if err != nil {
		return Config{}, err
	}
	instances, err = mergeCollectorInstances(instances, componentInstances)
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		DeploymentMode:           deploymentMode,
		ClaimsEnabled:            claimsEnabled,
		ReconcileInterval:        reconcileInterval,
		RunReconcileInterval:     runReconcileInterval,
		ReapInterval:             reapInterval,
		ClaimLeaseTTL:            claimLeaseTTL,
		HeartbeatInterval:        heartbeatInterval,
		ExpiredClaimLimit:        expiredClaimLimit,
		ExpiredClaimRequeueDelay: expiredClaimRequeueDelay,
		CollectorEgressPolicy:    collectorEgressPolicy,
		ExtensionEgressPolicy:    extensionEgressPolicy,
		TenantBoundary:           tenantBoundary,
		CollectorInstances:       instances,
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
	if err := c.TenantBoundary.validate(); err != nil {
		return err
	}
	activeClaimCollectors := 0
	for _, instance := range c.CollectorInstances {
		if err := instance.Validate(); err != nil {
			return fmt.Errorf("workflow coordinator collector instance: %w", err)
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
		return validateGCPClaimSchedulerConfiguration(instance)
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
		c.ReconcileInterval = defaultReconcileInterval
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
	return c
}

func envInt(getenv func(string) string, key string, fallback int) (int, error) {
	value := strings.TrimSpace(getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return parsed, nil
}

func envBool(getenv func(string) string, key string, fallback bool) (bool, error) {
	value := strings.TrimSpace(getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("%s: %w", key, err)
	}
	return parsed, nil
}

func envDuration(getenv func(string) string, key string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return parsed, nil
}
