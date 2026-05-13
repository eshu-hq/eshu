package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry"
	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry/acr"
	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry/dockerhub"
	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry/ecr"
	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry/gar"
	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry/ghcr"
	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry/harbor"
	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry/jfrog"
	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry/ociruntime"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const (
	envCollectorInstanceID = "ESHU_OCI_REGISTRY_COLLECTOR_INSTANCE_ID"
	envPollInterval        = "ESHU_OCI_REGISTRY_POLL_INTERVAL"
	envTargetsJSON         = "ESHU_OCI_REGISTRY_TARGETS_JSON"
	envClaimLeaseTTL       = "ESHU_OCI_REGISTRY_CLAIM_LEASE_TTL"
	envHeartbeatInterval   = "ESHU_OCI_REGISTRY_HEARTBEAT_INTERVAL"
	envOwnerID             = "ESHU_OCI_REGISTRY_COLLECTOR_OWNER_ID"
	envCollectorInstances  = "ESHU_COLLECTOR_INSTANCES_JSON"
)

type targetJSON struct {
	Provider       string   `json:"provider"`
	Registry       string   `json:"registry"`
	BaseURL        string   `json:"base_url"`
	RepositoryKey  string   `json:"repository_key"`
	Repository     string   `json:"repository"`
	Region         string   `json:"region"`
	RegistryID     string   `json:"registry_id"`
	RegistryHost   string   `json:"registry_host"`
	References     []string `json:"references"`
	TagLimit       int      `json:"tag_limit"`
	Visibility     string   `json:"visibility"`
	AuthMode       string   `json:"auth_mode"`
	SourceURI      string   `json:"source_uri"`
	UsernameEnv    string   `json:"username_env"`
	PasswordEnv    string   `json:"password_env"`
	BearerTokenEnv string   `json:"bearer_token_env"`
	AWSProfile     string   `json:"aws_profile"`
	FencingToken   int64    `json:"fencing_token"`
}

type claimedRuntimeConfig struct {
	Instance          workflow.DesiredCollectorInstance
	OwnerID           string
	PollInterval      time.Duration
	ClaimLeaseTTL     time.Duration
	HeartbeatInterval time.Duration
	OCI               ociruntime.Config
}

type ociRuntimeConfiguration struct {
	Targets []targetJSON `json:"targets"`
}

func loadRuntimeConfig(getenv func(string) string) (ociruntime.Config, error) {
	collectorID := strings.TrimSpace(getenv(envCollectorInstanceID))
	if collectorID == "" {
		return ociruntime.Config{}, fmt.Errorf("%s is required", envCollectorInstanceID)
	}
	rawTargets := strings.TrimSpace(getenv(envTargetsJSON))
	if rawTargets == "" {
		return ociruntime.Config{}, fmt.Errorf("%s is required", envTargetsJSON)
	}
	var decoded []targetJSON
	if err := json.Unmarshal([]byte(rawTargets), &decoded); err != nil {
		return ociruntime.Config{}, fmt.Errorf("decode %s: %w", envTargetsJSON, err)
	}
	targets := make([]ociruntime.TargetConfig, 0, len(decoded))
	for i, target := range decoded {
		mapped, err := mapTarget(target, getenv)
		if err != nil {
			return ociruntime.Config{}, fmt.Errorf("target %d: %w", i, err)
		}
		targets = append(targets, mapped)
	}
	pollInterval, err := parsePollInterval(getenv(envPollInterval))
	if err != nil {
		return ociruntime.Config{}, err
	}
	return ociruntime.Config{
		CollectorInstanceID: collectorID,
		PollInterval:        pollInterval,
		Targets:             targets,
	}, nil
}

func loadClaimedRuntimeConfig(getenv func(string) string) (claimedRuntimeConfig, error) {
	instances, err := workflow.ParseDesiredCollectorInstancesJSON(getenv(envCollectorInstances))
	if err != nil {
		return claimedRuntimeConfig{}, fmt.Errorf("parse %s: %w", envCollectorInstances, err)
	}
	instance, err := selectOCIRegistryInstance(instances, getenv(envCollectorInstanceID))
	if err != nil {
		return claimedRuntimeConfig{}, err
	}
	if err := validateOCIRegistryInstance(instance); err != nil {
		return claimedRuntimeConfig{}, err
	}
	ociConfig, err := parseOCIRegistryRuntimeConfiguration(instance, getenv)
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
		return claimedRuntimeConfig{}, fmt.Errorf("OCI registry collector heartbeat interval must be less than claim lease TTL")
	}
	return claimedRuntimeConfig{
		Instance:          instance,
		OwnerID:           ownerID(getenv),
		PollInterval:      pollInterval,
		ClaimLeaseTTL:     claimLeaseTTL,
		HeartbeatInterval: heartbeatInterval,
		OCI:               ociConfig,
	}, nil
}

func selectOCIRegistryInstance(
	instances []workflow.DesiredCollectorInstance,
	requestedInstanceID string,
) (workflow.DesiredCollectorInstance, error) {
	requestedInstanceID = strings.TrimSpace(requestedInstanceID)
	var matches []workflow.DesiredCollectorInstance
	for _, instance := range instances {
		if instance.CollectorKind != scope.CollectorOCIRegistry {
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
			return workflow.DesiredCollectorInstance{}, fmt.Errorf("OCI registry collector instance %q not found", requestedInstanceID)
		}
		return workflow.DesiredCollectorInstance{}, fmt.Errorf("no OCI registry collector instance configured")
	case 1:
		return matches[0], nil
	default:
		return workflow.DesiredCollectorInstance{}, fmt.Errorf("multiple OCI registry collector instances configured; set %s", envCollectorInstanceID)
	}
}

func validateOCIRegistryInstance(instance workflow.DesiredCollectorInstance) error {
	if err := instance.Validate(); err != nil {
		return fmt.Errorf("OCI registry collector instance: %w", err)
	}
	if instance.CollectorKind != scope.CollectorOCIRegistry {
		return fmt.Errorf("OCI registry collector requires collector_kind %q", scope.CollectorOCIRegistry)
	}
	if !instance.Enabled {
		return fmt.Errorf("OCI registry collector requires enabled collector instance")
	}
	if !instance.ClaimsEnabled {
		return fmt.Errorf("OCI registry collector requires claim-enabled collector instance")
	}
	return nil
}

func parseOCIRegistryRuntimeConfiguration(
	instance workflow.DesiredCollectorInstance,
	getenv func(string) string,
) (ociruntime.Config, error) {
	var decoded ociRuntimeConfiguration
	if err := json.Unmarshal([]byte(instance.Configuration), &decoded); err != nil {
		return ociruntime.Config{}, fmt.Errorf("decode OCI registry collector configuration: %w", err)
	}
	if len(decoded.Targets) == 0 {
		return ociruntime.Config{}, fmt.Errorf("OCI registry collector configuration requires targets")
	}
	targets := make([]ociruntime.TargetConfig, 0, len(decoded.Targets))
	for i, target := range decoded.Targets {
		mapped, err := mapTarget(target, getenv)
		if err != nil {
			return ociruntime.Config{}, fmt.Errorf("targets[%d]: %w", i, err)
		}
		targets = append(targets, mapped)
	}
	return ociruntime.Config{
		CollectorInstanceID: instance.InstanceID,
		Targets:             targets,
	}, nil
}

func mapTarget(target targetJSON, getenv func(string) string) (ociruntime.TargetConfig, error) {
	provider := ociregistry.Provider(strings.TrimSpace(target.Provider))
	repository := strings.TrimSpace(target.Repository)
	registry := strings.TrimSpace(firstNonBlank(target.Registry, target.RegistryHost))
	switch provider {
	case ociregistry.ProviderDockerHub:
		name, err := dockerhub.RepositoryName(repository)
		if err != nil {
			return ociruntime.TargetConfig{}, err
		}
		repository = name
		if registry == "" {
			registry = dockerhub.RegistryHost
		}
	case ociregistry.ProviderGHCR:
		name, err := ghcr.RepositoryName(repository)
		if err != nil {
			return ociruntime.TargetConfig{}, err
		}
		repository = name
		if registry == "" {
			registry = ghcr.RegistryHost
		}
	case ociregistry.ProviderJFrog:
		identity, err := jfrog.RepositoryIdentity(target.BaseURL, target.RepositoryKey, repository)
		if err != nil {
			return ociruntime.TargetConfig{}, err
		}
		registry = identity.Registry
		repository = identity.Repository
	case ociregistry.ProviderECR:
		if registry == "" {
			host, err := ecr.PrivateRegistryHost(target.RegistryID, target.Region)
			if err != nil {
				return ociruntime.TargetConfig{}, err
			}
			registry = host
		}
	case ociregistry.ProviderHarbor:
		identity, err := harbor.RepositoryIdentity(target.BaseURL, repository)
		if err != nil {
			return ociruntime.TargetConfig{}, err
		}
		registry = identity.Registry
		repository = identity.Repository
	case ociregistry.ProviderGoogleArtifactRegistry:
		host := firstNonBlank(target.RegistryHost, registry)
		identity, err := gar.RepositoryIdentity(host, repository)
		if err != nil {
			return ociruntime.TargetConfig{}, err
		}
		registry = identity.Registry
		repository = identity.Repository
	case ociregistry.ProviderAzureContainerRegistry:
		host := firstNonBlank(target.RegistryHost, registry)
		identity, err := acr.RepositoryIdentity(host, repository)
		if err != nil {
			return ociruntime.TargetConfig{}, err
		}
		registry = identity.Registry
		repository = identity.Repository
	default:
		return ociruntime.TargetConfig{}, fmt.Errorf("unsupported provider %q", target.Provider)
	}
	return ociruntime.TargetConfig{
		Provider:      provider,
		Registry:      registry,
		BaseURL:       strings.TrimSpace(target.BaseURL),
		RepositoryKey: strings.TrimSpace(target.RepositoryKey),
		Repository:    repository,
		Region:        strings.TrimSpace(target.Region),
		RegistryID:    strings.TrimSpace(target.RegistryID),
		RegistryHost:  registry,
		References:    target.References,
		TagLimit:      target.TagLimit,
		Visibility:    ociregistry.Visibility(strings.TrimSpace(target.Visibility)),
		AuthMode:      ociregistry.AuthMode(strings.TrimSpace(target.AuthMode)),
		SourceURI:     strings.TrimSpace(target.SourceURI),
		Username:      getenv(strings.TrimSpace(target.UsernameEnv)),
		Password:      getenv(strings.TrimSpace(target.PasswordEnv)),
		BearerToken:   getenv(strings.TrimSpace(target.BearerTokenEnv)),
		AWSProfile:    strings.TrimSpace(target.AWSProfile),
		FencingToken:  target.FencingToken,
	}, nil
}

func parsePollInterval(raw string) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", envPollInterval, err)
	}
	if value <= 0 {
		return 0, fmt.Errorf("%s must be positive", envPollInterval)
	}
	return value, nil
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
	return "collector-oci-registry"
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
