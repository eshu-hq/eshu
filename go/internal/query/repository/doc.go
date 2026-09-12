// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package repository holds the repository-handler family (Issue #6060,
// lane B): the Handler HTTP surface plus every file that declares
// one of its methods, the catalog routes served through that handler, the
// catalog workload-enrichment reads, the entity-semantics shaping the story
// reads share with the entity layer, and the repository
// story/context/stats/coverage/tree/content/branches/freshness reads. The
// OpenAPI fragments documenting the repository routes stay in the query
// root beside openapi/spec.go (the B2 precedent for family moves); the route
// handlers live here. Read-model loaders the family shares with the staying
// root package live in querycontract; file-content artifact readers live in
// repositoryartifacts; ref resolution and page shaping live in
// repository/readmodel.
package repository //nolint:dirgate // Lane-B B3 repository family for #6060: 41 non-test files vs the 40-file cap; the 41st is the deployment-evidence store split the 500-line file cap forced, and splitting a subpackage mid-move rechurns the queryplan file pins.
