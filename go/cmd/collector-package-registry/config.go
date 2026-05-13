package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/packageregistry"
	"github.com/eshu-hq/eshu/go/internal/collector/packageregistry/packageruntime"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const (
	envCollectorInstanceID = "ESHU_PACKAGE_REGISTRY_COLLECTOR_INSTANCE_ID"
	envPollInterval        = "ESHU_PACKAGE_REGISTRY_POLL_INTERVAL"
	envClaimLeaseTTL       = "ESHU_PACKAGE_REGISTRY_CLAIM_LEASE_TTL"
	envHeartbeatInterval   = "ESHU_PACKAGE_REGISTRY_HEARTBEAT_INTERVAL"
	envOwnerID             = "ESHU_PACKAGE_REGISTRY_COLLECTOR_OWNER_ID"
	envCollectorInstances  = "ESHU_COLLECTOR_INSTANCES_JSON"
)

type targetJSON struct {
	Provider       string   `json:"provider"`
	Ecosystem      string   `json:"ecosystem"`
	Registry       string   `json:"registry"`
	ScopeID        string   `json:"scope_id"`
	Namespace      string   `json:"namespace"`
	Packages       []string `json:"packages"`
	PackageLimit   int      `json:"package_limit"`
	VersionLimit   int      `json:"version_limit"`
	Visibility     string   `json:"visibility"`
	SourceURI      string   `json:"source_uri"`
	MetadataURL    string   `json:"metadata_url"`
	UsernameEnv    string   `json:"username_env"`
	PasswordEnv    string   `json:"password_env"`
	BearerTokenEnv string   `json:"bearer_token_env"`
}

type claimedRuntimeConfig struct {
	Instance          workflow.DesiredCollectorInstance
	OwnerID           string
	PollInterval      time.Duration
	ClaimLeaseTTL     time.Duration
	HeartbeatInterval time.Duration
	Source            packageruntime.SourceConfig
}

type packageRegistryRuntimeConfiguration struct {
	Targets []targetJSON `json:"targets"`
}

func loadClaimedRuntimeConfig(getenv func(string) string) (claimedRuntimeConfig, error) {
	instances, err := workflow.ParseDesiredCollectorInstancesJSON(getenv(envCollectorInstances))
	if err != nil {
		return claimedRuntimeConfig{}, fmt.Errorf("parse %s: %w", envCollectorInstances, err)
	}
	instance, err := selectPackageRegistryInstance(instances, getenv(envCollectorInstanceID))
	if err != nil {
		return claimedRuntimeConfig{}, err
	}
	if err := validatePackageRegistryInstance(instance); err != nil {
		return claimedRuntimeConfig{}, err
	}
	sourceConfig, err := parsePackageRegistryRuntimeConfiguration(instance, getenv)
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
		return claimedRuntimeConfig{}, fmt.Errorf("package registry collector heartbeat interval must be less than claim lease TTL")
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

func selectPackageRegistryInstance(
	instances []workflow.DesiredCollectorInstance,
	requestedInstanceID string,
) (workflow.DesiredCollectorInstance, error) {
	requestedInstanceID = strings.TrimSpace(requestedInstanceID)
	var matches []workflow.DesiredCollectorInstance
	for _, instance := range instances {
		if instance.CollectorKind != scope.CollectorPackageRegistry {
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
			return workflow.DesiredCollectorInstance{}, fmt.Errorf("package registry collector instance %q not found", requestedInstanceID)
		}
		return workflow.DesiredCollectorInstance{}, fmt.Errorf("no package registry collector instance configured")
	case 1:
		return matches[0], nil
	default:
		return workflow.DesiredCollectorInstance{}, fmt.Errorf("multiple package registry collector instances configured; set %s", envCollectorInstanceID)
	}
}

func validatePackageRegistryInstance(instance workflow.DesiredCollectorInstance) error {
	if err := instance.Validate(); err != nil {
		return fmt.Errorf("package registry collector instance: %w", err)
	}
	if instance.CollectorKind != scope.CollectorPackageRegistry {
		return fmt.Errorf("package registry collector requires collector_kind %q", scope.CollectorPackageRegistry)
	}
	if !instance.Enabled {
		return fmt.Errorf("package registry collector requires enabled collector instance")
	}
	if !instance.ClaimsEnabled {
		return fmt.Errorf("package registry collector requires claim-enabled collector instance")
	}
	return nil
}

func parsePackageRegistryRuntimeConfiguration(
	instance workflow.DesiredCollectorInstance,
	getenv func(string) string,
) (packageruntime.SourceConfig, error) {
	var decoded packageRegistryRuntimeConfiguration
	if err := json.Unmarshal([]byte(instance.Configuration), &decoded); err != nil {
		return packageruntime.SourceConfig{}, fmt.Errorf("decode package registry collector configuration: %w", err)
	}
	targets := make([]packageruntime.TargetConfig, 0, len(decoded.Targets))
	for i, target := range decoded.Targets {
		mapped, err := mapTarget(target, getenv)
		if err != nil {
			return packageruntime.SourceConfig{}, fmt.Errorf("targets[%d]: %w", i, err)
		}
		targets = append(targets, mapped)
	}
	return packageruntime.SourceConfig{
		CollectorInstanceID: instance.InstanceID,
		Targets:             targets,
		Provider:            packageruntime.HTTPMetadataProvider{},
	}, nil
}

func mapTarget(target targetJSON, getenv func(string) string) (packageruntime.TargetConfig, error) {
	visibility := packageregistry.Visibility(strings.TrimSpace(target.Visibility))
	if visibility == "" {
		visibility = packageregistry.VisibilityUnknown
	}
	return packageruntime.TargetConfig{
		Base: packageregistry.TargetConfig{
			Provider:     strings.TrimSpace(target.Provider),
			Ecosystem:    packageregistry.Ecosystem(strings.TrimSpace(target.Ecosystem)),
			Registry:     strings.TrimRight(strings.TrimSpace(target.Registry), "/"),
			ScopeID:      strings.TrimSpace(target.ScopeID),
			Namespace:    strings.TrimSpace(target.Namespace),
			Packages:     target.Packages,
			PackageLimit: target.PackageLimit,
			VersionLimit: target.VersionLimit,
			Visibility:   visibility,
			SourceURI:    strings.TrimSpace(firstNonBlank(target.SourceURI, target.MetadataURL)),
		},
		MetadataURL: strings.TrimRight(strings.TrimSpace(target.MetadataURL), "/"),
		Username:    getenv(strings.TrimSpace(target.UsernameEnv)),
		Password:    getenv(strings.TrimSpace(target.PasswordEnv)),
		BearerToken: getenv(strings.TrimSpace(target.BearerTokenEnv)),
	}, nil
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
	return "collector-package-registry"
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
