// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func postOwnershipPackets(t *testing.T, handler *IaCHandler, body string) (*httptest.ResponseRecorder, ResponseEnvelope) {
	t.Helper()
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(http.MethodPost, "/api/v0/replatforming/ownership-packets", bytes.NewBufferString(body))
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	var resp ResponseEnvelope
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("json.Unmarshal() error = %v body=%s", err, w.Body.String())
		}
	}
	return w, resp
}

func TestOwnershipPacketsUnsupportedOnLightweightProfile(t *testing.T) {
	t.Parallel()

	handler := &IaCHandler{
		Profile:    ProfileLocalLightweight,
		Management: fakeIaCManagementStore{},
	}
	w, _ := postOwnershipPackets(t, handler, `{"account_id":"123456789012"}`)
	if w.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNotImplemented)
	}
	var resp ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if resp.Error == nil || resp.Error.Code != ErrorCodeUnsupportedCapability {
		t.Fatalf("error = %#v, want unsupported_capability", resp.Error)
	}
}

func TestOwnershipPacketsRequiresScopeOrAccount(t *testing.T) {
	t.Parallel()

	handler := &IaCHandler{
		Profile:    ProfileLocalAuthoritative,
		Management: fakeIaCManagementStore{},
	}
	w, _ := postOwnershipPackets(t, handler, `{}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestOwnershipPacketsComposesCandidatesAndPreservesTruth(t *testing.T) {
	t.Parallel()

	stale := ownershipFinding(
		"arn:aws:lambda:us-east-1:123456789012:function/ambiguous",
		managementStatusCloudOnly,
		findingKindUnmanagedCloudResource,
		[]string{"billing", "checkout"},
		nil,
	)
	handler := &IaCHandler{
		Profile:    ProfileLocalAuthoritative,
		Management: fakeIaCManagementStore{rows: []IaCManagementFindingRow{stale}},
	}
	w, resp := postOwnershipPackets(t, handler, `{"account_id":"123456789012"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if resp.Truth == nil || resp.Truth.Capability != replatformingOwnershipCapability {
		t.Fatalf("truth = %#v, want capability %q", resp.Truth, replatformingOwnershipCapability)
	}
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("data type = %T, want map", resp.Data)
	}
	if got := data["ambiguous_count"]; got != float64(1) {
		t.Fatalf("ambiguous_count = %v, want 1", got)
	}
	packets, ok := data["ownership_packets"].([]any)
	if !ok || len(packets) != 1 {
		t.Fatalf("ownership_packets = %#v, want one packet", data["ownership_packets"])
	}
}

func TestOwnershipPacketsDoNotLeakRawTagValues(t *testing.T) {
	t.Parallel()

	finding := ownershipFinding(
		"arn:aws:lambda:us-east-1:123456789012:function/tagged",
		managementStatusCloudOnly,
		findingKindOrphanedCloudResource,
		nil,
		nil,
	)
	finding.Tags = map[string]string{"owner": "secret-team-name"}
	handler := &IaCHandler{
		Profile:    ProfileLocalAuthoritative,
		Management: fakeIaCManagementStore{rows: []IaCManagementFindingRow{finding}},
	}
	w, _ := postOwnershipPackets(t, handler, `{"account_id":"123456789012"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	// The raw tag value must not appear as an owner candidate anywhere in the
	// response body.
	if bytes.Contains(w.Body.Bytes(), []byte("secret-team-name")) {
		t.Fatalf("raw tag value leaked into ownership packet response: %s", w.Body.String())
	}
}
