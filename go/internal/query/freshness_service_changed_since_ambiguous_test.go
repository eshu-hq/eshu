// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"testing"
)

// TestServiceChangedSinceAmbiguousCandidateOutsideTheGrantIsRefused covers the
// second hole review round 3 found in the exclusivity fence (#6472 review,
// P1-B).
//
// The reducer's ambiguous branches (classifyServiceCatalogEntity and
// classifyRepoLocalServiceCatalogEntity, internal/reducer) leave
// decision.RepositoryID empty and list every matched repository in
// decision.CandidateRepositoryIDs. Such a decision still carries a service_id
// and an owner_ref, so buildServiceOwnershipMaterializations still writes it as
// a generation: an ambiguous correlation reaches the same globally keyed
// lineage an exact one does.
//
// The admission arm matches a row when ANY candidate is granted
// (candidate_repository_ids ?| $13). While the inverted statement was the plain
// negation of that arm, that same any-candidate match also made the whole row
// count as inside for the exclusivity probe. A row naming one repository the
// caller owns and one it does not looked uncontested, and the caller was
// admitted onto lineage the ungranted candidate also claims.
//
// So the two statements are not complements. A row is inside only when the
// grant covers some of its ownership evidence AND no candidate falls outside
// the grant.
func TestServiceChangedSinceAmbiguousCandidateOutsideTheGrantIsRefused(t *testing.T) {
	t.Parallel()

	t.Run("one ungranted candidate refuses the whole row", func(t *testing.T) {
		t.Parallel()

		// Mutation-sensitive: make the inverted statement the plain negation of
		// the admission arm again and this answers 200 carrying
		// "gen-current-shared" -- the cross-tenant read itself, reached through
		// a row the caller partly owns.
		ownership := &grantMirroringServiceOwnership{rows: ambiguousCandidateServiceCorrelations()}
		rec, reader := serveServiceChangedSinceOwnership(
			t, serviceChangedSinceGrantShared, scopedServiceChangedSinceTenantA(), ownership,
		)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d; an ambiguous row naming an ungranted candidate must not resolve; body = %s",
				rec.Code, http.StatusNotFound, rec.Body.String())
		}
		// Mutation-sensitive: filtering after the read would still answer 404,
		// having already read the contested tenant's evidence snapshots. The
		// refusal has to come before the lineage read.
		if reader.touched {
			t.Fatal("lineage reader was called for an ambiguous row with an ungranted candidate")
		}
		_, errEnvelope := decodeServiceChangedSinceEnvelope(t, rec)
		if got, want := errEnvelope["code"], string(ErrorCodeServiceNotFound); got != want {
			t.Fatalf("error.code = %v, want %q; the refusal must be the ordinary service-not-found", got, want)
		}
	})

	t.Run("an ambiguous row whose candidates are all granted still resolves", func(t *testing.T) {
		t.Parallel()

		// The failure mode of tightening an exclusivity check is refusing
		// everything. One tightening that looks right -- outside when the
		// repository_id is not granted OR some candidate is not granted --
		// refuses this row too, because an ambiguous decision never carries a
		// repository_id at all. Every service with any ambiguity would then
		// 404 for the tenant that wholly owns it. The row-truth table in the
		// evidence doc measures both shapes on Postgres 16.
		ownership := &grantMirroringServiceOwnership{rows: ambiguousCandidateServiceCorrelations()}
		rec, reader := serveServiceChangedSinceOwnership(
			t, serviceChangedSinceGrantServiceA, scopedServiceChangedSinceTenantA(), ownership,
		)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; ambiguity wholly inside the grant is still the caller's own service; body = %s",
				rec.Code, http.StatusOK, rec.Body.String())
		}
		if !reader.touched {
			t.Fatal("lineage reader was never called for an ambiguous row the grant wholly covers")
		}
		data, _ := decodeServiceChangedSinceEnvelope(t, rec)
		if got, want := data["service_id"], serviceChangedSinceGrantServiceA; got != want {
			t.Fatalf("data[service_id] = %v, want %q", got, want)
		}
	})
}

// mirroredServiceCorrelationMatchesGrantArm applies whichever of the two
// shipped grant arms the filter selected, so the fake answers each statement
// with that statement's own predicate instead of assuming they are
// complements.
//
// It lives beside the ambiguity tests rather than in the grant fixture file for
// a boring reason: that file is close to the repo's 500-line cap.
func mirroredServiceCorrelationMatchesGrantArm(
	filter ServiceCatalogCorrelationFilter, row mirroredServiceCorrelation,
) bool {
	if filter.OutsideGrant {
		return mirroredServiceCorrelationIsOutsideGrant(filter, row)
	}
	return mirroredServiceCorrelationGrantAdmits(filter, row)
}

// mirroredServiceCorrelationIsOutsideGrant is the inverted statement's grant
// clause: a row is outside unless its scope is granted, or the grant covers
// some of its ownership evidence AND every candidate repository it names is
// granted too.
//
// The last conjunct is the P1-B fix. Without it a row listing one granted and
// one ungranted candidate reads as inside, and the exclusivity probe reports
// nothing for a service id another tenant also claims.
func mirroredServiceCorrelationIsOutsideGrant(
	filter ServiceCatalogCorrelationFilter, row mirroredServiceCorrelation,
) bool {
	// fact.scope_id = ANY($14): a granted scope admits the row whatever its
	// payload says.
	if containsAuthString(filter.AllowedScopeIDs, row.scopeID) {
		return false
	}
	// (repository_id = ANY($13) OR candidate_repository_ids ?| $13): does the
	// grant cover any of this row's ownership evidence at all?
	granted := containsAuthString(filter.AllowedRepositoryIDs, row.repositoryID)
	for _, candidate := range row.candidateRepositoryIDs {
		if containsAuthString(filter.AllowedRepositoryIDs, candidate) {
			granted = true
			break
		}
	}
	if !granted {
		return true
	}
	// candidate_repository_ids <@ to_jsonb($13): is every candidate granted?
	// A row with no candidate list has no ungranted candidate, which is the
	// COALESCE(..., TRUE) in the shipped statement.
	for _, candidate := range row.candidateRepositoryIDs {
		if !containsAuthString(filter.AllowedRepositoryIDs, candidate) {
			return true
		}
	}
	return false
}

// ambiguousCandidateServiceCorrelations is the ambiguity fixture: the three row
// shapes the inverted grant clause has to tell apart, written the way the
// reducer emits them. An ambiguous decision carries no repository_id, only
// candidates, which is the detail that makes the over-refusing tightening
// wrong.
func ambiguousCandidateServiceCorrelations() []mirroredServiceCorrelation {
	return []mirroredServiceCorrelation{
		// One candidate the caller owns, one it does not. The admission arm
		// matches on the first; the exclusivity arm has to fire on the second.
		{
			serviceID:              serviceChangedSinceGrantShared,
			candidateRepositoryIDs: []string{"repo-a", "repo-b"},
			scopeID:                "scope-b",
		},
		// Ambiguous, but every candidate is inside the grant.
		{
			serviceID:              serviceChangedSinceGrantServiceA,
			candidateRepositoryIDs: []string{"repo-a"},
			scopeID:                "scope-b",
		},
		// The unambiguous shape, with no candidate key at all. The arm that
		// reads candidates has to leave this row exactly where it was.
		{
			serviceID:    serviceChangedSinceGrantServiceB,
			repositoryID: "repo-b",
			scopeID:      "scope-b",
		},
	}
}
