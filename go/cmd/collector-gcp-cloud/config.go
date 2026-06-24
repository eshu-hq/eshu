// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud"
	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud/gcpruntime"
)

// fileConfig is the declarative on-disk configuration for the GCP cloud
// collector binary's fixture mode. It is intentionally offline: scopes
// reference read-only credentials by name and reference Cloud Asset Inventory
// pages as local fixture files.
type fileConfig struct {
	// CollectorInstanceID is the configured runtime instance id. Required.
	CollectorInstanceID string `json:"collector_instance_id"`
	// PollInterval is an optional Go duration string (for example "30m").
	PollInterval string `json:"poll_interval"`
	// Scopes declares the bounded scopes to scan.
	Scopes []fileScope `json:"scopes"`
}

// fileScope is one declarative scope plus its offline fixture page files.
type fileScope struct {
	// ScopeID optionally overrides the derived scope id.
	ScopeID string `json:"scope_id"`
	// ParentScopeKind is one of organization, folder, or project.
	ParentScopeKind string `json:"parent_scope_kind"`
	// ParentScopeID is the provider parent identifier.
	ParentScopeID string `json:"parent_scope_id"`
	// AssetTypeFamily is the bounded asset family for the shard.
	AssetTypeFamily string `json:"asset_type_family"`
	// ContentFamily is the bounded content family for the shard.
	ContentFamily string `json:"content_family"`
	// LocationBucket is the bounded location bucket for the shard.
	LocationBucket string `json:"location_bucket"`
	// GenerationID optionally pins the generation id for replay.
	GenerationID string `json:"generation_id"`
	// FencingToken fences the scope's generation. Must be positive.
	FencingToken int64 `json:"fencing_token"`
	// CredentialRef names the read-only credential by name only.
	CredentialRef string `json:"credential_ref"`
	// PageFiles lists the offline Cloud Asset Inventory fixture pages, in order,
	// the fixture provider serves for this scope.
	PageFiles []string `json:"page_files"`
}

// loadFileConfig reads and parses the declarative config document at path.
func loadFileConfig(path string) (fileConfig, error) {
	raw, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return fileConfig{}, fmt.Errorf("read gcp collector config %q: %w", path, err)
	}
	var cfg fileConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return fileConfig{}, fmt.Errorf("parse gcp collector config %q: %w", path, err)
	}
	return cfg, nil
}

// runtimeConfig converts the declarative file config into a gcpruntime.Config.
// It resolves the poll interval and bounded scope shards but never resolves
// credential material or fixture pages; those are handled separately.
func (c fileConfig) runtimeConfig() (gcpruntime.Config, error) {
	cfg := gcpruntime.Config{CollectorInstanceID: strings.TrimSpace(c.CollectorInstanceID)}
	if raw := strings.TrimSpace(c.PollInterval); raw != "" {
		interval, err := time.ParseDuration(raw)
		if err != nil {
			return gcpruntime.Config{}, fmt.Errorf("parse gcp collector poll_interval %q: %w", raw, err)
		}
		cfg.PollInterval = interval
	}
	cfg.Scopes = make([]gcpruntime.ScopeConfig, 0, len(c.Scopes))
	for i := range c.Scopes {
		cfg.Scopes = append(cfg.Scopes, c.Scopes[i].scopeConfig())
	}
	if err := cfg.Validate(); err != nil {
		return gcpruntime.Config{}, err
	}
	return cfg, nil
}

func (s fileScope) scopeConfig() gcpruntime.ScopeConfig {
	return gcpruntime.ScopeConfig{
		ScopeID:         strings.TrimSpace(s.ScopeID),
		ParentScopeKind: gcpcloud.ParentScopeKind(strings.TrimSpace(s.ParentScopeKind)),
		ParentScopeID:   strings.TrimSpace(s.ParentScopeID),
		AssetTypeFamily: strings.TrimSpace(s.AssetTypeFamily),
		ContentFamily:   strings.TrimSpace(s.ContentFamily),
		LocationBucket:  strings.TrimSpace(s.LocationBucket),
		GenerationID:    strings.TrimSpace(s.GenerationID),
		FencingToken:    s.FencingToken,
		CredentialRef:   strings.TrimSpace(s.CredentialRef),
	}
}

// fixtureFiles maps each resolved scope id to its ordered offline page files so
// the fixture page provider serves the same scope identity the source uses.
func (c fileConfig) fixtureFiles(cfg gcpruntime.Config) map[string][]string {
	resolved := cfg.ResolvedScopes()
	out := make(map[string][]string, len(c.Scopes))
	for i := range c.Scopes {
		if i >= len(resolved) {
			break
		}
		out[resolved[i].ScopeID] = append([]string(nil), c.Scopes[i].PageFiles...)
	}
	return out
}
