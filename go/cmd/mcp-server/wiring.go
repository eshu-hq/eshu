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
) (http.Handler, *http.ServeMux, func(), error) {
	queryProfile, err := loadQueryProfile(getenv)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("load query profile: %w", err)
	}
	graphBackend, err := loadGraphBackend(getenv)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("load graph backend: %w", err)
	}

	apiKey, err := internalruntime.ResolveAPIKey(getenv)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("resolve api key: %w", err)
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

	// Build query layer
	neo4jReader := query.NewNeo4jReader(driver, neo4jDB)
	contentReader := query.NewContentReader(db)

	router := newMCPQueryRouter(db, neo4jReader, contentReader, queryProfile, graphBackend, logger)

	mux := http.NewServeMux()
	router.Mount(mux)

	// Wrap with auth middleware (protects all /api/v0/* routes when mounted by MCP server)
	authedHandler := query.AuthMiddleware(apiKey, mux)

	statusReader := pgstatus.NewStatusStore(pgstatus.SQLQueryer{DB: db})
	adminMux, err := mountRuntimeSurface("mcp-server", statusReader, prometheusHandler)
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

func newMCPQueryRouter(
	db *sql.DB,
	neo4jReader query.GraphQuery,
	contentReader query.ContentStore,
	queryProfile query.QueryProfile,
	graphBackend query.GraphBackend,
	logger *slog.Logger,
) *query.APIRouter {
	return &query.APIRouter{
		Repositories: &query.RepositoryHandler{
			Neo4j:   neo4jReader,
			Content: contentReader,
			Profile: queryProfile,
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
			Profile: queryProfile,
		},
		Infra: &query.InfraHandler{
			Neo4j:   neo4jReader,
			Profile: queryProfile,
		},
		IaC: &query.IaCHandler{
			Content:      contentReader,
			Reachability: query.NewPostgresIaCReachabilityStore(db),
			Management:   query.NewPostgresIaCManagementStore(db),
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
		PackageRegistry: &query.PackageRegistryHandler{
			Neo4j:   neo4jReader,
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
	}
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

func mountRuntimeSurface(
	serviceName string,
	reader status.Reader,
	prometheusHandler http.Handler,
) (*http.ServeMux, error) {
	return internalruntime.NewStatusAdminMux(
		serviceName,
		reader,
		nil,
		internalruntime.WithPrometheusHandler(prometheusHandler),
	)
}
