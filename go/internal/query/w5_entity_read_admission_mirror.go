// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"github.com/eshu-hq/eshu/go/internal/query/entity"
)

// entityReadAdmissionProductionMirror proves the production stores
// jointly serve the #9669 W5 entity pin (the 9 W5 entity routes plus
// the W5c-e impact deployment-trace enrichment leg by explicit dual-pin
// cite): *Neo4jReader carries the two bounded graph traversals (Run,
// RunSingle), *ContentReader carries the seventeen ContentStore reads,
// the type-asserted EntityNameSearcher facet, and the three narrow
// read-model facets, PostgresCICDRunCorrelationStore carries the CI/CD
// run correlation read, and the two Postgres supply-chain stores carry
// the service-story supply-chain reads. The assignment below fails
// `go build`, not only `go test`, the moment a pin method is removed,
// renamed, or re-signed on any production store. It works in one
// direction only: a new method on any production store does not widen
// the pin — adoption takes deliberate re-declaration in entity plus a
// frozen-list update. It pins no shared port — this assertion names
// the production stores only as the shapes that jointly serve the pin,
// it keeps the handlers' separate store fields unchanged, and no other
// family may cite this pin as coverage (beyond the explicit Impact
// TraceContext dual-pin cite).
//
// Rollback: delete this file alongside the pin. It asserts only; it
// wires no handler, route, or caller.
type entityReadAdmissionProductionMirror struct {
	*Neo4jReader
	*ContentReader
	PostgresCICDRunCorrelationStore
	PostgresContainerImageIdentityStore
	PostgresSBOMAttestationAttachmentStore
}

var _ entity.EntityReadAdmission = (*entityReadAdmissionProductionMirror)(nil)
