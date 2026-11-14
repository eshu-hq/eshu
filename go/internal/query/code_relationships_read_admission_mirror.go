// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"github.com/eshu-hq/eshu/go/internal/query/codequery"
)

// codeRelationshipProductionMirror proves the production stores
// jointly serve the #9669 G-b pin (POST /api/v0/code/relationships
// and POST /api/v0/code/relationships/story): *Neo4jReader carries
// the two bounded graph traversals (Run, RunSingle) and
// *ContentReader carries the twelve ContentStore reads plus the three
// grant-scoped reads (GetEntityContentInRepositories,
// SearchEntitiesByNameInRepositories,
// SearchEntitiesByLanguageAndTypeForAccess — the grant three live on
// *ContentReader, not on the ContentStore interface, so a
// GraphQuery+ContentStore mirror alone cannot prove them). The
// assignment below fails `go build`, not only `go test`, the moment a
// pin method is removed, renamed, or re-signed on either production
// store. It works in one direction only: a new method on either
// production store does not widen the pin — adoption takes deliberate
// re-declaration in codequery plus a frozen-list update. It pins no
// shared port — this assertion names *Neo4jReader and *ContentReader
// only as the production shapes that jointly serve the pin, it keeps
// the handlers' separate GraphQuery/ContentStore fields unchanged,
// and no other family may cite this pin as coverage.
//
// Rollback: delete this file alongside the pin. It asserts only; it
// wires no handler, route, or caller.
type codeRelationshipProductionMirror struct {
	*Neo4jReader
	*ContentReader
}

var _ codequery.CodeRelationshipReadAdmission = (*codeRelationshipProductionMirror)(nil)
