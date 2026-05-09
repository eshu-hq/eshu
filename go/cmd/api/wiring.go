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

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/recovery"
	internalruntime "github.com/eshu-hq/eshu/go/internal/runtime"
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
) (http.Handler, func(), error) {
	queryProfile, err := loadQueryProfile(getenv)
	if err != nil {
		return nil, nil, fmt.Errorf("load query profile: %w", err)
	}
	graphBackend, err := loadGraphBackend(getenv)
	if err != nil {
		return nil, nil, fmt.Errorf("load graph backend: %w", err)
	}

	apiKey, err := internalruntime.ResolveAPIKey(getenv)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve api key: %w", err)
	}

	driver, neo4jDB, err := openQueryGraph(ctx, getenv, queryProfile, logger)
	if err != nil {
		return nil, nil, err
	}

	// Open Postgres using pgx driver
	pgDSN := envOrDefault(getenv, "ESHU_POSTGRES_DSN",
		envOrDefault(getenv, "ESHU_CONTENT_STORE_DSN", ""))
	if pgDSN == "" {
		if driver != nil {
			_ = driver.Close(ctx)
		}
		return nil, nil, fmt.Errorf("ESHU_POSTGRES_DSN or ESHU_CONTENT_STORE_DSN is required")
	}

	db, err := sql.Open("pgx", pgDSN)
	if err != nil {
		if driver != nil {
			_ = driver.Close(ctx)
		}
		return nil, nil, fmt.Errorf("open postgres: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		if driver != nil {
			_ = driver.Close(ctx)
		}
		return nil, nil, fmt.Errorf("ping postgres: %w", err)
	}
	if logger != nil {
		logger.Info("postgres connected", telemetry.EventAttr("runtime.postgres.connected"))
	}

	// Build query layer
	neo4jReader := query.NewNeo4jReader(driver, neo4jDB)
	contentReader := query.NewContentReader(db)
	statusReader := pgstatus.NewStatusStore(pgstatus.SQLQueryer{DB: db})
	router, err := newRouter(db, neo4jReader, contentReader, queryProfile, graphBackend, logger)
	if err != nil {
		_ = db.Close()
		if driver != nil {
			_ = driver.Close(ctx)
		}
		return nil, nil, fmt.Errorf("new router: %w", err)
	}

	apiMux := http.NewServeMux()
	router.Mount(apiMux)

	mux, err := mountRuntimeSurface(apiMux, "eshu-api", statusReader, prometheusHandler)
	if err != nil {
		_ = db.Close()
		if driver != nil {
			_ = driver.Close(ctx)
		}
		return nil, nil, fmt.Errorf("mount runtime surface: %w", err)
	}

	// Wrap with auth middleware
	authedMux := query.AuthMiddleware(apiKey, mux)

	cleanup := func() {
		_ = db.Close()
		if driver != nil {
			_ = driver.Close(context.Background())
		}
	}

	return authedMux, cleanup, nil
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

func envOrDefault(getenv func(string) string, key, fallback string) string {
	v := strings.TrimSpace(getenv(key))
	if v == "" {
		return fallback
	}
	return v
}

func loadQueryProfile(getenv func(string) string) (query.QueryProfile, error) {
	raw := strings.TrimSpace(getenv("ESHU_QUERY_PROFILE"))
	if raw == "" {
		return query.ProfileProduction, nil
	}
	profile, err := query.ParseQueryProfile(raw)
	if err != nil {
		return "", err
	}
	return profile, nil
}

func loadGraphBackend(getenv func(string) string) (query.GraphBackend, error) {
	return query.ParseGraphBackend(strings.TrimSpace(getenv("ESHU_GRAPH_BACKEND")))
}

func newRouter(
	db *sql.DB,
	neo4jReader query.GraphQuery,
	contentReader query.ContentStore,
	queryProfile query.QueryProfile,
	graphBackend query.GraphBackend,
	logger *slog.Logger,
) (*query.APIRouter, error) {
	router := &query.APIRouter{
		Repositories: &query.RepositoryHandler{
			Neo4j:   neo4jReader,
			Content: contentReader,
			Profile: queryProfile,
			Logger:  logger,
		},
		Entities: &query.EntityHandler{
			Neo4j:   neo4jReader,
			Content: contentReader,
			Profile: queryProfile,
			Logger:  logger,
		},
		Code: &query.CodeHandler{
			GraphBackend: graphBackend,
			Neo4j:        neo4jReader,
			Content:      contentReader,
			Profile:      queryProfile,
		},
		Content: &query.ContentHandler{
			Content: contentReader,
		},
		Infra: &query.InfraHandler{
			Neo4j:   neo4jReader,
			Profile: queryProfile,
		},
		IaC: &query.IaCHandler{
			Content:      contentReader,
			Reachability: query.NewPostgresIaCReachabilityStore(db),
			Profile:      queryProfile,
		},
		Impact: &query.ImpactHandler{
			Neo4j:   neo4jReader,
			Content: contentReader,
			Profile: queryProfile,
			Logger:  logger,
		},
		Evidence: &query.EvidenceHandler{
			Content: contentReader,
			Profile: queryProfile,
		},
		Documentation: &query.DocumentationHandler{
			Content: contentReader,
			Profile: queryProfile,
		},
		Status: &query.StatusHandler{
			Neo4j:        neo4jReader,
			DB:           db,
			StatusReader: pgstatus.NewStatusStore(pgstatus.SQLQueryer{DB: db}),
		},
		Compare: &query.CompareHandler{
			Neo4j:   neo4jReader,
			Content: contentReader,
			Profile: queryProfile,
		},
		Admin: &query.AdminHandler{
			Store: query.NewPostgresAdminStore(db),
		},
	}
	if db == nil {
		return router, nil
	}

	recoveryHandler, err := recovery.NewHandler(pgstatus.NewRecoveryStore(pgstatus.SQLDB{DB: db}))
	if err != nil {
		return nil, fmt.Errorf("new recovery handler: %w", err)
	}
	reindexer, err := internalruntime.NewStatusRequestHandler(pgstatus.NewStatusRequestStore(pgstatus.SQLDB{DB: db}))
	if err != nil {
		return nil, fmt.Errorf("new status request handler: %w", err)
	}
	router.Admin.Recovery = recoveryHandler
	router.Admin.Reindexer = reindexer
	return router, nil
}

func mountRuntimeSurface(
	apiHandler http.Handler,
	serviceName string,
	reader status.Reader,
	prometheusHandler http.Handler,
) (http.Handler, error) {
	adminMux, err := internalruntime.NewStatusAdminMux(
		serviceName,
		reader,
		apiHandler,
		internalruntime.WithPrometheusHandler(prometheusHandler),
	)
	if err != nil {
		return nil, err
	}
	return adminMux, nil
}
