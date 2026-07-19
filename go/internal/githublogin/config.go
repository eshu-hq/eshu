// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package githublogin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

const configVersion = 1

type configFile struct {
	Version           int              `json:"version"`
	DefaultProviderID string           `json:"default_provider_id,omitempty"`
	StateTTL          string           `json:"state_ttl,omitempty"`
	Providers         []ProviderConfig `json:"providers"`
	// GroupRoleMappings reuses oidclogin.GroupRoleMapping unchanged: its
	// Group field holds a GitHub "org/team-slug" handle here instead of an
	// OIDC group claim value, but the shape (name -> role_ids) and the
	// resolver that consumes it (StaticGrantResolver) are identical — this
	// is the literal "same GrantResolver seam" issue #5166 requires.
	GroupRoleMappings []GroupRoleMapping `json:"group_role_mappings"`
	RoleGrants        []RoleGrant        `json:"role_grants"`
}

// LoadConfigFile reads an operator-managed GitHub login config file,
// mirroring oidclogin.LoadConfigFile's shape and validation rules exactly
// (same StaticGrantResolver seam — see service_types.go).
func LoadConfigFile(path string) (Config, StaticGrantResolver, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return Config{}, StaticGrantResolver{}, fmt.Errorf("github login config file is required")
	}
	raw, err := os.ReadFile(path) // #nosec G304 -- reads operator-managed GitHub config file at a path from deployment config, not an HTTP/MCP request param
	if err != nil {
		return Config{}, StaticGrantResolver{}, fmt.Errorf("read github login config file: %w", err)
	}
	var doc configFile
	if err := json.Unmarshal(raw, &doc); err != nil {
		return Config{}, StaticGrantResolver{}, fmt.Errorf("decode github login config file: %w", err)
	}
	if doc.Version != configVersion {
		return Config{}, StaticGrantResolver{}, fmt.Errorf("github login config version %d is unsupported", doc.Version)
	}
	stateTTL := time.Duration(0)
	if strings.TrimSpace(doc.StateTTL) != "" {
		parsed, err := time.ParseDuration(strings.TrimSpace(doc.StateTTL))
		if err != nil {
			return Config{}, StaticGrantResolver{}, fmt.Errorf("parse github login state ttl: %w", err)
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
	// StaticGrantResolver's group/role indexes are validated lazily by
	// ResolveGroupGrants (oidclogin keeps those index builders unexported);
	// exercise the resolver once here against an empty query so a malformed
	// config file (duplicate role ids, a mapping with no role ids) fails at
	// startup rather than at the first login attempt.
	if _, _, err := resolver.ResolveGroupGrants(context.Background(), GrantQuery{GroupHashes: []string{"startup-validation-probe"}}); err != nil {
		return Config{}, StaticGrantResolver{}, fmt.Errorf("validate github static grant config: %w", err)
	}
	return config, resolver, nil
}
