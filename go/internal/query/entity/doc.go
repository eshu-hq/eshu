// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package entity holds the entity-handler family (Issue #6060, lane B):
// the EntityHandler HTTP surface (entity resolve, entity context, workload
// context/story, service context/story, service investigation) plus every
// file that declares one of its methods, the workload runtime-topology and
// provisioned-platform reads behind the workload context, and the
// platform-topology edge shaping the platform reads share. It also absorbs
// the B4 EntityHandler service seam (service story envelope, service story
// handler, supply-chain enrichment, service investigation, service workload
// resolution): those methods moved with their receiver type.
//
// The OpenAPI fragments documenting the entity routes stay in the query
// root (openapi_paths_entities.go), where scripts/verify-openapi.sh
// requires every family's fragments to live; the root spec assembly
// consumes them directly.
//
// The *ContentReader target-support seam (service_story_target_support.go,
// service_story_target_support_source_only.go) stays in the query root: Go
// requires methods to live with their receiver type, and ContentReader is a
// later lane's family. Thin aliases and forwarders in the root
// entity_alias.go keep every other caller compiling unchanged.
package entity
