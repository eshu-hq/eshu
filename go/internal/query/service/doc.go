// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package service holds the service-handler family (Issue #6060, lane B):
// the ServiceCatalogHandler HTTP surface plus every pure helper behind the
// service context/story/investigation reads, the service evidence and
// hostname decoders, the service query enrichment, the service-story
// dossier/overview/evidence-graph/supply-chain/trace-path shaping, and the
// family's capability row. The OpenAPI fragments documenting the service
// routes stay in the query root (openapi_paths_service_*.go), where
// scripts/verify-openapi.sh requires every family's fragments to live;
// the root spec assembly consumes them directly.
//
// The staying root package keeps the *EntityHandler and *ContentReader
// route methods (service_investigation.go, service_story_handler.go,
// service_story_seam.go, service_story_supply_chain.go,
// service_workload_resolution.go, service_story_target_support.go,
// service_story_target_support_source_only.go): Go requires methods to live
// with their receiver type. Those stayers call into this package's exported
// homes; thin aliases and forwarders in the root service_alias.go keep
// every other caller compiling unchanged.
package service
