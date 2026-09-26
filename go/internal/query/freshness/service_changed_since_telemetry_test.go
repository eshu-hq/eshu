// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package freshness

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/service"
	"github.com/eshu-hq/eshu/go/internal/query/testutil"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// wiredServiceOwnership satisfies the ownership_unwired check. The route no
// longer reads it (#6475), so it answers nothing.
type wiredServiceOwnership struct{}

func (wiredServiceOwnership) ListServiceCatalogCorrelations(
	context.Context, service.CatalogCorrelationFilter,
) ([]service.CatalogCorrelationRow, error) {
	return nil, nil
}

// serviceLineageSince names a prior generation that exists in the lineage the
// fixture serves each service id from, so a served request reaches the span
// attributes instead of stopping at the prior-generation not-found.
func serviceLineageSince(serviceID string) string {
	if serviceID == testutil.ServiceLineageLegacyID {
		return "gen-legacy-prior"
	}
	return "gen-a-prior"
}

// recordServiceChangedSinceSpan drives the production handler with a
// recording tracer swapped in for this package's own freshnessHandlerTracer
// and returns the single span the route emitted, flattened to a name ->
// value map. The swap is package-local (handler_tracing.go), so this test
// does not run in parallel with itself or with any other test in this
// package that also swaps freshnessHandlerTracer.
func recordServiceChangedSinceSpan(
	t *testing.T, serviceID string, authCtx auth.AuthContext, ownership service.CatalogCorrelationStore,
) map[string]any {
	t.Helper()

	recorder := tracetest.NewSpanRecorder()
	provider := tracesdk.NewTracerProvider(tracesdk.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	previousTracer := freshnessHandlerTracer
	freshnessHandlerTracer = provider.Tracer("service-changed-since-grant-telemetry-test")
	t.Cleanup(func() { freshnessHandlerTracer = previousTracer })

	handler := &Handler{
		ServiceChangedSince: &testutil.GrantMirroringServiceChangedSince{Rows: testutil.TwoTenantServiceLineageRows()},
		ServiceOwnership:    ownership,
		Profile:             querycontract.ProfileLocalAuthoritative,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/freshness/services/changed-since?service_id="+serviceID+"&since_generation_id="+serviceLineageSince(serviceID),
		nil,
	)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	req = req.WithContext(auth.ContextWithAuthContext(req.Context(), authCtx))

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
// review fix (P1-2), carried onto the #6475 lineage binding. Every grant
// refusal on this route returns the route's ordinary service-not-found,
// byte-identical to the answer an unknown service gets. That
// indistinguishability is deliberate -- it stops the route being an existence
// oracle for another tenant's services -- but it leaves the operator with
// nothing unless the span says which refusal fired. The middleware already
// ADMITTED the request, so no governance-audit deny event fires for a
// handler-level refusal.
//
// The server-side span attributes are the fix. They never reach the caller, so
// they add no oracle, and the reason vocabulary is closed and carries no
// service, tenant, workspace, repository, or scope identifier. Since #6475
// not_granted comes from the lineage read itself (ServiceSummary.OutsideGrant):
// the id holds lineage rows, none in a scope the grant admits.
func TestServiceChangedSinceGrantRefusalIsRecordedOnTheSpan(t *testing.T) {
	for _, tc := range []struct {
		name      string
		serviceID string
		auth      auth.AuthContext
		ownership service.CatalogCorrelationStore
		// wantReason is empty for the cases that must carry no refusal
		// attribute at all.
		wantReason string
	}{
		{
			// The lineage exists but is unattributed, so no scoped grant
			// admits it.
			name:       "lineage outside the grant records not_granted",
			serviceID:  testutil.ServiceLineageLegacyID,
			auth:       testutil.ScopedChangedSinceTenantA(),
			ownership:  wiredServiceOwnership{},
			wantReason: telemetry.ServiceChangedSinceGrantRefusalNotGranted,
		},
		{
			name:       "empty grant records empty_grant",
			serviceID:  testutil.ServiceLineageSharedID,
			auth:       auth.AuthContext{Mode: auth.AuthModeScoped, TenantID: "tenant-a", WorkspaceID: "workspace-a"},
			ownership:  wiredServiceOwnership{},
			wantReason: telemetry.ServiceChangedSinceGrantRefusalEmptyGrant,
		},
		{
			name:       "unwired ownership records ownership_unwired",
			serviceID:  testutil.ServiceLineageSharedID,
			auth:       testutil.ScopedChangedSinceTenantA(),
			ownership:  nil,
			wantReason: telemetry.ServiceChangedSinceGrantRefusalOwnershipUnwired,
		},
		{
			// The case #6472 had to refuse as shared_ownership: tenant B also
			// declares the id. Its lineage now lives in scope-b, so tenant A
			// is served its own and nothing is refused.
			name:      "shared service id is served, not refused",
			serviceID: testutil.ServiceLineageSharedID,
			auth:      testutil.ScopedChangedSinceTenantA(),
			ownership: wiredServiceOwnership{},
		},
		{
			// A service with no lineage anywhere is a plain not-found, not a
			// grant refusal: an alert on refusals must not fire on typos.
			name:      "absent service carries no refusal attribute",
			serviceID: "component:default/nowhere",
			auth:      testutil.ScopedChangedSinceTenantA(),
			ownership: wiredServiceOwnership{},
		},
		{
			// The absence assertion that matters most for an alert: an
			// unscoped caller is never grant-refused, so a dashboard counting
			// this attribute must not pick up shared-key traffic.
			name:      "shared key carries no refusal attribute",
			serviceID: testutil.ServiceLineageLegacyID,
			auth:      auth.AuthContext{Mode: auth.AuthModeShared},
			ownership: nil,
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
			for _, identifier := range []string{
				testutil.ServiceLineageSharedID, testutil.ServiceLineageLegacyID,
				"tenant-a", "workspace-a", "repo-a", "scope-a",
			} {
				if reason == identifier {
					t.Fatalf("%s carries the identifying value %q",
						telemetry.SpanAttrServiceChangedSinceGrantRefusedReason, identifier)
				}
			}
		})
	}
}

// TestServiceChangedSinceLineageAttributesAreRecordedOnTheSpan pins the two
// #6475 attributes: a served legacy read marks unattributed, and a 409 records
// how many admitted scopes it listed -- a count, never the scope ids.
func TestServiceChangedSinceLineageAttributesAreRecordedOnTheSpan(t *testing.T) {
	legacy := recordServiceChangedSinceSpan(t, testutil.ServiceLineageLegacyID, auth.AuthContext{Mode: auth.AuthModeShared}, nil)
	if got := legacy[telemetry.SpanAttrServiceChangedSinceUnattributed]; got != true {
		t.Fatalf("%s = %#v on a legacy read, want true", telemetry.SpanAttrServiceChangedSinceUnattributed, got)
	}

	scoped := recordServiceChangedSinceSpan(t, testutil.ServiceLineageSharedID, testutil.ScopedChangedSinceTenantA(), wiredServiceOwnership{})
	if got := scoped[telemetry.SpanAttrServiceChangedSinceUnattributed]; got != false {
		t.Fatalf("%s = %#v on an attributed read, want false", telemetry.SpanAttrServiceChangedSinceUnattributed, got)
	}

	ambiguous := recordServiceChangedSinceSpan(t, testutil.ServiceLineageSharedID, auth.AuthContext{Mode: auth.AuthModeShared}, nil)
	if got := ambiguous[telemetry.SpanAttrServiceChangedSinceAmbiguousScopeCount]; got != int64(2) {
		t.Fatalf("%s = %#v on a two-scope conflict, want 2", telemetry.SpanAttrServiceChangedSinceAmbiguousScopeCount, got)
	}
	for key, value := range ambiguous {
		if value == "scope-a" || value == "scope-b" {
			t.Fatalf("span attribute %s carries the scope id %q", key, value)
		}
	}
}

// TestServiceChangedSinceGrantRefusalReasonsAreAClosedVocabulary pins the
// attribute names and the three reason strings the handler may emit. An
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
		{got: telemetry.SpanAttrServiceChangedSinceUnattributed, want: "eshu.service_changed_since.unattributed"},
		{got: telemetry.SpanAttrServiceChangedSinceAmbiguousScopeCount, want: "eshu.service_changed_since.ambiguous_scope_count"},
		{got: telemetry.ServiceChangedSinceGrantRefusalOwnershipUnwired, want: "ownership_unwired"},
	} {
		if tc.got != tc.want {
			t.Fatalf("telemetry constant = %q, want %q", tc.got, tc.want)
		}
	}
}
