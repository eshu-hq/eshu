// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/azurecloud/azureruntime"
)

const (
	envCollectorInstanceID = "ESHU_AZURE_COLLECTOR_INSTANCE_ID"
	envPollInterval        = "ESHU_AZURE_POLL_INTERVAL"
	envTargetsJSON         = "ESHU_AZURE_TARGETS_JSON"
	// envFixturePagesJSON points the runtime at a file-backed offline page
	// provider. It is for local proof and smoke tests only; production wiring
	// leaves it unset so the gated live seam is selected and no live Azure call
	// is ever a default.
	envFixturePagesJSON = "ESHU_AZURE_FIXTURE_PAGES_JSON"
)

// targetJSON is the declarative wire shape for one bounded Azure scope shard.
// CredentialRef names a read-only credential; it is never a secret value.
type targetJSON struct {
	TenantID           string `json:"tenant_id"`
	ScopeKind          string `json:"scope_kind"`
	ProviderScopeID    string `json:"provider_scope_id"`
	ResourceTypeFamily string `json:"resource_type_family"`
	LocationBucket     string `json:"location_bucket"`
	CredentialRef      string `json:"credential_ref"`
	SourceURI          string `json:"source_uri"`
	SourceLane         string `json:"source_lane"`
	FencingToken       int64  `json:"fencing_token"`
}

// fixturePagesConfig declares the file-backed offline page provider used by
// local proof and smoke tests. PagePaths are read in order and chained by each
// page's $skipToken. It never causes a live Azure call.
type fixturePagesConfig struct {
	PagePaths           []string `json:"page_paths"`
	Partial             bool     `json:"partial"`
	HiddenResourceCount int      `json:"hidden_resource_count"`
	Reason              string   `json:"reason"`
	Message             string   `json:"message"`
}

// loadRuntimeConfig builds the declarative azureruntime.Config from environment
// variables. Targets are JSON; credentials are referenced by name only.
func loadRuntimeConfig(getenv func(string) string) (azureruntime.Config, error) {
	collectorID := strings.TrimSpace(getenv(envCollectorInstanceID))
	if collectorID == "" {
		return azureruntime.Config{}, fmt.Errorf("%s is required", envCollectorInstanceID)
	}
	rawTargets := strings.TrimSpace(getenv(envTargetsJSON))
	if rawTargets == "" {
		return azureruntime.Config{}, fmt.Errorf("%s is required", envTargetsJSON)
	}
	var decoded []targetJSON
	if err := json.Unmarshal([]byte(rawTargets), &decoded); err != nil {
		return azureruntime.Config{}, fmt.Errorf("decode %s: %w", envTargetsJSON, err)
	}
	targets := make([]azureruntime.TargetConfig, 0, len(decoded))
	for _, target := range decoded {
		targets = append(targets, mapTarget(target))
	}
	pollInterval, err := parsePollInterval(getenv(envPollInterval))
	if err != nil {
		return azureruntime.Config{}, err
	}
	return azureruntime.Config{
		CollectorInstanceID: collectorID,
		PollInterval:        pollInterval,
		Targets:             targets,
	}, nil
}

func mapTarget(target targetJSON) azureruntime.TargetConfig {
	return azureruntime.TargetConfig{
		TenantID:           strings.TrimSpace(target.TenantID),
		ScopeKind:          strings.TrimSpace(target.ScopeKind),
		ProviderScopeID:    strings.TrimSpace(target.ProviderScopeID),
		ResourceTypeFamily: strings.TrimSpace(target.ResourceTypeFamily),
		LocationBucket:     strings.TrimSpace(target.LocationBucket),
		CredentialRef:      strings.TrimSpace(target.CredentialRef),
		SourceURI:          strings.TrimSpace(target.SourceURI),
		SourceLane:         strings.TrimSpace(target.SourceLane),
		FencingToken:       target.FencingToken,
	}
}

// loadFixturePagesConfig parses the optional file-backed offline provider
// configuration. A blank value means no fixture provider is configured and the
// gated live seam is selected.
func loadFixturePagesConfig(getenv func(string) string) (fixturePagesConfig, bool, error) {
	raw := strings.TrimSpace(getenv(envFixturePagesJSON))
	if raw == "" {
		return fixturePagesConfig{}, false, nil
	}
	var decoded fixturePagesConfig
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return fixturePagesConfig{}, false, fmt.Errorf("decode %s: %w", envFixturePagesJSON, err)
	}
	if len(decoded.PagePaths) == 0 {
		return fixturePagesConfig{}, false, fmt.Errorf("%s requires page_paths", envFixturePagesJSON)
	}
	return decoded, true, nil
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
