// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"strings"
	"testing"
)

// TestServiceChangedSinceSharedServiceIDIsRefused covers the collision the
// grant binding's first shape missed (#6472 review, P1).
//
// Admission used to be existential: one service-catalog correlation inside the
// caller's grant admitted the lineage read. But a catalog entity ref is
// relative to the catalog that declared it, not tenant-qualified, so two
// tenants that both run a service called `api` write the same
// `component:default/api` as their `service_id`.
// service_materialization_generations carries no repository, scope, or tenant
// column and the reducer's writer conflict key is the service id alone, so
// there is exactly one lineage for that id: whichever tenant materialized last
// owns the active generation. Tenant A's own correlation would then admit a
// read whose counts, generation ids, and sample evidence keys come from tenant
// B's materialization.
//
// Admission is now exclusive: the read runs only when nothing outside the
// caller's grant also correlates that service id. A shared id is refused with
// the route's ordinary not-found, exactly as an ungranted or absent one is.
// Splitting the lineage itself needs a scope column on those tables; until that
// schema change lands, refusing an ambiguous id is the only answer that cannot
// hand one tenant another tenant's evidence.
func TestServiceChangedSinceSharedServiceIDIsRefused(t *testing.T) {
	t.Parallel()

	t.Run("a shared service id is refused before the lineage read", func(t *testing.T) {
		t.Parallel()

		rec, reader, ownership := serveServiceChangedSinceGrant(
			t, serviceChangedSinceGrantShared, scopedServiceChangedSinceTenantA(),
		)

		// Mutation-sensitive: this is the cross-tenant read itself. Make
		// admission existential again -- admit as soon as one granted
		// correlation exists -- and tenant A gets 200 with a delta computed
		// from whichever tenant's lineage is currently active for the id.
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d; a service id claimed outside the grant must not resolve; body = %s",
				rec.Code, http.StatusNotFound, rec.Body.String())
		}
		// Mutation-sensitive: the exclusivity probe is the only thing that can
		// see the second owner, so the ownership store has to be reachable
		// here at all. The status assertion above, not this flag, is what
		// proves the probe ran.
		if !ownership.touched {
			t.Fatal("ownership store was never consulted for a scoped caller; the grant is not bound")
		}
		// Mutation-sensitive: filtering after the read would still answer 404,
		// but would already have read the other tenant's evidence snapshots.
		if reader.touched {
			t.Fatal("lineage reader was called for a shared service id; the refusal must precede the read")
		}
		_, errEnvelope := decodeServiceChangedSinceEnvelope(t, rec)
		if got, want := errEnvelope["code"], string(ErrorCodeServiceNotFound); got != want {
			t.Fatalf("error.code = %v, want %q; the refusal must be the ordinary service-not-found", got, want)
		}
		// The caller owns one of the two correlations, so it may well know the
		// id it typed. What it must not learn is anything about the other
		// owner or about the lineage it was refused.
		for _, leak := range []string{"repo-b", "scope-b", "gen-current-shared"} {
			if strings.Contains(rec.Body.String(), leak) {
				t.Fatalf("not-found body leaks refused detail %q: %s", leak, rec.Body.String())
			}
		}
	})

	t.Run("the refusal is indistinguishable from an absent service", func(t *testing.T) {
		t.Parallel()

		shared, _, _ := serveServiceChangedSinceGrant(
			t, serviceChangedSinceGrantShared, scopedServiceChangedSinceTenantA(),
		)
		absent, _, _ := serveServiceChangedSinceGrant(
			t, serviceChangedSinceGrantAbsent, scopedServiceChangedSinceTenantA(),
		)

		// Mutation-sensitive: a distinct status, code, or message for the
		// shared case would tell tenant A that some other tenant also runs a
		// service by that name -- a cross-tenant existence oracle keyed on a
		// name the caller can guess. Once the caller's own echoed selector is
		// normalized the two answers must be byte-identical.
		sharedShape := strings.ReplaceAll(shared.Body.String(), serviceChangedSinceGrantShared, "SELECTOR")
		absentShape := strings.ReplaceAll(absent.Body.String(), serviceChangedSinceGrantAbsent, "SELECTOR")
		if shared.Code != absent.Code || sharedShape != absentShape {
			t.Fatalf("a shared service id is distinguishable from an absent one:\n shared: %d %s\n absent: %d %s",
				shared.Code, sharedShape, absent.Code, absentShape)
		}
	})

	t.Run("a single-owner service in the grant still resolves", func(t *testing.T) {
		t.Parallel()

		// Mutation-sensitive: the failure mode of an exclusivity check is
		// refusing everything. Bind the probe to the wrong arrays, or forget
		// to negate the predicate, and the caller's own single-owner service
		// 404s here while the deny cases above stay green.
		rec, reader, _ := serveServiceChangedSinceGrant(
			t, serviceChangedSinceGrantServiceA, scopedServiceChangedSinceTenantA(),
		)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; a service only the caller owns must still resolve; body = %s",
				rec.Code, http.StatusOK, rec.Body.String())
		}
		if !reader.touched {
			t.Fatal("lineage reader was never called for a single-owner granted service")
		}
		data, _ := decodeServiceChangedSinceEnvelope(t, rec)
		if got, want := data["service_id"], serviceChangedSinceGrantServiceA; got != want {
			t.Fatalf("data[service_id] = %v, want %q", got, want)
		}
	})

	t.Run("an exclusivity probe failure is a 500, not a silent not-found", func(t *testing.T) {
		t.Parallel()

		// The shipped store rejects an outside-grant read that carries no
		// grant arrays, the same way it rejects a zero Limit. A handler that
		// folded that error into the refusal would hide a broken deployment
		// behind an answer that looks like ordinary tenant isolation.
		rec, reader := serveServiceChangedSinceOwnership(
			t, serviceChangedSinceGrantServiceA, scopedServiceChangedSinceTenantA(),
			&failingServiceOwnership{err: errServiceCatalogOutsideGrantNeedsAGrant},
		)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want %d; an ownership resolution failure must surface; body = %s",
				rec.Code, http.StatusInternalServerError, rec.Body.String())
		}
		if reader.touched {
			t.Fatal("lineage reader was called after the ownership store failed")
		}
	})
}
