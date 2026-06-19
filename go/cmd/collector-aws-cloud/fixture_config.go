package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/awsruntime"
)

// defaultFixturePollInterval is the source poll cadence used when a fixture
// config supplies no poll_interval. Offline replay re-emits the same facts each
// poll, so the cadence only governs how often the idempotent batch repeats.
const defaultFixturePollInterval = 30 * time.Minute

// fixtureFileConfig is the declarative on-disk configuration for the AWS cloud
// collector binary's fixture mode. It is intentionally offline: scopes carry
// their resources and relationships inline and reference no AWS credentials.
type fixtureFileConfig struct {
	// CollectorInstanceID is the configured runtime instance id. Required.
	CollectorInstanceID string `json:"collector_instance_id"`
	// PollInterval is an optional Go duration string (for example "30m").
	PollInterval string `json:"poll_interval"`
	// Scopes declares the bounded AWS scopes to replay.
	Scopes []fixtureFileScope `json:"scopes"`
}

// fixtureFileScope is one declarative AWS scope plus its offline resources and
// relationships.
type fixtureFileScope struct {
	// AccountID is the 12-digit AWS account id. Required.
	AccountID string `json:"account_id"`
	// Region is the AWS region. Required.
	Region string `json:"region"`
	// ServiceKind is the bounded AWS service family (for example s3). Required.
	ServiceKind string `json:"service_kind"`
	// ScopeID optionally overrides the derived scope id.
	ScopeID string `json:"scope_id"`
	// GenerationID optionally pins the generation id for replay.
	GenerationID string `json:"generation_id"`
	// FencingToken fences the scope's generation. Values <= 0 default to 1.
	FencingToken int64 `json:"fencing_token"`
	// Resources are the aws_resource observations replayed for this scope.
	Resources []awsruntime.FixtureResource `json:"resources"`
	// Relationships are the aws_relationship observations replayed for this scope.
	Relationships []awsruntime.FixtureRelationship `json:"relationships"`
}

// loadFixtureConfig reads and parses the declarative fixture config at path and
// resolves its poll interval. It rejects an empty document so fixture mode never
// runs against a config that would silently emit no facts.
func loadFixtureConfig(path string) (awsruntime.FixtureConfig, time.Duration, error) {
	raw, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return awsruntime.FixtureConfig{}, 0, fmt.Errorf("read aws fixture config %q: %w", path, err)
	}
	var fileCfg fixtureFileConfig
	if err := json.Unmarshal(raw, &fileCfg); err != nil {
		return awsruntime.FixtureConfig{}, 0, fmt.Errorf("parse aws fixture config %q: %w", path, err)
	}

	pollInterval := defaultFixturePollInterval
	if rawInterval := strings.TrimSpace(fileCfg.PollInterval); rawInterval != "" {
		interval, err := time.ParseDuration(rawInterval)
		if err != nil {
			return awsruntime.FixtureConfig{}, 0, fmt.Errorf("parse aws fixture poll_interval %q: %w", rawInterval, err)
		}
		pollInterval = interval
	}

	cfg := awsruntime.FixtureConfig{
		CollectorInstanceID: strings.TrimSpace(fileCfg.CollectorInstanceID),
		Scopes:              make([]awsruntime.FixtureScope, 0, len(fileCfg.Scopes)),
	}
	for i := range fileCfg.Scopes {
		cfg.Scopes = append(cfg.Scopes, fileCfg.Scopes[i].scope())
	}
	if err := cfg.Validate(); err != nil {
		return awsruntime.FixtureConfig{}, 0, err
	}
	return cfg, pollInterval, nil
}

func (s fixtureFileScope) scope() awsruntime.FixtureScope {
	return awsruntime.FixtureScope{
		AccountID:     strings.TrimSpace(s.AccountID),
		Region:        strings.TrimSpace(s.Region),
		ServiceKind:   strings.TrimSpace(s.ServiceKind),
		ScopeID:       strings.TrimSpace(s.ScopeID),
		GenerationID:  strings.TrimSpace(s.GenerationID),
		FencingToken:  s.FencingToken,
		Resources:     s.Resources,
		Relationships: s.Relationships,
	}
}
