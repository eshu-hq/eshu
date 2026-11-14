// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"github.com/eshu-hq/eshu/go/internal/query/impact"
	"github.com/eshu-hq/eshu/go/internal/query/impact/deployment"
)

// impactReadAdmissionProductionMirror proves the production stores
// jointly serve the #9669 W5 impact pin (the 13 W5 impact POSTs):
// *Neo4jReader carries the two bounded graph traversals (Run,
// RunSingle), *ContentReader carries the eight ContentStore reads and
// the code-topic investigation, PostgresKubernetesPodTemplateStore
// carries the two live pod-template reads, and PostgresECSTaskStore
// carries the running-task read. The assignment below fails `go
// build`, not only `go test`, the moment a pin method is removed,
// renamed, or re-signed on any production store. It works in one
// direction only: a new method on any production store does not widen
// the pin — adoption takes deliberate re-declaration in impact plus a
// frozen-list update. It pins no shared port — this assertion names
// the production stores only as the shapes that jointly serve the pin,
// it keeps the handler's separate store fields unchanged, and no other
// family may cite this pin as coverage.
//
// The trace-context enrichment leg is NOT served here: it requires
// EntityReadAdmission alongside this pin by explicit dual-pin cite
// (see the pin doc). This mirror covers the fourteen direct impact
// reads only.
//
// Rollback: delete this file alongside the pin. It asserts only; it
// wires no handler, route, or caller.
type impactReadAdmissionProductionMirror struct {
	*Neo4jReader
	*ContentReader
	deployment.PostgresKubernetesPodTemplateStore
	deployment.PostgresECSTaskStore
}

var _ impact.ImpactReadAdmission = (*impactReadAdmissionProductionMirror)(nil)
