// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package discovery resolves parser-supported files into stable repo-root file
// sets for the Go collector.
//
// Discovery applies repo-local .gitignore and .eshuignore files, including
// root-anchored gitignore patterns, operator overlays, hidden/generated/vendor
// skip rules, nested Git repository preference, and deterministic sorting
// before the collector snapshots files.
//
// PathRules compiles the location-only rules in Options. The walk applies them,
// and PathRules.ExcludesFile applies the same decision to a repo-relative path
// the walk never visited, so a caller cannot drift from the walk by copying the
// matchers. Repo-local .gitignore and .eshuignore rules are not part of it.
package discovery
