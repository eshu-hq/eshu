// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"github.com/eshu-hq/eshu/go/internal/query/cicd"
	"github.com/eshu-hq/eshu/go/internal/query/repository"
)

// repositoryStoryReadAdmissionProductionMirror proves the production
// stores jointly serve the #9669 W5 repository story pin (GET
// /api/v0/repositories/{repo_id}/story): *Neo4jReader carries the two
// bounded graph traversals (Run, RunSingle), *ContentReader carries
// the eight ContentStore reads and the four narrow read-model facets,
// and cicd.PostgresRunCorrelationStore carries the CI/CD run
// correlation read. The assignment below fails `go build`, not only
// `go test`, the moment a pin method is removed, renamed, or re-signed
// on any production store. It works in one direction only: a new
// method on any production store does not widen the pin — adoption
// takes deliberate re-declaration in repository plus a frozen-list
// update. It pins no shared port — this assertion names the production
// stores only as the shapes that jointly serve the pin, it keeps the
// handler's separate store fields unchanged, and no other route may
// cite this pin as coverage.
//
// Rollback: delete this file alongside the pin. It asserts only; it
// wires no handler, route, or caller.
type repositoryStoryReadAdmissionProductionMirror struct {
	*Neo4jReader
	*ContentReader
	cicd.PostgresRunCorrelationStore
}

var _ repository.RepositoryStoryReadAdmission = (*repositoryStoryReadAdmissionProductionMirror)(nil)
