package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/eshulocal"
	"github.com/eshu-hq/eshu/go/internal/query"
	pgstorage "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

func localHostEnv(dsn string, runtimeConfig localHostRuntimeConfig, graph *managedLocalGraph, overrides map[string]string) []string {
	values := map[string]string{
		"ESHU_POSTGRES_DSN":      dsn,
		"ESHU_FACT_STORE_DSN":    dsn,
		"ESHU_CONTENT_STORE_DSN": dsn,
		"ESHU_LISTEN_ADDR":       "127.0.0.1:0",
		"ESHU_METRICS_ADDR":      "127.0.0.1:0",
		"ESHU_QUERY_PROFILE":     string(runtimeConfig.Profile),
		"ESHU_DISABLE_NEO4J":     "",
	}
	if runtimeConfig.Profile == query.ProfileLocalLightweight {
		values["ESHU_DISABLE_NEO4J"] = "true"
	}
	if runtimeConfig.GraphBackend != "" {
		values["ESHU_GRAPH_BACKEND"] = string(runtimeConfig.GraphBackend)
	}
	for key, value := range graphEnvOverrides(graph) {
		values[key] = value
	}
	for key, value := range overrides {
		values[key] = value
	}
	return mergeEnvironment(eshuEnviron(), values)
}

func localHostIngesterOverrides(layout eshulocal.Layout, mode localHostMode, runtimeConfig localHostRuntimeConfig) map[string]string {
	overrides := map[string]string{
		"ESHU_WATCH_PATH":        layout.WorkspaceRoot,
		"ESHU_REPO_SOURCE_MODE":  "filesystem",
		"ESHU_FILESYSTEM_ROOT":   layout.WorkspaceRoot,
		"ESHU_FILESYSTEM_DIRECT": "true",
		"ESHU_REPOS_DIR":         filepath.Join(layout.CacheDir, "repos"),
	}
	if runtimeConfig.GraphBackend != "" {
		overrides["ESHU_GRAPH_BACKEND"] = string(runtimeConfig.GraphBackend)
	}
	return overrides
}

func resolveLocalHostRuntimeConfig(getenv func(string) string) (localHostRuntimeConfig, error) {
	profile := query.ProfileLocalLightweight
	rawProfile := strings.TrimSpace(getenv("ESHU_QUERY_PROFILE"))
	if rawProfile != "" {
		parsedProfile, err := query.ParseQueryProfile(rawProfile)
		if err != nil {
			return localHostRuntimeConfig{}, fmt.Errorf("parse ESHU_QUERY_PROFILE: %w", err)
		}
		switch parsedProfile {
		case query.ProfileLocalLightweight, query.ProfileLocalAuthoritative:
			profile = parsedProfile
		default:
			return localHostRuntimeConfig{}, fmt.Errorf(
				"local Eshu service supports only %q or %q query profiles, got %q",
				query.ProfileLocalLightweight,
				query.ProfileLocalAuthoritative,
				parsedProfile,
			)
		}
	}

	rawGraphBackend := strings.TrimSpace(getenv("ESHU_GRAPH_BACKEND"))
	if profile == query.ProfileLocalLightweight {
		if rawGraphBackend != "" {
			return localHostRuntimeConfig{}, fmt.Errorf("ESHU_GRAPH_BACKEND is not supported with %q", profile)
		}
		return localHostRuntimeConfig{Profile: profile}, nil
	}

	if rawGraphBackend == "" {
		return localHostRuntimeConfig{
			Profile:      profile,
			GraphBackend: query.GraphBackendNornicDB,
		}, nil
	}

	graphBackend, err := query.ParseGraphBackend(rawGraphBackend)
	if err != nil {
		return localHostRuntimeConfig{}, fmt.Errorf("parse ESHU_GRAPH_BACKEND: %w", err)
	}
	return localHostRuntimeConfig{
		Profile:      profile,
		GraphBackend: graphBackend,
	}, nil
}

func requestedAttachRuntimeConfig(getenv func(string) string) (localHostRuntimeConfig, bool, error) {
	explicit := strings.TrimSpace(getenv("ESHU_QUERY_PROFILE")) != "" || strings.TrimSpace(getenv("ESHU_GRAPH_BACKEND")) != ""
	if !explicit {
		return localHostRuntimeConfig{}, false, nil
	}

	runtimeConfig, err := resolveLocalHostRuntimeConfig(getenv)
	if err != nil {
		return localHostRuntimeConfig{}, false, err
	}
	return runtimeConfig, true, nil
}

func runtimeConfigFromOwnerRecord(record eshulocal.OwnerRecord) (localHostRuntimeConfig, error) {
	if strings.TrimSpace(record.Profile) == "" {
		return localHostRuntimeConfig{Profile: query.ProfileLocalLightweight}, nil
	}

	profile, err := query.ParseQueryProfile(record.Profile)
	if err != nil {
		return localHostRuntimeConfig{}, fmt.Errorf("parse owner record profile: %w", err)
	}
	if profile == query.ProfileLocalLightweight {
		return localHostRuntimeConfig{Profile: profile}, nil
	}
	if strings.TrimSpace(record.GraphBackend) == "" {
		return localHostRuntimeConfig{
			Profile:      profile,
			GraphBackend: query.GraphBackendNornicDB,
		}, nil
	}

	graphBackend, err := query.ParseGraphBackend(record.GraphBackend)
	if err != nil {
		return localHostRuntimeConfig{}, fmt.Errorf("parse owner record graph backend: %w", err)
	}
	return localHostRuntimeConfig{
		Profile:      profile,
		GraphBackend: graphBackend,
	}, nil
}

func mergeEnvironment(base []string, overrides map[string]string) []string {
	merged := make(map[string]string, len(base)+len(overrides))
	for _, entry := range base {
		for i := 0; i < len(entry); i++ {
			if entry[i] == '=' {
				merged[entry[:i]] = entry[i+1:]
				break
			}
		}
	}
	for key, value := range overrides {
		merged[key] = value
	}
	env := make([]string, 0, len(merged))
	for key, value := range merged {
		env = append(env, key+"="+value)
	}
	return env
}

func applyLocalBootstrap(ctx context.Context, dsn string) error {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("open local postgres bootstrap connection: %w", err)
	}
	defer func() { _ = db.Close() }()

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping local postgres bootstrap connection: %w", err)
	}
	if err := pgstorage.ApplyDefinitions(ctx, db, localBootstrapDefinitions(os.Getenv)); err != nil {
		return fmt.Errorf("apply local postgres bootstrap: %w", err)
	}
	return nil
}

func localBootstrapDefinitions(getenv func(string) string) []pgstorage.Definition {
	if deferContentSearchIndexes(getenv) {
		return pgstorage.BootstrapDefinitionsWithoutContentSearchIndexes()
	}
	return pgstorage.BootstrapDefinitions()
}

func deferContentSearchIndexes(getenv func(string) string) bool {
	if getenv == nil {
		return false
	}
	raw := strings.TrimSpace(getenv(deferContentSearchIndexesEnv))
	return raw == "1" || strings.EqualFold(raw, "true") || strings.EqualFold(raw, "yes")
}
