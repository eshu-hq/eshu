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
	"strings"
	"time"

	"go.opentelemetry.io/otel"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/samlauth"
	"github.com/eshu-hq/eshu/go/internal/secretcrypto"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const envSAMLProvidersJSON = "ESHU_SAML_PROVIDERS_JSON"

var _ query.SAMLStore = (*postgresSAMLStore)(nil)

type postgresSAMLStore struct {
	ledger    *pgstatus.SAMLSSOStore
	identity  *pgstatus.IdentitySubjectStore
	providers map[string]samlProviderRuntimeConfig
	// dbProviders resolves enabled DB-backed external_saml providers not
	// present in the env-file providers map (#4966, epic #4962; completes
	// #4978). nil when no keyring is configured — a deployment with no DEK
	// then serves env-file providers only, matching OIDC's dbProviders
	// nil-means-env-only contract.
	dbProviders samlProviderDBResolver
	now         func() time.Time
}

type samlProviderRuntimeConfig struct {
	provider query.SAMLProviderConfig
}

type samlProviderEnvConfig struct {
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

func newSAMLHandler(
	db *sql.DB,
	instruments *telemetry.Instruments,
	getenv func(string) string,
	sessions query.BrowserSessionStore,
	cookieSecureMode query.CookieSecureMode,
	providerSecretKeyring *secretcrypto.Keyring,
) (*query.SAMLHandler, error) {
	if strings.TrimSpace(getenv(envSAMLProvidersJSON)) == "" {
		return nil, nil
	}
	if db == nil {
		return nil, errors.New("postgres is required when SAML providers are configured")
	}
	if sessions == nil {
		return nil, errors.New("browser sessions are required when SAML providers are configured")
	}
	samlDB := pgstatus.ExecQueryer(pgstatus.SQLDB{DB: db})
	if instruments != nil {
		samlDB = &pgstatus.InstrumentedDB{
			Inner:       samlDB,
			Tracer:      otel.Tracer(telemetry.DefaultSignalName),
			Instruments: instruments,
			StoreName:   "saml_sso",
		}
	}
	store, err := newPostgresSAMLStore(
		pgstatus.NewSAMLSSOStore(samlDB),
		pgstatus.NewIdentitySubjectStore(samlDB),
		getenv,
		newSAMLDBProviderResolver(db, providerSecretKeyring),
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

func newPostgresSAMLStore(
	ledger *pgstatus.SAMLSSOStore,
	identity *pgstatus.IdentitySubjectStore,
	getenv func(string) string,
	dbProviders samlProviderDBResolver,
) (*postgresSAMLStore, error) {
	if ledger == nil {
		return nil, errors.New("saml ledger store is required")
	}
	if identity == nil {
		return nil, errors.New("identity subject store is required for saml providers")
	}
	providers, err := loadSAMLProviderConfigs(getenv)
	if err != nil {
		return nil, err
	}
	if len(providers) == 0 {
		return nil, errors.New("at least one SAML provider is required")
	}
	return &postgresSAMLStore{
		ledger:      ledger,
		identity:    identity,
		providers:   providers,
		dbProviders: dbProviders,
		now:         func() time.Time { return time.Now().UTC() },
	}, nil
}

func loadSAMLProviderConfigs(getenv func(string) string) (map[string]samlProviderRuntimeConfig, error) {
	raw := strings.TrimSpace(getenv(envSAMLProvidersJSON))
	if raw == "" {
		return nil, nil
	}
	var configs []samlProviderEnvConfig
	dec := json.NewDecoder(bytes.NewBufferString(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&configs); err != nil {
		return nil, fmt.Errorf("parse %s: %w", envSAMLProvidersJSON, err)
	}
	providers := make(map[string]samlProviderRuntimeConfig, len(configs))
	for _, cfg := range configs {
		runtime, err := samlProviderRuntimeFromEnvConfig(cfg, getenv)
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

func samlProviderRuntimeFromEnvConfig(
	cfg samlProviderEnvConfig,
	getenv func(string) string,
) (samlProviderRuntimeConfig, error) {
	cfg.ProviderConfigID = strings.TrimSpace(cfg.ProviderConfigID)
	cfg.ServiceProviderEntityID = strings.TrimSpace(cfg.ServiceProviderEntityID)
	cfg.ServiceProviderACSURL = strings.TrimSpace(cfg.ServiceProviderACSURL)
	cfg.IdentityProviderMetadataXMLEnv = strings.TrimSpace(cfg.IdentityProviderMetadataXMLEnv)
	cfg.ExpectedIdentityProviderEntityID = strings.TrimSpace(cfg.ExpectedIdentityProviderEntityID)
	cfg.HashScope = strings.TrimSpace(cfg.HashScope)
	if cfg.ProviderConfigID == "" || cfg.ServiceProviderEntityID == "" || cfg.ServiceProviderACSURL == "" {
		return samlProviderRuntimeConfig{}, errors.New("SAML provider_config_id, service_provider_entity_id, and service_provider_acs_url are required")
	}
	if cfg.IdentityProviderMetadataXMLEnv == "" {
		return samlProviderRuntimeConfig{}, errors.New("SAML identity_provider_metadata_xml_env is required")
	}
	metadataXML := strings.TrimSpace(getenv(cfg.IdentityProviderMetadataXMLEnv))
	if metadataXML == "" {
		return samlProviderRuntimeConfig{}, fmt.Errorf("SAML metadata env %q is empty", cfg.IdentityProviderMetadataXMLEnv)
	}
	if cfg.HashScope == "" {
		cfg.HashScope = cfg.ProviderConfigID
	}
	runtime := samlProviderRuntimeConfig{
		provider: query.SAMLProviderConfig{
			ProviderConfigID: cfg.ProviderConfigID,
			ServiceProvider: samlauth.ServiceProviderConfig{
				EntityID: cfg.ServiceProviderEntityID,
				ACSURL:   cfg.ServiceProviderACSURL,
			},
			IdentityProviderMetadataXML:      []byte(metadataXML),
			ExpectedIdentityProviderEntityID: cfg.ExpectedIdentityProviderEntityID,
			GroupMapping: samlauth.ClaimMapping{
				GroupAttributeNames: cleanSAMLStrings(cfg.GroupAttributeNames),
				RequireGroups:       cfg.RequireGroups,
				HashScope:           cfg.HashScope,
			},
			ClockSkew: time.Duration(cfg.ClockSkewSeconds) * time.Second,
		},
	}
	return runtime, nil
}

// GetSAMLProvider resolves providerID to its SAML config, preferring the
// env-file provider set (unchanged, tenant-agnostic lookup by id) and falling
// back to a DB-backed provider (#4966, epic #4962; completes #4978) via
// s.dbProviders when the id is not found there. Env config always wins on a
// colliding id — the DB fallback only runs when no env provider matched —
// matching the same env-authoritative precedence OIDC's Service.provider()
// enforces and auth_providers.go's ListLoginProviders doc comment describes.
// The DB fallback is a no-op when s.dbProviders is nil (no keyring
// configured, or an env-only deployment).
func (s *postgresSAMLStore) GetSAMLProvider(
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

func (s *postgresSAMLStore) CreateSAMLRequest(
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

func (s *postgresSAMLStore) ConsumeSAMLRequest(
	ctx context.Context,
	providerID string,
	requestIDHash string,
	relayStateHash string,
	now time.Time,
) (string, bool, error) {
	return s.ledger.ConsumeSAMLRequest(ctx, providerID, requestIDHash, relayStateHash, now)
}

func (s *postgresSAMLStore) ReserveSAMLReplay(
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

// ResolveSAMLPrincipal maps a validated principal to Eshu authorization
// context. providerID resolution mirrors GetSAMLProvider's env-then-DB
// precedence (#4966, epic #4962; completes #4978), but deliberately does NOT
// call the full GetSAMLProvider/samlDBProviderResolver.ResolveProvider for a
// DB-backed provider — that would open its sealed sp_private_key/
// sp_certificate for no reason, since ResolveSAMLExternalSubject only needs
// the provider_config_id (already re-validated as active by that query's own
// pc.status='active' predicate). Instead, a DB-only providerID is confirmed
// via the same lightweight HasActiveSAMLProviderConfig existence/active
// check GetSAMLProvider uses for an ENV-registered provider — no secret is
// touched. This closes the gap where a DB-backed provider could pass
// assertion verification (via GetSAMLProvider's DB fallback earlier in the
// ACS flow) and then fail here anyway because s.providers, indexed directly,
// never knows about DB-backed rows.
func (s *postgresSAMLStore) ResolveSAMLPrincipal(
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

// ListProviderIDs implements query.SAMLProviderIDLister. It returns the sorted
// list of provider_config_ids registered from the ESHU_SAML_PROVIDERS_JSON env
// config. The caller (authProviderListStore) is responsible for checking DB
// activity before surfacing these IDs to the pre-auth discovery endpoint.
func (s *postgresSAMLStore) ListProviderIDs() []string {
	ids := make([]string, 0, len(s.providers))
	for id := range s.providers {
		ids = append(ids, id)
	}
	// Stable ordering so the discovery response is deterministic.
	sortStrings(ids)
	return ids
}

func sortStrings(ss []string) {
	for i := 1; i < len(ss); i++ {
		key := ss[i]
		j := i - 1
		for j >= 0 && ss[j] > key {
			ss[j+1] = ss[j]
			j--
		}
		ss[j+1] = key
	}
}

func cleanSAMLStrings(values []string) []string {
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
