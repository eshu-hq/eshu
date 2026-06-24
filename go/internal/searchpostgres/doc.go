// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package searchpostgres adapts Postgres content search to the internal
// retrieval benchmark port.
//
// The package is a benchmark-only adapter for the current keyword baseline. It
// accepts bounded repository-scoped requests, reads the existing content search
// store, and projects rows through searchdocs so results remain derived search
// documents instead of canonical graph truth.
package searchpostgres
