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
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"go.opentelemetry.io/otel"

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
	scopedTokenResolver, err := scopedtoken.ResolverFromEnv(getenv)
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

	db, err := sql.Open("pgx", pgDSN)
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
	internalruntime.ConfigurePostgresPool(db, pgPoolCfg)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		if driver != nil {
			_ = driver.Close(ctx)
		}
		return nil, nil, nil, fmt.Errorf("ping postgres: %w", err)
	}
	if logger != nil {
		logger.Info("postgres connected", telemetry.EventAttr("runtime.postgres.connected"))
	}
	scopedTokenResolver = scopedtoken.ChainResolvers(
		scopedtoken.NewPostgresIdentityResolver(pgstatus.NewScopedAPITokenStore(pgstatus.SQLDB{DB: db})),
		scopedTokenResolver,
	)

	// Build query layer
	neo4jReader := query.NewNeo4jReader(driver, neo4jDB)
	contentReader := query.NewContentReader(db)
	// Build instruments before the status reader so the StatusStore can carry
	// the shared meter provider (see newStatusStore): the status query cache
	// metric eshu_dp_status_stage_counts_cache_total only emits when the
	// operator status-serving StatusStore has Instruments wired.
	instruments, err := telemetry.NewInstruments(otel.Meter(telemetry.DefaultSignalName))
	if err != nil {
		_ = db.Close()
		if driver != nil {
			_ = driver.Close(ctx)
		}
		return nil, nil, nil, fmt.Errorf("register query instruments: %w", err)
	}
	statusReader := status.WithSemanticProviderProfiles(
		newStatusStore(pgstatus.SQLQueryer{DB: db}, instruments),
		semanticProviderProfiles...,
	)
	metricsSource, err := metricsTimeSeriesSourceFromEnv(getenv, nil)
	if err != nil {
		_ = db.Close()
		if driver != nil {
			_ = driver.Close(ctx)
		}
		return nil, nil, nil, fmt.Errorf("configure metrics time-series source: %w", err)
	}
	governanceAudit := newGovernanceAuditStore(db, instruments)
	componentHome := strings.TrimSpace(getenv("ESHU_COMPONENT_HOME"))
	componentPolicy := componentPolicyFromEnv(getenv)
	readImpactFromWinners := query.SupplyChainImpactWinnersReadEnabled(getenv(query.SupplyChainImpactWinnersReadEnv))
	browserSessionAdapter := newPostgresBrowserSessionAdapter(db, instruments)
	router, err := newRouterWithSemanticEmbedding(
		db,
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
	)
	if err != nil {
		_ = db.Close()
		if driver != nil {
			_ = driver.Close(ctx)
		}
		return nil, nil, nil, fmt.Errorf("new router: %w", err)
	}
	oidcLoginHandler, err := newOIDCLoginHandler(getenv, db, instruments)
	if err != nil {
		_ = db.Close()
		if driver != nil {
			_ = driver.Close(ctx)
		}
		return nil, nil, nil, fmt.Errorf("configure oidc login: %w", err)
	}
	router.OIDCLogin = oidcLoginHandler
	oidcSessionRefreshWorker, err := newOIDCSessionRefreshWorker(getenv, db, instruments, logger)
	if err != nil {
		_ = db.Close()
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
	samlHandler, err := newSAMLHandler(db, instruments, getenv, browserSessionAdapter)
	if err != nil {
		_ = db.Close()
		if driver != nil {
			_ = driver.Close(ctx)
		}
		return nil, nil, nil, fmt.Errorf("configure saml sso: %w", err)
	}
	router.SAML = samlHandler
	router.AuthProviders = &query.AuthProviderListHandler{
		Store: newAuthProviderListStore(db, samlHandler, oidcLoginHandler),
	}

	apiMux := http.NewServeMux()
	router.Mount(apiMux)
	browserSessionResolver := newBrowserSessionResolver(db, instruments)

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
		Incidents:   newIncidentEvidenceSource(db, logger),
		SupplyChain: newSupplyChainEvidenceSource(db, logger),
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

	mux, err := mountRuntimeSurface(apiHandler, "eshu-api", statusReader, prometheusHandler, db, driver)
	if err != nil {
		_ = db.Close()
		if driver != nil {
			_ = driver.Close(ctx)
		}
		return nil, nil, nil, fmt.Errorf("mount runtime surface: %w", err)
	}

	authedMux := wrapAPIAuth(apiKey, scopedTokenResolver, browserSessionResolver, mux, adminRecoveryAuditAppender(governanceAudit))

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
		_ = db.Close()
		if driver != nil {
			_ = driver.Close(context.Background())
		}
	}

	return final, cleanup, instruments, nil
}

func openQueryGraph(
	ctx context.Context,
	getenv func(string) string,
	queryProfile query.QueryProfile,
	logger *slog.Logger,
) (neo4jdriver.DriverWithContext, string, error) {
	neo4jDB := envOrDefault(getenv, "DEFAULT_DATABASE", "nornic")
	if queryProfile == query.ProfileLocalLightweight || strings.EqualFold(envOrDefault(getenv, "ESHU_DISABLE_NEO4J", ""), "true") {
		return nil, neo4jDB, nil
	}

	driver, cfg, err := internalruntime.OpenNeo4jDriver(ctx, getenv)
	if err != nil {
		return nil, "", err
	}
	if logger != nil {
		logger.Info("neo4j connected", telemetry.EventAttr("runtime.neo4j.connected"), slog.String("neo4j_uri", cfg.URI))
	}
	return driver, cfg.DatabaseName, nil
}
