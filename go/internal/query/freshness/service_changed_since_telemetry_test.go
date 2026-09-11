// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package freshness

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/queryauth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
	"github.com/eshu-hq/eshu/go/internal/query/service"
	"github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// fakeServiceChangedSinceLineageReader always resolves the requested service
// to a lineage row. The span-attribute proof below only needs a reader that
// lets the two non-refused cases (granted, shared-key) reach
// span.SetAttributes; the grant-boundary correctness of the lineage read
// itself is proven separately by package query's
// service_changed_since_grant_test.go (TestServiceChangedSinceTwoTenantGrantBoundary).
type fakeServiceChangedSinceLineageReader struct{}

func (fakeServiceChangedSinceLineageReader) ComputeServiceChangedSinceDelta(
	_ context.Context, filter status.ServiceChangedSinceFilter,
) (status.ServiceChangedSinceSummary, error) {
	return status.ServiceChangedSinceSummary{
		ServiceID:                 filter.ServiceID,
		SinceGenerationID:         filter.SinceGenerationID,
		CurrentActiveGenerationID: "gen-current",
		SampleLimit:               filter.SampleLimit,
	}, nil
}

// fakeServiceOwnershipProbeResult answers serviceChangedSinceGrantAdmits'
// two-probe ownership check with a fixed pair of row counts, one for the
// admission probe and one for the exclusivity (OutsideGrant) probe. It is
// deliberately simpler than querytestutil's two-tenant correlation mirror:
// this proof only needs to land on each of the four closed refusal reasons
// plus the two non-refused paths, not re-prove the SQL-mirroring grant
// intersection the service_changed_since_grant_test.go sibling already
// proves in package query.
type fakeServiceOwnershipProbeResult struct {
	granted   []service.CatalogCorrelationRow
	contested []service.CatalogCorrelationRow
}

func (f fakeServiceOwnershipProbeResult) ListServiceCatalogCorrelations(
	_ context.Context, filter service.CatalogCorrelationFilter,
) ([]service.CatalogCorrelationRow, error) {
	if filter.OutsideGrant {
		return f.contested, nil
	}
	return f.granted, nil
}

var oneGrantedCorrelationRow = []service.CatalogCorrelationRow{{CorrelationID: "fact-a", ServiceID: "svc", RepositoryID: "repo-a"}}

// oneContestedCorrelationRow is a row owned by a repository outside tenant
// A's grant, so a shared service id resolves to contested ownership.
var oneContestedCorrelationRow = []service.CatalogCorrelationRow{{CorrelationID: "fact-b", ServiceID: "svc", RepositoryID: "repo-b"}}

// recordServiceChangedSinceSpan drives the production handler with a
// recording tracer swapped in for this package's own freshnessHandlerTracer
// and returns the single span the route emitted, flattened to a name ->
// value map. The swap is package-local (handler_tracing.go), so this test
// does not run in parallel with itself or with any other test in this
// package that also swaps freshnessHandlerTracer.
func recordServiceChangedSinceSpan(
	t *testing.T, serviceID string, auth queryauth.AuthContext, ownership service.CatalogCorrelationStore,
) map[string]any {
	t.Helper()

	recorder := tracetest.NewSpanRecorder()
	provider := tracesdk.NewTracerProvider(tracesdk.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	previousTracer := freshnessHandlerTracer
	freshnessHandlerTracer = provider.Tracer("service-changed-since-grant-telemetry-test")
	t.Cleanup(func() { freshnessHandlerTracer = previousTracer })

	handler := &Handler{
		ServiceChangedSince: fakeServiceChangedSinceLineageReader{},
		ServiceOwnership:    ownership,
		Profile:             querycontract.ProfileLocalAuthoritative,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/freshness/services/changed-since?service_id="+serviceID+"&since_generation_id=gen-prior",
		nil,
	)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	req = req.WithContext(queryauth.ContextWithAuthContext(req.Context(), auth))

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	spans := recorder.Ended()
	if got, want := len(spans), 1; got != want {
		t.Fatalf("ended spans = %d, want %d; the route must emit exactly one handler span", got, want)
	}
	if got, want := spans[0].Name(), telemetry.SpanQueryFreshnessServiceChangedSince; got != want {
		t.Fatalf("span name = %q, want %q", got, want)
	}
	attributes := map[string]any{}
	for _, item := range spans[0].Attributes() {
		attributes[string(item.Key)] = item.Value.AsInterface()
	}
	return attributes
}

// TestServiceChangedSinceGrantRefusalIsRecordedOnTheSpan is the #5167 round-1
// review fix (P1-2). Every grant refusal on this route returns the route's
// ordinary service-not-found, byte-identical to the answer an unknown service
// gets. That indistinguishability is deliberate -- it stops the route being an
// existence oracle for another tenant's services -- but it also left the
// operator with nothing. The middleware already ADMITTED the request, so no
// governance-audit deny event fires for a handler-level refusal, and before
// this change every refusal branch returned ahead of every
// span.SetAttributes call. An operator paged with "tenant A's token gets
// not-found for a service it owns" could not tell a grant refusal from missing
// lineage from an unwired ownership store.
//
// The server-side span attributes are the fix. They never reach the caller, so
// they add no oracle, and the reason vocabulary is closed and carries no
// service, tenant, workspace, repository, or scope identifier.
//
// This test moved here from package query's
// freshness_service_changed_since_telemetry_test.go (#6642): the handler it drives
// moved to this package, and the tracer seam handler_tracing.go documents is
// deliberately package-local, so a test swapping package query's
// queryHandlerTracer no longer observes any span this route emits.
func TestServiceChangedSinceGrantRefusalIsRecordedOnTheSpan(t *testing.T) {
	for _, tc := range []struct {
		name      string
		serviceID string
		auth      queryauth.AuthContext
		ownership service.CatalogCorrelationStore
		// wantReason is empty for the cases that must carry no refusal
		// attribute at all.
		wantReason string
	}{
		{
			name:       "ungranted service records not_granted",
			serviceID:  "svc-b",
			auth:       querytestutil.ScopedChangedSinceTenantA(),
			ownership:  fakeServiceOwnershipProbeResult{},
			wantReason: telemetry.ServiceChangedSinceGrantRefusalNotGranted,
		},
		{
			// The #6472 review case: tenant A owns one of the two
			// correlations for this id, so the existential check admitted it
			// and served tenant B's lineage.
			name:       "shared service id records shared_ownership",
			serviceID:  "svc-shared",
			auth:       querytestutil.ScopedChangedSinceTenantA(),
			ownership:  fakeServiceOwnershipProbeResult{granted: oneGrantedCorrelationRow, contested: oneContestedCorrelationRow},
			wantReason: telemetry.ServiceChangedSinceGrantRefusalSharedOwnership,
		},
		{
			name:       "empty grant records empty_grant",
			serviceID:  "svc-a",
			auth:       queryauth.AuthContext{Mode: queryauth.AuthModeScoped, TenantID: "tenant-a", WorkspaceID: "workspace-a"},
			ownership:  fakeServiceOwnershipProbeResult{},
			wantReason: telemetry.ServiceChangedSinceGrantRefusalEmptyGrant,
		},
		{
			name:       "unwired ownership records ownership_unwired",
			serviceID:  "svc-a",
			auth:       querytestutil.ScopedChangedSinceTenantA(),
			ownership:  nil,
			wantReason: telemetry.ServiceChangedSinceGrantRefusalOwnershipUnwired,
		},
		{
			name:      "granted service carries no refusal attribute",
			serviceID: "svc-a",
			auth:      querytestutil.ScopedChangedSinceTenantA(),
			ownership: fakeServiceOwnershipProbeResult{granted: oneGrantedCorrelationRow},
		},
		{
			// The absence assertion that matters most for an alert: an
			// unscoped caller is never grant-refused, so a dashboard counting
			// this attribute must not pick up shared-key traffic.
			name:      "shared key carries no refusal attribute",
			serviceID: "svc-b",
			auth:      queryauth.AuthContext{Mode: queryauth.AuthModeShared},
			ownership: fakeServiceOwnershipProbeResult{},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			attributes := recordServiceChangedSinceSpan(t, tc.serviceID, tc.auth, tc.ownership)

			refused, refusedSet := attributes[telemetry.SpanAttrServiceChangedSinceGrantRefused]
			reason, reasonSet := attributes[telemetry.SpanAttrServiceChangedSinceGrantRefusedReason]

			if tc.wantReason == "" {
				if refusedSet || reasonSet {
					t.Fatalf("span carries a grant-refusal attribute on a request that was not refused: %s = %#v (set = %t), %s = %#v (set = %t)",
						telemetry.SpanAttrServiceChangedSinceGrantRefused, refused, refusedSet,
						telemetry.SpanAttrServiceChangedSinceGrantRefusedReason, reason, reasonSet)
				}
				return
			}

			if !refusedSet {
				t.Fatalf("span is missing %s; a grant refusal stays indistinguishable from an unknown service without it",
					telemetry.SpanAttrServiceChangedSinceGrantRefused)
			}
			if refused != true {
				t.Fatalf("%s = %#v, want true", telemetry.SpanAttrServiceChangedSinceGrantRefused, refused)
			}
			if !reasonSet {
				t.Fatalf("span is missing %s; the operator cannot tell which refusal fired",
					telemetry.SpanAttrServiceChangedSinceGrantRefusedReason)
			}
			if reason != tc.wantReason {
				t.Fatalf("%s = %#v, want %q",
					telemetry.SpanAttrServiceChangedSinceGrantRefusedReason, reason, tc.wantReason)
			}

			// The reason vocabulary is closed and low-cardinality on purpose.
			// A service id or a tenant id here would put per-tenant identity
			// into every trace backend that samples this route, which is the
			// leak the not-found body already refuses to make.
			for _, identifier := range []string{
				"svc-a", "svc-b", "svc-shared", "tenant-a", "workspace-a", "repo-a", "scope-a",
			} {
				if reason == identifier {
					t.Fatalf("%s carries the identifying value %q",
						telemetry.SpanAttrServiceChangedSinceGrantRefusedReason, identifier)
				}
			}
		})
	}
}

// TestServiceChangedSinceGrantRefusalReasonsAreAClosedVocabulary pins the two
// attribute names and the four reason strings the handler may emit. An
// operator alert keys off these literals, so a rename is a contract change and
// must fail here first rather than silently in a dashboard.
func TestServiceChangedSinceGrantRefusalReasonsAreAClosedVocabulary(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		got  string
		want string
	}{
		{got: telemetry.SpanAttrServiceChangedSinceGrantRefused, want: "eshu.service_changed_since.grant_refused"},
		{got: telemetry.SpanAttrServiceChangedSinceGrantRefusedReason, want: "eshu.service_changed_since.grant_refused_reason"},
		{got: telemetry.ServiceChangedSinceGrantRefusalEmptyGrant, want: "empty_grant"},
		{got: telemetry.ServiceChangedSinceGrantRefusalNotGranted, want: "not_granted"},
		{got: telemetry.ServiceChangedSinceGrantRefusalSharedOwnership, want: "shared_ownership"},
		{got: telemetry.ServiceChangedSinceGrantRefusalOwnershipUnwired, want: "ownership_unwired"},
	} {
		if tc.got != tc.want {
			t.Fatalf("telemetry constant = %q, want %q", tc.got, tc.want)
		}
	}
}
