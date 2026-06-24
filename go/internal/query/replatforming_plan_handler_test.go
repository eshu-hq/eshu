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

// readySafetyGate returns a read-only safety gate that permits import-plan
// generation, used to build importable test findings.
func readySafetyGate() IaCManagementSafetyGate {
	return IaCManagementSafetyGate{
		Outcome:        "read_only_allowed",
		ReadOnly:       true,
		ReviewRequired: false,
	}
}

func decodeReplatformingPlanResponse(t *testing.T, w *httptest.ResponseRecorder) (ResponseEnvelope, map[string]any) {
	t.Helper()
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	var resp ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("data type = %T, want map[string]any", resp.Data)
	}
	return resp, data
}

func TestComposeReplatformingPlanReturnsReadyImportItem(t *testing.T) {
	t.Parallel()

	handler := &IaCHandler{
		Profile: ProfileLocalAuthoritative,
		Management: fakeIaCManagementStore{rows: []IaCManagementFindingRow{{
			ID:                "fact:aws-s3",
			Provider:          "aws",
			ResourceType:      "s3",
			ResourceID:        "payments-prod-logs",
			ARN:               "arn:aws:s3:::payments-prod-logs",
			FindingKind:       findingKindOrphanedCloudResource,
			ManagementStatus:  managementStatusCloudOnly,
			Confidence:        0.96,
			ScopeID:           "aws:123456789012:us-east-1:s3",
			ServiceCandidates: []string{"payments"},
			SafetyGate:        readySafetyGate(),
		}}},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/replatforming/plans", bytes.NewBufferString(`{
		"scope_kind": "account",
		"account_id": "123456789012",
		"region": "us-east-1",
		"limit": 10
	}`))
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	resp, data := decodeReplatformingPlanResponse(t, w)
	if got, want := resp.Truth.Capability, replatformingPlanReadinessCapability; got != want {
		t.Fatalf("truth capability = %q, want %q", got, want)
	}
	if got, want := resp.Truth.Level, TruthLevelDerived; got != want {
		t.Fatalf("truth level = %q, want %q", got, want)
	}
	plan := data["plan"].(map[string]any)
	if got, want := plan["contract_version"], ReplatformingPlanContractVersion; got != want {
		t.Fatalf("contract_version = %q, want %q", got, want)
	}
	nonGoals := plan["non_goals"].([]any)
	if len(nonGoals) == 0 {
		t.Fatal("plan non_goals is empty")
	}
	items := plan["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("items length = %d, want 1", len(items))
	}
	item := items[0].(map[string]any)
	if got, want := item["source_state"], string(ReplatformingSourceStateDerived); got != want {
		t.Fatalf("source_state = %q, want %q", got, want)
	}
	if got, want := item["stable_id"], "arn:aws:s3:::payments-prod-logs"; got != want {
		t.Fatalf("stable_id = %q, want %q", got, want)
	}
	importCandidate := item["import_candidate"].(map[string]any)
	if got, want := importCandidate["status"], ReplatformingImportStatusReady; got != want {
		t.Fatalf("import_candidate.status = %q, want %q", got, want)
	}
	if got, ok := importCandidate["import_block"].(string); !ok || got == "" {
		t.Fatalf("ready import_candidate missing import_block: %#v", importCandidate["import_block"])
	}
	owners := item["owner_candidates"].([]any)
	if len(owners) == 0 {
		t.Fatal("owner_candidates is empty, want a service owner candidate")
	}
	if got, want := data["items_count"], float64(1); got != want {
		t.Fatalf("items_count = %#v, want %#v", got, want)
	}
	if _, ok := data["recommended_next_calls"]; !ok {
		t.Fatal("response missing recommended_next_calls")
	}
}

func TestComposeReplatformingPlanRefusesSafetyGatedFinding(t *testing.T) {
	t.Parallel()

	handler := &IaCHandler{
		Profile: ProfileLocalAuthoritative,
		Management: fakeIaCManagementStore{rows: []IaCManagementFindingRow{{
			ID:               "fact:aws-sg",
			Provider:         "aws",
			AccountID:        "123456789012",
			Region:           "us-east-1",
			ResourceType:     "ec2",
			ResourceID:       "security-group/sg-123",
			ARN:              "arn:aws:ec2:us-east-1:123456789012:security-group/sg-123",
			FindingKind:      findingKindOrphanedCloudResource,
			ManagementStatus: managementStatusCloudOnly,
			Confidence:       0.94,
			ScopeID:          "aws:123456789012:us-east-1:ec2",
			WarningFlags:     []string{"security_sensitive_resource"},
			SafetyGate: IaCManagementSafetyGate{
				Outcome:        "security_review_required",
				ReadOnly:       true,
				ReviewRequired: true,
				RefusedActions: []string{"terraform_import_plan"},
				Warnings:       []string{"security_sensitive_resource"},
			},
		}}},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/replatforming/plans", bytes.NewBufferString(`{
		"scope_kind": "account",
		"account_id": "123456789012",
		"region": "us-east-1",
		"limit": 10
	}`))
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	_, data := decodeReplatformingPlanResponse(t, w)
	plan := data["plan"].(map[string]any)
	item := plan["items"].([]any)[0].(map[string]any)
	if got, want := item["source_state"], string(ReplatformingSourceStateRejected); got != want {
		t.Fatalf("source_state = %q, want %q", got, want)
	}
	importCandidate := item["import_candidate"].(map[string]any)
	if got, want := importCandidate["status"], ReplatformingImportStatusRefused; got != want {
		t.Fatalf("import_candidate.status = %q, want %q", got, want)
	}
	if _, ok := importCandidate["import_block"]; ok {
		t.Fatalf("refused import candidate unexpectedly carried import_block: %#v", importCandidate["import_block"])
	}
	reasons := importCandidate["refusal_reasons"].([]any)
	if len(reasons) == 0 {
		t.Fatal("refused import candidate missing refusal_reasons")
	}
}

func TestComposeReplatformingPlanAmbiguousOwnerCarriesReasons(t *testing.T) {
	t.Parallel()

	handler := &IaCHandler{
		Profile: ProfileLocalAuthoritative,
		Management: fakeIaCManagementStore{rows: []IaCManagementFindingRow{{
			ID:                    "fact:aws-ambiguous",
			Provider:              "aws",
			AccountID:             "123456789012",
			Region:                "us-east-1",
			ResourceType:          "dynamodb",
			ResourceID:            "table/orders",
			ARN:                   "arn:aws:dynamodb:us-east-1:123456789012:table/orders",
			FindingKind:           findingKindAmbiguousCloudResource,
			ManagementStatus:      managementStatusAmbiguous,
			Confidence:            0.5,
			ScopeID:               "aws:123456789012:us-east-1:dynamodb",
			ServiceCandidates:     []string{"orders", "checkout"},
			EnvironmentCandidates: []string{"prod", "staging"},
			SafetyGate:            readySafetyGate(),
		}}},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/replatforming/plans", bytes.NewBufferString(`{
		"scope_kind": "account",
		"account_id": "123456789012",
		"region": "us-east-1"
	}`))
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	_, data := decodeReplatformingPlanResponse(t, w)
	plan := data["plan"].(map[string]any)
	item := plan["items"].([]any)[0].(map[string]any)
	if got, want := item["source_state"], string(ReplatformingSourceStateAmbiguous); got != want {
		t.Fatalf("source_state = %q, want %q", got, want)
	}
	owners := item["owner_candidates"].([]any)
	if len(owners) < 2 {
		t.Fatalf("owner_candidates length = %d, want at least 2 competing service candidates", len(owners))
	}
	for _, raw := range owners {
		owner := raw.(map[string]any)
		if owner["kind"] != "service" {
			continue
		}
		reasons, ok := owner["ambiguity_reasons"].([]any)
		if !ok || len(reasons) == 0 {
			t.Fatalf("competing service owner candidate missing ambiguity_reasons: %#v", owner)
		}
	}
}

func TestComposeReplatformingPlanEmptyEvidenceIsBoundedAnswer(t *testing.T) {
	t.Parallel()

	handler := &IaCHandler{
		Profile:    ProfileLocalAuthoritative,
		Management: fakeIaCManagementStore{},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/replatforming/plans", bytes.NewBufferString(`{
		"scope_kind": "account",
		"account_id": "123456789012"
	}`))
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	_, data := decodeReplatformingPlanResponse(t, w)
	if got, want := data["items_count"], float64(0); got != want {
		t.Fatalf("items_count = %#v, want %#v", got, want)
	}
	plan := data["plan"].(map[string]any)
	if items, ok := plan["items"].([]any); ok && len(items) != 0 {
		t.Fatalf("plan items length = %d, want 0", len(items))
	}
}

func TestComposeReplatformingPlanTruncatesAndPaginates(t *testing.T) {
	t.Parallel()

	rows := make([]IaCManagementFindingRow, 0, 3)
	for _, name := range []string{"a", "b", "c"} {
		rows = append(rows, IaCManagementFindingRow{
			ID:               "fact:" + name,
			Provider:         "aws",
			ResourceType:     "s3",
			ResourceID:       "bucket-" + name,
			ARN:              "arn:aws:s3:::bucket-" + name,
			FindingKind:      findingKindOrphanedCloudResource,
			ManagementStatus: managementStatusCloudOnly,
			ScopeID:          "aws:123456789012:us-east-1:s3",
			SafetyGate:       readySafetyGate(),
		})
	}
	handler := &IaCHandler{
		Profile:    ProfileLocalAuthoritative,
		Management: fakeIaCManagementStore{rows: rows},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/replatforming/plans", bytes.NewBufferString(`{
		"scope_kind": "account",
		"account_id": "123456789012",
		"limit": 2,
		"offset": 0
	}`))
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	_, data := decodeReplatformingPlanResponse(t, w)
	if got, want := data["items_count"], float64(2); got != want {
		t.Fatalf("items_count = %#v, want %#v", got, want)
	}
	if got, want := data["truncated"], true; got != want {
		t.Fatalf("truncated = %#v, want %#v", got, want)
	}
	if got, want := data["next_offset"], float64(2); got != want {
		t.Fatalf("next_offset = %#v, want %#v", got, want)
	}
}

func TestComposeReplatformingPlanRequiresBoundedScope(t *testing.T) {
	t.Parallel()

	handler := &IaCHandler{
		Profile:    ProfileLocalAuthoritative,
		Management: fakeIaCManagementStore{},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/replatforming/plans", bytes.NewBufferString(`{"scope_kind": "account"}`))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
}

func TestComposeReplatformingPlanUnsupportedProfile(t *testing.T) {
	t.Parallel()

	handler := &IaCHandler{
		Profile:    ProfileLocalLightweight,
		Management: fakeIaCManagementStore{},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/replatforming/plans", bytes.NewBufferString(`{
		"scope_kind": "account",
		"account_id": "123456789012"
	}`))
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusNotImplemented; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	var resp ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if resp.Error == nil {
		t.Fatal("expected error envelope for unsupported profile")
	}
	if got, want := resp.Error.Code, ErrorCodeUnsupportedCapability; got != want {
		t.Fatalf("error code = %q, want %q", got, want)
	}
}
