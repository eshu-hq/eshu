// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package search implements entity search and result enrichment for the
// code-family queries: global entity-name search over the content store
// and content-metadata enrichment of graph search results. It split out
// of package codequery (#6060 naming follow-up) so search can be read,
// tested, and changed without pulling in the rest of the code surface,
// and so it can one day move into its own repo.
//
// The split keeps thin *CodeHandler methods in codequery
// (global_name_search.go, search_metadata.go) -- methods must live in
// their type's package -- and calls this leaf through qualification.
// The dead-code investigation next-call shapers that shared the old
// search_metadata.go home moved to the deadcode leaf, which owns them.
//
// Import discipline: this package may import the same dependency-neutral
// leaves codequery uses (entitysemantics, querycontract) but NEVER
// package codequery itself and NEVER root package query -- both would
// create an import cycle (codequery calls this leaf, and root aliases
// codequery).
package search
