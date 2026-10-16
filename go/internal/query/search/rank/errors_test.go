// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rank_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/search/rank"
)

func TestWriteErrorEmitsTheTypedEnvelope(t *testing.T) {
	t.Parallel()
	window := testWindow(10)
	first, err := rank.Walk(window, nil, rank.StartToken, 5)
	if err != nil {
		t.Fatalf("Walk error = %v", err)
	}
	window.Rows[2].Content = "edited"
	_, walkErr := rank.Walk(window, nil, first.NextCursor, 5)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/search", nil)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	if !rank.WriteError(rec, req, walkErr) {
		t.Fatal("WriteError returned false for a *rank.Error")
	}
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
	var envelope querycontract.ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if envelope.Error == nil || envelope.Error.Code != rank.ErrorCodeBasisMismatch {
		t.Fatalf("envelope error = %+v, want code %q", envelope.Error, rank.ErrorCodeBasisMismatch)
	}
	if envelope.Error.Details["reason"] != rank.ReasonCandidateSetChanged {
		t.Fatalf("details = %v, want reason %q", envelope.Error.Details, rank.ReasonCandidateSetChanged)
	}
	if envelope.Error.Details["restart_with"] != rank.StartToken {
		t.Fatalf("details = %v, want restart_with %q", envelope.Error.Details, rank.StartToken)
	}
}

func TestWriteErrorNamesTheCodeForPlainClients(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodPost, "/api/v0/content/entities/search", nil)
	rec := httptest.NewRecorder()
	if !rank.WriteError(rec, req, rank.Unsupported(rank.ReasonRankerDisabled)) {
		t.Fatal("WriteError returned false")
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, string(rank.ErrorCodeUnsupported)) || !strings.Contains(body, rank.ReasonRankerDisabled) {
		t.Fatalf("plain body %s does not name the code and reason", body)
	}
}

func TestWriteErrorIgnoresOtherErrors(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	if rank.WriteError(rec, req, errors.New("boom")) {
		t.Fatal("WriteError handled a non-rank error")
	}
	if rank.WriteError(rec, req, nil) {
		t.Fatal("WriteError handled a nil error")
	}
}
