// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"github.com/eshu-hq/eshu/go/internal/query/packagereg"
)

// PackageRegistryHandler exposes graph-backed package registry identity
// reads. The implementation moved to internal/query/packagereg (#6060); this
// alias preserves the compatibility surface for cmd/api, cmd/mcp-server, and
// root's own tests, none of which import the family package directly. The
// alias carries PackageRegistryHandler's exported methods (Mount) and fields
// unchanged; it cannot forward unexported helpers, but none of those cross
// this boundary.
type PackageRegistryHandler = packagereg.PackageRegistryHandler

// PackageRegistryCorrelationRow is one durable package correlation row. See
// packagereg.PackageRegistryCorrelationRow for the full field contract.
type PackageRegistryCorrelationRow = packagereg.PackageRegistryCorrelationRow

// PostgresPackageRegistryCorrelationStore reads reducer package correlation
// facts from Postgres. See packagereg.PostgresPackageRegistryCorrelationStore.
type PostgresPackageRegistryCorrelationStore = packagereg.PostgresPackageRegistryCorrelationStore

// GraphPackageRegistryAggregateStore reads package registry aggregate counts
// and inventory off the graph. See packagereg.GraphPackageRegistryAggregateStore.
type GraphPackageRegistryAggregateStore = packagereg.GraphPackageRegistryAggregateStore

// NewPostgresPackageRegistryCorrelationStore constructs the Postgres-backed
// PackageRegistryCorrelationStore. cmd/api and cmd/mcp-server call this
// through package query rather than packagereg directly (#6060); it forwards
// unchanged to packagereg.NewPostgresPackageRegistryCorrelationStore.
func NewPostgresPackageRegistryCorrelationStore(db packagereg.PackageRegistryCorrelationQueryer) PostgresPackageRegistryCorrelationStore {
	return packagereg.NewPostgresPackageRegistryCorrelationStore(db)
}

// NewGraphPackageRegistryAggregateStore constructs the graph-backed
// PackageRegistryAggregateStore. cmd/api and cmd/mcp-server call this through
// package query rather than packagereg directly (#6060); it forwards
// unchanged to packagereg.NewGraphPackageRegistryAggregateStore.
func NewGraphPackageRegistryAggregateStore(graph GraphQuery) GraphPackageRegistryAggregateStore {
	return packagereg.NewGraphPackageRegistryAggregateStore(graph)
}
