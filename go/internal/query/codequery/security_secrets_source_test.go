// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
)

// hardcodedSecretSourceContentStore is a store that reports which storage path
// served the read, the way ContentReader does (#7125).
type hardcodedSecretSourceContentStore struct {
	content.FakePortContentStore
	source HardcodedSecretReadSource
}

func (s *hardcodedSecretSourceContentStore) InvestigateHardcodedSecrets(
	ctx context.Context, req HardcodedSecretInvestigationRequest,
) ([]HardcodedSecretFindingRow, error) {
	rows, _, err := s.InvestigateHardcodedSecretsWithSource(ctx, req)
	return rows, err
}

func (s *hardcodedSecretSourceContentStore) InvestigateHardcodedSecretsWithSource(
	context.Context, HardcodedSecretInvestigationRequest,
) ([]HardcodedSecretFindingRow, HardcodedSecretReadSource, error) {
	return []HardcodedSecretFindingRow{{
		RepoID: "repo-1", RelativePath: "cmd/api/config.go", Language: "go", LineNumber: 42,
		LineText: `apiToken := "sk_live_1234567890abcdef"`, FindingKind: "api_token", Confidence: "high", Severity: "high",
	}}, s.source, nil
}

func investigateWithSource(t *testing.T, store ContentStore) ResponseEnvelope {
	t.Helper()
	handler := &CodeHandler{Content: store, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/security/secrets/investigate",
		bytes.NewBufferString(`{"repo_id":"repo-1"}`))
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	var envelope ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope
}

// TestHardcodedSecretLegacyScanReadIsLabelledLimitedNotSilent proves a read the
// legacy scan served says so in coverage and truth, and a side-table read does
// not carry the limitation.
func TestHardcodedSecretLegacyScanReadIsLabelledLimitedNotSilent(t *testing.T) {
	t.Parallel()

	legacy := investigateWithSource(t, &hardcodedSecretSourceContentStore{source: HardcodedSecretReadLegacyScan})
	coverage := legacy.Data.(map[string]any)["coverage"].(map[string]any)
	if got := coverage["read_path"]; got != "legacy_scan" {
		t.Fatalf("legacy read coverage.read_path = %#v, want legacy_scan", got)
	}
	limitations, _ := coverage["limitations"].([]any)
	if len(limitations) != 1 || !strings.Contains(limitations[0].(string), "not published ready") {
		t.Fatalf("legacy read coverage.limitations = %#v, want one not-published-ready limitation", limitations)
	}
	if !strings.Contains(legacy.Truth.Reason, "legacy content scan") {
		t.Fatalf("legacy read truth reason = %q, want it to name the legacy content scan", legacy.Truth.Reason)
	}

	fast := investigateWithSource(t, &hardcodedSecretSourceContentStore{source: HardcodedSecretReadSideTable})
	coverage = fast.Data.(map[string]any)["coverage"].(map[string]any)
	if got := coverage["read_path"]; got != "side_table" {
		t.Fatalf("side-table read coverage.read_path = %#v, want side_table", got)
	}
	if _, present := coverage["limitations"]; present {
		t.Fatalf("side-table read carries limitations %#v, want none", coverage["limitations"])
	}
	if strings.Contains(fast.Truth.Reason, "legacy") {
		t.Fatalf("side-table read truth reason = %q, want no legacy mention", fast.Truth.Reason)
	}

	// A store that cannot report a source keeps working and is the side table.
	plain := investigateWithSource(t, &hardcodedSecretInvestigationContentStore{})
	coverage = plain.Data.(map[string]any)["coverage"].(map[string]any)
	if got := coverage["read_path"]; got != "side_table" {
		t.Fatalf("source-blind store coverage.read_path = %#v, want side_table", got)
	}
}
