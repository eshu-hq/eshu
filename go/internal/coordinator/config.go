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
	defaultReconcileInterval = 30 * time.Second
	defaultClaimsEnabled     = false
	deploymentModeDark       = "dark"
	deploymentModeActive     = "active"
)

// Config captures workflow-coordinator runtime settings.
type Config struct {
	DeploymentMode           string
	ClaimsEnabled            bool
	ReconcileInterval        time.Duration
	ReapInterval             time.Duration
	ClaimLeaseTTL            time.Duration
	HeartbeatInterval        time.Duration
	ExpiredClaimLimit        int
	ExpiredClaimRequeueDelay time.Duration
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
	instances, err := parseCollectorInstances(getenv("ESHU_COLLECTOR_INSTANCES_JSON"))
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		DeploymentMode:           deploymentMode,
		ClaimsEnabled:            claimsEnabled,
		ReconcileInterval:        reconcileInterval,
		ReapInterval:             reapInterval,
		ClaimLeaseTTL:            claimLeaseTTL,
		HeartbeatInterval:        heartbeatInterval,
		ExpiredClaimLimit:        expiredClaimLimit,
		ExpiredClaimRequeueDelay: expiredClaimRequeueDelay,
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
	activeClaimCollectors := 0
	for _, instance := range c.CollectorInstances {
		if err := instance.Validate(); err != nil {
			return fmt.Errorf("workflow coordinator collector instance: %w", err)
		}
		if instance.ClaimsEnabled && !c.ClaimsEnabled {
			return fmt.Errorf("collector instance %q enables claims while coordinator claims are disabled", instance.InstanceID)
		}
		if instance.Enabled && instance.ClaimsEnabled {
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

func (c Config) withDefaults() Config {
	if strings.TrimSpace(c.DeploymentMode) == "" {
		c.DeploymentMode = deploymentModeDark
	}
	if c.ReconcileInterval <= 0 {
		c.ReconcileInterval = defaultReconcileInterval
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

type collectorInstanceConfig struct {
	InstanceID    string          `json:"instance_id"`
	CollectorKind string          `json:"collector_kind"`
	Mode          string          `json:"mode"`
	Enabled       bool            `json:"enabled"`
	Bootstrap     bool            `json:"bootstrap"`
	ClaimsEnabled bool            `json:"claims_enabled"`
	DisplayName   string          `json:"display_name"`
	Configuration json.RawMessage `json:"configuration"`
}

func parseCollectorInstances(raw string) ([]workflow.DesiredCollectorInstance, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}

	var decoded []collectorInstanceConfig
	if err := json.Unmarshal([]byte(trimmed), &decoded); err != nil {
		return nil, fmt.Errorf("parse ESHU_COLLECTOR_INSTANCES_JSON: %w", err)
	}

	instances := make([]workflow.DesiredCollectorInstance, 0, len(decoded))
	for _, candidate := range decoded {
		instance := workflow.DesiredCollectorInstance{
			InstanceID:    strings.TrimSpace(candidate.InstanceID),
			CollectorKind: scope.CollectorKind(strings.TrimSpace(candidate.CollectorKind)),
			Mode:          workflow.CollectorMode(strings.TrimSpace(candidate.Mode)),
			Enabled:       candidate.Enabled,
			Bootstrap:     candidate.Bootstrap,
			ClaimsEnabled: candidate.ClaimsEnabled,
			DisplayName:   strings.TrimSpace(candidate.DisplayName),
			Configuration: string(candidate.Configuration),
		}
		if strings.TrimSpace(instance.Configuration) == "" {
			instance.Configuration = "{}"
		}
		instances = append(instances, instance)
	}
	return instances, nil
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
