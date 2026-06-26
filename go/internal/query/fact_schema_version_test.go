// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func mountFactSchemaVersions(t *testing.T) http.Handler {
	t.Helper()
	mux := http.NewServeMux()
	(&FactSchemaVersionHandler{Profile: ProfileProduction}).Mount(mux)
	return mux
}

type factSchemaVersionEnvelope struct {
	Data  json.RawMessage `json:"data"`
	Truth struct {
		Capability string `json:"capability"`
	} `json:"truth"`
	Error *struct {
		Code string `json:"code"`
	} `json:"error"`
}

func decodeFactSchemaVersionEnvelope(t *testing.T, body []byte) factSchemaVersionEnvelope {
	t.Helper()
	var env factSchemaVersionEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode envelope: %v\n%s", err, body)
	}
	return env
}

func TestFactSchemaVersionListReturnsRegistry(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	mountFactSchemaVersions(t).ServeHTTP(rec, envelopeRequest("/api/v0/fact-schema-versions"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	env := decodeFactSchemaVersionEnvelope(t, rec.Body.Bytes())
	if env.Error != nil {
		t.Fatalf("unexpected error envelope: %+v", env.Error)
	}
	if env.Truth.Capability != factSchemaVersionListCapability {
		t.Fatalf("truth capability = %q, want %q", env.Truth.Capability, factSchemaVersionListCapability)
	}
	var data FactSchemaVersionListResponse
	if err := json.Unmarshal(env.Data, &data); err != nil {
		t.Fatalf("decode data: %v", err)
	}
	if data.SchemaVersion != factSchemaVersionSchemaVersion {
		t.Fatalf("schema version = %q, want %q", data.SchemaVersion, factSchemaVersionSchemaVersion)
	}
	if data.Count == 0 || data.Count != data.TotalCount {
		t.Fatalf("count=%d total=%d, want equal and non-zero", data.Count, data.TotalCount)
	}
	// Entries must be sorted and include a known core fact kind.
	var prev string
	var sawTerraform bool
	for _, entry := range data.FactSchemaVersions {
		if prev != "" && entry.FactKind < prev {
			t.Fatalf("entries not sorted: %q after %q", entry.FactKind, prev)
		}
		prev = entry.FactKind
		if entry.FactKind == facts.TerraformStateFactKinds()[0] {
			sawTerraform = true
			if entry.SchemaVersion == "" {
				t.Fatalf("terraform entry has empty schema version")
			}
		}
	}
	if !sawTerraform {
		t.Fatalf("registry missing terraform state fact kind")
	}
}

func TestFactSchemaVersionListHonorsLimit(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	mountFactSchemaVersions(t).ServeHTTP(rec, envelopeRequest("/api/v0/fact-schema-versions?limit=2"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var data FactSchemaVersionListResponse
	if err := json.Unmarshal(decodeFactSchemaVersionEnvelope(t, rec.Body.Bytes()).Data, &data); err != nil {
		t.Fatalf("decode data: %v", err)
	}
	if data.Count != 2 || data.Limit != 2 || !data.Truncated {
		t.Fatalf("count=%d limit=%d truncated=%t, want 2/2/true", data.Count, data.Limit, data.Truncated)
	}
}

func TestFactSchemaVersionListRejectsBadLimit(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	mountFactSchemaVersions(t).ServeHTTP(rec, envelopeRequest("/api/v0/fact-schema-versions?limit=0"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestFactSchemaVersionDetailReturnsSupportedVersion(t *testing.T) {
	t.Parallel()
	kind := facts.TerraformStateFactKinds()[0]
	want, _ := facts.SchemaVersion(kind)
	rec := httptest.NewRecorder()
	mountFactSchemaVersions(t).ServeHTTP(rec, envelopeRequest("/api/v0/fact-schema-versions/"+kind))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var data FactSchemaVersionDetailResponse
	if err := json.Unmarshal(decodeFactSchemaVersionEnvelope(t, rec.Body.Bytes()).Data, &data); err != nil {
		t.Fatalf("decode data: %v", err)
	}
	if data.FactKind != kind || data.SupportedVersion != want {
		t.Fatalf("detail = %q/%q, want %q/%q", data.FactKind, data.SupportedVersion, kind, want)
	}
	if data.Candidate != "" || data.Compatibility != "" {
		t.Fatalf("no candidate supplied but got candidate=%q compatibility=%q", data.Candidate, data.Compatibility)
	}
}

func TestFactSchemaVersionDetailClassifiesCandidate(t *testing.T) {
	t.Parallel()
	kind := facts.TerraformStateFactKinds()[0]
	rec := httptest.NewRecorder()
	mountFactSchemaVersions(t).ServeHTTP(rec, envelopeRequest("/api/v0/fact-schema-versions/"+kind+"?candidate=2.0.0"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var data FactSchemaVersionDetailResponse
	if err := json.Unmarshal(decodeFactSchemaVersionEnvelope(t, rec.Body.Bytes()).Data, &data); err != nil {
		t.Fatalf("decode data: %v", err)
	}
	if data.Candidate != "2.0.0" || data.Compatibility != string(facts.CompatibilityUnsupportedMajor) {
		t.Fatalf("candidate=%q compatibility=%q, want 2.0.0/unsupported_major", data.Candidate, data.Compatibility)
	}
}

func TestFactSchemaVersionDetail_LocalLightweightReturnsVersionData(t *testing.T) {
	t.Parallel()
	kind := facts.TerraformStateFactKinds()[0]
	handler := &FactSchemaVersionHandler{Profile: ProfileLocalLightweight}
	mux := http.NewServeMux()
	handler.Mount(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, envelopeRequest("/api/v0/fact-schema-versions/"+kind))
	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	env := decodeFactSchemaVersionEnvelope(t, rec.Body.Bytes())
	if env.Error != nil {
		t.Fatalf("unexpected error envelope: %+v", env.Error)
	}
	if env.Truth.Capability != factSchemaVersionDetailCapability {
		t.Fatalf("truth capability = %q, want %q", env.Truth.Capability, factSchemaVersionDetailCapability)
	}
}

func TestFactSchemaVersionDetailUnknownKindReturns404(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	mountFactSchemaVersions(t).ServeHTTP(rec, envelopeRequest("/api/v0/fact-schema-versions/dev.example.not_a_core_kind"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
	env := decodeFactSchemaVersionEnvelope(t, rec.Body.Bytes())
	if env.Error == nil || env.Error.Code != string(ErrorCodeNotFound) {
		t.Fatalf("error = %+v, want not_found", env.Error)
	}
}
