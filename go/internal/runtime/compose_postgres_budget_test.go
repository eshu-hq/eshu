// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
)

// TestComposePostgresMaxConnectionsCoversPoolBudget enforces the #4456 runtime
// connection-budget invariant: the Postgres server's max_connections must be at
// least as large as the sum of every runtime's per-process pool ceiling, so a
// repo-scale run cannot exhaust connections ("too many clients") merely by
// starting the standard service set.
//
// Peak demand = (number of services that open a Postgres pool) *
// defaultPostgresMaxOpenConns. If a new pool-holding service is added, or the
// per-process pool default is raised, without lifting max_connections, this test
// fails — keeping the envelope coherent by construction.
func TestComposePostgresMaxConnectionsCoversPoolBudget(t *testing.T) {
	t.Parallel()

	// These two standalone compose files define their own postgres service. The
	// remote-e2e stack extends the base postgres (inheriting max_connections) but
	// adds many more pool-holding services, so it is checked separately in
	// TestRemoteE2EStackFitsConnectionBudget — inheriting the ceiling is only safe
	// if the ceiling also covers remote-e2e's larger demand.
	for _, fileName := range []string{"docker-compose.yaml", "docker-compose.neo4j.yml"} {
		doc := readComposeDocument(t, fileName)

		poolServices := postgresPoolServiceCount(doc)
		if poolServices == 0 {
			t.Fatalf("%s: found no services with a Postgres pool; the budget check cannot be evaluated", fileName)
		}
		peakDemand := poolServices * defaultPostgresMaxOpenConns

		maxConns, err := composePostgresMaxConnections(doc)
		if err != nil {
			t.Fatalf("%s: %v", fileName, err)
		}
		// Require headroom above the worst-case pool budget for reserved/admin
		// connections: Postgres superuser_reserved_connections plus operator psql,
		// the admin-status probe, and transient tooling. Without this margin the
		// stack could sit at exactly max_connections and refuse the operator's own
		// diagnostic session at the worst moment.
		if maxConns < peakDemand+reservedAdminConnections {
			t.Fatalf("%s: postgres max_connections=%d is below the worst-case pool budget %d + %d reserved/admin (%d pool-holding services * %d per-process pool); raise ESHU_PG_MAX_CONNECTIONS or lower ESHU_POSTGRES_MAX_OPEN_CONNS",
				fileName, maxConns, peakDemand, reservedAdminConnections, poolServices, defaultPostgresMaxOpenConns)
		}
	}
}

// reservedAdminConnections is the headroom the budget must leave above the sum of
// per-process pools for superuser_reserved_connections, an operator psql session,
// the admin-status probe, and transient tooling.
const reservedAdminConnections = 20

// TestRemoteE2EStackFitsConnectionBudget checks the remote-e2e proof stack, which
// `extends` the base postgres (inheriting its max_connections) but composes many
// more pool-holding services via its include files — notably the full collector
// fleet. Inheriting the base ceiling is only safe if that ceiling also covers this
// larger stack; otherwise a remote proof run can exhaust connections while the base
// check stays green.
func TestRemoteE2EStackFitsConnectionBudget(t *testing.T) {
	t.Parallel()

	// The remote-e2e postgres must extend the base service so it inherits the
	// budgeted ceiling rather than defining its own (unbudgeted) one.
	foundation := readComposeDocument(t, "docker-compose.remote-e2e.foundation.yaml")
	pg, ok := foundation.Services["postgres"]
	if !ok || pg.Extends == nil {
		t.Fatal("docker-compose.remote-e2e.foundation.yaml postgres must extend the base postgres so it inherits the connection budget")
	}

	merged := mergeComposeServices(t,
		"docker-compose.remote-e2e.foundation.yaml",
		"docker-compose.remote-e2e.runtime.yaml",
		"docker-compose.remote-e2e.seed.yaml",
	)
	poolServices := postgresPoolServiceCount(merged)
	if poolServices == 0 {
		t.Fatal("remote-e2e merged stack has no Postgres pool services; the budget check cannot be evaluated")
	}
	peakDemand := poolServices * defaultPostgresMaxOpenConns

	// The remote-e2e postgres inherits max_connections from the base compose.
	base := readComposeDocument(t, "docker-compose.yaml")
	maxConns, err := composePostgresMaxConnections(base)
	if err != nil {
		t.Fatalf("base postgres max_connections: %v", err)
	}
	if maxConns < peakDemand+reservedAdminConnections {
		t.Fatalf("remote-e2e stack: inherited max_connections=%d is below its pool budget %d + %d reserved (%d pool-holding services * %d per-process pool); raise ESHU_PG_MAX_CONNECTIONS to cover the remote-e2e collector fleet",
			maxConns, peakDemand, reservedAdminConnections, poolServices, defaultPostgresMaxOpenConns)
	}
}

// mergeComposeServices reads several compose files and merges their service maps
// (later files override earlier on name collision), approximating docker compose's
// include/merge for the pool-budget count.
func mergeComposeServices(t *testing.T, fileNames ...string) composeDocument {
	t.Helper()
	merged := composeDocument{Services: map[string]composeService{}}
	for _, fileName := range fileNames {
		doc := readComposeDocument(t, fileName)
		for name, svc := range doc.Services {
			merged.Services[name] = svc
		}
	}
	return merged
}

// postgresPoolServiceCount counts services that open a Postgres connection pool,
// i.e. those carrying any of the runtime's DSN env keys (postgresDSNEnvKeys:
// ESHU_FACT_STORE_DSN, ESHU_CONTENT_STORE_DSN, ESHU_POSTGRES_DSN — the same set
// LoadPostgresConfig resolves, so a service configured with only a content or
// fact DSN is still counted). Transient (db-migrate) and profile-gated
// (workflow-coordinator) services are included: they can all be live
// simultaneously during a run, so the budget must cover them.
//
// Assumes one pool per service; a service that opened multiple sql.DB pools would
// be undercounted and must be modelled explicitly.
func postgresPoolServiceCount(doc composeDocument) int {
	n := 0
	for _, service := range doc.Services {
		if service.Environment == nil {
			continue
		}
		for _, key := range postgresDSNEnvKeys {
			if _, ok := service.Environment[key]; ok {
				n++
				break
			}
		}
	}
	return n
}

// composePostgresMaxConnections extracts the default max_connections the postgres
// service is started with, parsing the `-c max_connections=${ESHU_PG_MAX_CONNECTIONS:-N}`
// (or bare `max_connections=N`) token from its command list.
func composePostgresMaxConnections(doc composeDocument) (int, error) {
	pg, ok := doc.Services["postgres"]
	if !ok {
		return 0, fmt.Errorf("no postgres service defined")
	}
	args, ok := pg.Command.([]any)
	if !ok {
		return 0, fmt.Errorf("postgres command is not a list")
	}
	for _, raw := range args {
		token, ok := raw.(string)
		if !ok || !strings.Contains(token, "max_connections=") {
			continue
		}
		value := token[strings.Index(token, "max_connections=")+len("max_connections="):]
		// value is either "N" or "${ESHU_PG_MAX_CONNECTIONS:-N}"
		if idx := strings.Index(value, ":-"); idx >= 0 {
			value = value[idx+2:]
			value = strings.TrimRight(value, "}")
		}
		value = strings.TrimSpace(value)
		n, err := strconv.Atoi(value)
		if err != nil {
			return 0, fmt.Errorf("cannot parse max_connections from %q: %w", token, err)
		}
		return n, nil
	}
	return 0, fmt.Errorf("postgres command does not set max_connections")
}
