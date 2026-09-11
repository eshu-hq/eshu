// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package service holds the service-handler family (Issue #6060, lane B):
// the CatalogHandler HTTP surface plus every pure helper behind the
// service context/story/investigation reads, the service evidence and
// hostname decoders, the service query enrichment, the service-story
// dossier/overview/evidence-graph/supply-chain/trace-path shaping, and the
// family's capability row. The OpenAPI fragments documenting the service
// routes live in openapi/paths/service/, which scripts/verify-openapi.sh
// scans recursively; openapi/spec.go assembles them into the published
// document.
//
// The root package keeps the *ContentReader route methods
// (service_story_target_support.go,
// service_story_target_support_source_only.go) and the entity package keeps
// the *EntityHandler ones (entity/service_investigation.go,
// entity/service_story_handler.go, entity/service_story_seam.go,
// entity/service_story_supply_chain.go,
// entity/service_workload_resolution.go): Go requires methods to live with
// their receiver type. Those callers use this package's exported
// homes; thin aliases and forwarders in the root service_alias.go keep
// every other caller compiling unchanged.
package service
