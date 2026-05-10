package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/redact"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const (
	defaultPollInterval         = time.Second
	defaultSourceMaxBytes int64 = 0
)

type runtimeConfig struct {
	Instance             workflow.DesiredCollectorInstance
	OwnerID              string
	PollInterval         time.Duration
	ClaimLeaseTTL        time.Duration
	HeartbeatInterval    time.Duration
	RedactionKey         redact.Key
	SourceMaxBytes       int64
	AWSRoleARN           string
	AWSCredentials       awsCredentialConfig
	AWSTargetScopes      []awsTargetScopeConfig
	AWSDynamoDBLockTable string
}

type terraformStateRuntimeConfiguration struct {
	AWS          terraformStateRuntimeAWSConfiguration `json:"aws"`
	TargetScopes []terraformStateRuntimeTargetScope    `json:"target_scopes"`
}

type terraformStateRuntimeAWSConfiguration struct {
	RoleARN                 string `json:"role_arn"`
	ExternalID              string `json:"external_id"`
	DynamoDBTable           string `json:"dynamodb_table"`
	LegacyDynamoDBLockTable string `json:"dynamodb_lock_table"`
}

type terraformStateRuntimeTargetScope struct {
	TargetScopeID      string   `json:"target_scope_id"`
	Provider           string   `json:"provider"`
	DeploymentMode     string   `json:"deployment_mode"`
	CredentialMode     string   `json:"credential_mode"`
	RoleARN            string   `json:"role_arn"`
	ExternalID         string   `json:"external_id"`
	AllowedRegions     []string `json:"allowed_regions"`
	AllowedBackends    []string `json:"allowed_backends"`
	RedactionPolicyRef string   `json:"redaction_policy_ref"`
}

type awsCredentialMode string

const (
	awsCredentialModeDefault               awsCredentialMode = "default"
	awsCredentialModeCentralAssumeRole     awsCredentialMode = "central_assume_role"
	awsCredentialModeLocalWorkloadIdentity awsCredentialMode = "local_workload_identity"
)

type awsCredentialConfig struct {
	Mode       awsCredentialMode
	RoleARN    string
	ExternalID string
}

type awsTargetScopeConfig struct {
	TargetScopeID      string
	Provider           string
	DeploymentMode     string
	Credentials        awsCredentialConfig
	AllowedRegions     []string
	AllowedBackends    []string
	RedactionPolicyRef string
}

func loadRuntimeConfig(getenv func(string) string) (runtimeConfig, error) {
	if getenv == nil {
		getenv = os.Getenv
	}

	instances, err := workflow.ParseDesiredCollectorInstancesJSON(getenv("ESHU_COLLECTOR_INSTANCES_JSON"))
	if err != nil {
		return runtimeConfig{}, fmt.Errorf("parse ESHU_COLLECTOR_INSTANCES_JSON: %w", err)
	}
	instance, err := selectTerraformStateInstance(instances, getenv("ESHU_TFSTATE_COLLECTOR_INSTANCE_ID"))
	if err != nil {
		return runtimeConfig{}, err
	}
	if err := validateTerraformStateInstance(instance); err != nil {
		return runtimeConfig{}, err
	}

	pollInterval, err := envDuration(getenv, "ESHU_TFSTATE_COLLECTOR_POLL_INTERVAL", defaultPollInterval)
	if err != nil {
		return runtimeConfig{}, err
	}
	claimLeaseTTL, err := envDuration(getenv, "ESHU_TFSTATE_COLLECTOR_CLAIM_LEASE_TTL", workflow.DefaultClaimLeaseTTL())
	if err != nil {
		return runtimeConfig{}, err
	}
	heartbeatInterval, err := envDurationWithAlias(
		getenv,
		"ESHU_TFSTATE_COLLECTOR_HEARTBEAT_INTERVAL",
		"ESHU_TFSTATE_COLLECTOR_HEARTBEAT",
		workflow.DefaultHeartbeatInterval(),
	)
	if err != nil {
		return runtimeConfig{}, err
	}
	if heartbeatInterval >= claimLeaseTTL {
		return runtimeConfig{}, fmt.Errorf(
			"terraform state collector heartbeat interval must be less than claim lease TTL (%s or %s)",
			"ESHU_TFSTATE_COLLECTOR_HEARTBEAT_INTERVAL",
			"ESHU_TFSTATE_COLLECTOR_HEARTBEAT",
		)
	}

	redactionKey, err := loadRedactionKey(getenv)
	if err != nil {
		return runtimeConfig{}, err
	}
	sourceMaxBytes, err := envInt64(getenv, "ESHU_TFSTATE_SOURCE_MAX_BYTES", defaultSourceMaxBytes)
	if err != nil {
		return runtimeConfig{}, err
	}
	if sourceMaxBytes < 0 {
		return runtimeConfig{}, fmt.Errorf("ESHU_TFSTATE_SOURCE_MAX_BYTES must not be negative")
	}
	awsCredentials, awsTargetScopes, err := parseAWSCredentialConfig(instance.Configuration)
	if err != nil {
		return runtimeConfig{}, err
	}
	awsDynamoDBLockTable, err := parseAWSDynamoDBLockTable(instance.Configuration)
	if err != nil {
		return runtimeConfig{}, err
	}

	return runtimeConfig{
		Instance:             instance,
		OwnerID:              ownerID(getenv),
		PollInterval:         pollInterval,
		ClaimLeaseTTL:        claimLeaseTTL,
		HeartbeatInterval:    heartbeatInterval,
		RedactionKey:         redactionKey,
		SourceMaxBytes:       sourceMaxBytes,
		AWSRoleARN:           awsCredentials.RoleARN,
		AWSCredentials:       awsCredentials,
		AWSTargetScopes:      awsTargetScopes,
		AWSDynamoDBLockTable: awsDynamoDBLockTable,
	}, nil
}

func selectTerraformStateInstance(
	instances []workflow.DesiredCollectorInstance,
	requestedInstanceID string,
) (workflow.DesiredCollectorInstance, error) {
	requestedInstanceID = strings.TrimSpace(requestedInstanceID)
	matches := make([]workflow.DesiredCollectorInstance, 0, 1)
	for _, instance := range instances {
		if instance.CollectorKind != scope.CollectorTerraformState {
			continue
		}
		if requestedInstanceID != "" && instance.InstanceID != requestedInstanceID {
			continue
		}
		if instance.Enabled && instance.ClaimsEnabled {
			matches = append(matches, instance)
		}
	}
	if requestedInstanceID != "" {
		if len(matches) == 1 {
			return matches[0], nil
		}
		return workflow.DesiredCollectorInstance{}, fmt.Errorf(
			"terraform_state collector instance %q must exist, be enabled, and enable claims",
			requestedInstanceID,
		)
	}
	if len(matches) != 1 {
		return workflow.DesiredCollectorInstance{}, fmt.Errorf(
			"ESHU_TFSTATE_COLLECTOR_INSTANCE_ID is required when %d enabled claim-capable terraform_state instances exist",
			len(matches),
		)
	}
	return matches[0], nil
}

func validateTerraformStateInstance(instance workflow.DesiredCollectorInstance) error {
	if instance.CollectorKind != scope.CollectorTerraformState {
		return fmt.Errorf("collector kind %q must be %q", instance.CollectorKind, scope.CollectorTerraformState)
	}
	if !instance.Enabled {
		return fmt.Errorf("terraform_state collector instance %q must be enabled", instance.InstanceID)
	}
	if !instance.ClaimsEnabled {
		return fmt.Errorf("terraform_state collector instance %q must enable claims", instance.InstanceID)
	}
	if err := instance.Validate(); err != nil {
		return fmt.Errorf("terraform_state collector instance %q: %w", instance.InstanceID, err)
	}
	return nil
}

func loadRedactionKey(getenv func(string) string) (redact.Key, error) {
	value := strings.TrimSpace(getenv("ESHU_TFSTATE_REDACTION_KEY"))
	if value == "" {
		return redact.Key{}, fmt.Errorf("ESHU_TFSTATE_REDACTION_KEY is required")
	}
	key, err := redact.NewKey([]byte(value))
	if err != nil {
		return redact.Key{}, fmt.Errorf("ESHU_TFSTATE_REDACTION_KEY: %w", err)
	}
	return key, nil
}

func parseAWSCredentialConfig(raw string) (awsCredentialConfig, []awsTargetScopeConfig, error) {
	var config terraformStateRuntimeConfiguration
	if err := json.Unmarshal([]byte(normalizeJSON(raw)), &config); err != nil {
		return awsCredentialConfig{}, nil, fmt.Errorf("terraform_state runtime configuration: %w", err)
	}
	targetScopes := make([]awsTargetScopeConfig, 0, len(config.TargetScopes))
	for _, scope := range config.TargetScopes {
		targetScopes = append(targetScopes, awsTargetScopeConfig{
			TargetScopeID:  strings.TrimSpace(scope.TargetScopeID),
			Provider:       strings.ToLower(strings.TrimSpace(scope.Provider)),
			DeploymentMode: strings.ToLower(strings.TrimSpace(scope.DeploymentMode)),
			Credentials: awsCredentialConfig{
				Mode:       awsCredentialMode(strings.ToLower(strings.TrimSpace(scope.CredentialMode))),
				RoleARN:    strings.TrimSpace(scope.RoleARN),
				ExternalID: strings.TrimSpace(scope.ExternalID),
			},
			AllowedRegions:     trimmedStrings(scope.AllowedRegions),
			AllowedBackends:    lowerTrimmedStrings(scope.AllowedBackends),
			RedactionPolicyRef: strings.TrimSpace(scope.RedactionPolicyRef),
		})
	}
	if len(targetScopes) == 1 {
		return targetScopes[0].Credentials, targetScopes, nil
	}
	if len(targetScopes) > 1 {
		credentials, err := sharedTargetScopeCredentials(targetScopes)
		if err != nil {
			return awsCredentialConfig{}, nil, err
		}
		return credentials, targetScopes, nil
	}
	return legacyAWSCredentialConfig(config.AWS), targetScopes, nil
}

func sharedTargetScopeCredentials(targetScopes []awsTargetScopeConfig) (awsCredentialConfig, error) {
	credentials := targetScopes[0].Credentials
	for _, targetScope := range targetScopes[1:] {
		if targetScope.Credentials != credentials {
			return awsCredentialConfig{}, fmt.Errorf(
				"terraform_state runtime target_scopes require identical AWS credentials until candidates carry target_scope_id",
			)
		}
	}
	return credentials, nil
}

func legacyAWSCredentialConfig(config terraformStateRuntimeAWSConfiguration) awsCredentialConfig {
	roleARN := strings.TrimSpace(config.RoleARN)
	if roleARN == "" {
		return awsCredentialConfig{Mode: awsCredentialModeDefault}
	}
	return awsCredentialConfig{
		Mode:       awsCredentialModeCentralAssumeRole,
		RoleARN:    roleARN,
		ExternalID: strings.TrimSpace(config.ExternalID),
	}
}

func parseAWSDynamoDBLockTable(raw string) (string, error) {
	var config terraformStateRuntimeConfiguration
	if err := json.Unmarshal([]byte(normalizeJSON(raw)), &config); err != nil {
		return "", fmt.Errorf("terraform_state runtime configuration: %w", err)
	}
	if table := strings.TrimSpace(config.AWS.DynamoDBTable); table != "" {
		return table, nil
	}
	return strings.TrimSpace(config.AWS.LegacyDynamoDBLockTable), nil
}

func ownerID(getenv func(string) string) string {
	if configured := strings.TrimSpace(getenv("ESHU_TFSTATE_COLLECTOR_OWNER_ID")); configured != "" {
		return configured
	}
	hostname, err := os.Hostname()
	if err != nil || strings.TrimSpace(hostname) == "" {
		hostname = "unknown-host"
	}
	return fmt.Sprintf("collector-terraform-state:%s:%d", strings.TrimSpace(hostname), os.Getpid())
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
	if parsed <= 0 {
		return 0, fmt.Errorf("%s must be positive", key)
	}
	return parsed, nil
}

func envDurationWithAlias(
	getenv func(string) string,
	key string,
	alias string,
	fallback time.Duration,
) (time.Duration, error) {
	if strings.TrimSpace(getenv(key)) != "" {
		return envDuration(getenv, key, fallback)
	}
	return envDuration(getenv, alias, fallback)
}

func envInt64(getenv func(string) string, key string, fallback int64) (int64, error) {
	value := strings.TrimSpace(getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return parsed, nil
}

func normalizeJSON(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "{}"
	}
	return trimmed
}

func trimmedStrings(values []string) []string {
	trimmed := make([]string, 0, len(values))
	for _, value := range values {
		trimmed = append(trimmed, strings.TrimSpace(value))
	}
	return trimmed
}

func lowerTrimmedStrings(values []string) []string {
	trimmed := make([]string, 0, len(values))
	for _, value := range values {
		trimmed = append(trimmed, strings.ToLower(strings.TrimSpace(value)))
	}
	return trimmed
}
