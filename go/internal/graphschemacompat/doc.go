// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package graphschemacompat records graph schema markers and checks whether a
// runtime may write to the active graph schema.
//
// The package reads the Postgres graph_schema_applications marker written by
// schema bootstrap and compares the latest backend fingerprint with the
// fingerprint compiled into the current writer. It also writes that marker
// after strict graph DDL or adoption succeeds. It does not inspect the graph
// backend at runtime, so steady-state pods avoid repeated SHOW CONSTRAINTS or
// SHOW INDEXES scans on large retained graph stores.
package graphschemacompat
