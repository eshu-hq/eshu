// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/secrets"
)

// recordingPostureSummaryStore and recordingGrantPostureStore satisfy the
// secrets package's IAMPostureSummaryStore and IAMGrantPostureStore
// interfaces (spelled here through the SecretsIAMHandler alias) for every
// secrets/IAM test that stays in this root package: this file's graph-read
// sweep below, plus secrets_iam_authz_test.go and
// capability_matrix_secrets_iam_test.go. One definition per fixture keeps
// the three staying files from redeclaring it.
type recordingPostureSummaryStore struct {
	summary     secrets.IAMPostureSummary
	lastScopeID string
}

func (s *recordingPostureSummaryStore) SummarizeSecretsIAMPosture(
	_ context.Context, scopeID string,
) (secrets.IAMPostureSummary, error) {
	s.lastScopeID = scopeID
	return s.summary, nil
}

type recordingGrantPostureStore struct {
	posture     secrets.IAMGrantPosture
	err         error
	lastScopeID string
}

func (s *recordingGrantPostureStore) SummarizeS3ExternalPrincipalGrantPosture(
	_ context.Context, scopeID string,
) (secrets.IAMGrantPosture, error) {
	s.lastScopeID = scopeID
	if s.err != nil {
		return secrets.IAMGrantPosture{}, s.err
	}
	return s.posture, nil
}

// TestSecretsIAMPostureSummaryGraphReadSweep proves the grant-section read maps
// bounded graph-read sentinels onto the 503/504 contract. GrantPosture is wired
// in production to a graph-backed store (secrets.NewGraphIAMGrantPostureStore),
// so a backend timeout or outage during SummarizeS3ExternalPrincipalGrantPosture
// must not collapse into a generic 500. The Postgres Summary read succeeds
// first, so the sentinel can only originate from the graph grant read.
func TestSecretsIAMPostureSummaryGraphReadSweep(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			handler := &SecretsIAMHandler{
				Summary:      &recordingPostureSummaryStore{},
				GrantPosture: &recordingGrantPostureStore{err: test.err},
				Profile:      ProfileProduction,
			}
			mux := http.NewServeMux()
			handler.Mount(mux)

			req := httptest.NewRequest(http.MethodGet, "/api/v0/secrets-iam/posture-summary?scope_id=scope-1", nil)
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}
