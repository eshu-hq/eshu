// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/samlauth"
	"github.com/eshu-hq/eshu/go/internal/secretcrypto"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// coreSAMLStore is the core-owned Postgres SAML admission implementation:
// env-file providers win; DB-backed providers resolve through the sealed
// secret path only when a keyring is configured.
type coreSAMLStore struct {
	ledger      *pgstatus.SAMLSSOStore
	identity    *pgstatus.IdentitySubjectStore
	providers   map[string]coreSAMLProviderRuntimeConfig
	dbProviders coreSAMLProviderDBResolver
	now         func() time.Time
}

var _ query.SAMLStore = (*coreSAMLStore)(nil)

// coreSAMLProviderRuntimeConfig is one env-file SAML provider's runtime view.
type coreSAMLProviderRuntimeConfig struct {
	provider query.SAMLProviderConfig
}

// coreSAMLProviderEnvConfig is the ESHU_SAML_PROVIDERS_JSON entry shape.
type coreSAMLProviderEnvConfig struct {
	ProviderConfigID                 string   `json:"provider_config_id"`
	ServiceProviderEntityID          string   `json:"service_provider_entity_id"`
	ServiceProviderACSURL            string   `json:"service_provider_acs_url"`
	IdentityProviderMetadataXMLEnv   string   `json:"identity_provider_metadata_xml_env"`
	ExpectedIdentityProviderEntityID string   `json:"expected_identity_provider_entity_id"`
	GroupAttributeNames              []string `json:"group_attribute_names"`
	RequireGroups                    bool     `json:"require_groups"`
	HashScope                        string   `json:"hash_scope"`
	ClockSkewSeconds                 int      `json:"clock_skew_seconds"`
}

// coreSAMLProviderDBResolver resolves DB-backed SAML providers. It is a
// narrow interface so tests can substitute a fake for real Postgres.
type coreSAMLProviderDBResolver interface {
	ResolveProvider(ctx context.Context, providerConfigID string) (query.SAMLProviderConfig, bool, error)
}

// newCoreSAMLHandler builds the SAML handler. It returns nil when
// ESHU_SAML_PROVIDERS_JSON is unset (SAML has no DB-only activation).
func newCoreSAMLHandler(
	rawDB *sql.DB,
	instruments *telemetry.Instruments,
	getenv func(string) string,
	sessions query.BrowserSessionStore,
	cookieSecureMode query.CookieSecureMode,
	providerSecretKeyring *secretcrypto.Keyring,
) (*query.SAMLHandler, error) {
	if coreEnvOrEmpty(getenv, coreEnvSAMLProvidersJSON) == "" {
		return nil, nil
	}
	if rawDB == nil {
		return nil, errors.New("postgres is required when SAML providers are configured")
	}
	if sessions == nil {
		return nil, errors.New("browser sessions are required when SAML providers are configured")
	}
	samlDB := coreInstrumentedDB(rawDB, instruments, "saml_sso")
	store, err := newCorePostgresSAMLStore(
		pgstatus.NewSAMLSSOStore(samlDB),
		pgstatus.NewIdentitySubjectStore(samlDB),
		getenv,
		newCoreSAMLDBProviderResolver(rawDB, providerSecretKeyring),
	)
	if err != nil {
		return nil, err
	}
	return &query.SAMLHandler{
		Store:        store,
		Sessions:     sessions,
		Verifier:     query.CrewjamSAMLVerifier{},
		CookieSecure: cookieSecureMode,
	}, nil
}

func newCorePostgresSAMLStore(
	ledger *pgstatus.SAMLSSOStore,
	identity *pgstatus.IdentitySubjectStore,
	getenv func(string) string,
	dbProviders coreSAMLProviderDBResolver,
) (*coreSAMLStore, error) {
	if ledger == nil {
		return nil, errors.New("saml ledger store is required")
	}
	if identity == nil {
		return nil, errors.New("identity subject store is required for saml providers")
	}
	providers, err := loadCoreSAMLProviderConfigs(getenv)
	if err != nil {
		return nil, err
	}
	if len(providers) == 0 {
		return nil, errors.New("at least one SAML provider is required")
	}
	return &coreSAMLStore{
		ledger:      ledger,
		identity:    identity,
		providers:   providers,
		dbProviders: dbProviders,
		now:         func() time.Time { return time.Now().UTC() },
	}, nil
}

func loadCoreSAMLProviderConfigs(getenv func(string) string) (map[string]coreSAMLProviderRuntimeConfig, error) {
	raw := coreEnvOrEmpty(getenv, coreEnvSAMLProvidersJSON)
	if raw == "" {
		return nil, nil
	}
	var configs []coreSAMLProviderEnvConfig
	dec := json.NewDecoder(bytes.NewBufferString(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&configs); err != nil {
		return nil, fmt.Errorf("parse %s: %w", coreEnvSAMLProvidersJSON, err)
	}
	providers := make(map[string]coreSAMLProviderRuntimeConfig, len(configs))
	for _, cfg := range configs {
		runtime, err := samlProviderRuntimeFromCoreEnvConfig(cfg, getenv)
		if err != nil {
			return nil, err
		}
		providerID := runtime.provider.ProviderConfigID
		if _, exists := providers[providerID]; exists {
			return nil, fmt.Errorf("duplicate SAML provider_config_id %q", providerID)
		}
		providers[providerID] = runtime
	}
	return providers, nil
}

func samlProviderRuntimeFromCoreEnvConfig(
	cfg coreSAMLProviderEnvConfig,
	getenv func(string) string,
) (coreSAMLProviderRuntimeConfig, error) {
	cfg.ProviderConfigID = strings.TrimSpace(cfg.ProviderConfigID)
	cfg.ServiceProviderEntityID = strings.TrimSpace(cfg.ServiceProviderEntityID)
	cfg.ServiceProviderACSURL = strings.TrimSpace(cfg.ServiceProviderACSURL)
	cfg.IdentityProviderMetadataXMLEnv = strings.TrimSpace(cfg.IdentityProviderMetadataXMLEnv)
	cfg.ExpectedIdentityProviderEntityID = strings.TrimSpace(cfg.ExpectedIdentityProviderEntityID)
	cfg.HashScope = strings.TrimSpace(cfg.HashScope)
	if cfg.ProviderConfigID == "" || cfg.ServiceProviderEntityID == "" || cfg.ServiceProviderACSURL == "" {
		return coreSAMLProviderRuntimeConfig{}, errors.New("SAML provider_config_id, service_provider_entity_id, and service_provider_acs_url are required")
	}
	if cfg.IdentityProviderMetadataXMLEnv == "" {
		return coreSAMLProviderRuntimeConfig{}, errors.New("SAML identity_provider_metadata_xml_env is required")
	}
	metadataXML := coreEnvOrEmpty(getenv, cfg.IdentityProviderMetadataXMLEnv)
	if metadataXML == "" {
		return coreSAMLProviderRuntimeConfig{}, fmt.Errorf("SAML metadata env %q is empty", cfg.IdentityProviderMetadataXMLEnv)
	}
	if cfg.HashScope == "" {
		cfg.HashScope = cfg.ProviderConfigID
	}
	return coreSAMLProviderRuntimeConfig{
		provider: query.SAMLProviderConfig{
			ProviderConfigID: cfg.ProviderConfigID,
			ServiceProvider: samlauth.ServiceProviderConfig{
				EntityID: cfg.ServiceProviderEntityID,
				ACSURL:   cfg.ServiceProviderACSURL,
			},
			IdentityProviderMetadataXML:      []byte(metadataXML),
			ExpectedIdentityProviderEntityID: cfg.ExpectedIdentityProviderEntityID,
			GroupMapping: samlauth.ClaimMapping{
				GroupAttributeNames: cleanCoreSAMLStrings(cfg.GroupAttributeNames),
				RequireGroups:       cfg.RequireGroups,
				HashScope:           cfg.HashScope,
			},
			ClockSkew: time.Duration(cfg.ClockSkewSeconds) * time.Second,
		},
	}, nil
}

// GetSAMLProvider resolves providerID to its SAML config, preferring
// env-file providers and falling back to DB-backed providers.
func (s *coreSAMLStore) GetSAMLProvider(
	ctx context.Context,
	providerID string,
) (query.SAMLProviderConfig, bool, error) {
	providerID = strings.TrimSpace(providerID)
	provider, ok := s.providers[providerID]
	if !ok {
		if s.dbProviders == nil || providerID == "" {
			return query.SAMLProviderConfig{}, false, nil
		}
		return s.dbProviders.ResolveProvider(ctx, providerID)
	}
	if s.identity != nil {
		active, err := s.identity.HasActiveSAMLProviderConfig(ctx, provider.provider.ProviderConfigID)
		if err != nil {
			return query.SAMLProviderConfig{}, false, err
		}
		if !active {
			return query.SAMLProviderConfig{}, false, nil
		}
	}
	cfg := provider.provider
	cfg.IdentityProviderMetadataXML = append([]byte(nil), cfg.IdentityProviderMetadataXML...)
	cfg.GroupMapping.GroupAttributeNames = append([]string(nil), cfg.GroupMapping.GroupAttributeNames...)
	return cfg, true, nil
}

// CreateSAMLRequest persists one SP-initiated request record.
func (s *coreSAMLStore) CreateSAMLRequest(
	ctx context.Context,
	providerID string,
	record query.SAMLRequestCreateRecord,
) error {
	return s.ledger.CreateSAMLRequest(ctx, pgstatus.SAMLAuthnRequestRecord{
		ProviderConfigID: strings.TrimSpace(providerID),
		RequestIDHash:    record.RequestIDHash,
		RelayStateHash:   record.RelayStateHash,
		ReturnToPath:     record.ReturnToPath,
		IssuedAt:         record.IssuedAt,
		ExpiresAt:        record.ExpiresAt,
		CreatedAt:        record.IssuedAt,
		UpdatedAt:        record.IssuedAt,
	})
}

// ConsumeSAMLRequest consumes one SP-initiated request exactly once.
func (s *coreSAMLStore) ConsumeSAMLRequest(
	ctx context.Context,
	providerID string,
	requestIDHash string,
	relayStateHash string,
	now time.Time,
) (string, bool, error) {
	return s.ledger.ConsumeSAMLRequest(ctx, providerID, requestIDHash, relayStateHash, now)
}

// ReserveSAMLReplay reserves one assertion replay key.
func (s *coreSAMLStore) ReserveSAMLReplay(
	ctx context.Context,
	providerID string,
	replayHash string,
	expiresAt time.Time,
) (bool, error) {
	return s.ledger.ReserveSAMLReplay(ctx, pgstatus.SAMLReplayKeyRecord{
		ProviderConfigID: strings.TrimSpace(providerID),
		ReplayHash:       replayHash,
		ObservedAt:       s.now(),
		ExpiresAt:        expiresAt,
	})
}

// ResolveSAMLPrincipal maps a validated principal to authorization context.
// A DB-only providerID is confirmed via the lightweight active check, never
// by opening its sealed secret.
func (s *coreSAMLStore) ResolveSAMLPrincipal(
	ctx context.Context,
	providerID string,
	principal samlauth.Principal,
	now time.Time,
) (query.AuthContext, bool, error) {
	providerID = strings.TrimSpace(providerID)
	providerConfigID := providerID
	if provider, ok := s.providers[providerID]; ok {
		providerConfigID = provider.provider.ProviderConfigID
	} else {
		if s.dbProviders == nil || s.identity == nil {
			return query.AuthContext{}, false, nil
		}
		active, err := s.identity.HasActiveSAMLProviderConfig(ctx, providerID)
		if err != nil {
			return query.AuthContext{}, false, err
		}
		if !active {
			return query.AuthContext{}, false, nil
		}
	}
	if s.identity != nil {
		result, err := s.identity.ResolveSAMLExternalSubject(ctx, pgstatus.SAMLExternalSubjectResolutionRequest{
			ProviderConfigID:      providerConfigID,
			ExternalSubjectIDHash: principal.ExternalSubjectHash,
			GroupClaimsHash:       principal.GroupClaimHash,
			Now:                   now,
		})
		if err != nil {
			return query.AuthContext{}, false, err
		}
		if result.Resolved {
			return query.AuthContext{
				TenantID:                     result.Auth.TenantID,
				WorkspaceID:                  result.Auth.WorkspaceID,
				SubjectClass:                 result.Auth.SubjectClass,
				SubjectIDHash:                result.Auth.SubjectIDHash,
				PolicyRevisionHash:           result.Auth.PolicyRevisionHash,
				AllScopes:                    result.Auth.AllScopes,
				RoleIDs:                      append([]string(nil), result.Auth.RoleIDs...),
				PermissionCatalogEnforced:    result.Auth.PermissionCatalogEnforced,
				AllowedPermissionFeatures:    append([]string(nil), result.Auth.AllowedPermissionFeatures...),
				AllowedPermissionDataClasses: append([]string(nil), result.Auth.AllowedPermissionDataClasses...),
			}, true, nil
		}
		return query.AuthContext{}, false, nil
	}
	return query.AuthContext{}, false, nil
}

// ListProviderIDs implements SAMLProviderIDLister: the sorted env-config
// provider ids. The caller checks DB activity before surfacing them.
func (s *coreSAMLStore) ListProviderIDs() []string {
	ids := make([]string, 0, len(s.providers))
	for id := range s.providers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func cleanCoreSAMLStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
