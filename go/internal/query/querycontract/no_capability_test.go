// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// TestWriteGraphReadErrorWithNoCapabilityOmitsTheField pins the NoCapability
// contract: the reader verdict keeps its status, Retry-After, code, and
// message, and the envelope carries no capability field at all.
func TestWriteGraphReadErrorWithNoCapabilityOmitsTheField(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/api/v0/status/pipeline", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	if !WriteGraphReadError(rec, req, fmt.Errorf("private reader detail: %w", db.ErrReaderStale), NoCapability) {
		t.Fatal("WriteGraphReadError() = false, want the reader verdict written")
	}
	if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("status = %d, Retry-After = %q; want 503 with Retry-After", rec.Code, rec.Header().Get("Retry-After"))
	}
	var body struct {
		Error map[string]any `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v; body = %s", err, rec.Body.String())
	}
	if _, present := body.Error["capability"]; present {
		t.Fatalf("envelope carries a capability field: %s", rec.Body.String())
	}
	if body.Error["code"] != string(ErrorCodeBackendUnavailable) || body.Error["message"] != ErrReaderRetryable.Error() {
		t.Fatalf("envelope = %v, want backend_unavailable with the fixed reader message", body.Error)
	}
	if strings.Contains(rec.Body.String(), "private") {
		t.Fatalf("body leaked the reader error text: %s", rec.Body.String())
	}
}
