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
) (http.Handler, *http.ServeMux, func(), error) {
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
	// graph dial). Without this the mcp-server pool is database/sql-default
	// unbounded, which would let a read burst exceed the whole-stack connection
	// budget (#4456). Only the pool sizes are applied; the DSN resolved above is kept.
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
	// identityResolver is kept separate from scopedTokenResolver (the
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
		return nil, nil, nil, fmt.Errorf("register query instruments: %w", err)
	}

	// IdP bearer-token resolver (#5162): see cmd/api's identical wiring
	// comment. Returns (nil, nil) when ESHU_AUTH_RESOURCE_URI is unset.
	oidcBearerResolver, err := newOIDCBearerResolver(ctx, getenv, db, instruments, logger)
	if err != nil {
		_ = db.Close()
		if driver != nil {
			_ = driver.Close(ctx)
		}
		return nil, nil, nil, fmt.Errorf("construct oidc bearer resolver: %w", err)
	}
	scopedTokenResolver = scopedtoken.ChainResolvers(identityResolver, oidcBearerResolver, scopedTokenResolver)

	// Build query layer
	neo4jReader := query.NewNeo4jReader(driver, neo4jDB)
	contentReader := query.NewContentReader(db)
	statusReader := status.WithSemanticProviderProfiles(
		newStatusStore(pgstatus.SQLQueryer{DB: db}, instruments),
		semanticProviderProfiles...,
	)
	governanceAudit := pgstatus.NewGovernanceAuditStore(pgstatus.SQLDB{DB: db})

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
	authedHandler := query.AuthMiddlewareWithScopedTokensAndGovernanceAudit(apiKey, scopedTokenResolver, instrumentedMux, governanceAudit)

	adminMux, err := mountRuntimeSurface("mcp-server", statusReader, prometheusHandler, db, driver)
	if err != nil {
		_ = db.Close()
		if driver != nil {
			_ = driver.Close(ctx)
		}
		return nil, nil, nil, fmt.Errorf("mount runtime surface: %w", err)
	}

	cleanup := func() {
		_ = db.Close()
		if driver != nil {
			_ = driver.Close(context.Background())
		}
	}

	return authedHandler, adminMux, cleanup, nil
}
