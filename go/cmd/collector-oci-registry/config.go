package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry"
	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry/dockerhub"
	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry/ecr"
	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry/ghcr"
	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry/jfrog"
	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry/ociruntime"
)

const (
	envCollectorInstanceID = "ESHU_OCI_REGISTRY_COLLECTOR_INSTANCE_ID"
	envPollInterval        = "ESHU_OCI_REGISTRY_POLL_INTERVAL"
	envTargetsJSON         = "ESHU_OCI_REGISTRY_TARGETS_JSON"
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

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
