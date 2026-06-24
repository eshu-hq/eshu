// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package nodelockfile parses Node/TypeScript package-manager lockfiles
// (yarn.lock classic, yarn.lock Berry, pnpm-lock.yaml) into
// content_entity dependency rows compatible with the npm ecosystem identity
// matching used by the supply-chain impact reducer.
//
// Parse reads one yarn or pnpm lockfile and returns a parser payload whose
// "variables" bucket carries one row per resolved package. Each row preserves
// the canonical npm ecosystem (package_manager = "npm") alongside the
// explicit package manager flavor (package_manager_flavor = "yarn" or
// "pnpm"), the exact installed version from the lockfile, the
// importer-derived dependency_path / dependency_depth / direct_dependency
// evidence, and runtime-vs-dev scope where the lockfile records it.
//
// Workspace and file-protocol entries are intentionally NOT emitted as remote
// package rows. Those entries do not prove a package/version identity the
// vulnerability impact reducer can join to a registry advisory; treating them
// as remote would invent a false positive. Malformed lockfiles are recorded
// through lockfile_parse_state, and unsupported Yarn Berry protocols are emitted
// as audit-only unsupported_dependency rows with lockfile_unsupported_feature so
// readiness can surface evidence gaps instead of returning nothing.
package nodelockfile
