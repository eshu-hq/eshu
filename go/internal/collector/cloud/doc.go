// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package cloud groups the collector-owned public-cloud provider collectors
// that read provider control-plane metadata for inventory and drift evidence.
//
// This package is a documentation-only namespace. Collector implementations
// live in its leaf packages; collection wiring, fact emission, and runtime
// behavior stay in the owning provider packages. Provider resource packages
// move with their collector, and production cutover to separate collector
// repositories follows the ecosystem inventory, not this nesting.
package cloud
