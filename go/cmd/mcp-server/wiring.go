// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
	"go.opentelemetry.io/otel"

	"github.com/eshu-hq/eshu/go/internal/governanceauditasync"
	"github.com/eshu-hq/eshu/go/internal/query"
	internalruntime "github.com/eshu-hq/eshu/go/internal/runtime"
	"github.com/eshu-hq/eshu/go/internal/scopedtoken"
	"github.com/eshu-hq/eshu/go/internal/searchembedruntime"
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

func wireAPI(
	ctx context.Context,
	getenv func(string) string,
	logger *slog.Logger,
	prometheusHandler http.Handler,
) (http.Handler, *http.ServeMux, func(), mcpAuthWiring, error) {
	queryProfile, err := loadQueryProfile(getenv)
	if err != nil {
		return nil, nil, nil, mcpAuthWiring{}, fmt.Errorf("load query profile: %w", err)
	}
	graphBackend, err := loadGraphBackend(getenv)
	if err != nil {
		return nil, nil, nil, mcpAuthWiring{}, fmt.Errorf("load graph backend: %w", err)
	}
	semanticProviderProfiles, err := semanticprofile.LoadStatusesFromEnv(getenv)
	if err != nil {
		return nil, nil, nil, mcpAuthWiring{}, fmt.Errorf("load semantic provider profiles: %w", err)
	}
	semanticPolicy, err := semanticpolicy.LoadFromEnv(getenv)
	if err != nil {
		return nil, nil, nil, mcpAuthWiring{}, fmt.Errorf("load semantic extraction policy: %w", err)
	}
	semanticSearchEmbedding, err := searchembedruntime.ConfigFromEnv(getenv, nil)
	if err != nil {
		return nil, nil, nil, mcpAuthWiring{}, fmt.Errorf("load semantic search embedder: %w", err)
	}
	semanticProviderProfiles = semanticpolicy.ApplyToProviderStatuses(
		semanticProviderProfiles,
		semanticPolicy,
	)

	apiKey, err := internalruntime.ResolveAPIKey(getenv)
	if err != nil {
		return nil, nil, nil, mcpAuthWiring{}, fmt.Errorf("resolve api key: %w", err)
	}
	// fileScopedTokenResolver is the raw ESHU_SCOPED_TOKENS_FILE registry
	// resolver, kept separate (not yet merged with identity/OIDC) so both
	// requireMCPHTTPCredentialSource below and the enforcement predicate
	// (auth_enforcement.go) can see whether THIS specific knob was
	// configured, distinct from the always-wired Postgres identity
	// resolver added to the chain further down.
	fileScopedTokenResolver, err := scopedtoken.ResolverFromEnv(getenv)
	if err != nil {
		return nil, nil, nil, mcpAuthWiring{}, fmt.Errorf("resolve scoped token registry: %w", err)
	}
	governanceStatus := query.GovernanceStatusConfigFromEnv(getenv, apiKey != "")

	// Validate the Postgres pool config before dialing any datastore, so an invalid
	// ESHU_POSTGRES_MAX_OPEN_CONNS/idle/lifetime is reported regardless of graph
	// backend availability (validation-before-datastore invariant). It is applied
	// after sql.Open below.
	pgPoolCfg, err := internalruntime.LoadPostgresConfig(getenv)
	if err != nil {
		return nil, nil, nil, mcpAuthWiring{}, fmt.Errorf("load postgres pool config: %w", err)
	}

	driver, neo4jDB, err := openQueryGraph(ctx, getenv, queryProfile, logger)
	if err != nil {
		return nil, nil, nil, mcpAuthWiring{}, err
	}

	// Open Postgres using pgx driver
	pgDSN := envOrDefault(getenv, "ESHU_POSTGRES_DSN",
		envOrDefault(getenv, "ESHU_CONTENT_STORE_DSN", ""))
	if pgDSN == "" {
		if driver != nil {
			_ = driver.Close(ctx)
		}
		return nil, nil, nil, mcpAuthWiring{}, fmt.Errorf("ESHU_POSTGRES_DSN or ESHU_CONTENT_STORE_DSN is required")
	}

	db, err := sql.Open("pgx", pgDSN)
	if err != nil {
		if driver != nil {
			_ = driver.Close(ctx)
		}
		return nil, nil, nil, mcpAuthWiring{}, fmt.Errorf("open postgres: %w", err)
	}
	// Bound the pool to the shared per-process ceiling (validated above, before the
	// graph dial). Without this the mcp-server pool is database/sql-default
	// unbounded, which would let a read burst exceed the whole-stack connection
	// budget (#4456). Only the pool sizes are applied; the DSN resolved above is kept.
	internalruntime.ConfigurePostgresPool(db, pgPoolCfg)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		if driver != nil {
			_ = driver.Close(ctx)
		}
		return nil, nil, nil, mcpAuthWiring{}, fmt.Errorf("ping postgres: %w", err)
	}
	if logger != nil {
		logger.Info("postgres connected", telemetry.EventAttr("runtime.postgres.connected"))
	}
	// identityResolver is kept separate from fileScopedTokenResolver (the
	// file-registry resolver loaded above) until after instruments exists
	// below: the IdP bearer resolver (#5162) needs instruments and must sit
	// BETWEEN identity and file-registry in the chain
	// (identity -> bearer -> file), matching cmd/api's wiring exactly.
	identityResolver := scopedtoken.NewPostgresIdentityResolver(pgstatus.NewScopedAPITokenStore(pgstatus.SQLDB{DB: db}))
	instruments, err := telemetry.NewInstruments(otel.Meter("mcp-server"))
	if err != nil {
		_ = db.Close()
		if driver != nil {
			_ = driver.Close(ctx)
		}
		return nil, nil, nil, mcpAuthWiring{}, fmt.Errorf("register query instruments: %w", err)
	}

	// IdP bearer-token resolver (#5162): see cmd/api's identical wiring
	// comment. Returns (nil, nil) when ESHU_AUTH_RESOURCE_URI is unset.
	oidcBearerResolver, err := newOIDCBearerResolver(ctx, getenv, db, instruments, logger)
	if err != nil {
		_ = db.Close()
		if driver != nil {
			_ = driver.Close(ctx)
		}
		return nil, nil, nil, mcpAuthWiring{}, fmt.Errorf("construct oidc bearer resolver: %w", err)
	}
	// authSourceConfigured is the single wiring-time predicate (see
	// authEnforcementConfigured, auth_enforcement.go) feeding BOTH gates that
	// used to compute this independently: requireMCPHTTPCredentialSource's "no
	// silent open mode over HTTP" startup gate (issue #5168) below, and the
	// per-request headerless-dev-open gate inside authMiddleware (the
	// auth-headerless-bypass hardening) via authedHandler and transportAuth
	// further down. True when at least one of the three explicit,
	// operator-facing credential knobs is set. See mcpAuthWiring's doc comment
	// for why the always-wired identityResolver itself is deliberately
	// excluded from this signal.
	authSourceConfigured := authEnforcementConfigured(apiKey, fileScopedTokenResolver, oidcBearerResolver)
	scopedTokenResolver := scopedtoken.ChainResolvers(identityResolver, oidcBearerResolver, fileScopedTokenResolver)
	logAuthEnforcementPosture(logger, authSourceConfigured)

	// F-2 (issue #5163) OAuth 2.1 discovery. The provider/policy stores feed the
	// SAME DeriveAuthPosture the login picker uses; the issuer lister is the
	// bearer resolver itself (it structurally implements
	// query.OAuthAuthorizationServerLister). oauthChallengePolicy is threaded
	// into the credential middleware below (a nil interface when discovery is
	// disabled, keeping 401s byte-identical), and oauthDiscoveryHandler is
	// mounted unauthenticated on adminMux after mountRuntimeSurface.
	identitySubjectStore := pgstatus.NewIdentitySubjectStore(pgstatus.ExecQueryer(pgstatus.SQLDB{DB: db}))
	var oauthIssuerLister query.OAuthAuthorizationServerLister
	if lister, ok := oidcBearerResolver.(query.OAuthAuthorizationServerLister); ok {
		oauthIssuerLister = lister
	}
	oauthDiscoveryHandler, oauthChallengePolicy := buildMCPOAuthDiscovery(
		getenv,
		&mcpAuthProviderStore{identity: identitySubjectStore},
		&mcpSignInPolicyStore{identity: identitySubjectStore},
		oauthIssuerLister,
		logger,
	)

	// Build query layer
	neo4jReader := query.NewNeo4jReader(
		driver,
		neo4jDB,
		query.WithNeo4jReaderObservability(logger, instruments),
	)
	contentReader := query.NewContentReader(db)
	// #5563 upgrade gate: seed pre-ledger CloudResource graph rows before the
	// indexed owner-ledger list path is mounted. Graph-disabled profiles skip
	// this because the capability is unsupported and no graph can be read.
	if driver != nil {
		if err := query.BackfillCloudResourceOwnerLedger(ctx, db, neo4jReader); err != nil {
			_ = db.Close()
			_ = driver.Close(ctx)
			return nil, nil, nil, mcpAuthWiring{}, fmt.Errorf("backfill cloud resource owner ledger: %w", err)
		}
	}
	statusReader := status.WithSemanticProviderProfiles(
		newStatusStore(pgstatus.SQLQueryer{DB: db}, instruments),
		semanticProviderProfiles...,
	)
	governanceAudit := pgstatus.NewGovernanceAuditStore(pgstatus.SQLDB{DB: db})
	// allowedReadAudit is the F-9 (#5170) allowed-read governance-audit sink:
	// a bounded, non-blocking async appender over the SAME durable
	// governanceAudit store the denial paths already use synchronously. It
	// is wired ONLY into mcpAuthWiring.transportAuth below (never into
	// authedHandler's /api/v0/* middleware), so a scoped-token or
	// OIDC-bearer MCP transport read gets exactly one allowed-read audit
	// event, and Close()d from cleanup before db.Close() so shutdown does
	// not lose the buffered tail. See go/internal/governanceauditasync for
	// the non-blocking design and go/internal/query/auth_audit.go's
	// recordScopedReadAuthorized for the emission point.
	allowedReadAudit := governanceauditasync.NewAsyncAppender(
		governanceAudit,
		governanceauditasync.Metrics{
			Emitted:         instruments.GovernanceAuditAllowedEmitted,
			Dropped:         instruments.GovernanceAuditAllowedDropped,
			PersistFailures: instruments.GovernanceAuditAllowedPersistFailures,
		},
	)

	componentHome := strings.TrimSpace(getenv("ESHU_COMPONENT_HOME"))
	componentPolicy := componentPolicyFromEnv(getenv)
	readImpactFromWinners := query.SupplyChainImpactWinnersReadEnabled(getenv(query.SupplyChainImpactWinnersReadEnv))
	router := newMCPQueryRouterWithSemanticEmbedding(
		db,
		neo4jReader,
		contentReader,
		statusReader,
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
	)

	mux := http.NewServeMux()
	router.Mount(mux)

	// Mount the service intelligence report route so the get_service_intelligence_report
	// MCP tool dispatches to a real handler. It lives in its own package (importing
	// query and serviceintel) and is mounted here, not by the query router, so query
	// never depends on serviceintel — no cycle. The incidents_support section is
	// sourced from durable incident-routing evidence over Postgres.
	(&serviceintelhttp.ReportHandler{
		Entities:    router.Entities,
		Incidents:   newIncidentEvidenceSource(db, logger),
		SupplyChain: newSupplyChainEvidenceSource(db, logger),
	}).Mount(mux)

	// Mount POST /api/v0/ask and wire the governed narration posture. The engine
	// uses the MCP server's own in-process mux as the MCPRunner handler so the
	// engine's tool calls dispatch through this server's routes — the same
	// pattern as cmd/api. Default-off when ESHU_ASK_ENABLED is unset or no
	// agent_reasoning provider profile is configured.
	mountAskAndNarration(getenv, mux, apiKey, router.Status, logger)

	// Record per-endpoint duration/error metrics for every read route, then wrap
	// with auth middleware (shared token + optional scoped-token registry;
	// protects all /api/v0/* routes when mounted by MCP server)
	instrumentedMux := query.RequestMetricsMiddleware(mux)
	// authedHandler does NOT get allowedReadAudit: it is the /api/v0/* HTTP
	// API surface, out of F-9's MCP-transport-only scope (design addendum
	// §2/§6), and tools/call dispatches internally through the same
	// credential chain as the transport middleware below, so wiring it here
	// too would double-emit one logical MCP read.
	authedHandler := buildTransportAuthMiddleware(apiKey, scopedTokenResolver, governanceAudit, authSourceConfigured, oauthChallengePolicy, nil)(instrumentedMux)

	adminMux, err := mountRuntimeSurface("mcp-server", statusReader, prometheusHandler, db, driver)
	if err != nil {
		_ = db.Close()
		if driver != nil {
			_ = driver.Close(ctx)
		}
		return nil, nil, nil, mcpAuthWiring{}, fmt.Errorf("mount runtime surface: %w", err)
	}

	// Mount the unauthenticated RFC 9728 discovery route(s) on the base
	// adminMux, which server.httpMux serves directly (unlike /sse, /mcp/message,
	// and /api/, which are credential-gated). An anonymous MCP client must be
	// able to fetch this document to learn where to authenticate. Nil when
	// discovery is disabled (ESHU_AUTH_RESOURCE_URI unset or invalid).
	if oauthDiscoveryHandler != nil {
		oauthDiscoveryHandler.Mount(adminMux)
	}

	cleanup := func() {
		// Close allowedReadAudit BEFORE db.Close(): its worker's final
		// shutdown flush still needs a live connection, and Close() is
		// bounded (default 5s) so a stuck sink cannot hang shutdown.
		_ = allowedReadAudit.Close()
		_ = db.Close()
		if driver != nil {
			_ = driver.Close(context.Background())
		}
	}

	// authWiring lets main.go authenticate the MCP HTTP transport (GET /sse,
	// POST /mcp/message) with the SAME credential chain AND the SAME
	// authSourceConfigured predicate protecting /api/v0/* and tools/call's
	// internal dispatch (issue #5168, closed for the transport by the
	// auth-headerless-bypass hardening): a scoped-token-file-only or
	// OIDC-only deployment with no ESHU_API_KEY now denies a headerless
	// initialize/tools/list/ping/SSE-establishment request instead of serving
	// it open.
	authWiring := mcpAuthWiring{
		transportAuth:              buildTransportAuthMiddleware(apiKey, scopedTokenResolver, governanceAudit, authSourceConfigured, oauthChallengePolicy, allowedReadAudit),
		credentialSourceConfigured: authSourceConfigured,
	}

	return authedHandler, adminMux, cleanup, authWiring, nil
}

// buildTransportAuthMiddleware constructs the enforcement-aware credential
// middleware used for both wireAPI's own /api/v0/* authedHandler and
// mcpAuthWiring.transportAuth (GET /sse, POST /mcp/message). Factored out so
// tests can exercise the SAME production composition wireAPI uses without
// standing up a real Postgres connection (see
// cmd/mcp-server/auth_enforcement_wiring_test.go).
//
// allowedAudit is the F-9 (#5170) allowed-read governance-audit sink. wireAPI
// passes nil for authedHandler and a real governanceauditasync.AsyncAppender
// only for mcpAuthWiring.transportAuth, so only the MCP transport emits
// allowed-read events (see wiring.go's two call sites for the rationale). A
// nil allowedAudit is a safe no-op, byte-identical to before F-9.
func buildTransportAuthMiddleware(
	apiKey string,
	scopedTokenResolver query.ScopedTokenResolver,
	governanceAudit query.GovernanceAuditAppender,
	authSourceConfigured bool,
	oauthChallenge query.OAuthChallengePolicy,
	allowedAudit query.GovernanceAuditAppender,
) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return query.AuthMiddlewareWithScopedTokensGovernanceAuditEnforcementOAuthChallengeAndAllowedReadAudit(
			apiKey, scopedTokenResolver, next, governanceAudit, authSourceConfigured, oauthChallenge, allowedAudit,
		)
	}
}
