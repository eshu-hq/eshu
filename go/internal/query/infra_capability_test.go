// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

func TestInfraRelationshipsLocalAuthoritativeUsesGraphInsteadOfCapabilityGate(t *testing.T) {
	t.Parallel()

	graphCalled := false
	handler := &InfraHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeRepoGraphReader{
			runSingle: func(ctx context.Context, cypher string, params map[string]any) (map[string]any, error) {
				graphCalled = true
				// #7006: one label per MATCH, never the disjunction string --
				// a disjunction silently matches zero rows on the pinned
				// NornicDB build. The fake answers on the first label tried
				// (Repository, impactRelationshipAnchorLabels[0]).
				if strings.Contains(cypher, "|") {
					t.Fatalf("cypher contains a label disjunction, which silently matches zero rows on the pinned NornicDB build:\n%s", cypher)
				}
				if !strings.Contains(cypher, "MATCH (n:"+impactRelationshipAnchorLabels[0]+") WHERE n.id = $entity_id") {
					t.Fatalf("cypher = %q, want a single-label entity relationship anchor (#7006)", cypher)
				}
				if got, want := params["entity_id"], "workload:eshu"; got != want {
					t.Fatalf("entity_id param = %#v, want %#v", got, want)
				}
				// #7006 telemetry fix: bounded query name must reach the read.
				if got, want := querycontract.GraphQueryNameFromContext(ctx), "platform_impact.deployment_chain"; got != want {
					t.Fatalf("graph query name = %q, want %q", got, want)
				}
				return map[string]any{
					"id":       "workload:eshu",
					"name":     "eshu",
					"labels":   []any{"Workload"},
					"outgoing": []any{},
					"incoming": []any{},
				}, nil
			},
		},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v0/infra/relationships", bytes.NewBufferString(`{"entity_id":"workload:eshu"}`))
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()

	handler.getRelationships(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !graphCalled {
		t.Fatal("handler did not query the graph")
	}

	var envelope ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if envelope.Error != nil {
		t.Fatalf("error = %#v, want nil", envelope.Error)
	}
	if envelope.Truth == nil {
		t.Fatal("truth = nil, want truth envelope")
	}
	if got, want := envelope.Truth.Profile, ProfileLocalAuthoritative; got != want {
		t.Fatalf("truth.profile = %q, want %q", got, want)
	}
	if got, want := envelope.Truth.Capability, "platform_impact.deployment_chain"; got != want {
		t.Fatalf("truth.capability = %q, want %q", got, want)
	}
}
