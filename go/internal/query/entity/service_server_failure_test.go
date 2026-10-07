// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	supplychain "github.com/eshu-hq/eshu/go/internal/query/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
	"github.com/eshu-hq/eshu/go/internal/query/tracing"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// Service-route stages a server failure can come from.
const (
	serviceFailQuery      = "query"
	serviceFailEnrichment = "enrichment"
	serviceFailCICD       = "cicd"
	serviceFailSupply     = "supply chain"
)

type failingCICDRunCorrelationStore struct{ err error }

func (s failingCICDRunCorrelationStore) ListCICDRunCorrelations(context.Context, querycontract.CICDRunCorrelationFilter) ([]querycontract.CICDRunCorrelationRow, error) {
	return nil, s.err
}

type failingImageIdentityStore struct{ err error }

func (s failingImageIdentityStore) ListContainerImageIdentities(context.Context, supplychain.ContainerImageIdentityFilter) ([]supplychain.ContainerImageIdentityRow, error) {
	return nil, s.err
}

// serviceFailureHandler builds a handler whose service resolves to
// workload:api with one deployment image, and whose read for stage fails
// with err.
func serviceFailureHandler(stage string, err error) *Handler {
	workload := map[string]any{"id": "workload:api", "name": "api", "kind": "service", "repo_id": "repo://example/api", "repo_name": "api"}
	reader := graph.FakeWorkloadGraphReader{
		RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			switch {
			case stage == serviceFailQuery:
				return nil, err
			case strings.Contains(cypher, "HAS_DEPLOYMENT_EVIDENCE") && stage == serviceFailEnrichment:
				return nil, err
			case strings.Contains(cypher, "MATCH (w:Workload {id: $workload_id})<-[:DEFINES]-(r:Repository)"):
				return []map[string]any{{"repo_id": "repo://example/api", "repo_name": "api"}}, nil
			case strings.Contains(cypher, "collect(DISTINCT dr.id) as defining"), strings.Contains(cypher, "w.name = $service_name"):
				return []map[string]any{workload}, nil
			case strings.Contains(cypher, "HAS_DEPLOYMENT_EVIDENCE") &&
				strings.Contains(cypher, "(r:Repository {id: $repo_id})<-[:EVIDENCES_REPOSITORY_RELATIONSHIP]-(artifact:EvidenceArtifact)"):
				return []map[string]any{serviceStoryDeploymentImageArtifact(serviceStoryTestImageRef)}, nil
			default:
				return nil, nil
			}
		},
		RunSingleFn: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
			if stage == serviceFailQuery {
				return nil, err
			}
			if strings.Contains(cypher, "w.id = $workload_id") {
				return workload, nil
			}
			return nil, nil
		},
	}
	handler := &Handler{Neo4j: reader, Profile: querycontract.ProfileLocalAuthoritative}
	switch stage {
	case serviceFailCICD:
		handler.CICDRunCorrelations = failingCICDRunCorrelationStore{err: err}
	case serviceFailSupply:
		handler.ContainerImageIdentities = failingImageIdentityStore{err: err}
		handler.SBOMAttachments = &serviceStorySBOMAttachmentStore{}
	}
	return handler
}

// TestServiceRoutesServerFailureAnswersFixedMessage is the #7626 regression
// for every 500 on the service context, investigation, and story routes. Each
// answered "<prefix>: <backend error>" and left the span untouched, and the
// story's ci/cd and supply-chain steps also turned a reader fence into a 500.
// Each must answer its fixed message with a span error, map a reader fence to
// the retryable 503, and answer a client cancel with 499 and no span error.
func TestServiceRoutesServerFailureAnswersFixedMessage(t *testing.T) {
	t.Parallel()

	sites := []struct {
		route   string
		stage   string
		message string
	}{
		{"/api/v0/services/api/context", serviceFailQuery, serviceContextQueryFailedMessage},
		{"/api/v0/services/api/context", serviceFailEnrichment, serviceContextEnrichmentFailedMessage},
		{"/api/v0/investigations/services/api", serviceFailQuery, serviceInvestigationQueryFailedMessage},
		{"/api/v0/investigations/services/api", serviceFailEnrichment, serviceInvestigationEnrichmentFailedMessage},
		{"/api/v0/services/api/story", serviceFailQuery, serviceStoryQueryFailedMessage},
		{"/api/v0/services/api/story", serviceFailEnrichment, serviceStoryEnrichmentFailedMessage},
		{"/api/v0/services/api/story", serviceFailCICD, serviceStoryCICDEvidenceFailedMessage},
		{"/api/v0/services/api/story", serviceFailSupply, serviceStorySupplyChainFailedMessage},
	}
	cases := []struct {
		name       string
		err        error
		cancel     bool
		wantStatus int
	}{
		{"backend failure", errors.New("private neo4j: connection reset by 10.0.0.7"), false, http.StatusInternalServerError},
		{"reader stale", fmt.Errorf("private read store: %w", db.ErrReaderStale), false, http.StatusServiceUnavailable},
		{"client cancel", fmt.Errorf("private read store: %w", context.Canceled), true, querycontract.StatusClientClosedRequest},
	}
	for _, site := range sites {
		for _, tc := range cases {
			t.Run(site.route+"/"+site.stage+"/"+tc.name, func(t *testing.T) {
				t.Parallel()

				recorder := tracetest.NewSpanRecorder()
				provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
				t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
				ctx, span := provider.Tracer("service-server-failure-test").Start(context.Background(), "request")
				ctx, cancel := context.WithCancel(ctx)
				defer cancel()
				if tc.cancel {
					cancel()
				}

				mux := http.NewServeMux()
				serviceFailureHandler(site.stage, tc.err).Mount(mux)
				rec := httptest.NewRecorder()
				mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, site.route, nil).WithContext(ctx))
				span.End()

				assertServiceServerFailure(t, rec, recorder.Ended(), site.message, tc.wantStatus, tc.cancel)
			})
		}
	}
}

func assertServiceServerFailure(t *testing.T, rec *httptest.ResponseRecorder, ended []sdktrace.ReadOnlySpan, message string, wantStatus int, wantCanceled bool) {
	t.Helper()
	body := rec.Body.String()
	if rec.Code != wantStatus {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, wantStatus, body)
	}
	if strings.Contains(body, "private") {
		t.Fatalf("body leaked the backend error text: %s", body)
	}
	if wantStatus == http.StatusServiceUnavailable {
		if rec.Header().Get("Retry-After") == "" {
			t.Fatal("reader fence 503 is missing Retry-After")
		}
	} else if !strings.Contains(body, message) {
		t.Fatalf("body = %s, want the fixed %q", body, message)
	}
	if len(ended) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(ended))
	}
	wantError := wantStatus == http.StatusInternalServerError
	hasException, hasCanceled := false, false
	for _, event := range ended[0].Events() {
		hasException = hasException || event.Name == "exception"
		hasCanceled = hasCanceled || event.Name == tracing.ClientCanceledEvent
	}
	status := ended[0].Status()
	if (status.Code == codes.Error) != wantError || hasException != wantError {
		t.Fatalf("span status = %v (%q), exception recorded = %v; want error recorded = %v",
			status.Code, status.Description, hasException, wantError)
	}
	if wantError && status.Description != message {
		t.Fatalf("span status description = %q, want the fixed %q", status.Description, message)
	}
	if hasCanceled != wantCanceled {
		t.Fatalf("%s event recorded = %v, want %v", tracing.ClientCanceledEvent, hasCanceled, wantCanceled)
	}
}
