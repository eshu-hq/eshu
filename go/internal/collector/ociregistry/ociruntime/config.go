package ociruntime

import (
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry"
)

const (
	defaultTagLimit     = 20
	maxTagLimit         = 100
	defaultPollInterval = 5 * time.Minute
)

// Config describes one OCI registry collector runtime.
type Config struct {
	CollectorInstanceID string
	PollInterval        time.Duration
	Targets             []TargetConfig
}

// TargetConfig describes one bounded OCI registry repository scan target.
type TargetConfig struct {
	Provider      ociregistry.Provider
	Registry      string
	BaseURL       string
	RepositoryKey string
	Repository    string
	Region        string
	RegistryID    string
	RegistryHost  string
	References    []string
	TagLimit      int
	Visibility    ociregistry.Visibility
	AuthMode      ociregistry.AuthMode
	SourceURI     string
	Username      string
	Password      string
	BearerToken   string
	AWSProfile    string
	FencingToken  int64
}

func (c Config) validated() (Config, error) {
	collectorID := strings.TrimSpace(c.CollectorInstanceID)
	if collectorID == "" {
		return Config{}, fmt.Errorf("collector instance ID is required")
	}
	pollInterval := c.PollInterval
	if pollInterval == 0 {
		pollInterval = defaultPollInterval
	}
	if pollInterval < 0 {
		return Config{}, fmt.Errorf("poll interval must not be negative")
	}
	if len(c.Targets) == 0 {
		return Config{}, fmt.Errorf("at least one OCI registry target is required")
	}
	targets := make([]TargetConfig, 0, len(c.Targets))
	for i, target := range c.Targets {
		validated, err := target.validated()
		if err != nil {
			return Config{}, fmt.Errorf("target %d: %w", i, err)
		}
		targets = append(targets, validated)
	}
	return Config{CollectorInstanceID: collectorID, PollInterval: pollInterval, Targets: targets}, nil
}

func (t TargetConfig) validated() (TargetConfig, error) {
	provider := ociregistry.Provider(strings.TrimSpace(string(t.Provider)))
	if provider == "" {
		return TargetConfig{}, fmt.Errorf("provider is required")
	}
	registry := strings.TrimSpace(t.Registry)
	if registry == "" {
		return TargetConfig{}, fmt.Errorf("registry is required")
	}
	repository := strings.Trim(strings.TrimSpace(t.Repository), "/")
	if repository == "" {
		return TargetConfig{}, fmt.Errorf("repository is required")
	}
	tagLimit := t.TagLimit
	if tagLimit == 0 {
		tagLimit = defaultTagLimit
	}
	if tagLimit < 0 || tagLimit > maxTagLimit {
		return TargetConfig{}, fmt.Errorf("tag_limit must be between 0 and %d", maxTagLimit)
	}
	references := make([]string, 0, len(t.References))
	for _, ref := range t.References {
		ref = strings.TrimSpace(ref)
		if ref != "" {
			references = append(references, ref)
		}
	}
	return TargetConfig{
		Provider:      provider,
		Registry:      registry,
		BaseURL:       strings.TrimSpace(t.BaseURL),
		RepositoryKey: strings.TrimSpace(t.RepositoryKey),
		Repository:    repository,
		Region:        strings.TrimSpace(t.Region),
		RegistryID:    strings.TrimSpace(t.RegistryID),
		RegistryHost:  strings.TrimSpace(t.RegistryHost),
		References:    references,
		TagLimit:      tagLimit,
		Visibility:    t.Visibility,
		AuthMode:      t.AuthMode,
		SourceURI:     strings.TrimSpace(t.SourceURI),
		Username:      strings.TrimSpace(t.Username),
		Password:      t.Password,
		BearerToken:   strings.TrimSpace(t.BearerToken),
		AWSProfile:    strings.TrimSpace(t.AWSProfile),
		FencingToken:  t.FencingToken,
	}, nil
}

func (t TargetConfig) identity() ociregistry.RepositoryIdentity {
	return ociregistry.RepositoryIdentity{
		Provider:   t.Provider,
		Registry:   t.Registry,
		Repository: t.Repository,
	}
}
