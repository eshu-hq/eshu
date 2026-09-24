// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"github.com/eshu-hq/eshu/go/internal/query/package/registry"
)

// PackageRegistryHandler exposes graph-backed package registry identity
// reads. The implementation moved to internal/query/packagereg (#6060) and
// then, with the family's own name/export destutter, to
// internal/query/package/registry (#6642 Part D); this alias preserves the
// compatibility surface for cmd/api, cmd/mcp-server, and root's own tests,
// none of which import the family package directly. The alias carries
// registry.Handler's exported methods (Mount) and fields unchanged; it
// cannot forward unexported helpers, but none of those cross this boundary.
type PackageRegistryHandler = registry.Handler

// PackageRegistryCorrelationRow is one durable package correlation row. See
// registry.CorrelationRow for the full field contract.
type PackageRegistryCorrelationRow = registry.CorrelationRow

// PostgresPackageRegistryCorrelationStore reads reducer package correlation
// facts from Postgres. See registry.PostgresCorrelationStore.
type PostgresPackageRegistryCorrelationStore = registry.PostgresCorrelationStore

// GraphPackageRegistryAggregateStore reads package registry aggregate counts
// and inventory off the graph. See registry.GraphAggregateStore.
type GraphPackageRegistryAggregateStore = registry.GraphAggregateStore

// NewPostgresPackageRegistryCorrelationStore constructs the Postgres-backed
// PackageRegistryCorrelationStore. cmd/api and cmd/mcp-server call this
// through package query rather than the registry family package directly
// (#6060); it forwards unchanged to registry.NewPostgresCorrelationStore.
func NewPostgresPackageRegistryCorrelationStore(db registry.CorrelationQueryer) PostgresPackageRegistryCorrelationStore {
	return registry.NewPostgresCorrelationStore(db)
}

// NewGraphPackageRegistryAggregateStore constructs the graph-backed
// PackageRegistryAggregateStore. cmd/api and cmd/mcp-server call this through
// package query rather than the registry family package directly (#6060); it
// forwards unchanged to registry.NewGraphAggregateStore.
func NewGraphPackageRegistryAggregateStore(graph GraphQuery) GraphPackageRegistryAggregateStore {
	return registry.NewGraphAggregateStore(graph)
}
