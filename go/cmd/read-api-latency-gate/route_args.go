// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

// RepresentativeScopeID is a scope_id this gate's own seed plan guarantees to
// exist: BuildSeedPlan names the first collector-kind's first scope this way
// (see seed_plan.go's ScopeID format), and scope.AllCollectorKinds()[0] is
// always CollectorGit, so this ID is stable across runs as long as
// SeedPlanOptions.TotalScopes >= 1.
var RepresentativeScopeID = fmt.Sprintf("seed-scope-%s-0000", scope.AllCollectorKinds()[0])

// RouteQueryArgs maps a no-arg GET route to a representative query string
// (no leading "?") that lets it run its real query instead of 400ing on a
// missing required selector — the vacuity fix for issue #6797: a route that
// only ever validates its input and never reaches the database is not a
// latency sample.
//
// This is a manually-curated, non-exhaustive subset: it covers routes whose
// handler accepts a `scope_id` selector (verified by reading their query
// package source; see each entry's comment) that this gate's seeded corpus
// actually has data for. A route needing a selector this gate has no
// seeded data for (package name, CVE id, repository id from an unseeded
// domain, ...) is intentionally left out — it stays not-exercised, counted
// against ExercisedCoverageFloor instead of being given a fabricated
// argument that would not exercise anything more real than the 400 it
// replaces.
var RouteQueryArgs = map[string]string{
	// go/internal/query/admission_decisions.go
	"GET /api/v0/evidence/admission-decisions": "scope_id=" + RepresentativeScopeID,
	// go/internal/query/ci_cd.go
	"GET /api/v0/ci-cd/run-correlations": "scope_id=" + RepresentativeScopeID,
	// go/internal/query/cloud_inventory_readback.go
	"GET /api/v0/cloud/inventory": "scope_id=" + RepresentativeScopeID,
	// go/internal/query/documentation.go
	"GET /api/v0/documentation/findings": "scope_id=" + RepresentativeScopeID,
	// go/internal/query/documentation_facts.go
	"GET /api/v0/documentation/facts": "scope_id=" + RepresentativeScopeID,
	// go/internal/query/kubernetes.go
	"GET /api/v0/kubernetes/correlations": "scope_id=" + RepresentativeScopeID,
	// go/internal/query/observability_coverage.go
	"GET /api/v0/observability/coverage/correlations": "scope_id=" + RepresentativeScopeID,
	// go/internal/query/semantic_evidence.go
	"GET /api/v0/semantic/code-hints":                 "scope_id=" + RepresentativeScopeID,
	"GET /api/v0/semantic/documentation-observations": "scope_id=" + RepresentativeScopeID,
}
