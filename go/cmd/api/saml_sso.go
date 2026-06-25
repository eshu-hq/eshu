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
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const envSAMLProvidersJSON = "ESHU_SAML_PROVIDERS_JSON"

var _ query.SAMLStore = (*postgresSAMLStore)(nil)

type postgresSAMLStore struct {
	ledger    *pgstatus.SAMLSSOStore
	identity  *pgstatus.IdentitySubjectStore
	providers map[string]samlProviderRuntimeConfig
	now       func() time.Time
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
			Tracer:      otel.Tracer("eshu-api"),
			Instruments: instruments,
			StoreName:   "saml_sso",
		}
	}
	store, err := newPostgresSAMLStore(
		pgstatus.NewSAMLSSOStore(samlDB),
		pgstatus.NewIdentitySubjectStore(samlDB),
		getenv,
	)
	if err != nil {
		return nil, err
	}
	return &query.SAMLHandler{
		Store:    store,
		Sessions: sessions,
		Verifier: query.CrewjamSAMLVerifier{},
	}, nil
}

func newPostgresSAMLStore(
	ledger *pgstatus.SAMLSSOStore,
	identity *pgstatus.IdentitySubjectStore,
	getenv func(string) string,
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
		ledger:    ledger,
		identity:  identity,
		providers: providers,
		now:       func() time.Time { return time.Now().UTC() },
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

func (s *postgresSAMLStore) GetSAMLProvider(
	ctx context.Context,
	providerID string,
) (query.SAMLProviderConfig, bool, error) {
	provider, ok := s.providers[strings.TrimSpace(providerID)]
	if !ok {
		return query.SAMLProviderConfig{}, false, nil
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

func (s *postgresSAMLStore) ResolveSAMLPrincipal(
	ctx context.Context,
	providerID string,
	principal samlauth.Principal,
	now time.Time,
) (query.AuthContext, bool, error) {
	provider, ok := s.providers[strings.TrimSpace(providerID)]
	if !ok {
		return query.AuthContext{}, false, nil
	}
	if s.identity != nil {
		result, err := s.identity.ResolveSAMLExternalSubject(ctx, pgstatus.SAMLExternalSubjectResolutionRequest{
			ProviderConfigID:      provider.provider.ProviderConfigID,
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
