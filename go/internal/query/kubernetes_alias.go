// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query //nolint:dirgate // B5 root alias shim for #6642: the Kubernetes correlation aliases must live in package query so the APIRouter field, the supply-chain port assertion, and the cmd/api and cmd/mcp-server wiring compile unchanged.

import (
	"database/sql"

	"github.com/eshu-hq/eshu/go/internal/query/kubernetes"
)

// kubernetes_alias.go is the root alias shim for the Kubernetes correlation
// family, which moved to internal/query/kubernetes (#6642). cmd/api and
// cmd/mcp-server build the handler and both stores through package query, and
// the APIRouter field names the handler, so these stay until the #6642 alias
// sweep.

// KubernetesHandler serves GET /api/v0/kubernetes/correlations. See
// kubernetes.Handler.
type KubernetesHandler = kubernetes.Handler

// PostgresKubernetesRuntimeWorkloadStore is the Postgres-backed current
// Kubernetes workload inventory the supply-chain runtime probe reads. See
// kubernetes.PostgresRuntimeWorkloadStore.
type PostgresKubernetesRuntimeWorkloadStore = kubernetes.PostgresRuntimeWorkloadStore

// NewPostgresKubernetesCorrelationStore constructs the Postgres-backed
// correlation store. It forwards unchanged to
// kubernetes.NewPostgresCorrelationStore.
func NewPostgresKubernetesCorrelationStore(db kubernetes.CorrelationQueryer) kubernetes.PostgresCorrelationStore {
	return kubernetes.NewPostgresCorrelationStore(db)
}

// NewPostgresKubernetesRuntimeWorkloadStore constructs the Postgres-backed
// workload inventory. It forwards unchanged to
// kubernetes.NewPostgresRuntimeWorkloadStore.
func NewPostgresKubernetesRuntimeWorkloadStore(db *sql.DB) *kubernetes.PostgresRuntimeWorkloadStore {
	return kubernetes.NewPostgresRuntimeWorkloadStore(db)
}
