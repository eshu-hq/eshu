package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/awsruntime"
	"github.com/eshu-hq/eshu/go/internal/redact"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const defaultPollInterval = time.Second

type runtimeConfig struct {
	Instance          workflow.DesiredCollectorInstance
	OwnerID           string
	PollInterval      time.Duration
	ClaimLeaseTTL     time.Duration
	HeartbeatInterval time.Duration
	AWS               awsruntime.Config
	AWSRedactionKey   redact.Key
}

type awsRuntimeConfiguration struct {
	TargetScopes []awsTargetScopeConfiguration `json:"target_scopes"`
}

type awsTargetScopeConfiguration struct {
	AccountID           string                     `json:"account_id"`
	AllowedRegions      []string                   `json:"allowed_regions"`
	AllowedServices     []string                   `json:"allowed_services"`
	MaxConcurrentClaims int                        `json:"max_concurrent_claims"`
	Credentials         awsCredentialConfiguration `json:"credentials"`
}

type awsCredentialConfiguration struct {
	Mode            string `json:"mode"`
	RoleARN         string `json:"role_arn"`
	ExternalID      string `json:"external_id"`
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
	SessionToken    string `json:"session_token"`
}

func loadRuntimeConfig(getenv func(string) string) (runtimeConfig, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	instances, err := workflow.ParseDesiredCollectorInstancesJSON(getenv("ESHU_COLLECTOR_INSTANCES_JSON"))
	if err != nil {
		return runtimeConfig{}, fmt.Errorf("parse ESHU_COLLECTOR_INSTANCES_JSON: %w", err)
	}
	instance, err := selectAWSInstance(instances, getenv("ESHU_AWS_COLLECTOR_INSTANCE_ID"))
	if err != nil {
		return runtimeConfig{}, err
	}
	if err := validateAWSInstance(instance); err != nil {
		return runtimeConfig{}, err
	}
	awsConfig, err := parseAWSRuntimeConfiguration(instance)
	if err != nil {
		return runtimeConfig{}, err
	}
	redactionKey, err := loadAWSRedactionKeyIfNeeded(getenv, awsConfig)
	if err != nil {
		return runtimeConfig{}, err
	}
	pollInterval, err := envDuration(getenv, "ESHU_AWS_COLLECTOR_POLL_INTERVAL", defaultPollInterval)
	if err != nil {
		return runtimeConfig{}, err
	}
	claimLeaseTTL, err := envDuration(getenv, "ESHU_AWS_COLLECTOR_CLAIM_LEASE_TTL", workflow.DefaultClaimLeaseTTL())
	if err != nil {
		return runtimeConfig{}, err
	}
	heartbeatInterval, err := envDuration(getenv, "ESHU_AWS_COLLECTOR_HEARTBEAT_INTERVAL", workflow.DefaultHeartbeatInterval())
	if err != nil {
		return runtimeConfig{}, err
	}
	if heartbeatInterval >= claimLeaseTTL {
		return runtimeConfig{}, fmt.Errorf("AWS collector heartbeat interval must be less than claim lease TTL")
	}
	return runtimeConfig{
		Instance:          instance,
		OwnerID:           ownerID(getenv),
		PollInterval:      pollInterval,
		ClaimLeaseTTL:     claimLeaseTTL,
		HeartbeatInterval: heartbeatInterval,
		AWS:               awsConfig,
		AWSRedactionKey:   redactionKey,
	}, nil
}

func selectAWSInstance(
	instances []workflow.DesiredCollectorInstance,
	requestedInstanceID string,
) (workflow.DesiredCollectorInstance, error) {
	requestedInstanceID = strings.TrimSpace(requestedInstanceID)
	var matches []workflow.DesiredCollectorInstance
	for _, instance := range instances {
		if instance.CollectorKind != scope.CollectorAWS {
			continue
		}
		if requestedInstanceID != "" && instance.InstanceID != requestedInstanceID {
			continue
		}
		matches = append(matches, instance)
	}
	switch len(matches) {
	case 0:
		if requestedInstanceID != "" {
			return workflow.DesiredCollectorInstance{}, fmt.Errorf("AWS collector instance %q not found", requestedInstanceID)
		}
		return workflow.DesiredCollectorInstance{}, fmt.Errorf("no AWS collector instance configured")
	case 1:
		return matches[0], nil
	default:
		return workflow.DesiredCollectorInstance{}, fmt.Errorf("multiple AWS collector instances configured; set ESHU_AWS_COLLECTOR_INSTANCE_ID")
	}
}

func validateAWSInstance(instance workflow.DesiredCollectorInstance) error {
	if err := instance.Validate(); err != nil {
		return fmt.Errorf("AWS collector instance: %w", err)
	}
	if instance.CollectorKind != scope.CollectorAWS {
		return fmt.Errorf("AWS collector requires collector_kind %q", scope.CollectorAWS)
	}
	if !instance.Enabled {
		return fmt.Errorf("AWS collector requires enabled collector instance")
	}
	if !instance.ClaimsEnabled {
		return fmt.Errorf("AWS collector requires claim-enabled collector instance")
	}
	return nil
}

func parseAWSRuntimeConfiguration(instance workflow.DesiredCollectorInstance) (awsruntime.Config, error) {
	var decoded awsRuntimeConfiguration
	if err := json.Unmarshal([]byte(instance.Configuration), &decoded); err != nil {
		return awsruntime.Config{}, fmt.Errorf("decode AWS collector configuration: %w", err)
	}
	if len(decoded.TargetScopes) == 0 {
		return awsruntime.Config{}, fmt.Errorf("AWS collector configuration requires target_scopes")
	}
	targets := make([]awsruntime.TargetScope, 0, len(decoded.TargetScopes))
	for i, target := range decoded.TargetScopes {
		mapped, err := mapTargetScope(target)
		if err != nil {
			return awsruntime.Config{}, fmt.Errorf("target_scopes[%d]: %w", i, err)
		}
		targets = append(targets, mapped)
	}
	return awsruntime.Config{
		CollectorInstanceID: instance.InstanceID,
		Targets:             targets,
	}, nil
}

func mapTargetScope(target awsTargetScopeConfiguration) (awsruntime.TargetScope, error) {
	accountID := strings.TrimSpace(target.AccountID)
	if accountID == "" {
		return awsruntime.TargetScope{}, fmt.Errorf("account_id is required")
	}
	if len(target.AllowedRegions) == 0 {
		return awsruntime.TargetScope{}, fmt.Errorf("allowed_regions is required")
	}
	if len(target.AllowedServices) == 0 {
		return awsruntime.TargetScope{}, fmt.Errorf("allowed_services is required")
	}
	credentials, err := mapCredentialConfig(target.Credentials)
	if err != nil {
		return awsruntime.TargetScope{}, err
	}
	return awsruntime.TargetScope{
		AccountID:           accountID,
		AllowedRegions:      trimStrings(target.AllowedRegions),
		AllowedServices:     trimStrings(target.AllowedServices),
		MaxConcurrentClaims: target.MaxConcurrentClaims,
		Credentials:         credentials,
	}, nil
}

func mapCredentialConfig(config awsCredentialConfiguration) (awsruntime.CredentialConfig, error) {
	if strings.TrimSpace(config.AccessKeyID) != "" ||
		strings.TrimSpace(config.SecretAccessKey) != "" ||
		strings.TrimSpace(config.SessionToken) != "" {
		return awsruntime.CredentialConfig{}, fmt.Errorf("static AWS credential fields are not allowed")
	}
	mode := awsruntime.CredentialMode(strings.TrimSpace(config.Mode))
	switch mode {
	case awsruntime.CredentialModeCentralAssumeRole:
		if strings.TrimSpace(config.RoleARN) == "" {
			return awsruntime.CredentialConfig{}, fmt.Errorf("central_assume_role credentials require role_arn")
		}
	case awsruntime.CredentialModeLocalWorkloadIdentity:
	default:
		return awsruntime.CredentialConfig{}, fmt.Errorf("unsupported AWS credential mode %q", config.Mode)
	}
	return awsruntime.CredentialConfig{
		Mode:       mode,
		RoleARN:    strings.TrimSpace(config.RoleARN),
		ExternalID: strings.TrimSpace(config.ExternalID),
	}, nil
}

func loadAWSRedactionKeyIfNeeded(
	getenv func(string) string,
	config awsruntime.Config,
) (redact.Key, error) {
	if !awsConfigNeedsRedactionKey(config) {
		return redact.Key{}, nil
	}
	value := strings.TrimSpace(getenv("ESHU_AWS_REDACTION_KEY"))
	if value == "" {
		return redact.Key{}, fmt.Errorf("ESHU_AWS_REDACTION_KEY is required when ecs or lambda service scans are enabled")
	}
	key, err := redact.NewKey([]byte(value))
	if err != nil {
		return redact.Key{}, fmt.Errorf("ESHU_AWS_REDACTION_KEY: %w", err)
	}
	return key, nil
}

func awsConfigNeedsRedactionKey(config awsruntime.Config) bool {
	for _, target := range config.Targets {
		for _, service := range target.AllowedServices {
			switch strings.TrimSpace(service) {
			case awscloud.ServiceECS, awscloud.ServiceLambda:
				return true
			}
		}
	}
	return false
}

func envDuration(getenv func(string) string, key string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}
	if value <= 0 {
		return 0, fmt.Errorf("%s must be positive", key)
	}
	return value, nil
}

func ownerID(getenv func(string) string) string {
	for _, key := range []string{"ESHU_AWS_COLLECTOR_OWNER_ID", "HOSTNAME"} {
		if value := strings.TrimSpace(getenv(key)); value != "" {
			return value
		}
	}
	return "collector-aws-cloud"
}

func trimStrings(values []string) []string {
	trimmed := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			trimmed = append(trimmed, value)
		}
	}
	return trimmed
}
