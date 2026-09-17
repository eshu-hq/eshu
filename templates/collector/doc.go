// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package collector is the starting point for a standalone Eshu collector
// repository. Copy this directory as the root of a new eshu-collector-<source>
// repository, rename the module path and component identity, and replace the
// Report/Collect core with real source observation.
//
// Boundary: this package imports only the published SDK modules
// (sdk/go/collector, sdk/go/factschema). It never imports go/internal,
// another collector implementation, or any core Postgres/graph/queue handle.
// Every one of those would fail the import-boundary test and block cutover
// per #4047.
package collector
