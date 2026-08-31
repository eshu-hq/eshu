// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package gcpplanner validates GCP Cloud Asset Inventory planning requests
// and builds deterministic workflow rows for enabled configured scopes.
//
// The planner requires explicit `live_collection_enabled=true` opt-in before
// admitting any claim-enabled scheduling, defaults optional scope fields
// (asset_type_family, content_family, location_bucket) and derives a scope ID
// when one is not configured, plans scopes in sorted scope-ID order, and
// emits privacy-safe requested-scope metadata that omits credential
// references. EnabledScopes and ValidateClaimSchedulerConfiguration expose
// the same parsing and validation for the root coordinator's freshness
// handoff loop and config loader, which need it without depending on this
// package's private configuration types. Callers retain responsibility for
// clocks, scheduling, tenant-grant authorization, durable admission, freshness
// trigger claim/handoff/reap, retries, queue and lease behavior, and
// telemetry.
package gcpplanner
