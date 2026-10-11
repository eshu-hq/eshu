// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriteContentSubstringIndexUnavailableAnswersOnlyTheSentinelText(t *testing.T) {
	t.Parallel()

	const canary = "password=hunter2 MATCH (n)"
	err := fmt.Errorf("search content files (%s): %w", canary, ErrContentSubstringIndexesNotReady)

	rec := httptest.NewRecorder()
	if !WriteContentSubstringIndexUnavailable(rec, err) {
		t.Fatal("WriteContentSubstringIndexUnavailable() = false, want true for a wrapped sentinel")
	}
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
	body := rec.Body.String()
	if strings.Contains(body, canary) {
		t.Fatalf("body leaked the wrapped error text: %s", body)
	}
	var payload struct {
		Detail string `json:"detail"`
	}
	if decodeErr := json.Unmarshal([]byte(body), &payload); decodeErr != nil {
		t.Fatalf("decode body %q: %v", body, decodeErr)
	}
	if payload.Detail != ErrContentSubstringIndexesNotReady.Error() {
		t.Fatalf("detail = %q, want %q", payload.Detail, ErrContentSubstringIndexesNotReady.Error())
	}
}

func TestWriteContentSubstringIndexUnavailableIgnoresOtherErrors(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	if WriteContentSubstringIndexUnavailable(rec, errors.New("graph read failed")) {
		t.Fatal("WriteContentSubstringIndexUnavailable() = true, want false for an unrelated error")
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("body = %q, want untouched response", rec.Body.String())
	}
}
