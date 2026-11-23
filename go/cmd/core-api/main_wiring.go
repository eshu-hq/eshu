// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/cli/support"
	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/query/ask"
	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/impact"
	"github.com/eshu-hq/eshu/go/internal/scopedtoken"
	"github.com/eshu-hq/eshu/go/internal/secretcrypto"
	"github.com/eshu-hq/eshu/go/internal/serviceintelhttp"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/governance/audit"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel"
)

// scopedtokenPostgresIdentityResolver builds the Postgres-backed scoped-token
// identity resolver for the auth chain.
func scopedtokenPostgresIdentityResolver(rawDB *sql.DB) query.ScopedTokenResolver {
	if rawDB == nil {
		return nil
	}
	return scopedtoken.NewPostgresIdentityResolver(
		pgstatus.NewScopedAPITokenStore(db.ExecQueryer(pgstatus.SQLDB{DB: rawDB})),
	)
}

// scopedtokenFileResolver builds the scoped-token file-registry resolver.
func scopedtokenFileResolver(getenv func(string) string) (query.ScopedTokenResolver, error) {
	return scopedtoken.ResolverFromEnv(getenv)
}

// Core operator surface for login-provider activation and session tuning.
const (
	coreEnvAuthOIDCEnabled                 = "ESHU_AUTH_OIDC_ENABLED"
	coreEnvAuthOIDCConfigFile              = "ESHU_AUTH_OIDC_CONFIG_FILE"
	coreEnvAuthOIDCProviderID              = "ESHU_AUTH_OIDC_PROVIDER_ID"
	coreEnvAuthOIDCStateTTL                = "ESHU_AUTH_OIDC_STATE_TTL"
	coreEnvAuthOIDCSessionRefreshWindow    = "ESHU_AUTH_OIDC_SESSION_REFRESH_WINDOW"
	coreEnvAuthOIDCLoginRatePerSec         = "ESHU_AUTH_OIDC_LOGIN_RATE_PER_SEC"
	coreEnvAuthOIDCLoginRateBurst          = "ESHU_AUTH_OIDC_LOGIN_RATE_BURST"
	coreEnvAuthOIDCLoginProviderRatePerMin = "ESHU_AUTH_OIDC_LOGIN_PROVIDER_RATE_PER_MIN"
	coreEnvAuthOIDCLoginProviderBurst      = "ESHU_AUTH_OIDC_LOGIN_PROVIDER_BURST"
	coreEnvAuthGitHubEnabled               = "ESHU_AUTH_GITHUB_ENABLED"
	coreEnvAuthGitHubConfigFile            = "ESHU_AUTH_GITHUB_CONFIG_FILE"
	coreEnvAuthGitHubProviderID            = "ESHU_AUTH_GITHUB_PROVIDER_ID"
	coreEnvAuthGitHubStateTTL              = "ESHU_AUTH_GITHUB_STATE_TTL"
	coreEnvAuthGitHubSessionRefreshWindow  = "ESHU_AUTH_GITHUB_SESSION_REFRESH_WINDOW"
	coreEnvSAMLProvidersJSON               = "ESHU_SAML_PROVIDERS_JSON"
)

// coreEnvOrEmpty reads one operator variable. A nil getenv reads as empty.
func coreEnvOrEmpty(getenv func(string) string, key string) string {
	if getenv == nil {
		return ""
	}
	return strings.TrimSpace(getenv(key))
}

// coreInstrumentedDB wraps a writer handle with OTEL tracing when
// instruments are wired; otherwise it returns the plain handle.
func coreInstrumentedDB(
	rawDB *sql.DB,
	instruments *telemetry.Instruments,
	storeName string,
) db.ExecQueryer {
	inner := db.ExecQueryer(pgstatus.SQLDB{DB: rawDB})
	if instruments == nil {
		return inner
	}
	return &pgstatus.InstrumentedDB{
		Inner:       inner,
		Tracer:      otel.Tracer(telemetry.DefaultSignalName),
		Instruments: instruments,
		StoreName:   storeName,
	}
}

// unwrapNilAdmissionPort returns nil when port holds a nil dynamic value of
// any nilable kind, so a typed-nil admission implementation fails closed to
// 503 instead of hiding from the handler's nil check.
func unwrapNilAdmissionPort[T any](port T) T {
	v := reflect.ValueOf(port)
	if !v.IsValid() {
		return port
	}
	k := v.Kind()
	if (k == reflect.Chan ||
		k == reflect.Func ||
		k == reflect.Interface ||
		k == reflect.Map ||
		k == reflect.Pointer ||
		k == reflect.Slice ||
		k == reflect.UnsafePointer) && v.IsNil() {
		var zero T
		return zero
	}
	return port
}

// coreRecoveryAuditAppender adapts the governance audit reader into the
// append surface admin handlers need.
func coreRecoveryAuditAppender(
	reader query.GovernanceAuditSummaryReader,
) query.GovernanceAuditAppender {
	return query.CoreGovernanceAuditAppender(reader)
}

// newCoreGovernanceAuditReader builds the Postgres governance-audit reader
// over the shared read store.
func newCoreGovernanceAuditReader(
	reader db.Queryer,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) auditstore.GovernanceAuditReader {
	if instruments != nil {
		reader = &pgstatus.InstrumentedQueryer{
			Inner:       reader,
			Instruments: instruments,
			StoreName:   "governance_audit",
		}
	}
	return auditstore.NewGovernanceAuditReader(reader).WithLogger(logger)
}

// chainResolvers merges identity, OIDC bearer, and file-registry resolvers
// into one chain (identity first, then bearer, then file). ChainResolvers
// drops nils and returns nil when no resolver is configured.
func chainResolvers(resolvers ...query.ScopedTokenResolver) query.ScopedTokenResolver {
	return scopedtoken.ChainResolvers(resolvers...)
}

// buildCoreAuthWiring builds the binary-owned auth-family handlers: local
// identity, sessions, setup, sign-in policy, OIDC/SAML/GitHub login, the
// provider list, and provider-config admin. Secret-sealed material never
// crosses package query — every keyring-touching adapter is constructed
// here (mirroring the retired cmd/api wiring). A nil database yields
// fail-closed handlers, never a panic.
func buildCoreAuthWiring(
	getenv func(string) string,
	rawDB *sql.DB,
	instruments *telemetry.Instruments,
	keyring *secretcrypto.Keyring,
	cookieSecureMode query.CookieSecureMode,
	bootstrapMode string,
	governanceAudit query.GovernanceAuditSummaryReader,
	logger *slog.Logger,
) (*query.CoreAuthWiring, error) {
	audit := query.CoreGovernanceAuditAppender(governanceAudit)
	localIdentity := &query.LocalIdentityHandler{
		Audit:        audit,
		CookieSecure: cookieSecureMode,
	}
	if store := newCorePostgresLocalIdentityAdapter(rawDB, instruments); store != nil {
		localIdentity.Store = store
	}
	localIdentity.Sessions = newCoreBrowserSessionStore(rawDB, instruments)
	browserSessions := newCoreBrowserSessionHandler(rawDB, instruments, cookieSecureMode)

	adminIdentityReads := &query.AdminIdentityReadHandler{}
	if store := newCoreAdminIdentityReadAdapter(rawDB, instruments); store != nil {
		adminIdentityReads.Store = store
	}
	if reader := newCoreAdminGovernanceAuditReaderWithReadStore(
		newCoreReadQueryer(rawDB), instruments, governanceAudit, logger,
	); reader != nil {
		adminIdentityReads.Audit = reader
	}
	adminIdentityMutations := newCoreAdminIdentityMutationHandler(
		unwrapNilAdmissionPort(newCorePostgresAdminIdentityMutationAdapter(rawDB, instruments)),
		governanceAudit,
	)

	signInPolicyReads := newCoreSignInPolicyReadHandler(rawDB, instruments)
	var signInPolicyAdmission query.SignInPolicyAdmission
	if adapter := newCorePostgresSignInPolicyAdapter(rawDB, instruments); adapter != nil {
		signInPolicyAdmission = adapter
	}
	signInPolicyMutations := newCoreSignInPolicyMutationHandler(signInPolicyAdmission, instruments, governanceAudit)
	localIdentity.SignInPolicy = signInPolicyReads.Store
	localIdentity.Instruments = instruments
	if adapter, ok := localIdentity.Store.(*corePostgresLocalIdentityAdapter); ok && adapter != nil {
		adapter.setCoreTOTPSecretKeyring(keyring)
	}

	setup := newCoreSetupHandler(
		unwrapNilAdmissionPort(newCorePostgresSetupAdapter(rawDB, keyring, instruments)),
		rawDB, instruments, governanceAudit, cookieSecureMode, bootstrapMode,
	)

	oidcLoginHandler, err := newCoreOIDCLoginHandler(getenv, rawDB, instruments, keyring, logger)
	if err != nil {
		return nil, fmt.Errorf("configure oidc login: %w", err)
	}
	if oidcLoginHandler == nil {
		oidcLoginHandler = &query.OIDCLoginHandler{}
	} else {
		oidcLoginHandler.Audit = audit
	}

	sessionAdapter := newCorePostgresBrowserSessionAdapter(rawDB, instruments)
	var samlSessions query.BrowserSessionStore
	if sessionAdapter != nil {
		samlSessions = sessionAdapter
	}
	samlHandler, err := newCoreSAMLHandler(
		rawDB, instruments, getenv, samlSessions, cookieSecureMode, keyring,
	)
	if err != nil {
		return nil, fmt.Errorf("configure saml sso: %w", err)
	}
	if samlHandler == nil {
		samlHandler = &query.SAMLHandler{}
	} else {
		if sessionAdapter != nil {
			samlHandler.SignInPolicy = sessionAdapter
		}
		samlHandler.Audit = audit
	}

	githubLoginHandler, err := newCoreGitHubLoginHandler(getenv, rawDB, instruments, keyring)
	if err != nil {
		return nil, fmt.Errorf("configure github login: %w", err)
	}
	if githubLoginHandler == nil {
		githubLoginHandler = &query.GitHubLoginHandler{}
	} else {
		githubLoginHandler.Audit = audit
	}

	authProviders := &query.AuthProviderListHandler{
		Store: newCoreAuthProviderListStore(rawDB, samlHandler, oidcLoginHandler, githubLoginHandler),
	}
	if sessionAdapter != nil {
		authProviders.Policy = sessionAdapter
	}

	return &query.CoreAuthWiring{
		LocalIdentity:            localIdentity,
		BrowserSessions:          browserSessions,
		SessionList:              newCoreBrowserSessionListHandler(rawDB, instruments),
		AdminIdentityReads:       adminIdentityReads,
		AdminIdentityMutations:   adminIdentityMutations,
		Profile:                  newCoreProfileHandler(rawDB, instruments),
		SignInPolicyReads:        signInPolicyReads,
		SignInPolicyMutations:    signInPolicyMutations,
		Setup:                    setup,
		OIDCLogin:                oidcLoginHandler,
		SAML:                     samlHandler,
		GitHubLogin:              githubLoginHandler,
		AuthProviders:            authProviders,
		AdminProviderConfigReads: newCoreAdminProviderConfigReadHandler(rawDB, oidcLoginHandler, samlHandler, logger),
		AdminProviderConfigMutations: newCoreAdminProviderConfigMutationHandler(
			rawDB, governanceAudit, keyring,
			newCoreProviderConfigConnectionTester(rawDB, keyring),
			oidcLoginHandler, samlHandler, logger,
		),
	}, nil
}

// newCoreReadQueryer returns the read store as a queryer, or nil when the
// database is unwired.
func newCoreReadQueryer(rawDB *sql.DB) db.Queryer {
	if rawDB == nil {
		return nil
	}
	return pgstatus.NewSQLReadStore(rawDB)
}

// componentPolicyFromCoreEnv builds the component trust policy from env.
func componentPolicyFromCoreEnv(getenv func(string) string) component.Policy {
	if getenv == nil {
		return component.Policy{}
	}
	policy := component.Policy{
		Mode:              strings.TrimSpace(getenv("ESHU_COMPONENT_TRUST_MODE")),
		AllowedIDs:        splitCoreEnvList(getenv("ESHU_COMPONENT_ALLOW_IDS")),
		AllowedPublishers: splitCoreEnvList(getenv("ESHU_COMPONENT_ALLOW_PUBLISHERS")),
		RevokedIDs:        splitCoreEnvList(getenv("ESHU_COMPONENT_REVOKE_IDS")),
		RevokedPublishers: splitCoreEnvList(getenv("ESHU_COMPONENT_REVOKE_PUBLISHERS")),
		CoreVersion:       strings.TrimSpace(getenv("ESHU_COMPONENT_CORE_VERSION")),
	}
	return component.ConfigureProvenanceFromEnv(policy, getenv)
}

func splitCoreEnvList(raw string) []string {
	var out []string
	for _, field := range strings.Split(raw, ",") {
		if value := strings.TrimSpace(field); value != "" {
			out = append(out, value)
		}
	}
	return out
}

// mountCoreCLISupport registers the CLI-support endpoints (packet render,
// report compose, bundle capture/validate, answer-quality score). They live
// here rather than in the query constructor because the handler package
// imports query.
func mountCoreCLISupport(mux *http.ServeMux, _ *query.APIRouter) {
	(&support.Handlers{}).Mount(mux)
}

// mountCoreServiceIntelligence registers the service intelligence report
// route over durable Postgres evidence sources. It lives here because the
// handler package imports query. readImpactFromWinners carries the #10880
// slice 2b aggregate read gate so the report route follows the same
// cutover as the count and inventory routes.
func mountCoreServiceIntelligence(rawDB *sql.DB, logger *slog.Logger, readImpactFromWinners bool) func(*http.ServeMux, *query.APIRouter) {
	return func(mux *http.ServeMux, router *query.APIRouter) {
		if router == nil {
			return
		}
		reader := pgstatus.NewSQLReadStore(rawDB)
		supplyChainAggregates := impact.NewPostgresAggregateStoreWithReadStore(reader, readImpactFromWinners)
		// The probe-failure WARN names the deployment the 503 blames, so a
		// lagging maintainer is distinguishable from an unwired route.
		supplyChainAggregates.Logger = logger
		(&serviceintelhttp.ReportHandler{
			Entities: router.Entities,
			Incidents: serviceintelhttp.NewDurableIncidentEvidenceSource(
				pgstatus.NewServiceCatalogIDResolver(reader),
				pgstatus.NewServiceIncidentEvidenceLoader(reader),
				logger,
			),
			SupplyChain: serviceintelhttp.NewDurableSupplyChainEvidenceSource(
				supplyChainAggregates,
				logger,
			),
		}).Mount(mux)
	}
}

// mountCoreAsk mounts the ask engine handler built by main.
func mountCoreAsk(handler *ask.Handler) func(*http.ServeMux, *query.APIRouter) {
	return func(mux *http.ServeMux, _ *query.APIRouter) {
		if handler == nil {
			return
		}
		handler.Mount(mux)
	}
}

// mountCoreMetrics exposes the Prometheus handler on the bare mux (public
// via publicHTTPPaths, sharing the auth chain like every other route).
// A nil handler mounts nothing.
func mountCoreMetrics(prometheusHandler http.Handler) func(*http.ServeMux, *query.APIRouter) {
	return func(mux *http.ServeMux, _ *query.APIRouter) {
		if prometheusHandler == nil {
			return
		}
		mux.Handle("/metrics", prometheusHandler)
	}
}

// isKeyNotConfigured reports the empty-DEK sentinel from KeyringFromEnv.
func isKeyNotConfigured(err error) bool {
	return err != nil && strings.Contains(err.Error(), "not configured")
}

// startCoreOIDCSessionRefresh starts the background OIDC active-session
// revocation refresh worker when configured. A nil worker (the feature is
// off) starts nothing and reports no error.
func startCoreOIDCSessionRefresh(
	ctx context.Context,
	getenv func(string) string,
	rawDB *sql.DB,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) error {
	worker, err := CoreOIDCSessionRefreshWorker(getenv, rawDB, instruments, logger)
	if err != nil {
		return fmt.Errorf("configure oidc session refresh: %w", err)
	}
	if worker == nil {
		return nil
	}
	go worker.Run(ctx)
	logger.Info("oidc session refresh worker started",
		telemetry.EventAttr("auth.oidc.session_refresh.started"))
	return nil
}

// v1PrefixAliasMiddleware rewrites /api/v1/* paths to /api/v0/* before auth
// so scoped-token and browser-session route classification sees the v0 path.
func v1PrefixAliasMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/v1/") {
			r2 := r.Clone(r.Context())
			u := *r.URL
			u.Path = "/api/v0/" + r.URL.Path[len("/api/v1/"):]
			r2.URL = &u
			next.ServeHTTP(w, r2)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// wrapCoreAPIMiddleware is the exact outer /api chain serve() installs:
// the deprecation headers outside the v1 alias rewrite, so the header
// check sees the original /api/v1/ spelling and /api/v1/ requests never
// carry Deprecation/Sunset. Only direct /api/v0/ requests do.
func wrapCoreAPIMiddleware(handler http.Handler, sunset string) http.Handler {
	return deprecationHeadersMiddleware(v1PrefixAliasMiddleware(handler), sunset)
}

// deprecationHeadersMiddleware adds Deprecation/Sunset headers to /api/v0/*
// responses. It runs outside the v1 rewrite so /api/v1/ requests never
// carry the headers.
func deprecationHeadersMiddleware(next http.Handler, sunset string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/v0/") {
			w.Header().Set("Deprecation", "true")
			w.Header().Set("Sunset", sunset)
		}
		next.ServeHTTP(w, r)
	})
}

// readCoreSunsetDate reads the Sunset header date, defaulting to the pinned
// value on unset or unparsable input.
func readCoreSunsetDate(getenv func(string) string) string {
	const defaultDate = "Thu, 01 Jul 2027 00:00:00 GMT"
	if getenv == nil {
		return defaultDate
	}
	if v := strings.TrimSpace(getenv("ESHU_API_V0_SUNSET_DATE")); v != "" {
		if _, err := time.Parse(time.RFC1123, v); err == nil {
			return v
		}
	}
	return defaultDate
}

// coreShutdownTimeout reads the graceful shutdown timeout, defaulting to
// 30 s on unset or invalid input.
func coreShutdownTimeout(getenv func(string) string) time.Duration {
	if getenv != nil {
		if raw := strings.TrimSpace(getenv("ESHU_API_SHUTDOWN_TIMEOUT")); raw != "" {
			if d, err := time.ParseDuration(raw); err == nil {
				return d
			}
		}
	}
	return defaultShutdown
}
