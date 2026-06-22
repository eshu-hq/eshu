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
	providers map[string]samlProviderRuntimeConfig
	now       func() time.Time
}

type samlProviderRuntimeConfig struct {
	provider query.SAMLProviderConfig
	rules    []samlAuthRuleConfig
}

type samlProviderEnvConfig struct {
	ProviderConfigID                 string               `json:"provider_config_id"`
	ServiceProviderEntityID          string               `json:"service_provider_entity_id"`
	ServiceProviderACSURL            string               `json:"service_provider_acs_url"`
	IdentityProviderMetadataXMLEnv   string               `json:"identity_provider_metadata_xml_env"`
	ExpectedIdentityProviderEntityID string               `json:"expected_identity_provider_entity_id"`
	GroupAttributeNames              []string             `json:"group_attribute_names"`
	RequireGroups                    bool                 `json:"require_groups"`
	HashScope                        string               `json:"hash_scope"`
	ClockSkewSeconds                 int                  `json:"clock_skew_seconds"`
	AuthRules                        []samlAuthRuleConfig `json:"auth_rules"`
}

type samlAuthRuleConfig struct {
	RequiredGroupKeys    []string `json:"required_group_keys"`
	TenantID             string   `json:"tenant_id"`
	WorkspaceID          string   `json:"workspace_id"`
	SubjectClass         string   `json:"subject_class"`
	PolicyRevisionHash   string   `json:"policy_revision_hash"`
	AllScopes            bool     `json:"all_scopes"`
	AllowedScopeIDs      []string `json:"allowed_scope_ids"`
	AllowedRepositoryIDs []string `json:"allowed_repository_ids"`
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
	store, err := newPostgresSAMLStore(pgstatus.NewSAMLSSOStore(samlDB), getenv)
	if err != nil {
		return nil, err
	}
	return &query.SAMLHandler{
		Store:    store,
		Sessions: sessions,
		Verifier: query.CrewjamSAMLVerifier{},
	}, nil
}

func newPostgresSAMLStore(ledger *pgstatus.SAMLSSOStore, getenv func(string) string) (*postgresSAMLStore, error) {
	providers, err := loadSAMLProviderConfigs(getenv)
	if err != nil {
		return nil, err
	}
	if len(providers) == 0 {
		return nil, errors.New("at least one SAML provider is required")
	}
	return &postgresSAMLStore{
		ledger:    ledger,
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
	rules, err := normalizeSAMLAuthRules(cfg.AuthRules)
	if err != nil {
		return samlProviderRuntimeConfig{}, err
	}
	if len(rules) == 0 {
		return samlProviderRuntimeConfig{}, errors.New("at least one SAML auth rule is required")
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
		rules: rules,
	}
	return runtime, nil
}

func normalizeSAMLAuthRules(rules []samlAuthRuleConfig) ([]samlAuthRuleConfig, error) {
	out := make([]samlAuthRuleConfig, 0, len(rules))
	for _, rule := range rules {
		rule.RequiredGroupKeys = cleanSAMLGroupKeys(rule.RequiredGroupKeys)
		rule.TenantID = strings.TrimSpace(rule.TenantID)
		rule.WorkspaceID = strings.TrimSpace(rule.WorkspaceID)
		rule.SubjectClass = strings.TrimSpace(rule.SubjectClass)
		rule.PolicyRevisionHash = strings.TrimSpace(rule.PolicyRevisionHash)
		rule.AllowedScopeIDs = cleanSAMLStrings(rule.AllowedScopeIDs)
		rule.AllowedRepositoryIDs = cleanSAMLStrings(rule.AllowedRepositoryIDs)
		if rule.SubjectClass == "" {
			rule.SubjectClass = "external_saml"
		}
		if rule.TenantID == "" || rule.WorkspaceID == "" || rule.PolicyRevisionHash == "" {
			return nil, errors.New("SAML auth rules require tenant_id, workspace_id, and policy_revision_hash")
		}
		if !rule.AllScopes && len(rule.AllowedScopeIDs) == 0 && len(rule.AllowedRepositoryIDs) == 0 {
			return nil, errors.New("SAML auth rules require all_scopes or at least one allowed scope/repository")
		}
		out = append(out, rule)
	}
	return out, nil
}

func (s *postgresSAMLStore) GetSAMLProvider(
	_ context.Context,
	providerID string,
) (query.SAMLProviderConfig, bool, error) {
	provider, ok := s.providers[strings.TrimSpace(providerID)]
	if !ok {
		return query.SAMLProviderConfig{}, false, nil
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
) (bool, error) {
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
	_ context.Context,
	providerID string,
	principal samlauth.Principal,
	_ time.Time,
) (query.AuthContext, bool, error) {
	provider, ok := s.providers[strings.TrimSpace(providerID)]
	if !ok {
		return query.AuthContext{}, false, nil
	}
	groupKeys := samlGroupKeySet(principal.GroupKeys)
	for _, rule := range provider.rules {
		if !samlRuleMatchesGroups(rule, groupKeys) {
			continue
		}
		return query.AuthContext{
			TenantID:             rule.TenantID,
			WorkspaceID:          rule.WorkspaceID,
			SubjectClass:         rule.SubjectClass,
			SubjectIDHash:        principal.ExternalSubjectHash,
			PolicyRevisionHash:   rule.PolicyRevisionHash,
			AllScopes:            rule.AllScopes,
			AllowedScopeIDs:      append([]string(nil), rule.AllowedScopeIDs...),
			AllowedRepositoryIDs: append([]string(nil), rule.AllowedRepositoryIDs...),
		}, true, nil
	}
	return query.AuthContext{}, false, nil
}

func samlRuleMatchesGroups(rule samlAuthRuleConfig, groupKeys map[string]struct{}) bool {
	for _, required := range rule.RequiredGroupKeys {
		if _, ok := groupKeys[required]; !ok {
			return false
		}
	}
	return true
}

func samlGroupKeySet(groups []string) map[string]struct{} {
	out := make(map[string]struct{}, len(groups))
	for _, group := range cleanSAMLGroupKeys(groups) {
		out[group] = struct{}{}
	}
	return out
}

func cleanSAMLGroupKeys(values []string) []string {
	cleaned := cleanSAMLStrings(values)
	for i, value := range cleaned {
		cleaned[i] = strings.ToLower(value)
	}
	return cleaned
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
