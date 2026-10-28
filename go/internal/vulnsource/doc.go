// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package vulnsource defines shared vulnerability source checkpoint and
// freshness state values used by collectors, storage, status, and query
// surfaces.
//
// SourceForScopeID derives the stored source token from a vulnerability scope
// id; it is the one parser shared by the extension host's source-state
// recorder and pinned against the coordinator's and the readiness reader's
// scope grammar (#9420). DefaultFreshnessWindow is the shared freshness window.
package vulnsource
