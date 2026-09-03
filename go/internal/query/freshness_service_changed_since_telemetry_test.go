// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// recordServiceChangedSinceSpan drives the production handler with a recording
// tracer swapped in for queryHandlerTracer and returns the single span the
// route emitted, flattened to a name -> value map. It mirrors
// TestHandleLanguageQueryEmitsLanguageQuerySpan's setup; the tracer swap is
// process-global, so these subtests deliberately do not run in parallel.
func recordServiceChangedSinceSpan(
	t *testing.T, serviceID string, auth AuthContext, ownership ServiceCatalogCorrelationStore,
) map[string]any {
	t.Helper()

	recorder := tracetest.NewSpanRecorder()
	provider := tracesdk.NewTracerProvider(tracesdk.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	previousTracer := queryHandlerTracer
	queryHandlerTracer = provider.Tracer("service-changed-since-grant-telemetry-test")
	t.Cleanup(func() { queryHandlerTracer = previousTracer })

	serveServiceChangedSinceOwnership(t, serviceID, auth, ownership)

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
func TestServiceChangedSinceGrantRefusalIsRecordedOnTheSpan(t *testing.T) {
	mirroringOwnership := func() ServiceCatalogCorrelationStore {
		return &grantMirroringServiceOwnership{rows: twoTenantServiceCorrelations()}
	}

	for _, tc := range []struct {
		name      string
		serviceID string
		auth      AuthContext
		ownership func() ServiceCatalogCorrelationStore
		// wantReason is empty for the cases that must carry no refusal
		// attribute at all.
		wantReason string
	}{
		{
			name:       "ungranted service records not_granted",
			serviceID:  serviceChangedSinceGrantServiceB,
			auth:       scopedServiceChangedSinceTenantA(),
			ownership:  mirroringOwnership,
			wantReason: telemetry.ServiceChangedSinceGrantRefusalNotGranted,
		},
		{
			// The #6472 review case: tenant A owns one of the two
			// correlations for this id, so the existential check admitted it
			// and served tenant B's lineage.
			name:       "shared service id records shared_ownership",
			serviceID:  serviceChangedSinceGrantShared,
			auth:       scopedServiceChangedSinceTenantA(),
			ownership:  mirroringOwnership,
			wantReason: telemetry.ServiceChangedSinceGrantRefusalSharedOwnership,
		},
		{
			name:       "empty grant records empty_grant",
			serviceID:  serviceChangedSinceGrantServiceA,
			auth:       AuthContext{Mode: AuthModeScoped, TenantID: "tenant-a", WorkspaceID: "workspace-a"},
			ownership:  mirroringOwnership,
			wantReason: telemetry.ServiceChangedSinceGrantRefusalEmptyGrant,
		},
		{
			name:       "unwired ownership records ownership_unwired",
			serviceID:  serviceChangedSinceGrantServiceA,
			auth:       scopedServiceChangedSinceTenantA(),
			ownership:  func() ServiceCatalogCorrelationStore { return nil },
			wantReason: telemetry.ServiceChangedSinceGrantRefusalOwnershipUnwired,
		},
		{
			name:      "granted service carries no refusal attribute",
			serviceID: serviceChangedSinceGrantServiceA,
			auth:      scopedServiceChangedSinceTenantA(),
			ownership: mirroringOwnership,
		},
		{
			// The absence assertion that matters most for an alert: an
			// unscoped caller is never grant-refused, so a dashboard counting
			// this attribute must not pick up shared-key traffic.
			name:      "shared key carries no refusal attribute",
			serviceID: serviceChangedSinceGrantServiceB,
			auth:      AuthContext{Mode: AuthModeShared},
			ownership: mirroringOwnership,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			attributes := recordServiceChangedSinceSpan(t, tc.serviceID, tc.auth, tc.ownership())

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
				serviceChangedSinceGrantServiceA,
				serviceChangedSinceGrantServiceB,
				serviceChangedSinceGrantShared,
				"tenant-a",
				"workspace-a",
				"repo-a",
				"scope-a",
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
