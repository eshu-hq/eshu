package oidclogin

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query"
)

const (
	defaultStateTTL  = 10 * time.Minute
	oidcSecretBytes  = 32
	oidcSubjectClass = "external_oidc_user"
)

// Config describes the enabled OIDC provider set.
type Config struct {
	DefaultProviderID string           `json:"default_provider_id,omitempty"`
	StateTTL          time.Duration    `json:"-"`
	Providers         []ProviderConfig `json:"providers"`
}

// ProviderConfig is one backend OIDC Authorization Code provider.
type ProviderConfig struct {
	ProviderConfigID string   `json:"provider_config_id"`
	IssuerURL        string   `json:"issuer_url"`
	ClientID         string   `json:"client_id"`
	ClientSecretFile string   `json:"client_secret_file,omitempty"`
	RedirectURL      string   `json:"redirect_url"`
	Scopes           []string `json:"scopes,omitempty"`
	TenantID         string   `json:"tenant_id"`
	WorkspaceID      string   `json:"workspace_id"`
	SubjectClaim     string   `json:"subject_claim,omitempty"`
	EmailClaim       string   `json:"email_claim,omitempty"`
	GroupsClaim      string   `json:"groups_claim,omitempty"`
}

// StateRecord is one hash-only OIDC login state.
type StateRecord struct {
	StateHash        string
	NonceHash        string
	ProviderConfigID string
	ProviderKeyHash  string
	IssuerHash       string
	ClientIDHash     string
	TenantID         string
	WorkspaceID      string
	RedirectURIHash  string
	ReturnToPath     string
	IssuedAt         time.Time
	ExpiresAt        time.Time
	UpdatedAt        time.Time
}

// StateStore persists and consumes server-side OIDC state.
type StateStore interface {
	CreateState(context.Context, StateRecord) error
	ConsumeState(context.Context, string, time.Time) (StateRecord, bool, error)
}

// GrantQuery asks a resolver to map hashed external groups to Eshu grants.
//
// GroupHashes drives login-time resolution from external group claims. RoleIDs
// drives active-session refresh re-resolution from a session's already granted
// roles, which lets the refresher detect revoked role targets, tombstoned or
// expired mappings, and policy revision drift without re-querying the provider.
// A resolver should treat the two as alternative inputs, preferring GroupHashes
// when present.
type GrantQuery struct {
	ProviderConfigID string
	TenantID         string
	WorkspaceID      string
	GroupHashes      []string
	RoleIDs          []string
	AsOf             time.Time
}

// GrantResolution is the Eshu-owned role and concrete grant result.
//
// AllowedPermissionFeatures and AllowedPermissionDataClasses carry the
// permission-catalog grants for the resolved roles so the issued cookie session
// enforces identically to a scoped token for the same roles.
//
// PermissionCatalogEnforced is the resolver's explicit declaration that the
// issued session must be gated by the permission catalog. Only a resolver that
// supplies a real catalog snapshot may set it: the database resolver sets it for
// scoped (non-admin) roles, while the file-backed static resolver leaves it
// false because it carries no catalog snapshot. Enforcement must be declared,
// not inferred from AllScopes or from an empty feature set, since a legitimate
// database role may grant zero features and still require enforcement.
type GrantResolution struct {
	RoleIDs                      []string
	PolicyRevisionHash           string
	AllScopes                    bool
	PermissionCatalogEnforced    bool
	AllowedScopeIDs              []string
	AllowedRepositoryIDs         []string
	AllowedPermissionFeatures    []string
	AllowedPermissionDataClasses []string
}

// GrantResolver resolves external groups through Eshu-owned role mappings.
type GrantResolver interface {
	ResolveGroupGrants(context.Context, GrantQuery) (GrantResolution, bool, error)
}

// TokenSet carries provider tokens only long enough to verify the ID token.
type TokenSet struct {
	IDToken string
}

// VerifiedClaims is the validated, normalized ID token claim subset.
type VerifiedClaims struct {
	Subject string
	Nonce   string
	Email   string
	Groups  []string
}

// Connector performs provider-specific OAuth2 and ID token verification.
type Connector interface {
	AuthCodeURL(state string, nonce string) string
	Exchange(context.Context, string) (TokenSet, error)
	VerifyIDToken(context.Context, string) (VerifiedClaims, error)
}

// ConnectorFactory constructs an OIDC connector for one provider.
type ConnectorFactory func(context.Context, ProviderConfig) (Connector, error)

// Option customizes Service behavior for tests and wiring.
type Option func(*Service)

// Service handles backend OIDC Authorization Code login.
type Service struct {
	config           Config
	stateStore       StateStore
	grantResolver    GrantResolver
	connectorFactory ConnectorFactory
	now              func() time.Time
	newSecret        func() (string, error)
	configErr        error
}

// NewService constructs an OIDC login service.
func NewService(
	config Config,
	stateStore StateStore,
	grantResolver GrantResolver,
	connectorFactory ConnectorFactory,
	options ...Option,
) *Service {
	normalized, err := ValidateConfig(config)
	service := &Service{
		config:           normalized,
		stateStore:       stateStore,
		grantResolver:    grantResolver,
		connectorFactory: connectorFactory,
		now:              func() time.Time { return time.Now().UTC() },
		newSecret:        randomSecret,
		configErr:        err,
	}
	for _, option := range options {
		option(service)
	}
	return service
}

// ValidateConfig returns the normalized OIDC login config or an error.
func ValidateConfig(config Config) (Config, error) {
	return normalizeConfig(config)
}

// WithNow overrides the clock used for state expiry.
func WithNow(now func() time.Time) Option {
	return func(s *Service) {
		if now != nil {
			s.now = now
		}
	}
}

// WithSecretGenerator overrides state and nonce secret generation.
func WithSecretGenerator(newSecret func() (string, error)) Option {
	return func(s *Service) {
		if newSecret != nil {
			s.newSecret = newSecret
		}
	}
}

// StartOIDCLogin stores state/nonce hashes and returns the provider redirect.
func (s *Service) StartOIDCLogin(
	ctx context.Context,
	req query.OIDCLoginStartRequest,
) (query.OIDCLoginStartResponse, error) {
	provider, err := s.provider(req.ProviderConfigID)
	if err != nil {
		return query.OIDCLoginStartResponse{}, err
	}
	tenantID, workspaceID, err := resolveProviderContext(provider, req.TenantID, req.WorkspaceID)
	if err != nil {
		return query.OIDCLoginStartResponse{}, err
	}
	state, err := s.newSecret()
	if err != nil {
		return query.OIDCLoginStartResponse{}, fmt.Errorf("%w: create state", query.ErrOIDCLoginUnavailable)
	}
	nonce, err := s.newSecret()
	if err != nil {
		return query.OIDCLoginStartResponse{}, fmt.Errorf("%w: create nonce", query.ErrOIDCLoginUnavailable)
	}
	now := s.now().UTC()
	record := StateRecord{
		StateHash:        SHA256Hash(state),
		NonceHash:        SHA256Hash(nonce),
		ProviderConfigID: provider.ProviderConfigID,
		ProviderKeyHash:  SHA256Hash(provider.IssuerURL + "\x00" + provider.ClientID),
		IssuerHash:       SHA256Hash(provider.IssuerURL),
		ClientIDHash:     SHA256Hash(provider.ClientID),
		TenantID:         tenantID,
		WorkspaceID:      workspaceID,
		RedirectURIHash:  SHA256Hash(provider.RedirectURL),
		ReturnToPath:     safeReturnPath(req.ReturnToPath),
		IssuedAt:         now,
		ExpiresAt:        now.Add(s.stateTTL()),
		UpdatedAt:        now,
	}
	if err := s.stateStore.CreateState(ctx, record); err != nil {
		return query.OIDCLoginStartResponse{}, fmt.Errorf("%w: persist state", query.ErrOIDCLoginUnavailable)
	}
	connector, err := s.connector(ctx, provider)
	if err != nil {
		return query.OIDCLoginStartResponse{}, err
	}
	return query.OIDCLoginStartResponse{RedirectURL: connector.AuthCodeURL(state, nonce)}, nil
}

// CompleteOIDCLogin validates the callback and returns a browser-session auth context.
func (s *Service) CompleteOIDCLogin(
	ctx context.Context,
	req query.OIDCLoginCompleteRequest,
) (query.OIDCLoginCompleteResponse, error) {
	if err := s.ready(); err != nil {
		return query.OIDCLoginCompleteResponse{}, err
	}
	state := strings.TrimSpace(req.State)
	code := strings.TrimSpace(req.Code)
	if state == "" || code == "" {
		return query.OIDCLoginCompleteResponse{}, query.ErrOIDCLoginInvalidRequest
	}
	now := s.now().UTC()
	record, ok, err := s.stateStore.ConsumeState(ctx, SHA256Hash(state), now)
	if err != nil {
		return query.OIDCLoginCompleteResponse{}, fmt.Errorf("%w: consume state", query.ErrOIDCLoginUnavailable)
	}
	if !ok {
		return query.OIDCLoginCompleteResponse{}, query.ErrOIDCLoginDenied
	}
	provider, err := s.provider(record.ProviderConfigID)
	if err != nil {
		return query.OIDCLoginCompleteResponse{}, err
	}
	if record.RedirectURIHash != SHA256Hash(provider.RedirectURL) {
		return query.OIDCLoginCompleteResponse{}, query.ErrOIDCLoginDenied
	}
	connector, err := s.connector(ctx, provider)
	if err != nil {
		return query.OIDCLoginCompleteResponse{}, err
	}
	tokens, err := connector.Exchange(ctx, code)
	if err != nil {
		return query.OIDCLoginCompleteResponse{}, fmt.Errorf("%w: exchange code", query.ErrOIDCLoginDenied)
	}
	claims, err := connector.VerifyIDToken(ctx, strings.TrimSpace(tokens.IDToken))
	if err != nil {
		return query.OIDCLoginCompleteResponse{}, fmt.Errorf("%w: verify id token", query.ErrOIDCLoginDenied)
	}
	if SHA256Hash(claims.Nonce) != record.NonceHash || strings.TrimSpace(claims.Subject) == "" {
		return query.OIDCLoginCompleteResponse{}, query.ErrOIDCLoginDenied
	}
	groupHashes := hashStrings(claims.Groups)
	if len(groupHashes) == 0 {
		return query.OIDCLoginCompleteResponse{}, query.ErrOIDCLoginDenied
	}
	grants, ok, err := s.grantResolver.ResolveGroupGrants(ctx, GrantQuery{
		ProviderConfigID: provider.ProviderConfigID,
		TenantID:         record.TenantID,
		WorkspaceID:      record.WorkspaceID,
		GroupHashes:      groupHashes,
		AsOf:             now,
	})
	if err != nil {
		return query.OIDCLoginCompleteResponse{}, fmt.Errorf("%w: resolve grants", query.ErrOIDCLoginUnavailable)
	}
	if !ok || len(grants.RoleIDs) == 0 {
		return query.OIDCLoginCompleteResponse{}, query.ErrOIDCLoginDenied
	}
	subjectIDHash := SHA256Hash(provider.ProviderConfigID + ":" + strings.TrimSpace(claims.Subject))
	return query.OIDCLoginCompleteResponse{
		Auth: query.AuthContext{
			Mode:                         query.AuthModeScoped,
			TenantID:                     record.TenantID,
			WorkspaceID:                  record.WorkspaceID,
			SubjectClass:                 oidcSubjectClass,
			SubjectIDHash:                subjectIDHash,
			PolicyRevisionHash:           grants.PolicyRevisionHash,
			RoleIDs:                      append([]string(nil), grants.RoleIDs...),
			AllScopes:                    grants.AllScopes,
			PermissionCatalogEnforced:    grants.PermissionCatalogEnforced,
			AllowedScopeIDs:              append([]string(nil), grants.AllowedScopeIDs...),
			AllowedRepositoryIDs:         append([]string(nil), grants.AllowedRepositoryIDs...),
			AllowedPermissionFeatures:    append([]string(nil), grants.AllowedPermissionFeatures...),
			AllowedPermissionDataClasses: append([]string(nil), grants.AllowedPermissionDataClasses...),
		},
		ProviderConfigID:    provider.ProviderConfigID,
		ProviderSubjectID:   subjectIDHash,
		ProviderGroupHashes: append([]string(nil), groupHashes...),
		ProviderProofAt:     now,
		ReturnToPath:        safeReturnPath(record.ReturnToPath),
	}, nil
}

func (s *Service) provider(providerConfigID string) (ProviderConfig, error) {
	if err := s.ready(); err != nil {
		return ProviderConfig{}, err
	}
	providerConfigID = strings.TrimSpace(providerConfigID)
	if providerConfigID == "" {
		providerConfigID = s.config.DefaultProviderID
	}
	for _, provider := range s.config.Providers {
		if provider.ProviderConfigID == providerConfigID {
			return provider, nil
		}
	}
	return ProviderConfig{}, query.ErrOIDCLoginInvalidRequest
}

func (s *Service) ready() error {
	if s == nil || s.stateStore == nil || s.grantResolver == nil || s.connectorFactory == nil {
		return query.ErrOIDCLoginUnavailable
	}
	if s.configErr != nil {
		return fmt.Errorf("%w: %v", query.ErrOIDCLoginInvalidRequest, s.configErr)
	}
	return nil
}

func (s *Service) connector(ctx context.Context, provider ProviderConfig) (Connector, error) {
	connector, err := s.connectorFactory(ctx, provider)
	if err != nil {
		return nil, fmt.Errorf("%w: connect provider", query.ErrOIDCLoginUnavailable)
	}
	return connector, nil
}

func (s *Service) stateTTL() time.Duration {
	if s.config.StateTTL > 0 {
		return s.config.StateTTL
	}
	return defaultStateTTL
}

// SHA256Hash returns the stable sha256: audit hash for a transient value.
func SHA256Hash(value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func randomSecret() (string, error) {
	var bytes [oidcSecretBytes]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes[:]), nil
}

func normalizeConfig(config Config) (Config, error) {
	config.DefaultProviderID = strings.TrimSpace(config.DefaultProviderID)
	if config.StateTTL < 0 {
		return Config{}, errors.New("state ttl must be non-negative")
	}
	providers := make([]ProviderConfig, 0, len(config.Providers))
	seen := map[string]struct{}{}
	for _, provider := range config.Providers {
		normalized, err := normalizeProvider(provider)
		if err != nil {
			return Config{}, err
		}
		if _, exists := seen[normalized.ProviderConfigID]; exists {
			return Config{}, errors.New("provider_config_id must be unique")
		}
		seen[normalized.ProviderConfigID] = struct{}{}
		providers = append(providers, normalized)
	}
	if len(providers) == 0 {
		return Config{}, errors.New("at least one provider is required")
	}
	if config.DefaultProviderID == "" {
		config.DefaultProviderID = providers[0].ProviderConfigID
	}
	if _, ok := seen[config.DefaultProviderID]; !ok {
		return Config{}, errors.New("default provider must reference a configured provider")
	}
	config.Providers = providers
	return config, nil
}

func normalizeProvider(provider ProviderConfig) (ProviderConfig, error) {
	provider.ProviderConfigID = strings.TrimSpace(provider.ProviderConfigID)
	provider.IssuerURL = strings.TrimSpace(provider.IssuerURL)
	provider.ClientID = strings.TrimSpace(provider.ClientID)
	provider.ClientSecretFile = strings.TrimSpace(provider.ClientSecretFile)
	provider.RedirectURL = strings.TrimSpace(provider.RedirectURL)
	provider.TenantID = strings.TrimSpace(provider.TenantID)
	provider.WorkspaceID = strings.TrimSpace(provider.WorkspaceID)
	provider.SubjectClaim = defaultString(strings.TrimSpace(provider.SubjectClaim), "sub")
	provider.EmailClaim = defaultString(strings.TrimSpace(provider.EmailClaim), "email")
	provider.GroupsClaim = defaultString(strings.TrimSpace(provider.GroupsClaim), "groups")
	provider.Scopes = cleanStrings(provider.Scopes)
	if len(provider.Scopes) == 0 {
		provider.Scopes = []string{"openid", "profile", "email", "groups"}
	}
	if provider.ProviderConfigID == "" || provider.IssuerURL == "" ||
		provider.ClientID == "" || provider.RedirectURL == "" ||
		provider.TenantID == "" || provider.WorkspaceID == "" {
		return ProviderConfig{}, errors.New("provider id, issuer, client id, redirect url, tenant, and workspace are required")
	}
	return provider, nil
}

func resolveProviderContext(provider ProviderConfig, tenantID string, workspaceID string) (string, string, error) {
	tenantID = defaultString(strings.TrimSpace(tenantID), provider.TenantID)
	workspaceID = defaultString(strings.TrimSpace(workspaceID), provider.WorkspaceID)
	if tenantID != provider.TenantID || workspaceID != provider.WorkspaceID {
		return "", "", query.ErrOIDCLoginInvalidRequest
	}
	return tenantID, workspaceID, nil
}

func hashStrings(values []string) []string {
	hashes := make([]string, 0, len(values))
	for _, value := range cleanStrings(values) {
		hashes = append(hashes, SHA256Hash(value))
	}
	return cleanStrings(hashes)
}

func cleanStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		cleaned = append(cleaned, value)
	}
	sort.Strings(cleaned)
	return cleaned
}

func safeReturnPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return ""
	}
	if strings.ContainsAny(path, "\r\n\t") {
		return ""
	}
	return path
}

func defaultString(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
