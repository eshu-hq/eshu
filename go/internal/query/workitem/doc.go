// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package workitem implements the source-only work-item evidence read route
// behind GET /api/v0/work-items/evidence (#6642, split off the #6060 lane A
// query-root restructure): Handler, its Mount method and route dispatch, the
// Postgres-backed EvidenceStore, the typed work_item.* fact decode wrappers,
// and the evidence-state classification, pagination, and span-attribute
// shaping that surface reports.
//
// Handler gates every read on the shared work_item.evidence.list capability
// (querycontract.CapabilityUnsupported) before touching the store, resolves
// scoped-token repository grants through querycontract before dispatch (an
// empty grant returns the bounded zero-evidence page without a store read),
// and reports every result under querycontract.TruthBasisSemanticFacts: the
// route is source-only and never verifies pull request, commit, deployment,
// incident, runtime artifact, image, version, or service identity from Jira
// evidence alone. A fact whose payload is missing a required identity anchor
// is classified input_invalid by the typed decode seam and dropped from the
// result -- logged at debug level -- rather than producing a row with an
// empty-string identity (Contract System v1's accuracy guarantee).
//
// This package imports querycontract (profiles, envelopes, capability
// registration, HTTP helpers, the repository-access-filter port), queryauth
// (the scoped-token AuthContext its own tests construct directly), decode
// (the classified decode-error type), queryspan (the shared handler-span
// seam), sdk/go/factschema and sdk/go/factschema/workitem/v1 (the typed
// decode seam), and go/internal/facts (the work_item fact-kind registry); it
// MUST NOT import the query root, or root would cycle back through its own
// compatibility aliases in work_item_alias.go, which import this package for
// the Handler type alias and the forwarders cmd/api and cmd/mcp-server wiring
// still use. See README.md for the file layout and move evidence, and
// AGENTS.md for the per-symbol export rationale.
package workitem
