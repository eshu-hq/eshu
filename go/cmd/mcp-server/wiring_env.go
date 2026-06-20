package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/query"
	internalruntime "github.com/eshu-hq/eshu/go/internal/runtime"
	"github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// envSemanticSearchLocalEmbedder is the environment variable that selects the
// local embedder strategy for semantic search (empty/"hash"/"local_hash").
const envSemanticSearchLocalEmbedder = "ESHU_SEMANTIC_SEARCH_LOCAL_EMBEDDER"

// componentPolicyFromEnv builds a component trust policy from environment
// variables. It reads the trust mode, allowed/revoked IDs and publishers,
// and the core component version.
func componentPolicyFromEnv(getenv func(string) string) component.Policy {
	return component.ConfigureProvenanceFromEnv(component.Policy{
		Mode:              strings.TrimSpace(getenv("ESHU_COMPONENT_TRUST_MODE")),
		AllowedIDs:        componentEnvList(getenv("ESHU_COMPONENT_ALLOW_IDS")),
		AllowedPublishers: componentEnvList(getenv("ESHU_COMPONENT_ALLOW_PUBLISHERS")),
		RevokedIDs:        componentEnvList(getenv("ESHU_COMPONENT_REVOKE_IDS")),
		RevokedPublishers: componentEnvList(getenv("ESHU_COMPONENT_REVOKE_PUBLISHERS")),
		CoreVersion:       strings.TrimSpace(getenv("ESHU_COMPONENT_CORE_VERSION")),
	}, getenv)
}

// componentEnvList splits a comma-separated env value into trimmed, non-empty
// strings. An empty or whitespace-only value yields a nil slice.
func componentEnvList(raw string) []string {
	fields := strings.Split(raw, ",")
	values := make([]string, 0, len(fields))
	for _, field := range fields {
		if value := strings.TrimSpace(field); value != "" {
			values = append(values, value)
		}
	}
	return values
}

// openQueryGraph opens a Neo4j/NornicDB driver using the shared bolt
// configuration. Returns (nil, databaseName, nil) for local_lightweight
// or when ESHU_DISABLE_NEO4J=true.
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

// envOrDefault returns the trimmed value of key, or fallback when empty.
func envOrDefault(getenv func(string) string, key, fallback string) string {
	v := strings.TrimSpace(getenv(key))
	if v == "" {
		return fallback
	}
	return v
}

// loadSemanticSearchLocalEmbedder parses ESHU_SEMANTIC_SEARCH_LOCAL_EMBEDDER.
// Valid values are "", "hash", and "local_hash".
func loadSemanticSearchLocalEmbedder(getenv func(string) string) (string, error) {
	raw := strings.TrimSpace(getenv(envSemanticSearchLocalEmbedder))
	switch raw {
	case "", "hash", "local_hash":
		return raw, nil
	default:
		return "", fmt.Errorf("invalid %s %q", envSemanticSearchLocalEmbedder, raw)
	}
}

// loadQueryProfile parses ESHU_QUERY_PROFILE. An empty value defaults to
// query.ProfileProduction.
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

// loadGraphBackend parses ESHU_GRAPH_BACKEND. An empty value defaults to
// NornicDB (the canonical graph backend).
func loadGraphBackend(getenv func(string) string) (query.GraphBackend, error) {
	return query.ParseGraphBackend(strings.TrimSpace(getenv("ESHU_GRAPH_BACKEND")))
}

// mountRuntimeSurface attaches the runtime admin surface (health probes,
// /metrics, /admin/status) for the MCP server. The returned mux routes
// admin paths internally; /api/v0/* paths are NOT wired here — they live on
// the main application mux returned by wireAPI.
func mountRuntimeSurface(
	serviceName string,
	reader status.Reader,
	prometheusHandler http.Handler,
	db *sql.DB,
	driver neo4jdriver.DriverWithContext,
) (*http.ServeMux, error) {
	return internalruntime.NewStatusAdminMux(
		serviceName,
		reader,
		nil,
		internalruntime.WithPrometheusHandler(prometheusHandler),
		internalruntime.WithReadinessProbes(
			internalruntime.ReadinessProbesForDependencies(db, driver)...,
		),
	)
}
