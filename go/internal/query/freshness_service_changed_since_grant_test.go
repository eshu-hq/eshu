// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/status"
)

// The catalog entity refs the two-tenant fixture uses. They are catalog entity
// refs (`component:default/...`), not graph workload ids: the service lineage
// tables key on the reducer's decision.ServiceID, which is the catalog ref the
// collector emitted, so a fixture that used `workload:...` here would prove
// nothing about the identifier this route actually receives.
const (
	serviceChangedSinceGrantServiceA = "component:default/tenant-a"
	serviceChangedSinceGrantServiceB = "component:default/tenant-b"
	serviceChangedSinceGrantAbsent   = "component:default/nowhere"
	serviceChangedSinceGrantPriorGen = "gen-prior"
)

// mirroredServiceCorrelation is one reducer_service_catalog_correlation fact
// reduced to the four values listServiceCatalogCorrelationsQuery's service and
// grant arms read: the service_id selector arm ($6), and the three grant
// disjuncts ($13/$14) -- payload repository_id, payload
// candidate_repository_ids, and the row's own ingestion scope_id.
type mirroredServiceCorrelation struct {
	serviceID              string
	repositoryID           string
	candidateRepositoryIDs []string
	scopeID                string
}

// grantMirroringServiceOwnership is the #5167 two-tenant ownership fixture, in
// the same shape grantMirroringChangedSince carries for the sibling route: the
// fake does not merely record the filter it was handed, it applies the SAME
// intersection listServiceCatalogCorrelationsQuery applies
// (service_catalog_correlations.go, the $6 and $13/$14 arms), so a handler that
// stops passing the caller's grant resolves the other tenant's service here
// exactly as it would in Postgres, and the assertions below fail.
type grantMirroringServiceOwnership struct {
	rows    []mirroredServiceCorrelation
	touched bool
}

func (g *grantMirroringServiceOwnership) ListServiceCatalogCorrelations(
	_ context.Context, filter ServiceCatalogCorrelationFilter,
) ([]ServiceCatalogCorrelationRow, error) {
	g.touched = true

	out := make([]ServiceCatalogCorrelationRow, 0, len(g.rows))
	for _, row := range g.rows {
		// The service selector arm: ($6 = '' OR payload->>'service_id' = $6).
		if filter.ServiceID != "" && filter.ServiceID != row.serviceID {
			continue
		}
		if !mirroredServiceCorrelationGrantAdmits(filter, row) {
			continue
		}
		out = append(out, ServiceCatalogCorrelationRow{
			CorrelationID:          "fact-" + row.serviceID,
			ServiceID:              row.serviceID,
			RepositoryID:           row.repositoryID,
			CandidateRepositoryIDs: row.candidateRepositoryIDs,
		})
		if filter.Limit > 0 && len(out) >= filter.Limit {
			break
		}
	}
	return out, nil
}

// mirroredServiceCorrelationGrantAdmits is the $13/$14 arm of the shipped
// predicate, including the part that makes the handler's access.Empty() short
// circuit load-bearing: when BOTH grant arrays are empty the whole disjunction
// collapses to TRUE, so a scoped caller with no grant at all would read every
// tenant's correlation if the handler ever reached the store with an empty
// grant.
func mirroredServiceCorrelationGrantAdmits(
	filter ServiceCatalogCorrelationFilter, row mirroredServiceCorrelation,
) bool {
	if len(filter.AllowedRepositoryIDs) == 0 && len(filter.AllowedScopeIDs) == 0 {
		return true
	}
	if containsAuthString(filter.AllowedRepositoryIDs, row.repositoryID) {
		return true
	}
	for _, candidate := range row.candidateRepositoryIDs {
		if containsAuthString(filter.AllowedRepositoryIDs, candidate) {
			return true
		}
	}
	return containsAuthString(filter.AllowedScopeIDs, row.scopeID)
}

// touchRecordingServiceChangedSince is the lineage reader with a touched flag.
// The flag is the point of the fixture: the grant binding for this route is a
// refusal BEFORE the lineage read, so "did the ungranted caller's request reach
// the lineage tables at all" is the assertion, not only the status code.
type touchRecordingServiceChangedSince struct {
	lineage map[string]string
	touched bool
}

func (f *touchRecordingServiceChangedSince) ComputeServiceChangedSinceDelta(
	_ context.Context, filter status.ServiceChangedSinceFilter,
) (status.ServiceChangedSinceSummary, error) {
	f.touched = true

	current, ok := f.lineage[filter.ServiceID]
	if !ok {
		// An unknown service resolves to an empty summary, which the handler
		// turns into its ordinary service-not-found.
		return status.ServiceChangedSinceSummary{}, nil
	}
	return status.ServiceChangedSinceSummary{
		ServiceID:                 filter.ServiceID,
		SinceGenerationID:         filter.SinceGenerationID,
		CurrentActiveGenerationID: current,
		SampleLimit:               filter.SampleLimit,
		Categories: []status.ChangedSinceCategoryDelta{{
			Category: status.ChangedSinceCategoryOwnership,
			Counts:   status.ChangedSinceCounts{Added: 1},
		}},
	}, nil
}

func twoTenantServiceCorrelations() []mirroredServiceCorrelation {
	return []mirroredServiceCorrelation{
		{serviceID: serviceChangedSinceGrantServiceA, repositoryID: "repo-a", scopeID: "scope-a"},
		{serviceID: serviceChangedSinceGrantServiceB, repositoryID: "repo-b", scopeID: "scope-b"},
	}
}

func twoTenantServiceLineage() map[string]string {
	return map[string]string{
		serviceChangedSinceGrantServiceA: "gen-current-repo-a",
		serviceChangedSinceGrantServiceB: "gen-current-repo-b",
	}
}

func scopedServiceChangedSinceTenantA() AuthContext {
	return AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: []string{"repo-a"},
		AllowedScopeIDs:      []string{"scope-a"},
	}
}

// serveServiceChangedSinceOwnership drives the production handler with an
// explicit ownership store so a case can pass nil for it. Each call builds its
// own fakes so parallel subtests never share a touched flag.
func serveServiceChangedSinceOwnership(
	t *testing.T, serviceID string, auth AuthContext, ownership ServiceCatalogCorrelationStore,
) (*httptest.ResponseRecorder, *touchRecordingServiceChangedSince) {
	t.Helper()

	reader := &touchRecordingServiceChangedSince{lineage: twoTenantServiceLineage()}
	handler := &FreshnessHandler{
		ServiceChangedSince: reader,
		ServiceOwnership:    ownership,
		Profile:             ProfileLocalAuthoritative,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/freshness/services/changed-since?service_id="+url.QueryEscape(serviceID)+
			"&since_generation_id="+serviceChangedSinceGrantPriorGen,
		nil,
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	req = req.WithContext(ContextWithAuthContext(req.Context(), auth))

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec, reader
}

// serveServiceChangedSinceGrant is the ordinary case: a wired ownership store
// over the two-tenant correlation fixture.
func serveServiceChangedSinceGrant(
	t *testing.T, serviceID string, auth AuthContext,
) (*httptest.ResponseRecorder, *touchRecordingServiceChangedSince, *grantMirroringServiceOwnership) {
	t.Helper()

	ownership := &grantMirroringServiceOwnership{rows: twoTenantServiceCorrelations()}
	rec, reader := serveServiceChangedSinceOwnership(t, serviceID, auth, ownership)
	return rec, reader, ownership
}

func decodeServiceChangedSinceEnvelope(
	t *testing.T, rec *httptest.ResponseRecorder,
) (map[string]any, map[string]any) {
	t.Helper()

	var envelope struct {
		Data  map[string]any `json:"data"`
		Error map[string]any `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response envelope: %v; body = %s", err, rec.Body.String())
	}
	return envelope.Data, envelope.Error
}

// TestServiceChangedSinceTwoTenantGrantBoundary is the proof #5167 requires
// before GET /api/v0/freshness/services/changed-since may leave the pending
// row-filtering ledger. Unlike its two siblings this route's tables carry no
// repository or scope column, so the grant cannot live in the lineage SQL; it
// is bound by resolving the catalog service_id through the service-catalog
// correlation facts under the caller's grant and refusing BEFORE the lineage
// read. Each subtest therefore pins which stores were consulted, not only the
// status code.
func TestServiceChangedSinceTwoTenantGrantBoundary(t *testing.T) {
	t.Parallel()

	t.Run("in grant returns the delta", func(t *testing.T) {
		t.Parallel()

		rec, reader, ownership := serveServiceChangedSinceGrant(
			t, serviceChangedSinceGrantServiceA, scopedServiceChangedSinceTenantA(),
		)

		// Mutation-sensitive: refuse whenever access.Scoped(), or hand the
		// store the wrong grant arrays, and the caller's OWN service 404s
		// here. This is the assertion that separates "the grant is bound" from
		// "the grant is bound too tightly", which a deny-only test cannot see.
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; a granted service must still resolve; body = %s",
				rec.Code, http.StatusOK, rec.Body.String())
		}
		// Mutation-sensitive: the ownership store is the only thing binding
		// this route to a grant. If a future edit resolves ownership from the
		// lineage summary instead, this flag goes false and the refusal proved
		// by the next subtest stops being reachable at all.
		if !ownership.touched {
			t.Fatal("ownership store was never consulted for a scoped caller; the grant is not bound")
		}
		// Mutation-sensitive: proves the ownership check is a gate in front of
		// the read, not a filter applied after it.
		if !reader.touched {
			t.Fatal("lineage reader was never called for a granted service")
		}
		data, _ := decodeServiceChangedSinceEnvelope(t, rec)
		if got, want := data["service_id"], serviceChangedSinceGrantServiceA; got != want {
			t.Fatalf("data[service_id] = %v, want %q", got, want)
		}
	})

	t.Run("out of grant is not found before the lineage read", func(t *testing.T) {
		t.Parallel()

		rec, reader, _ := serveServiceChangedSinceGrant(
			t, serviceChangedSinceGrantServiceB, scopedServiceChangedSinceTenantA(),
		)

		// Mutation-sensitive: this is the cross-tenant read itself. Delete the
		// access.Scoped() block in listServiceChangedSince and tenant B's
		// delta comes back 200.
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d; an ungranted service must not resolve; body = %s",
				rec.Code, http.StatusNotFound, rec.Body.String())
		}
		// Mutation-sensitive: the refusal must happen BEFORE the lineage read.
		// Filtering after the read would still return 404, but would already
		// have read another tenant's service_evidence_snapshots rows.
		if reader.touched {
			t.Fatal("lineage reader was called for an ungranted service; the refusal must precede the read")
		}
		_, errEnvelope := decodeServiceChangedSinceEnvelope(t, rec)
		// Mutation-sensitive: a distinct code (403, or "not authorized") would
		// turn the route into an existence oracle -- a caller could enumerate
		// other tenants' services by the shape of the refusal. It must be the
		// route's ordinary service-not-found.
		if got, want := errEnvelope["code"], string(ErrorCodeServiceNotFound); got != want {
			t.Fatalf("error.code = %v, want %q; the refusal must be the ordinary service-not-found", got, want)
		}
		// Mutation-sensitive: the message echoes the selector the caller
		// typed, which the caller already knows. It must not carry the
		// internal identity of the lineage it declined to return.
		for _, leak := range []string{"repo-b", "scope-b", "gen-current-repo-b"} {
			if strings.Contains(rec.Body.String(), leak) {
				t.Fatalf("not-found body leaks the other tenant's identity %q: %s", leak, rec.Body.String())
			}
		}

		// Mutation-sensitive: the strongest form of the oracle assertion. The
		// refusal for a service that EXISTS but is ungranted must be
		// byte-identical, once the caller's own echoed selector is normalized,
		// to the refusal for a service that exists nowhere. Any future
		// divergence -- an added detail field, a different message branch --
		// reintroduces the oracle and fails here.
		absent, _, _ := serveServiceChangedSinceGrant(
			t, serviceChangedSinceGrantAbsent, scopedServiceChangedSinceTenantA(),
		)
		ungrantedShape := strings.ReplaceAll(rec.Body.String(), serviceChangedSinceGrantServiceB, "SELECTOR")
		absentShape := strings.ReplaceAll(absent.Body.String(), serviceChangedSinceGrantAbsent, "SELECTOR")
		if absent.Code != rec.Code || absentShape != ungrantedShape {
			t.Fatalf("an ungranted service is distinguishable from an absent one:\n ungranted: %d %s\n absent:    %d %s",
				rec.Code, ungrantedShape, absent.Code, absentShape)
		}
	})

	t.Run("empty grant touches neither store", func(t *testing.T) {
		t.Parallel()

		rec, reader, ownership := serveServiceChangedSinceGrant(
			t, serviceChangedSinceGrantServiceA,
			AuthContext{Mode: AuthModeScoped, TenantID: "tenant-a", WorkspaceID: "workspace-a"},
		)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d; a scoped caller with no grant must resolve nothing; body = %s",
				rec.Code, http.StatusNotFound, rec.Body.String())
		}
		// Mutation-sensitive: this is the load-bearing access.Empty() short
		// circuit. ServiceCatalogCorrelationFilter's grant clause collapses to
		// TRUE when both arrays are empty, so dropping the short circuit sends
		// an ungranted caller into the store and a row comes back -- flipping
		// this case to 200 with tenant A's delta.
		if ownership.touched {
			t.Fatal("ownership store was consulted on an empty grant; the permissive filter would admit every tenant")
		}
		if reader.touched {
			t.Fatal("lineage reader was called on an empty grant")
		}
	})

	t.Run("shared key never consults the ownership store", func(t *testing.T) {
		t.Parallel()

		for _, serviceID := range []string{serviceChangedSinceGrantServiceA, serviceChangedSinceGrantServiceB} {
			serviceID := serviceID
			t.Run(serviceID, func(t *testing.T) {
				t.Parallel()

				rec, reader, ownership := serveServiceChangedSinceGrant(
					t, serviceID, AuthContext{Mode: AuthModeShared},
				)

				// Mutation-sensitive: resolve ownership for every caller
				// rather than only when access.Scoped(), and the shared-key
				// operator silently loses every service whose catalog entity
				// has since been removed, because an unscoped caller carries
				// no allowed ids for the correlation row to match. That
				// failure is invisible to the scoped cases above.
				if rec.Code != http.StatusOK {
					t.Fatalf("status = %d, want %d; the shared key must stay unbounded across tenants; body = %s",
						rec.Code, http.StatusOK, rec.Body.String())
				}
				// Mutation-sensitive: pins that the unscoped path adds no
				// query at all, so promoting this route costs an unscoped
				// operator nothing.
				if ownership.touched {
					t.Fatal("ownership store was consulted for an unscoped caller; the unscoped path must add no query")
				}
				if !reader.touched {
					t.Fatal("lineage reader was never called for an unscoped caller")
				}
				data, _ := decodeServiceChangedSinceEnvelope(t, rec)
				if got := data["service_id"]; got != serviceID {
					t.Fatalf("data[service_id] = %v, want %q", got, serviceID)
				}
			})
		}
	})

	t.Run("scoped caller fails closed when ownership is unwired", func(t *testing.T) {
		t.Parallel()

		rec, reader := serveServiceChangedSinceOwnership(
			t, serviceChangedSinceGrantServiceA, scopedServiceChangedSinceTenantA(), nil,
		)

		// Mutation-sensitive: a nil ownership store is the deployment in which
		// the route cannot resolve ownership at all. Skipping the check when
		// the store is nil -- the natural "don't break the nil case" edit --
		// would open the route to every scoped caller, which is exactly the
		// cross-tenant read this change exists to close. It must fail closed.
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d; an unwired ownership store must fail closed for a scoped caller; body = %s",
				rec.Code, http.StatusNotFound, rec.Body.String())
		}
		if reader.touched {
			t.Fatal("lineage reader was called with no ownership store wired; the scoped path must fail closed")
		}
	})
}
