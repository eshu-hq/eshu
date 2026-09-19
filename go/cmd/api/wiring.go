// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	_ "github.com/jackc/pgx/v5/stdlib"
	"go.opentelemetry.io/otel"

	"github.com/eshu-hq/eshu/go/internal/query"
	internalruntime "github.com/eshu-hq/eshu/go/internal/runtime"
	"github.com/eshu-hq/eshu/go/internal/scopedtoken"
	"github.com/eshu-hq/eshu/go/internal/searchembedruntime"
	"github.com/eshu-hq/eshu/go/internal/secretcrypto"
	"github.com/eshu-hq/eshu/go/internal/semanticpolicy"
	"github.com/eshu-hq/eshu/go/internal/semanticprofile"
	"github.com/eshu-hq/eshu/go/internal/serviceintelhttp"
	"github.com/eshu-hq/eshu/go/internal/status"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

var (
	_ query.GraphQuery   = (*query.Neo4jReader)(nil)
	_ query.ContentStore = (*query.ContentReader)(nil)
)

const envSemanticSearchLocalEmbedder = searchembedruntime.EnvLocalEmbedder

func wireAPI(
	ctx context.Context,
	getenv func(string) string,
	logger *slog.Logger,
	prometheusHandler http.Handler,
) (http.Handler, func(), *telemetry.Instruments, error) {
	queryProfile, err := loadQueryProfile(getenv)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("load query profile: %w", err)
	}
	graphBackend, err := loadGraphBackend(getenv)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("load graph backend: %w", err)
	}
	semanticProviderProfiles, err := semanticprofile.LoadStatusesFromEnv(getenv)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("load semantic provider profiles: %w", err)
	}
	semanticPolicy, err := semanticpolicy.LoadFromEnv(getenv)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("load semantic extraction policy: %w", err)
	}
	semanticSearchEmbedding, err := searchembedruntime.ConfigFromEnv(getenv, nil)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("load semantic search embedder: %w", err)
	}
	semanticProviderProfiles = semanticpolicy.ApplyToProviderStatuses(
		semanticProviderProfiles,
		semanticPolicy,
	)

	apiKey, err := internalruntime.ResolveAPIKey(getenv)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("resolve api key: %w", err)
	}
	// fileScopedTokenResolver is captured in its own variable (not reused as
	// scopedTokenResolver) so it survives the ChainResolvers merge below and
	// can feed the authEnforcementConfigured predicate. Renamed to mirror
	// F-7's cmd/mcp-server rename (#5168) for a trivial future merge.
	fileScopedTokenResolver, err := scopedtoken.ResolverFromEnv(getenv)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("resolve scoped token registry: %w", err)
	}
	governanceStatus := query.GovernanceStatusConfigFromEnv(getenv, apiKey != "")

	// Validate the Postgres pool config before dialing any datastore, so an invalid
	// ESHU_POSTGRES_MAX_OPEN_CONNS/idle/lifetime is reported regardless of graph
	// backend availability (validation-before-datastore invariant). It is applied
	// after sql.Open below.
	pgPoolCfg, err := internalruntime.LoadPostgresConfig(getenv)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("load postgres pool config: %w", err)
	}

	driver, neo4jDB, err := openQueryGraph(ctx, getenv, queryProfile, logger)
	if err != nil {
		return nil, nil, nil, err
	}

	// Open Postgres using pgx driver
	pgDSN := envOrDefault(getenv, "ESHU_POSTGRES_DSN",
		envOrDefault(getenv, "ESHU_CONTENT_STORE_DSN", ""))
	if pgDSN == "" {
		if driver != nil {
			_ = driver.Close(ctx)
		}
		return nil, nil, nil, fmt.Errorf("ESHU_POSTGRES_DSN or ESHU_CONTENT_STORE_DSN is required")
	}

	rawDB, err := sql.Open("pgx", pgDSN)
	if err != nil {
		if driver != nil {
			_ = driver.Close(ctx)
		}
		return nil, nil, nil, fmt.Errorf("open postgres: %w", err)
	}
	// Bound the pool to the shared per-process ceiling (validated above, before the
	// graph dial). Without this the api pool is database/sql-default unbounded, which
	// would let a read burst exceed the whole-stack connection budget (#4456). Only
	// the pool sizes are applied; the DSN resolved above is kept.
	internalruntime.ConfigurePostgresPool(rawDB, pgPoolCfg)
	if err := rawDB.PingContext(ctx); err != nil {
		_ = rawDB.Close()
		if driver != nil {
			_ = driver.Close(ctx)
		}
		return nil, nil, nil, fmt.Errorf("ping postgres: %w", err)
	}
	if logger != nil {
		logger.Info("postgres connected", telemetry.EventAttr("runtime.postgres.connected"))
	}
	// identityResolver is kept separate from scopedTokenResolver (the
	// file-registry resolver loaded above) until after instruments exists
	// below: the IdP bearer resolver (#5162) needs instruments and must sit
	// BETWEEN identity and file-registry in the chain
	// (identity -> bearer -> file), so the three-way ChainResolvers call is
	// deferred to just after instruments is built instead of assembled here.
	identityResolver := scopedtoken.NewPostgresIdentityResolver(pgstatus.NewScopedAPITokenStore(pgstatus.SQLDB{DB: rawDB}))

	// Build instruments before the status reader so the StatusStore can carry
	// the shared meter provider (see newStatusStore): the per-read status
	// metric eshu_dp_status_snapshot_read_duration_seconds only emits when the
	// operator status-serving StatusStore has Instruments wired.
	instruments, err := telemetry.NewInstruments(otel.Meter(telemetry.DefaultSignalName))
	if err != nil {
		_ = rawDB.Close()
		if driver != nil {
			_ = driver.Close(ctx)
		}
		return nil, nil, nil, fmt.Errorf("register query instruments: %w", err)
	}
	// Construct the universal graph-read policy only after instruments exist.
	// The startup owner-ledger backfill uses this same bounded reader, so a slow
	// graph cannot hold process startup beyond the per-read budget.
	neo4jReader := query.NewNeo4jReader(
		driver,
		neo4jDB,
		query.WithNeo4jReaderObservability(logger, instruments),
	)
	contentReader := query.NewContentReader(rawDB)
	// #5563 upgrade gate: seed pre-ledger CloudResource graph rows before the
	// indexed owner-ledger list path is mounted, then start the #6793 infra read
	// model backfill in the background. Graph-disabled profiles skip both.
	if driver != nil {
		if err := query.RunStartupBackfills(ctx, rawDB, neo4jReader, logger, instruments); err != nil {
			_ = rawDB.Close()
			_ = driver.Close(ctx)
			return nil, nil, nil, fmt.Errorf("backfill cloud resource owner ledger: %w", err)
		}
	}
	statusReader := status.WithSemanticProviderProfiles(
		newStatusStore(newStatusQueryer(rawDB, instruments), instruments),
		semanticProviderProfiles...,
	)
	metricsSource, err := metricsTimeSeriesSourceFromEnv(getenv, nil)
	if err != nil {
		_ = rawDB.Close()
		if driver != nil {
			_ = driver.Close(ctx)
		}
		return nil, nil, nil, fmt.Errorf("configure metrics time-series source: %w", err)
	}
	governanceAudit := newGovernanceAuditStore(rawDB, instruments, logger)

	// IdP bearer-token resolver (#5162): validates an IdP-issued OAuth2
	// access token presented as Authorization: Bearer <token> against the
	// canonical Eshu resource URI (ESHU_AUTH_RESOURCE_URI). Returns
	// (nil, nil) when that env var is unset, which is what makes the
	// three-way chain below degrade to the pre-#5162 identity -> file chain
	// on a token-only deployment.
	oidcBearerResolver, err := newOIDCBearerResolver(ctx, getenv, rawDB, instruments, logger)
	if err != nil {
		_ = rawDB.Close()
		if driver != nil {
			_ = driver.Close(ctx)
		}
		return nil, nil, nil, fmt.Errorf("construct oidc bearer resolver: %w", err)
	}
	// Headerless dev-open vs. enforced posture; see auth_enforcement.go.
	enforcement := authEnforcementConfigured(apiKey, fileScopedTokenResolver, oidcBearerResolver)
	scopedTokenResolver := scopedtoken.ChainResolvers(identityResolver, oidcBearerResolver, fileScopedTokenResolver)
	logAuthEnforcementPosture(logger, enforcement)

	// Bootstrap identity seeding (epic #4962, issue #4963): seed the first
	// local owner/admin identity exactly once before the router mounts any
	// auth-gated route. Fails closed on any seeding error, matching every
	// other wiring step in this function.
	seedIdentityDB := db.ExecQueryer(pgstatus.SQLDB{DB: rawDB})
	if instruments != nil {
		seedIdentityDB = &pgstatus.InstrumentedDB{
			Inner:       seedIdentityDB,
			Tracer:      otel.Tracer(telemetry.DefaultSignalName),
			Instruments: instruments,
			StoreName:   "identity_bootstrap_credential",
		}
	}
	if err := seedInitialAdmin(ctx, seedIdentityDB, getenv, instruments, logger, adminRecoveryAuditAppender(governanceAudit)); err != nil {
		_ = rawDB.Close()
		if driver != nil {
			_ = driver.Close(ctx)
		}
		return nil, nil, nil, fmt.Errorf("seed initial admin: %w", err)
	}

	componentHome := strings.TrimSpace(getenv("ESHU_COMPONENT_HOME"))
	componentPolicy := componentPolicyFromEnv(getenv)
	readImpactFromWinners := query.SupplyChainImpactWinnersReadEnabled(getenv(query.SupplyChainImpactWinnersReadEnv))
	cookieSecureMode, err := query.ValidateCookieSecureMode(getenv(query.CookieSecureModeEnv))
	if err != nil {
		_ = rawDB.Close()
		if driver != nil {
			_ = driver.Close(ctx)
		}
		return nil, nil, nil, fmt.Errorf("configure cookie secure mode: %w", err)
	}
	browserSessionAdapter := newPostgresBrowserSessionAdapter(rawDB, instruments)
	router, err := newRouterWithSemanticEmbedding(
		rawDB,
		neo4jReader,
		contentReader,
		statusReader,
		metricsSource,
		queryProfile,
		graphBackend,
		logger,
		instruments,
		semanticSearchEmbedding,
		componentHome,
		componentPolicy,
		governanceStatus,
		governanceAudit,
		readImpactFromWinners,
		cookieSecureMode,
	)
	if err != nil {
		_ = rawDB.Close()
		if driver != nil {
			_ = driver.Close(ctx)
		}
		return nil, nil, nil, fmt.Errorf("new router: %w", err)
	}
	// Provider-config secret keyring (#4966, epic #4962). ErrKeyNotConfigured
	// is non-fatal at boot: a deployment that never creates a DB-backed
	// provider config with a secret does not need a DEK. Any OTHER error
	// (malformed key, wrong length) is fatal — a misconfigured DEK must not
	// silently degrade to "no encryption" for a feature that seals secrets.
	// See secretcrypto/README.md's "Behavior when unset: FAIL CLOSED" section.
	// Built before newOIDCLoginHandler because the OIDC login runtime needs it
	// to open DB-backed provider secrets at token-exchange time.
	providerSecretKeyring, err := secretcrypto.KeyringFromEnv(getenv)
	if err != nil && !errors.Is(err, secretcrypto.ErrKeyNotConfigured) {
		_ = rawDB.Close()
		if driver != nil {
			_ = driver.Close(ctx)
		}
		return nil, nil, nil, fmt.Errorf("configure provider secret keyring: %w", err)
	}
	if errors.Is(err, secretcrypto.ErrKeyNotConfigured) {
		providerSecretKeyring = nil
		if logger != nil {
			logger.Info(
				"provider config secret encryption key not configured; provider-config secret writes will fail closed until ESHU_AUTH_SECRET_ENC_KEY(_FILE) is set",
				telemetry.EventAttr("auth.provider_config.keyring_unconfigured"),
			)
		}
	}

	// TOTP MFA secret keyring (#4986). Reuses providerSecretKeyring — same
	// ESHU_AUTH_SECRET_ENC_KEY(_FILE) DEK source, distinct AAD scheme (see
	// identity_local_totp.go's totpSecretAAD) — so a nil keyring here (DEK
	// unconfigured) is not fatal; TOTP enrollment and login-time
	// verification both fail closed with ErrLocalIdentityTOTPKeyringUnavailable.
	// Wired via a type assertion on router.LocalIdentity.Store rather than a
	// newLocalIdentityHandler parameter because that handler is constructed
	// by newRouterWithSemanticEmbedding above, before providerSecretKeyring
	// exists — the same reason router.Setup and newOIDCLoginHandler below
	// are wired post-construction instead of threaded through the router
	// constructor's parameter list.
	if adapter, ok := router.LocalIdentity.Store.(*postgresLocalIdentityAdapter); ok {
		adapter.setTOTPSecretKeyring(providerSecretKeyring)
	}

	// Tag-history cursor sealing (#6564): the same DEK under its own AAD
	// scheme, so a nil keyring is not fatal -- grant-filtered paging fails
	// closed and unscoped callers are unaffected. The guard is required, not
	// defensive: a nil *Keyring in the Sealer field is a NON-nil Sealer.
	if providerSecretKeyring != nil {
		router.TagHistory.Cursors = providerSecretKeyring
	}

	// First-run setup wizard (#4965). Reuses providerSecretKeyring — the same
	// ESHU_AUTH_SECRET_ENC_KEY(_FILE) material seed_initial_admin.go sealed
	// the bootstrap credential envelope with — so a nil keyring here (DEK
	// unconfigured) is not fatal; VerifyBootstrapCredential fails closed.
	bootstrapMode, err := loadAuthBootstrapMode(getenv)
	if err != nil {
		_ = rawDB.Close()
		if driver != nil {
			_ = driver.Close(ctx)
		}
		return nil, nil, nil, fmt.Errorf("configure setup wizard: %w", err)
	}
	router.Setup = newSetupHandler(rawDB, providerSecretKeyring, instruments, governanceAudit, cookieSecureMode, bootstrapMode)

	oidcLoginHandler, err := newOIDCLoginHandler(getenv, rawDB, instruments, providerSecretKeyring, logger)
	if err != nil {
		_ = rawDB.Close()
		if driver != nil {
			_ = driver.Close(ctx)
		}
		return nil, nil, nil, fmt.Errorf("configure oidc login: %w", err)
	}
	if oidcLoginHandler != nil {
		// Governance audit for SSO callback outcomes (issue #5601): reuses
		// the same governanceAudit appender local login, break-glass, and
		// bootstrap already write identity_authentication events through.
		oidcLoginHandler.Audit = adminRecoveryAuditAppender(governanceAudit)
	}
	router.OIDCLogin = oidcLoginHandler
	oidcSessionRefreshWorker, err := newOIDCSessionRefreshWorker(getenv, rawDB, instruments, logger)
	if err != nil {
		_ = rawDB.Close()
		if driver != nil {
			_ = driver.Close(ctx)
		}
		return nil, nil, nil, fmt.Errorf("configure oidc session refresh: %w", err)
	}
	if oidcSessionRefreshWorker != nil {
		go oidcSessionRefreshWorker.Run(ctx)
		if logger != nil {
			logger.Info(
				"oidc session refresh worker started",
				telemetry.EventAttr("auth.oidc.session_refresh.started"),
			)
		}
	}
	samlHandler, err := newSAMLHandler(rawDB, instruments, getenv, browserSessionAdapter, cookieSecureMode, providerSecretKeyring)
	if err != nil {
		_ = rawDB.Close()
		if driver != nil {
			_ = driver.Close(ctx)
		}
		return nil, nil, nil, fmt.Errorf("configure saml sso: %w", err)
	}
	router.SAML = samlHandler
	if samlHandler != nil {
		// Per-tenant session timeout override (issue #4968, epic #4962):
		// browserSessionAdapter already implements query.SignInPolicyReadStore
		// (see cmd/api/browser_sessions.go's GetSignInPolicy), so SAML's
		// session issuance resolves the same override BrowserSessionHandler
		// and LocalIdentityHandler do.
		samlHandler.SignInPolicy = browserSessionAdapter
		// Governance audit for SSO callback outcomes (issue #5601).
		samlHandler.Audit = adminRecoveryAuditAppender(governanceAudit)
	}
	githubLoginHandler, err := newGitHubLoginHandler(getenv, rawDB, instruments, providerSecretKeyring)
	if err != nil {
		_ = rawDB.Close()
		if driver != nil {
			_ = driver.Close(ctx)
		}
		return nil, nil, nil, fmt.Errorf("configure github login: %w", err)
	}
	if githubLoginHandler != nil {
		// Governance audit for SSO callback outcomes (issue #5601).
		githubLoginHandler.Audit = adminRecoveryAuditAppender(governanceAudit)
	}
	router.GitHubLogin = githubLoginHandler
	authProviders := &query.AuthProviderListHandler{
		Store: newAuthProviderListStore(rawDB, samlHandler, oidcLoginHandler, githubLoginHandler),
	}
	if browserSessionAdapter != nil {
		// browserSessionAdapter already implements query.SignInPolicyReadStore
		// (see its use for samlHandler.SignInPolicy above and
		// cmd/api/browser_sessions.go's GetSignInPolicy) — reusing it here
		// keeps the derived AuthPosture's local_login_offered field backed by
		// the exact same sign-in-policy read path require_sso enforcement
		// elsewhere in this file uses, rather than a second adapter instance.
		// The explicit nil check (rather than assigning unconditionally, as
		// samlHandler.SignInPolicy does above) avoids handing
		// AuthProviderListHandler a non-nil query.SignInPolicyReadStore
		// interface wrapping a nil *postgresBrowserSessionAdapter — which
		// newPostgresBrowserSessionAdapter returns when db == nil — since
		// DeriveAuthPosture's own nil-store check (policy != nil) cannot see
		// through that wrapping and would otherwise call a method that
		// dereferences the nil receiver.
		authProviders.Policy = browserSessionAdapter
	}
	router.AuthProviders = authProviders

	providerConfigTester := newProviderConfigConnectionTester(rawDB, providerSecretKeyring)
	router.AdminProviderConfigReads = newAdminProviderConfigReadHandler(rawDB, oidcLoginHandler, samlHandler, logger)
	router.AdminProviderConfigMutations = newAdminProviderConfigMutationHandler(rawDB, governanceAudit, providerSecretKeyring, providerConfigTester, oidcLoginHandler, samlHandler, logger)

	// Tenant sign-in policy (epic #4962, issue #4968): built before
	// router.LocalIdentity below so its SignInPolicyReadStore can be wired
	// into the local-login require_sso gate in the same call.
	router.SignInPolicyReads = newSignInPolicyReadHandler(rawDB, instruments)
	router.SignInPolicyMutations = newSignInPolicyMutationHandler(rawDB, instruments, governanceAudit)
	router.LocalIdentity.SignInPolicy = router.SignInPolicyReads.Store
	router.LocalIdentity.Instruments = instruments

	apiMux := http.NewServeMux()
	router.Mount(apiMux)
	browserSessionResolver := newBrowserSessionResolver(rawDB, instruments)

	// The Ask engine's in-process runner must dispatch inner tool calls through
	// the scoped-auth-wrapped handler (authedMux below) so each inner read
	// re-runs the scoped-route gate under the caller's token. That handler does
	// not exist yet (it wraps this mux), so wire the runner to a deferred handler
	// now and install authedMux into it once built.
	askInnerHandler := &deferredHandler{}
	mountAskAndNarration(getenv, apiMux, askInnerHandler, apiKey, router.Status, logger)

	// Mount the service intelligence report route. It lives in its own package
	// (which imports both query and serviceintel) and is mounted here rather than
	// by the query router, so query never depends on serviceintel — no cycle. The
	// incidents_support section is sourced from durable incident-routing evidence
	// (catalog-service-id resolver + incident evidence loader, both over Postgres).
	(&serviceintelhttp.ReportHandler{
		Entities:    router.Entities,
		Incidents:   newIncidentEvidenceSource(rawDB, logger),
		SupplyChain: newSupplyChainEvidenceSource(rawDB, logger),
	}).Mount(apiMux)

	// Record per-endpoint duration/error metrics for every API route. The
	// middleware wraps the application mux only; the admin surface (probes,
	// /metrics) is mounted separately and is intentionally not counted.
	instrumentedAPI := query.RequestMetricsMiddleware(apiMux)

	apiHandler := http.Handler(instrumentedAPI)
	oidcRateLimiter := newOIDCRateLimiter(getenv, instruments)
	if oidcRateLimiter != nil {
		apiHandler = oidcRateLimiter.Middleware(apiHandler)
	}

	mux, err := mountRuntimeSurface(apiHandler, "eshu-api", statusReader, prometheusHandler, rawDB, driver)
	if err != nil {
		_ = rawDB.Close()
		if driver != nil {
			_ = driver.Close(ctx)
		}
		return nil, nil, nil, fmt.Errorf("mount runtime surface: %w", err)
	}

	authedMux := wrapAPIAuth(
		apiKey,
		scopedTokenResolver,
		browserSessionResolver,
		mux,
		adminRecoveryAuditAppender(governanceAudit),
		governanceStatus,
		enforcement,
	)

	// Rewrite /api/v1/* to /api/v0/* before auth so scoped-token and
	// browser-session route classification sees the v0 path.
	// Must wrap authedMux (not be wrapped by it) because wrapAPIAuth
	// puts auth as the outer layer.
	v1Rewritten := v1PrefixAliasMiddleware(authedMux)

	// Add Deprecation + Sunset headers to /api/v0/* responses. Must be
	// the outermost layer so it sees the original request path before
	// the v1→v0 rewrite transforms /api/v1/* paths.
	sunsetDate := readSunsetDate(getenv)
	final := deprecationHeadersMiddleware(v1Rewritten, sunsetDate)

	// Install the fully-wrapped handler into the Ask runner's deferred
	// handler so inner tool dispatches re-run auth + the scoped-route
	// gate under the caller's token.
	askInnerHandler.Set(final)

	cleanup := func() {
		_ = rawDB.Close()
		if driver != nil {
			_ = driver.Close(context.Background())
		}
	}

	return final, cleanup, instruments, nil
}
