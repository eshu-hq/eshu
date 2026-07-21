// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
)

const (
	// EffectiveOwnerSourceServiceCatalog labels an effective owner resolved
	// from a service-catalog manifest declaration.
	EffectiveOwnerSourceServiceCatalog = "service_catalog"
	// EffectiveOwnerSourceCodeowners labels an effective owner resolved from
	// a repository's CODEOWNERS rules (last-match-wins).
	EffectiveOwnerSourceCodeowners = "codeowners"
)

// effectiveRepositoryOwnerCorrelationLimit bounds the manifest-precedence
// lookup. It reuses serviceCatalogCorrelationMaxLimit (200): a single
// repository can have more than one catalog provider/entity correlated to it,
// and the resolver must see every row to find an exact/derived declaration,
// not just the first page.
const effectiveRepositoryOwnerCorrelationLimit = serviceCatalogCorrelationMaxLimit

// EffectiveRepositoryOwner is the resolved owner_ref plus its provenance for
// GET /api/v0/codeowners/ownership's "effective_owner" field. A zero value
// (empty OwnerRef and Source) means neither source resolved an owner.
type EffectiveRepositoryOwner struct {
	OwnerRef string `json:"owner_ref,omitempty"`
	Source   string `json:"source,omitempty"`
}

// resolveEffectiveRepositoryOwner implements the manifest-vs-codeowners
// precedence contract (issue #5419 Phase 4):
//
//  1. A service-catalog manifest declaration wins when
//     ListServiceCatalogCorrelations returns a row for repoID with a
//     non-empty OwnerRef and an "exact" or "derived" reducer Outcome. Other
//     outcomes ("ambiguous", "unresolved", "stale", "rejected") are not a
//     resolved manifest declaration and are skipped even when OwnerRef is
//     non-empty, so a disputed or stale catalog claim never outranks a live
//     CODEOWNERS rule.
//  2. Otherwise, the repository's CODEOWNERS rules apply with last-match-wins
//     semantics (sdk/go/factschema/codeowners/v1.Ownership's documented
//     contract): the DECLARES_CODEOWNER edge with the highest order_index is
//     the last pattern in the file that would match, so its owner is the
//     repository-wide fallback. codeownersLastMatchOwnerCypher resolves this
//     with a dedicated DESC-ordered, LIMIT-1 read (see its doc comment for
//     why the paginated ascending list cannot be reused here).
//  3. If neither source resolves an owner, the zero-value
//     EffectiveRepositoryOwner is returned -- not an error, since "no
//     resolvable owner" is a valid, common repository state.
//
// correlations or neo4j may be nil; a nil correlations store skips step 1 and
// a nil neo4j reader skips step 2, so a caller invoking this without full
// wiring still gets whichever precedence branch it can serve.
func resolveEffectiveRepositoryOwner(
	ctx context.Context,
	neo4j GraphQuery,
	correlations ServiceCatalogCorrelationStore,
	repoID string,
) (EffectiveRepositoryOwner, error) {
	if correlations != nil {
		rows, err := correlations.ListServiceCatalogCorrelations(ctx, ServiceCatalogCorrelationFilter{
			RepositoryID: repoID,
			Limit:        effectiveRepositoryOwnerCorrelationLimit,
		})
		if err != nil {
			return EffectiveRepositoryOwner{}, fmt.Errorf("resolve manifest owner: %w", err)
		}
		for _, row := range rows {
			if row.OwnerRef == "" {
				continue
			}
			if row.Outcome == "exact" || row.Outcome == "derived" {
				return EffectiveRepositoryOwner{
					OwnerRef: row.OwnerRef,
					Source:   EffectiveOwnerSourceServiceCatalog,
				}, nil
			}
		}
	}

	if neo4j == nil {
		return EffectiveRepositoryOwner{}, nil
	}
	cypher, params := codeownersLastMatchOwnerCypher(repoID)
	row, err := neo4j.RunSingle(ctx, cypher, params)
	if err != nil {
		return EffectiveRepositoryOwner{}, fmt.Errorf("resolve codeowners last-match owner: %w", err)
	}
	ownerRef := StringVal(row, "owner_ref")
	if ownerRef == "" {
		return EffectiveRepositoryOwner{}, nil
	}
	return EffectiveRepositoryOwner{OwnerRef: ownerRef, Source: EffectiveOwnerSourceCodeowners}, nil
}
