// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package oidcbearer

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/oidclogin"
)

// envRevisionID is the constant RevisionID assigned to every env-file
// provider. Env-file providers only change when the process restarts with a
// different config file, so there is no revision to track; a constant value
// means the verifier cache reuses one built verifier for the life of the
// process instead of rebuilding it every TTL tick.
const envRevisionID = "env"

// envProviderSource adapts the operator-managed OIDC login config file
// (oidclogin.Config.Providers, ESHU_AUTH_OIDC_CONFIG_FILE) into a
// ProviderSource. It is static for the process lifetime: the config is
// loaded once at startup and never re-read, matching how the interactive
// login path treats the same config.
type envProviderSource struct {
	providers []BearerProvider
}

// NewEnvProviderSource builds a ProviderSource from an already-loaded
// oidclogin.Config. Every provider in config.Providers becomes one
// BearerProvider; WorkspaceID is always populated because env-file providers
// require it (see oidclogin's normalizeProvider).
func NewEnvProviderSource(config oidclogin.Config) ProviderSource {
	providers := make([]BearerProvider, 0, len(config.Providers))
	for _, p := range config.Providers {
		providers = append(providers, BearerProvider{
			ProviderConfigID: p.ProviderConfigID,
			IssuerURL:        p.IssuerURL,
			TenantID:         p.TenantID,
			WorkspaceID:      p.WorkspaceID,
			GroupsClaim:      p.GroupsClaim,
			SubjectClaim:     p.SubjectClaim,
			RevisionID:       envRevisionID,
		})
	}
	return &envProviderSource{providers: providers}
}

// ActiveBearerProviders implements ProviderSource.
func (s *envProviderSource) ActiveBearerProviders(context.Context) ([]BearerProvider, error) {
	if s == nil {
		return nil, nil
	}
	// Defensive copy: the cache rebuild (cache.go) never mutates its input,
	// but returning the same backing array to every caller would let one
	// caller's slice growth alias another's.
	out := make([]BearerProvider, len(s.providers))
	copy(out, s.providers)
	return out, nil
}
