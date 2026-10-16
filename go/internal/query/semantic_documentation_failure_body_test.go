// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/failure"
)

// TestSemanticAndDocumentationFailureBodiesKeepTheirShape pins that #7674
// slice 14 changed only the status and span of a failed read, not the body.
// A server fault (500) and a client cancel (499) answer the same body each
// route answered before: the documentation routes keep error_code, message,
// and correlation_id with no capability field, the semantic evidence routes
// keep error_code, message, and capability, and the envelope form carries the
// same fields under error.
func TestSemanticAndDocumentationFailureBodiesKeepTheirShape(t *testing.T) {
	t.Parallel()

	const correlationID = "corr-7674-s14"
	cases := []struct {
		name   string
		cancel bool
		err    error
		status int
	}{
		{name: "fault", err: errors.New("backend: " + failure.Canary), status: http.StatusInternalServerError},
		{
			name: "client cancel", cancel: true,
			err:    fmt.Errorf("backend: %s: %w", failure.Canary, context.Canceled),
			status: querycontract.StatusClientClosedRequest,
		},
	}
	for _, route := range semanticDocumentationFailureRoutes() {
		semantic := route.route.Message == semanticEvidenceReadFailedMessage
		for _, tc := range cases {
			for _, envelope := range []bool{false, true} {
				label := fmt.Sprintf("%s / %s / envelope=%v", route.route.Name, tc.name, envelope)
				rec := serveFailureBody(route.route, tc.err, tc.cancel, envelope, correlationID)
				if rec.Code != tc.status {
					t.Errorf("%s: status = %d, want %d", label, rec.Code, tc.status)
					continue
				}
				got := decodeFailureBody(t, label, rec.Body.Bytes(), envelope)
				want := map[string]any{
					"message": route.route.Message,
				}
				codeKey := "error_code"
				if envelope {
					codeKey = "code"
				}
				want[codeKey] = string(ErrorCodeInternalError)
				if semantic {
					want["capability"] = route.capability
				} else {
					want["correlation_id"] = correlationID
				}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("%s: body fields = %v, want %v", label, failureBodyKeys(got), failureBodyKeys(want))
					t.Errorf("%s: body = %v, want %v", label, got, want)
				}
			}
		}
	}
}

// serveFailureBody serves one request for route with its read failing with
// err, through the real mux, with or without the envelope media type.
func serveFailureBody(
	route failure.Route, err error, cancel, envelope bool, correlationID string,
) *httptest.ResponseRecorder {
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	if cancel {
		stop()
	}
	mux := http.NewServeMux()
	route.Handler(err).Mount(mux)
	req := httptest.NewRequest(http.MethodGet, route.Path, nil).WithContext(ctx)
	req.Header.Set("X-Correlation-ID", correlationID)
	if envelope {
		req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// decodeFailureBody returns the error fields of body: the plain object, or the
// envelope's error object.
func decodeFailureBody(t *testing.T, label string, body []byte, envelope bool) map[string]any {
	t.Helper()
	if !envelope {
		var plain map[string]any
		if err := json.Unmarshal(body, &plain); err != nil {
			t.Fatalf("%s: decode body: %v; body = %s", label, err, body)
		}
		return plain
	}
	var wrapped struct {
		Data  any            `json:"data"`
		Truth any            `json:"truth"`
		Error map[string]any `json:"error"`
	}
	if err := json.Unmarshal(body, &wrapped); err != nil {
		t.Fatalf("%s: decode envelope: %v; body = %s", label, err, body)
	}
	if wrapped.Data != nil || wrapped.Truth != nil {
		t.Errorf("%s: envelope data/truth = %v/%v, want null", label, wrapped.Data, wrapped.Truth)
	}
	return wrapped.Error
}

// failureBodyKeys lists m's keys in order for a failure message.
func failureBodyKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// TestDocumentationAggregatesUnconfiguredAnswer503WithoutRetryAfter pins the
// configuration 503 the two aggregate routes keep: with no aggregate store
// wired, they answer 503 backend_unavailable with no Retry-After, so a client
// can tell it from the retryable reader-fence 503.
func TestDocumentationAggregatesUnconfiguredAnswer503WithoutRetryAfter(t *testing.T) {
	t.Parallel()

	for _, path := range []string{"/api/v0/documentation/findings/count", "/api/v0/documentation/findings/inventory"} {
		mux := http.NewServeMux()
		(&DocumentationHandler{}).Mount(mux)
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s: status = %d, want 503", path, rec.Code)
		}
		if got := rec.Header().Get("Retry-After"); got != "" {
			t.Errorf("%s: Retry-After = %q, want none on the configuration 503", path, got)
		}
		got := decodeFailureBody(t, path, rec.Body.Bytes(), true)
		if got["code"] != string(ErrorCodeBackendUnavailable) {
			t.Errorf("%s: code = %v, want %s", path, got["code"], ErrorCodeBackendUnavailable)
		}
	}
}

// TestSemanticAndDocumentationNotImplementedBodies pins the 501 bodies the
// OpenAPI spec now declares for the two semantic evidence routes and the two
// aggregate routes. A semantic evidence route with no fact read model answers
// read_model_unavailable with error_code, message, and capability. An
// aggregate route on a profile that does not support
// documentation_findings.aggregate answers unsupported_capability with
// error_code, message, capability, and correlation_id; its envelope also
// carries the current and required profiles.
func TestSemanticAndDocumentationNotImplementedBodies(t *testing.T) {
	t.Parallel()

	const correlationID = "corr-7674-s14-501"
	type notImplementedCase struct {
		path         string
		handler      failure.Mounter
		plain        map[string]any
		envelope     map[string]any
		wantProfiles bool
	}
	var cases []notImplementedCase
	for path, capability := range map[string]string{
		"/api/v0/semantic/documentation-observations?scope_id=scope-1": semanticDocumentationObservationsCapability,
		"/api/v0/semantic/code-hints?scope_id=scope-1":                 semanticCodeHintsCapability,
	} {
		const message = "semantic evidence routes require the Postgres fact read model"
		cases = append(cases, notImplementedCase{
			path: path, handler: &SemanticEvidenceHandler{},
			plain:    map[string]any{"error_code": "documentation_read_model_unavailable", "message": message, "capability": capability},
			envelope: map[string]any{"code": "documentation_read_model_unavailable", "message": message, "capability": capability},
		})
	}
	for _, path := range []string{"/api/v0/documentation/findings/count", "/api/v0/documentation/findings/inventory"} {
		const message = "documentation routes require durable documentation facts"
		capability := documentationFindingAggregateCapability
		cases = append(cases, notImplementedCase{
			path: path, handler: &DocumentationHandler{Profile: ProfileLocalLightweight},
			plain: map[string]any{
				"error_code": "unsupported_capability", "message": message,
				"capability": capability, "correlation_id": correlationID,
			},
			envelope: map[string]any{
				"code": "unsupported_capability", "message": message,
				"capability": capability, "correlation_id": correlationID,
				"profiles": map[string]any{
					"current":  string(ProfileLocalLightweight),
					"required": string(querycontract.RequiredProfile(capability)),
				},
			},
			wantProfiles: true,
		})
	}
	for _, tc := range cases {
		for _, envelope := range []bool{false, true} {
			label := fmt.Sprintf("%s / envelope=%v", tc.path, envelope)
			mux := http.NewServeMux()
			tc.handler.Mount(mux)
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			req.Header.Set("X-Correlation-ID", correlationID)
			want := tc.plain
			if envelope {
				req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
				want = tc.envelope
			}
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != http.StatusNotImplemented {
				t.Errorf("%s: status = %d, want 501; body = %s", label, rec.Code, rec.Body.String())
				continue
			}
			if got := decodeFailureBody(t, label, rec.Body.Bytes(), envelope); !reflect.DeepEqual(got, want) {
				t.Errorf("%s: body = %v, want %v", label, got, want)
			}
		}
	}
}

// TestSemanticEvidenceCapabilitiesSupportedOnEveryProfile backs the OpenAPI
// 501 description's last sentence: every runtime profile supports both
// semantic evidence capabilities today, so their unsupported_capability 501
// is not reachable through a profile. If a profile drops one, this fails and
// the description must change with it.
func TestSemanticEvidenceCapabilitiesSupportedOnEveryProfile(t *testing.T) {
	t.Parallel()

	for _, capability := range []string{semanticDocumentationObservationsCapability, semanticCodeHintsCapability} {
		for _, profile := range []QueryProfile{
			ProfileLocalLightweight, ProfileLocalAuthoritative, ProfileLocalFullStack, ProfileProduction,
		} {
			if querycontract.CapabilityUnsupported(profile, capability) {
				t.Errorf("%s is unsupported on %s; update the 501 description in openapi/paths/search/semantic_evidence.go", capability, profile)
			}
		}
	}
}
