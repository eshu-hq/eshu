// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package incident

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/incident/model"
	incidentsql "github.com/eshu-hq/eshu/go/internal/query/incident/sql"
	"github.com/eshu-hq/eshu/go/internal/query/incident/store"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

type recordingIncidentContextStore struct {
	snapshot   model.IncidentContextSnapshot
	err        error
	lastFilter model.IncidentContextFilter
}

func (s *recordingIncidentContextStore) ReadIncidentContext(
	_ context.Context,
	filter model.IncidentContextFilter,
) (model.IncidentContextSnapshot, error) {
	s.lastFilter = filter
	return s.snapshot, s.err
}

type unusedIncidentContextQueryer struct{}

func (unusedIncidentContextQueryer) QueryContext(
	context.Context,
	string,
	...any,
) (*sql.Rows, error) {
	return nil, fmt.Errorf("query must not run for invalid filters")
}

func TestIncidentContextHandlerUsesBoundedStore(t *testing.T) {
	t.Parallel()

	store := &recordingIncidentContextStore{
		snapshot: model.IncidentContextSnapshot{
			Query: model.IncidentContextQuery{
				Provider:           "pagerduty",
				ProviderIncidentID: "PABC123",
				ScopeID:            "pagerduty-prod",
				ServiceID:          "P-SVC",
				Limit:              6,
			},
			Incident: model.IncidentContextIncident{
				Provider:           "pagerduty",
				ProviderIncidentID: "PABC123",
				Title:              "Checkout elevated error rate",
				Service: model.IncidentContextReference{
					ID:      "P-SVC",
					Summary: "checkout-api",
				},
				EvidenceFactID: "incident-fact",
			},
		},
	}
	handler := &IncidentHandler{Context: store, Profile: querycontract.ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/incidents/PABC123/context?provider=pagerduty&scope_id=pagerduty-prod&service_id=P-SVC&limit=5",
		nil,
	)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := store.lastFilter.Provider, "pagerduty"; got != want {
		t.Fatalf("provider = %q, want %q", got, want)
	}
	if got, want := store.lastFilter.ProviderIncidentID, "PABC123"; got != want {
		t.Fatalf("provider incident id = %q, want %q", got, want)
	}
	if got, want := store.lastFilter.ScopeID, "pagerduty-prod"; got != want {
		t.Fatalf("scope id = %q, want %q", got, want)
	}
	if got, want := store.lastFilter.ServiceID, "P-SVC"; got != want {
		t.Fatalf("service id = %q, want %q", got, want)
	}
	if got, want := store.lastFilter.Limit, 6; got != want {
		t.Fatalf("limit = %d, want limit+1 %d", got, want)
	}

	var envelope querycontract.ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal envelope: %v", err)
	}
	if envelope.Truth == nil || envelope.Truth.Capability != model.Capability {
		t.Fatalf("truth = %#v, want incident context capability", envelope.Truth)
	}
	dataBytes, err := json.Marshal(envelope.Data)
	if err != nil {
		t.Fatalf("json.Marshal data: %v", err)
	}
	var body model.IncidentContextResponse
	if err := json.Unmarshal(dataBytes, &body); err != nil {
		t.Fatalf("json.Unmarshal data: %v", err)
	}
	data, ok := envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope data type = %T, want map", envelope.Data)
	}
	packet := querytestutil.RequireAnswerPacketCompanion(t, data, "incident.context")
	if got, want := packet["primary_tool"], "get_incident_context"; got != want {
		t.Fatalf("answer_packet.primary_tool = %#v, want %#v", got, want)
	}
	querytestutil.AssertIncidentEdge(t, body.EvidencePath, model.IncidentSlotWorkItem, model.IncidentTruthMissing)
}

func TestIncidentContextHandlerRequiresIncidentIDAndLimit(t *testing.T) {
	t.Parallel()

	handler := &IncidentHandler{Context: &recordingIncidentContextStore{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	for _, target := range []string{
		"/api/v0/incidents/%20/context?limit=5",
		"/api/v0/incidents/PABC123/context?limit=0",
		"/api/v0/incidents/PABC123/context?limit=1000",
	} {
		target := target
		t.Run(target, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, target, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if got, want := w.Code, http.StatusBadRequest; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
			}
		})
	}
}

func TestIncidentContextHandlerReturnsAmbiguousCandidates(t *testing.T) {
	t.Parallel()

	store := &recordingIncidentContextStore{
		err: model.IncidentContextAmbiguousError{
			ProviderIncidentID: "PABC123",
			Candidates: []model.IncidentContextIncidentCandidate{
				{Provider: "pagerduty", ProviderIncidentID: "PABC123", ScopeID: "pd-prod"},
				{Provider: "pagerduty", ProviderIncidentID: "PABC123", ScopeID: "pd-stage"},
			},
		},
	}
	handler := &IncidentHandler{Context: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/incidents/PABC123/context?limit=5", nil)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusConflict; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"code":"ambiguous"`) {
		t.Fatalf("body = %s, want ambiguous code", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"pd-stage"`) {
		t.Fatalf("body = %s, want candidate scope", w.Body.String())
	}
}

func TestPostgresIncidentContextStoreRejectsUnboundedFilter(t *testing.T) {
	t.Parallel()

	store := store.NewStore(unusedIncidentContextQueryer{})
	_, err := store.ReadIncidentContext(context.Background(), model.IncidentContextFilter{Limit: 5})
	if err == nil {
		t.Fatal("ReadIncidentContext() error = nil, want required incident id error")
	}
	if !strings.Contains(err.Error(), "provider_incident_id is required") {
		t.Fatalf("error = %q, want provider_incident_id requirement", err.Error())
	}
}

func TestIncidentContextQueriesStayBoundedToActiveFacts(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"scope.active_generation_id = fact.generation_id",
		"fact.fact_kind = 'incident.record'",
		"fact.payload->>'provider_incident_id' = $2",
		"NULLIF(fact.payload->>'provider_incident_id', '') IS NULL",
		"fact.source_record_id = $2",
		"($3 = '' OR fact.scope_id = $3)",
		"LIMIT $4",
	} {
		queryText := incidentsql.ListIncidentsQuery
		if !strings.Contains(queryText, want) {
			t.Fatalf("queryText missing %q:\n%s", want, queryText)
		}
	}
	for _, forbidden := range []string{
		"MATCH ",
		"CALL db",
	} {
		queryText := incidentsql.ListIncidentsQuery
		if strings.Contains(queryText, forbidden) {
			t.Fatalf("queryText must not use graph scan %q:\n%s", forbidden, queryText)
		}
	}
	for _, want := range []string{
		"fact.fact_kind = 'change.record'",
		"fact.scope_id = $2",
		"fact.generation_id = $3",
		"($4::timestamptz IS NULL OR NULLIF(fact.payload->>'timestamp', '')::timestamptz >= $4::timestamptz)",
		"($5::timestamptz IS NULL OR NULLIF(fact.payload->>'timestamp', '')::timestamptz <= $5::timestamptz)",
		"LIMIT $6",
	} {
		queryText := incidentsql.ListChangeCandidatesQuery
		if !strings.Contains(queryText, want) {
			t.Fatalf("queryText missing %q:\n%s", want, queryText)
		}
	}
}

func TestIncidentContextChangeCandidateQueryCastsNullableTimeParametersEverywhere(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"$4::timestamptz IS NULL",
		"NULLIF(fact.payload->>'timestamp', '')::timestamptz >= $4::timestamptz",
		"$5::timestamptz IS NULL",
		"NULLIF(fact.payload->>'timestamp', '')::timestamptz <= $5::timestamptz",
	} {
		queryText := incidentsql.ListChangeCandidatesQuery
		if !strings.Contains(queryText, want) {
			t.Fatalf("queryText missing %q:\n%s", want, queryText)
		}
	}
}

func TestIncidentContextChangeCandidateQueryCastsServiceIDParameter(t *testing.T) {
	t.Parallel()

	want := "jsonb_build_object('id', $1::text)"
	queryText := incidentsql.ListChangeCandidatesQuery
	if !strings.Contains(queryText, want) {
		t.Fatalf("incidentsql.ListChangeCandidatesQuery missing %q:\n%s", want, queryText)
	}
}

func TestIncidentContextRuntimeQueriesStayBoundedToExplicitEvidence(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"fact.fact_kind = 'service_catalog.operational_link'",
		"fact.payload->>'url' = $1",
		"scope.active_generation_id = fact.generation_id",
		"LIMIT $2",
	} {
		queryText := incidentsql.ListServiceCatalogOperationalLinksQuery
		if !strings.Contains(queryText, want) {
			t.Fatalf("queryText missing %q:\n%s", want, queryText)
		}
	}
	for _, want := range []string{
		"fact.fact_kind = 'reducer_kubernetes_correlation'",
		"fact.payload->>'source_digest' = $1",
		"fact.payload->>'image_ref' = $2",
		"fact.payload->>'outcome' IN ('exact', 'derived', 'ambiguous')",
		"LIMIT $3",
	} {
		queryText := incidentsql.ListKubernetesCorrelationsByImageQuery
		if !strings.Contains(queryText, want) {
			t.Fatalf("queryText missing %q:\n%s", want, queryText)
		}
	}
	for _, want := range []string{
		"fact.fact_kind = 'reducer_ci_cd_run_correlation'",
		"fact.payload->>'image_ref' = $1",
		"fact.payload->>'outcome' IN ('exact', 'derived', 'ambiguous')",
		"LIMIT $2",
	} {
		queryText := incidentsql.ListCICDRunCorrelationsByImageRefQuery
		if !strings.Contains(queryText, want) {
			t.Fatalf("queryText missing %q:\n%s", want, queryText)
		}
	}
	for _, want := range []string{
		"FROM webhook_refresh_triggers",
		"provider = 'github'",
		"event_kind = 'pull_request_merged'",
		"decision = 'accepted'",
		"target_sha = $1",
		"pull_request_url <> ''",
		"LIMIT $2",
	} {
		queryText := incidentsql.ListPullRequestsByCommitQuery
		if !strings.Contains(queryText, want) {
			t.Fatalf("queryText missing %q:\n%s", want, queryText)
		}
	}
	for _, want := range []string{
		"fact.fact_kind = 'work_item.record'",
		"fact.payload->>'work_item_key' = $1",
		"LIMIT $2",
	} {
		queryText := incidentsql.ListWorkItemRecordsByKeyQuery
		if !strings.Contains(queryText, want) {
			t.Fatalf("queryText missing %q:\n%s", want, queryText)
		}
	}
}
