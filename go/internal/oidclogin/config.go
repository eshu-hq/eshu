// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package oidclogin

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

const configVersion = 1

type configFile struct {
	Version           int                `json:"version"`
	DefaultProviderID string             `json:"default_provider_id,omitempty"`
	StateTTL          string             `json:"state_ttl,omitempty"`
	Providers         []ProviderConfig   `json:"providers"`
	GroupRoleMappings []GroupRoleMapping `json:"group_role_mappings"`
	RoleGrants        []RoleGrant        `json:"role_grants"`
}

// LoadConfigFile reads an operator-managed OIDC config file.
func LoadConfigFile(path string) (Config, StaticGrantResolver, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return Config{}, StaticGrantResolver{}, fmt.Errorf("oidc config file is required")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, StaticGrantResolver{}, fmt.Errorf("read oidc config file: %w", err)
	}
	var doc configFile
	if err := json.Unmarshal(raw, &doc); err != nil {
		return Config{}, StaticGrantResolver{}, fmt.Errorf("decode oidc config file: %w", err)
	}
	if doc.Version != configVersion {
		return Config{}, StaticGrantResolver{}, fmt.Errorf("oidc config version %d is unsupported", doc.Version)
	}
	stateTTL := time.Duration(0)
	if strings.TrimSpace(doc.StateTTL) != "" {
		parsed, err := time.ParseDuration(strings.TrimSpace(doc.StateTTL))
		if err != nil {
			return Config{}, StaticGrantResolver{}, fmt.Errorf("parse oidc state ttl: %w", err)
		}
		stateTTL = parsed
	}
	config, err := ValidateConfig(Config{
		DefaultProviderID: doc.DefaultProviderID,
		StateTTL:          stateTTL,
		Providers:         doc.Providers,
	})
	if err != nil {
		return Config{}, StaticGrantResolver{}, err
	}
	resolver := StaticGrantResolver{
		GroupRoleMappings: append([]GroupRoleMapping(nil), doc.GroupRoleMappings...),
		RoleGrants:        append([]RoleGrant(nil), doc.RoleGrants...),
	}
	if _, err := resolver.groupRoleIndex(); err != nil {
		return Config{}, StaticGrantResolver{}, err
	}
	if _, err := resolver.roleGrantIndex(); err != nil {
		return Config{}, StaticGrantResolver{}, err
	}
	return config, resolver, nil
}
