// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"database/sql"
	"net/http"
	"strings"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/component"
	internalruntime "github.com/eshu-hq/eshu/go/internal/runtime"
	"github.com/eshu-hq/eshu/go/internal/status"
)

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

// mountRuntimeSurface attaches the runtime admin surface (health probes,
// /metrics, /admin/status) to the API handler. The returned handler routes
// admin paths internally and delegates everything else to apiHandler.
func mountRuntimeSurface(
	apiHandler http.Handler,
	serviceName string,
	reader status.Reader,
	prometheusHandler http.Handler,
	db *sql.DB,
	driver neo4jdriver.DriverWithContext,
) (http.Handler, error) {
	adminMux, err := internalruntime.NewStatusAdminMux(
		serviceName,
		reader,
		apiHandler,
		internalruntime.WithPrometheusHandler(prometheusHandler),
		internalruntime.WithReadinessProbes(
			internalruntime.ReadinessProbesForDependencies(db, driver)...,
		),
	)
	if err != nil {
		return nil, err
	}
	return adminMux, nil
}
