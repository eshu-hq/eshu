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
	if !isAWSAccountID(accountID) {
		return awsruntime.TargetScope{}, fmt.Errorf("account_id must be a 12 digit AWS account ID")
	}
	allowedRegions, err := validateAllowedRegions(target.AllowedRegions)
	if err != nil {
		return awsruntime.TargetScope{}, err
	}
	allowedServices, err := validateAllowedServices(target.AllowedServices)
	if err != nil {
		return awsruntime.TargetScope{}, err
	}
	if target.MaxConcurrentClaims < 0 {
		return awsruntime.TargetScope{}, fmt.Errorf("max_concurrent_claims must be zero or positive")
	}
	credentials, err := mapCredentialConfig(target.Credentials, accountID)
	if err != nil {
		return awsruntime.TargetScope{}, err
	}
	return awsruntime.TargetScope{
		AccountID:           accountID,
		AllowedRegions:      allowedRegions,
		AllowedServices:     allowedServices,
		MaxConcurrentClaims: target.MaxConcurrentClaims,
		Credentials:         credentials,
	}, nil
}

func mapCredentialConfig(
	config awsCredentialConfiguration,
	accountID string,
) (awsruntime.CredentialConfig, error) {
	if strings.TrimSpace(config.AccessKeyID) != "" ||
		strings.TrimSpace(config.SecretAccessKey) != "" ||
		strings.TrimSpace(config.SessionToken) != "" {
		return awsruntime.CredentialConfig{}, fmt.Errorf("static AWS credential fields are not allowed")
	}
	mode := awsruntime.CredentialMode(strings.TrimSpace(config.Mode))
	roleARN := strings.TrimSpace(config.RoleARN)
	externalID := strings.TrimSpace(config.ExternalID)
	switch mode {
	case awsruntime.CredentialModeCentralAssumeRole:
		if roleARN == "" {
			return awsruntime.CredentialConfig{}, fmt.Errorf("central_assume_role credentials require role_arn")
		}
		roleAccountID, err := accountIDFromIAMRoleARN(roleARN)
		if err != nil {
			return awsruntime.CredentialConfig{}, err
		}
		if roleAccountID != accountID {
			return awsruntime.CredentialConfig{}, fmt.Errorf("central_assume_role role_arn account %q must match account_id %q", roleAccountID, accountID)
		}
		if externalID == "" {
			return awsruntime.CredentialConfig{}, fmt.Errorf("central_assume_role credentials require external_id")
		}
	case awsruntime.CredentialModeLocalWorkloadIdentity:
		if roleARN != "" || externalID != "" {
			return awsruntime.CredentialConfig{}, fmt.Errorf("local_workload_identity credentials must not set role_arn or external_id")
		}
	default:
		return awsruntime.CredentialConfig{}, fmt.Errorf("unsupported AWS credential mode %q", config.Mode)
	}
	return awsruntime.CredentialConfig{
		Mode:       mode,
		RoleARN:    roleARN,
		ExternalID: externalID,
	}, nil
}

func validateAllowedRegions(values []string) ([]string, error) {
	if len(values) == 0 {
		return nil, fmt.Errorf("allowed_regions is required")
	}
	regions := make([]string, 0, len(values))
	for _, value := range values {
		region := strings.TrimSpace(value)
		switch region {
		case "":
			return nil, fmt.Errorf("allowed_regions must not contain empty entries")
		case "*":
			return nil, fmt.Errorf("allowed_regions must not contain wildcard entries")
		default:
			regions = append(regions, region)
		}
	}
	return regions, nil
}

// validateAllowedServices rejects broad or unknown scanner names before any
// workflow claim can acquire AWS credentials.
func validateAllowedServices(values []string) ([]string, error) {
	if len(values) == 0 {
		return nil, fmt.Errorf("allowed_services is required")
	}
	services := make([]string, 0, len(values))
	for _, value := range values {
		service := strings.TrimSpace(value)
		switch {
		case service == "":
			return nil, fmt.Errorf("allowed_services must not contain empty entries")
		case service == "*":
			return nil, fmt.Errorf("allowed_services must not contain wildcard entries")
		case !isSupportedAWSService(service):
			return nil, fmt.Errorf("unsupported allowed service %q", service)
		default:
			services = append(services, service)
		}
	}
	return services, nil
}

// isSupportedAWSService mirrors the production scanner registry at the config
// boundary so unsafe target scopes fail during startup instead of at claim time.
func isSupportedAWSService(service string) bool {
	switch service {
	case awscloud.ServiceIAM,
		awscloud.ServiceECR,
		awscloud.ServiceECS,
		awscloud.ServiceEC2,
		awscloud.ServiceELBv2,
		awscloud.ServiceRoute53,
		awscloud.ServiceLambda,
		awscloud.ServiceEKS,
		awscloud.ServiceSQS,
		awscloud.ServiceSNS,
		awscloud.ServiceEventBridge,
		awscloud.ServiceS3,
		awscloud.ServiceRDS,
		awscloud.ServiceDynamoDB,
		awscloud.ServiceCloudWatchLogs,
		awscloud.ServiceCloudFront,
		awscloud.ServiceAPIGateway,
		awscloud.ServiceSecretsManager,
		awscloud.ServiceSSM:
		return true
	default:
		return false
	}
}

// accountIDFromIAMRoleARN extracts the account segment from a role ARN and
// rejects non-role ARNs so target scopes cannot point at a different account.
func accountIDFromIAMRoleARN(roleARN string) (string, error) {
	parts := strings.SplitN(strings.TrimSpace(roleARN), ":", 6)
	if len(parts) != 6 ||
		parts[0] != "arn" ||
		strings.TrimSpace(parts[1]) == "" ||
		parts[2] != "iam" ||
		parts[3] != "" ||
		!isAWSAccountID(parts[4]) ||
		!strings.HasPrefix(parts[5], "role/") ||
		strings.TrimSpace(strings.TrimPrefix(parts[5], "role/")) == "" {
		return "", fmt.Errorf("central_assume_role credentials require role_arn to be an IAM role ARN")
	}
	return parts[4], nil
}

func isAWSAccountID(value string) bool {
	if len(value) != 12 {
		return false
	}
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
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
