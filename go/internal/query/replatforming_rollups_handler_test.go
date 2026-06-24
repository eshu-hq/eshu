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

func rollupFinding(arn, status, kind string, services, environments []string) IaCManagementFindingRow {
	parsed := parseAWSManagementARN(arn)
	return IaCManagementFindingRow{
		ID:                    "fact:" + arn,
		Provider:              "aws",
		AccountID:             "123456789012",
		Region:                "us-east-1",
		ResourceType:          parsed.resourceType,
		ResourceID:            parsed.resourceID,
		ARN:                   arn,
		FindingKind:           kind,
		ManagementStatus:      status,
		ScopeID:               "aws:123456789012:us-east-1:lambda",
		GenerationID:          "generation:aws-1",
		SourceSystem:          "aws",
		ServiceCandidates:     services,
		EnvironmentCandidates: environments,
	}
}

func postRollups(t *testing.T, handler *IaCHandler, body string) (*httptest.ResponseRecorder, ResponseEnvelope) {
	t.Helper()
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(http.MethodPost, "/api/v0/replatforming/rollups", bytes.NewBufferString(body))
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

func rollupGroups(t *testing.T, data map[string]any, dimension string) []map[string]any {
	t.Helper()
	dims, ok := data["dimensions"].(map[string]any)
	if !ok {
		t.Fatalf("dimensions missing or wrong type: %#v", data["dimensions"])
	}
	raw, ok := dims[dimension].([]any)
	if !ok {
		t.Fatalf("dimension %q missing or wrong type: %#v", dimension, dims[dimension])
	}
	out := make([]map[string]any, 0, len(raw))
	for _, g := range raw {
		out = append(out, g.(map[string]any))
	}
	return out
}

func findGroup(groups []map[string]any, key string) map[string]any {
	for _, g := range groups {
		if g["key"] == key {
			return g
		}
	}
	return nil
}

func sourceStateCount(t *testing.T, group map[string]any, state string) float64 {
	t.Helper()
	counts, ok := group["source_state_counts"].(map[string]any)
	if !ok {
		t.Fatalf("source_state_counts missing: %#v", group)
	}
	if counts[state] == nil {
		return 0
	}
	return counts[state].(float64)
}

func TestReplatformingRollupsRequiresBoundedScope(t *testing.T) {
	t.Parallel()
	handler := &IaCHandler{Profile: ProfileLocalAuthoritative, Management: fakeIaCManagementStore{}}
	w, _ := postRollups(t, handler, `{}`)
	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
}

func TestReplatformingRollupsUnsupportedProfile(t *testing.T) {
	t.Parallel()
	handler := &IaCHandler{Profile: ProfileLocalLightweight, Management: fakeIaCManagementStore{}}
	w, _ := postRollups(t, handler, `{"account_id":"123456789012"}`)
	if got, want := w.Code, http.StatusNotImplemented; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	var resp ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if resp.Error == nil || resp.Error.Code != ErrorCodeUnsupportedCapability {
		t.Fatalf("error code = %#v, want %q", resp.Error, ErrorCodeUnsupportedCapability)
	}
}

func TestReplatformingRollupsEmptyScope(t *testing.T) {
	t.Parallel()
	handler := &IaCHandler{Profile: ProfileLocalAuthoritative, Management: fakeIaCManagementStore{}}
	w, resp := postRollups(t, handler, `{"account_id":"123456789012"}`)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	data := resp.Data.(map[string]any)
	if got, want := data["total_findings_count"].(float64), float64(0); got != want {
		t.Fatalf("total_findings_count = %v, want %v", got, want)
	}
	if got, want := resp.Truth.Capability, "replatforming.rollups.readiness"; got != want {
		t.Fatalf("truth capability = %q, want %q", got, want)
	}
	// Empty scope still returns each dimension as an empty, present list.
	for _, dim := range []string{"account", "environment", "service"} {
		if g := rollupGroups(t, data, dim); len(g) != 0 {
			t.Fatalf("dimension %q = %#v, want empty", dim, g)
		}
	}
}

func TestReplatformingRollupsPreservesSourceStateAndReadiness(t *testing.T) {
	t.Parallel()
	store := fakeIaCManagementStore{rows: []IaCManagementFindingRow{
		// managed_by_terraform -> exact, import not applicable, not refused.
		rollupFinding("arn:aws:lambda:us-east-1:123456789012:function:exact-fn",
			"managed_by_terraform", "unmanaged_cloud_resource",
			[]string{"payments"}, []string{"prod"}),
		// cloud_only -> derived.
		rollupFinding("arn:aws:s3:::derived-bucket",
			"cloud_only", "unmanaged_cloud_resource",
			[]string{"payments"}, []string{"prod"}),
		// stale_iac_candidate -> stale (must not flatten to clean).
		rollupFinding("arn:aws:logs:us-east-1:123456789012:log-group:/stale",
			"stale_iac_candidate", "orphaned_cloud_resource",
			[]string{"billing"}, []string{"staging"}),
		// unknown_management -> unknown.
		rollupFinding("arn:aws:sns:us-east-1:123456789012:unknown-topic",
			"unknown_management", "unknown_cloud_resource",
			[]string{"billing"}, []string{"staging"}),
		// ambiguous_management -> ambiguous, multiple service candidates -> ambiguous bucket.
		rollupFinding("arn:aws:iam::123456789012:role/ambiguous-role",
			"ambiguous_management", "ambiguous_cloud_resource",
			[]string{"payments", "billing"}, nil),
	}}
	handler := &IaCHandler{Profile: ProfileLocalAuthoritative, Management: store}
	w, resp := postRollups(t, handler, `{"account_id":"123456789012","region":"us-east-1"}`)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	data := resp.Data.(map[string]any)
	if got, want := data["total_findings_count"].(float64), float64(5); got != want {
		t.Fatalf("total_findings_count = %v, want %v", got, want)
	}

	// Account dimension: one account, all five findings, source states preserved.
	accounts := rollupGroups(t, data, "account")
	if len(accounts) != 1 {
		t.Fatalf("account groups = %d, want 1", len(accounts))
	}
	acct := accounts[0]
	if got, want := acct["key"], "123456789012"; got != want {
		t.Fatalf("account key = %q, want %q", got, want)
	}
	if got, want := acct["total"].(float64), float64(5); got != want {
		t.Fatalf("account total = %v, want %v", got, want)
	}
	// The handler normalizes each finding's safety gate exactly like the
	// row-level surfaces do: unknown, stale, and ambiguous (here an IAM role)
	// management statuses require review, so they resolve to the rejected source
	// state rather than presenting as their evidence-derived state. Only the
	// managed_by_terraform (exact) and cloud_only (derived) findings survive
	// non-rejected. This proves source-state preservation: rejected wins and
	// nothing is flattened into a silent "clean" total.
	for state, want := range map[string]float64{
		"exact": 1, "derived": 1, "rejected": 3,
	} {
		if got := sourceStateCount(t, acct, state); got != want {
			t.Fatalf("account source_state_counts[%q] = %v, want %v", state, got, want)
		}
	}
	for _, state := range []string{"partial", "unavailable", "unsupported", "stale", "unknown", "ambiguous"} {
		if got := sourceStateCount(t, acct, state); got != 0 {
			t.Fatalf("account source_state_counts[%q] = %v, want 0 (safety gate resolves these to rejected)", state, got)
		}
	}

	// Readiness rollup must keep import-ready separate from needs-review/refused.
	readiness, ok := acct["readiness"].(map[string]any)
	if !ok {
		t.Fatalf("readiness missing: %#v", acct)
	}
	// Only the cloud_only/derived finding with a supported family is import-ready.
	if got, want := readiness["import_ready"].(float64), float64(1); got != want {
		t.Fatalf("import_ready = %v, want %v", got, want)
	}
	// The exact (managed_by_terraform) finding is not importable and not refused.
	if got, want := readiness["needs_review"].(float64), float64(1); got != want {
		t.Fatalf("needs_review = %v, want %v", got, want)
	}
	// Stale, unknown, and ambiguous findings are safety-refused.
	if got, want := readiness["refused"].(float64), float64(3); got != want {
		t.Fatalf("refused = %v, want %v", got, want)
	}

	// Service dimension: payments and billing each attributed singly; ambiguous role bucketed.
	services := rollupGroups(t, data, "service")
	if g := findGroup(services, "payments"); g == nil {
		t.Fatalf("service payments group missing: %#v", services)
	} else if got, want := g["total"].(float64), float64(2); got != want {
		t.Fatalf("payments total = %v, want %v", got, want)
	}
	if g := findGroup(services, replatformingRollupAmbiguousKey); g == nil {
		t.Fatalf("service ambiguous bucket missing: %#v", services)
	} else if got, want := g["total"].(float64), float64(1); got != want {
		t.Fatalf("service ambiguous total = %v, want %v", got, want)
	}

	// Environment dimension: prod=2, staging=2, and the role with no env -> unattributed=1.
	environments := rollupGroups(t, data, "environment")
	if g := findGroup(environments, "prod"); g == nil || g["total"].(float64) != 2 {
		t.Fatalf("environment prod group = %#v, want total 2", g)
	}
	if g := findGroup(environments, replatformingRollupUnattributedKey); g == nil || g["total"].(float64) != 1 {
		t.Fatalf("environment unattributed group = %#v, want total 1", g)
	}
}

func TestReplatformingRollupsRejectedWinsOverEvidence(t *testing.T) {
	t.Parallel()
	// A cloud_only finding for a security-sensitive resource (KMS) is refused by
	// the normalized safety gate, so it must resolve to rejected, never
	// import_ready, even though its evidence-derived state would be derived.
	finding := rollupFinding("arn:aws:kms:us-east-1:123456789012:key/abcd",
		"cloud_only", "unmanaged_cloud_resource",
		[]string{"payments"}, []string{"prod"})
	store := fakeIaCManagementStore{rows: []IaCManagementFindingRow{finding}}
	handler := &IaCHandler{Profile: ProfileLocalAuthoritative, Management: store}
	w, resp := postRollups(t, handler, `{"account_id":"123456789012"}`)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	data := resp.Data.(map[string]any)
	acct := rollupGroups(t, data, "account")[0]
	if got := sourceStateCount(t, acct, "rejected"); got != 1 {
		t.Fatalf("rejected count = %v, want 1", got)
	}
	if got := sourceStateCount(t, acct, "derived"); got != 0 {
		t.Fatalf("derived count = %v, want 0 (rejected wins)", got)
	}
	readiness := acct["readiness"].(map[string]any)
	if got, want := readiness["refused"].(float64), float64(1); got != want {
		t.Fatalf("refused = %v, want %v", got, want)
	}
	if got, want := readiness["import_ready"].(float64), float64(0); got != want {
		t.Fatalf("import_ready = %v, want %v", got, want)
	}
}

func TestReplatformingRollupsTruncationFlag(t *testing.T) {
	t.Parallel()
	rows := make([]IaCManagementFindingRow, 0, 3)
	for _, name := range []string{"a", "b", "c"} {
		rows = append(rows, rollupFinding("arn:aws:s3:::bucket-"+name,
			"cloud_only", "unmanaged_cloud_resource", []string{"svc"}, []string{"prod"}))
	}
	store := fakeIaCManagementStore{rows: rows}
	handler := &IaCHandler{Profile: ProfileLocalAuthoritative, Management: store}
	// limit 2 of 3 -> truncated true, rollup covers only the bounded page.
	w, resp := postRollups(t, handler, `{"account_id":"123456789012","limit":2}`)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	data := resp.Data.(map[string]any)
	if got, want := data["truncated"].(bool), true; got != want {
		t.Fatalf("truncated = %v, want %v", got, want)
	}
	if got, want := data["rollup_findings_count"].(float64), float64(2); got != want {
		t.Fatalf("rollup_findings_count = %v, want %v", got, want)
	}
	if got, want := data["total_findings_count"].(float64), float64(3); got != want {
		t.Fatalf("total_findings_count = %v, want %v", got, want)
	}
}
