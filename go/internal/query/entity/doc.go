// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package entity holds the entity-handler family (Issue #6060, lane B):
// the Handler HTTP surface (entity resolve, entity context, workload
// context/story, service context/story, service investigation) plus every
// file that declares one of its methods, the workload runtime-topology and
// provisioned-platform reads behind the workload context, and the
// platform-topology edge shaping the platform reads share. It also absorbs
// the B4 Handler service seam (service story envelope, service story
// handler, supply-chain enrichment, service investigation, service workload
// resolution): those methods moved with their receiver type.
//
// The OpenAPI fragments documenting the entity routes stay in the query
// root (openapi/paths/search/entities.go), where scripts/verify-openapi.sh
// requires every family's fragments to live; the root spec assembly
// consumes them directly.
//
// The *ContentReader target-support seam (service_story_target_support.go,
// service_story_target_support_source_only.go) stays in the query root: Go
// requires methods to live with their receiver type, and ContentReader is a
// later lane's family. Thin aliases and forwarders in the root
// entity_alias.go keep every other caller compiling unchanged.
//
// Failure contract (#7626, #7674): every route and the BuildServiceStoryEnvelope
// seam answer a failed read with querycontract.WriteGraphReadError's verdict
// first (a reader fence answers 503 with Retry-After), then the route's
// configuration-state 503 sentinels with only the sentinel's own text, then a
// fixed per-step message through tracing.WriteServerFailure or
// tracing.ServerFailureEnvelope: 500 with the error recorded on the request
// span, or 499 with no span error when the client canceled. Backend error text
// never reaches a response body.
package entity
