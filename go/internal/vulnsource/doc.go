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
//
// The package also owns the coordinator-minted scope shapes a core binary must
// read without a collector. ParseDerivedOSVSlotScope, DerivedScopePackageKeys,
// and SlotScopePackageKeys read the derived OSV forms and yield the package keys
// readiness joins on; NVDWindowScopeID and ParseNVDWindowScope mint and read the
// per-window NVD form, with NormalizeNVDWindowTime keeping planned, sent, and
// stored bounds bit-identical. Its only Eshu dependency is packageidentity.
package vulnsource
