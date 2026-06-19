package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/sbomruntime"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const (
	envCollectorInstanceID = "ESHU_SBOM_ATTESTATION_COLLECTOR_INSTANCE_ID"
	envPollInterval        = "ESHU_SBOM_ATTESTATION_POLL_INTERVAL"
	envClaimLeaseTTL       = "ESHU_SBOM_ATTESTATION_CLAIM_LEASE_TTL"
	envHeartbeatInterval   = "ESHU_SBOM_ATTESTATION_HEARTBEAT_INTERVAL"
	envOwnerID             = "ESHU_SBOM_ATTESTATION_COLLECTOR_OWNER_ID"
	envCollectorInstances  = "ESHU_COLLECTOR_INSTANCES_JSON"
)

type targetJSON struct {
	ScopeID            string `json:"scope_id"`
	SourceType         string `json:"source_type"`
	ArtifactKind       string `json:"artifact_kind"`
	DocumentFormat     string `json:"document_format"`
	Provider           string `json:"provider"`
	Registry           string `json:"registry"`
	RegistryHost       string `json:"registry_host"`
	Region             string `json:"region"`
	AWSProfile         string `json:"aws_profile"`
	Repository         string `json:"repository"`
	DocumentURL        string `json:"document_url"`
	SourceURI          string `json:"source_uri"`
	SourceRecordID     string `json:"source_record_id"`
	SubjectDigest      string `json:"subject_digest"`
	ReferrerDigest     string `json:"referrer_digest"`
	VerificationResult string `json:"verification_result"`
	VerificationPolicy string `json:"verification_policy"`
	UsernameEnv        string `json:"username_env"`
	PasswordEnv        string `json:"password_env"`
	BearerTokenEnv     string `json:"bearer_token_env"`
	MaxBytes           int64  `json:"max_bytes"`
}

type claimedRuntimeConfig struct {
	Instance          workflow.DesiredCollectorInstance
	OwnerID           string
	PollInterval      time.Duration
	ClaimLeaseTTL     time.Duration
	HeartbeatInterval time.Duration
	Source            sbomruntime.SourceConfig
}

type sbomAttestationRuntimeConfiguration struct {
	Targets []targetJSON `json:"targets"`
}

func loadClaimedRuntimeConfig(getenv func(string) string) (claimedRuntimeConfig, error) {
	instances, err := workflow.ParseDesiredCollectorInstancesJSON(getenv(envCollectorInstances))
	if err != nil {
		return claimedRuntimeConfig{}, fmt.Errorf("parse %s: %w", envCollectorInstances, err)
	}
	instance, err := selectSBOMAttestationInstance(instances, getenv(envCollectorInstanceID))
	if err != nil {
		return claimedRuntimeConfig{}, err
	}
	if err := validateSBOMAttestationInstance(instance); err != nil {
		return claimedRuntimeConfig{}, err
	}
	sourceConfig, err := parseSBOMAttestationRuntimeConfiguration(instance, getenv)
	if err != nil {
		return claimedRuntimeConfig{}, err
	}
	pollInterval, err := envDuration(getenv, envPollInterval, time.Second)
	if err != nil {
		return claimedRuntimeConfig{}, err
	}
	claimLeaseTTL, err := envDuration(getenv, envClaimLeaseTTL, workflow.DefaultClaimLeaseTTL())
	if err != nil {
		return claimedRuntimeConfig{}, err
	}
	heartbeatInterval, err := envDuration(getenv, envHeartbeatInterval, workflow.DefaultHeartbeatInterval())
	if err != nil {
		return claimedRuntimeConfig{}, err
	}
	if heartbeatInterval >= claimLeaseTTL {
		return claimedRuntimeConfig{}, fmt.Errorf("SBOM attestation collector heartbeat interval must be less than claim lease TTL")
	}
	return claimedRuntimeConfig{
		Instance:          instance,
		OwnerID:           ownerID(getenv),
		PollInterval:      pollInterval,
		ClaimLeaseTTL:     claimLeaseTTL,
		HeartbeatInterval: heartbeatInterval,
		Source:            sourceConfig,
	}, nil
}

func selectSBOMAttestationInstance(
	instances []workflow.DesiredCollectorInstance,
	requestedInstanceID string,
) (workflow.DesiredCollectorInstance, error) {
	requestedInstanceID = strings.TrimSpace(requestedInstanceID)
	var matches []workflow.DesiredCollectorInstance
	for _, instance := range instances {
		if instance.CollectorKind != scope.CollectorSBOMAttestation {
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
			return workflow.DesiredCollectorInstance{}, fmt.Errorf("SBOM attestation collector instance %q not found", requestedInstanceID)
		}
		return workflow.DesiredCollectorInstance{}, fmt.Errorf("no SBOM attestation collector instance configured")
	case 1:
		return matches[0], nil
	default:
		return workflow.DesiredCollectorInstance{}, fmt.Errorf("multiple SBOM attestation collector instances configured; set %s", envCollectorInstanceID)
	}
}

func validateSBOMAttestationInstance(instance workflow.DesiredCollectorInstance) error {
	if err := instance.Validate(); err != nil {
		return fmt.Errorf("SBOM attestation collector instance: %w", err)
	}
	if instance.CollectorKind != scope.CollectorSBOMAttestation {
		return fmt.Errorf("SBOM attestation collector requires collector_kind %q", scope.CollectorSBOMAttestation)
	}
	if !instance.Enabled {
		return fmt.Errorf("SBOM attestation collector requires enabled collector instance")
	}
	if !instance.ClaimsEnabled {
		return fmt.Errorf("SBOM attestation collector requires claim-enabled collector instance")
	}
	return nil
}

func parseSBOMAttestationRuntimeConfiguration(
	instance workflow.DesiredCollectorInstance,
	getenv func(string) string,
) (sbomruntime.SourceConfig, error) {
	var decoded sbomAttestationRuntimeConfiguration
	if err := json.Unmarshal([]byte(instance.Configuration), &decoded); err != nil {
		return sbomruntime.SourceConfig{}, fmt.Errorf("decode SBOM attestation collector configuration: %w", err)
	}
	targets := make([]sbomruntime.TargetConfig, 0, len(decoded.Targets))
	for i, target := range decoded.Targets {
		mapped := mapTarget(target, getenv)
		if strings.TrimSpace(mapped.ScopeID) == "" {
			return sbomruntime.SourceConfig{}, fmt.Errorf("targets[%d]: scope_id is required", i)
		}
		targets = append(targets, mapped)
	}
	return sbomruntime.SourceConfig{
		CollectorInstanceID: instance.InstanceID,
		Targets:             targets,
		Provider:            sbomruntime.HTTPProvider{},
	}, nil
}

func mapTarget(target targetJSON, getenv func(string) string) sbomruntime.TargetConfig {
	return sbomruntime.TargetConfig{
		ScopeID:            strings.TrimSpace(target.ScopeID),
		SourceType:         sbomruntime.SourceType(strings.TrimSpace(target.SourceType)),
		ArtifactKind:       sbomruntime.ArtifactKind(strings.TrimSpace(target.ArtifactKind)),
		DocumentFormat:     sbomruntime.DocumentFormat(strings.TrimSpace(target.DocumentFormat)),
		Provider:           strings.TrimSpace(target.Provider),
		Registry:           strings.TrimRight(strings.TrimSpace(target.Registry), "/"),
		RegistryHost:       strings.TrimRight(strings.TrimSpace(target.RegistryHost), "/"),
		Region:             strings.TrimSpace(target.Region),
		AWSProfile:         strings.TrimSpace(target.AWSProfile),
		Repository:         strings.Trim(strings.TrimSpace(target.Repository), "/"),
		DocumentURL:        strings.TrimSpace(target.DocumentURL),
		SourceURI:          strings.TrimSpace(firstNonBlank(target.SourceURI, target.DocumentURL, target.Registry)),
		SourceRecordID:     strings.TrimSpace(target.SourceRecordID),
		SubjectDigest:      strings.TrimSpace(target.SubjectDigest),
		ReferrerDigest:     strings.TrimSpace(target.ReferrerDigest),
		VerificationResult: strings.TrimSpace(target.VerificationResult),
		VerificationPolicy: strings.TrimSpace(target.VerificationPolicy),
		Username:           getenv(strings.TrimSpace(target.UsernameEnv)),
		Password:           getenv(strings.TrimSpace(target.PasswordEnv)),
		BearerToken:        getenv(strings.TrimSpace(target.BearerTokenEnv)),
		MaxBytes:           target.MaxBytes,
	}
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
	for _, key := range []string{envOwnerID, "HOSTNAME"} {
		if value := strings.TrimSpace(getenv(key)); value != "" {
			return value
		}
	}
	return "collector-sbom-attestation"
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
