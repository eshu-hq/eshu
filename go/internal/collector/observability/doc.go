// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package observability groups collector-owned source observers that read
// live observability-provider metadata for freshness and drift evidence.
//
// This package is a documentation-only namespace. Observer implementations
// live in its leaf packages; collection wiring, fact emission, and runtime
// behavior stay in the owning collector packages.
package observability
