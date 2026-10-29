// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/infra/inventory"
)

// OpenPostgresForWorkload opens a writer or authority-control pool whose every
// new physical connection is checked against the protected database contract.
// An unset contract retains legacy operation for non-authority workloads.
func OpenPostgresForWorkload(ctx context.Context, getenv func(string) string, workload PostgresWorkload) (*sql.DB, error) {
	return OpenPostgresForWorkloadWithSessionHooks(ctx, getenv, workload, nil)
}

// OpenPostgresForWorkloadWithSessionHooks preserves bootstrap-index's
// per-connection session hooks after role attestation and before pool use.
func OpenPostgresForWorkloadWithSessionHooks(ctx context.Context, getenv func(string) string, workload PostgresWorkload, hooks []inventory.ConnectHook, extraSessionSQL ...string) (*sql.DB, error) {
	if getenv == nil {
		return nil, fmt.Errorf("postgres environment reader is required")
	}
	route := PostgresRouteWriter
	switch workload {
	case PostgresWorkloadRuntime:
	case PostgresWorkloadCoordinator:
		route = PostgresRouteControl
	case PostgresWorkloadBootstrap:
		route = PostgresRouteBootstrap
	case PostgresWorkloadAdvisoryProducer:
		route = PostgresRouteProducer
	default:
		return nil, fmt.Errorf("postgres workload cannot use the shared writer opener")
	}
	contract, err := LoadPostgresRoleContract(getenv, workload, route)
	if err != nil {
		return nil, err
	}
	if workload == PostgresWorkloadCoordinator && strings.TrimSpace(getenv("ESHU_WORKFLOW_COORDINATOR_SCANNER_IMAGE_DISCOVERY_POLICY_JSON")) != "" && contract == nil {
		return nil, fmt.Errorf("scanner image discovery policy requires ESHU_POSTGRES_ROLE_CONTRACT_JSON")
	}
	if workload == PostgresWorkloadAdvisoryProducer {
		if contract == nil {
			return nil, fmt.Errorf("advisory producer requires ESHU_POSTGRES_ROLE_CONTRACT_JSON")
		}
		producerDSN := strings.TrimSpace(getenv("ESHU_POSTGRES_PRODUCER_DSN"))
		if producerDSN == "" {
			return nil, fmt.Errorf("advisory producer requires ESHU_POSTGRES_PRODUCER_DSN")
		}
		if producerDSN == firstEnvValue(getenv, postgresDSNEnvKeys...) {
			return nil, fmt.Errorf("ESHU_POSTGRES_PRODUCER_DSN must differ from the runtime writer DSN")
		}
		producerEnv := func(key string) string {
			switch key {
			case "ESHU_FACT_STORE_DSN", "ESHU_CONTENT_STORE_DSN", "ESHU_POSTGRES_DSN":
				return producerDSN
			default:
				return getenv(key)
			}
		}
		return openPostgresWithRoleAttestor(ctx, producerEnv, contract.Attest, hooks, extraSessionSQL...)
	}
	if contract == nil {
		return openPostgresWithRoleAttestor(ctx, getenv, nil, hooks, extraSessionSQL...)
	}
	return openPostgresWithRoleAttestor(ctx, getenv, contract.Attest, hooks, extraSessionSQL...)
}
