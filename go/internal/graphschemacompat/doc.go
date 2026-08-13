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
//
// RequireCompatible answers for a writer that is starting. WriteFence carries
// the same decision onto the write path of a writer that is already running,
// for the case a schema application is recorded underneath it -- a Helm upgrade
// records the new marker from a pre-upgrade hook while the previous pods still
// serve. It caches its decision, so the cost is one indexed marker read per
// interval rather than one per write.
package graphschemacompat
