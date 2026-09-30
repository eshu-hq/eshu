// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"errors"
	"net/http"

	internalruntime "github.com/eshu-hq/eshu/go/internal/runtime"
	pgaccess "github.com/eshu-hq/eshu/go/internal/runtime/postgres"
	"github.com/eshu-hq/eshu/go/internal/status"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// mountRuntimeSurfaceWithPostgresAccess gives only trusted admin methods a
// checkpoint source; readiness checks both pools and the fenced status schema.
func mountRuntimeSurfaceWithPostgresAccess(serviceName string, reader status.Reader, prometheusHandler http.Handler, access *pgaccess.Access, driver neo4jdriver.DriverWithContext) (*http.ServeMux, error) {
	if access == nil {
		return nil, errors.New("postgres access is required for the runtime surface")
	}
	probes := internalruntime.ReadinessProbesForDependencies(nil, driver)
	probes = append(probes, internalruntime.ReadinessProbe{Name: "postgres", Check: access.Ping})
	return internalruntime.NewStatusAdminMux(serviceName,
		pgaccess.NewTrustedStatusReader(reader, access), nil,
		internalruntime.WithPrometheusHandler(prometheusHandler),
		internalruntime.WithReadinessProbes(probes...),
	)
}
