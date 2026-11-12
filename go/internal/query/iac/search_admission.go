// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iac

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// IaCInventorySearchAdmissionContractVersion versions the #9669 W3
// supplemental admission-contract shape for the active-inventory search read
// behind GET /api/v0/iac/resources. It adopts exactly the one
// InventoryStore.SearchActive read the resources list authorizes, pages, and
// generation-guards with — re-declared here with the exported
// IaCInventorySearch shape instead of embedding InventoryStore, so the pin
// grants nothing beyond that single read and no other family can cite it as
// coverage. A breaking change to the read shape fails the frozen method-set
// test until that list is deliberately updated; bump this version alongside
// (manually — the bump is convention, the frozen list is the enforcement)
// so dest consumers can pin the shape.
const IaCInventorySearchAdmissionContractVersion = "v0"

// IaCInventorySearchKind is the exported kind seam for the supplemental pin.
// InventorySearch.Kind carries the unexported resourceKind type, so a
// satellite package cannot construct a non-zero search through it; this
// exported kind names the same closed set with the same values, and
// toInventorySearch maps it back. A pin the satellite cannot call is void
// on arrival, so this seam is load-bearing, not convenience.
type IaCInventorySearchKind string

const (
	// IaCInventorySearchKindResource selects TerraformResource nodes.
	IaCInventorySearchKindResource IaCInventorySearchKind = "resource"
	// IaCInventorySearchKindModule selects TerraformModule nodes.
	IaCInventorySearchKindModule IaCInventorySearchKind = "module"
	// IaCInventorySearchKindDataSource selects TerraformDataSource nodes.
	IaCInventorySearchKindDataSource IaCInventorySearchKind = "data-source"
)

// IaCInventorySearch is the exported search shape for the supplemental pin.
// It mirrors InventorySearch field for field with the exported kind seam in
// place of the unexported one, so an external caller builds the full search
// — query, filters, keyset cursor, and limit — without naming internals.
type IaCInventorySearch struct {
	Kind       IaCInventorySearchKind
	Query      string
	Type       string
	Provider   string
	Module     string
	Repository string
	AfterName  string
	AfterID    string
	Limit      int
}

// toInventorySearch maps the exported search onto the internal shape the
// core-owned store serves. It reports false for any kind outside the closed
// set, so an unknown kind is rejected at the seam before the store is
// touched.
func (s IaCInventorySearch) toInventorySearch() (InventorySearch, bool) {
	kind := resourceKind(s.Kind)
	if _, ok := resourceKindLabels[kind]; !ok {
		return InventorySearch{}, false
	}
	return InventorySearch{
		Kind:       kind,
		Query:      s.Query,
		Type:       s.Type,
		Provider:   s.Provider,
		Module:     s.Module,
		Repository: s.Repository,
		AfterName:  s.AfterName,
		AfterID:    s.AfterID,
		Limit:      s.Limit,
	}, true
}

// IaCInventorySearchAdmission is the versioned supplemental admission-contract
// port for the active-inventory search read. The satellite keeps the HTTP
// shape and calls this versioned port, which the core-owned Postgres
// inventory store serves through the witness below. Narrow by construction:
// the single method re-declares the shared-port signature in this IaC-scoped
// shape instead of embedding or aliasing InventoryStore, so no other family
// can cite this pin as coverage.
//
// The IaCReadAdmission v0 pin stays untouched: its eight-method frozen set is
// byte-identical, and this supplement rides beside it, never inside it.
//
// Rollback: revert this pin. The seam is unwired — no handler, route, or
// caller depends on it yet — so rollback moves no traffic and changes no
// data.
type IaCInventorySearchAdmission interface {
	SearchActive(context.Context, IaCInventorySearch, querycontract.RepositoryAccessFilter) ([]InventoryCandidate, error)
}

// The mirror assertion keeps the supplemental pin honest about its source:
// PostgresIaCInventoryStore satisfies the internal InventoryStore port whose
// SearchActive this pin adopts, so the witness below composes the same
// store production serves. The pin itself mirrors only that one read — a
// new shared-port method is adopted only by deliberate re-declaration plus
// a frozen-list update.
var _ InventoryStore = PostgresIaCInventoryStore{}

// iacInventorySearchAdmissionWitness proves IaCInventorySearchAdmission is
// satisfiable: it composes the core-owned PostgresIaCInventoryStore and
// serves the pin by mapping the exported search onto the internal shape.
// Unknown kinds are rejected at the seam before the store is touched.
type iacInventorySearchAdmissionWitness struct {
	store PostgresIaCInventoryStore
}

func (w *iacInventorySearchAdmissionWitness) SearchActive(
	ctx context.Context,
	search IaCInventorySearch,
	access querycontract.RepositoryAccessFilter,
) ([]InventoryCandidate, error) {
	internal, ok := search.toInventorySearch()
	if !ok {
		return nil, fmt.Errorf("unknown IaC inventory kind %q", search.Kind)
	}
	return w.store.SearchActive(ctx, internal, access)
}

var _ IaCInventorySearchAdmission = (*iacInventorySearchAdmissionWitness)(nil)
