// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil/failure"
)

// infraGraphFails builds an InfraHandler whose graph fails every read with
// err.
func infraGraphFails(err error) failure.Mounter {
	return &InfraHandler{Neo4j: failingRunSingleGraph{err: err}}
}

// TestInfraSpannedRoutesAnswerFixedServerFailures is the #7674 slice 10
// regression for the infra, graph-summary, aggregate, and cloud resource reads
// that open a handler span. Each graph or store failure answered 500 with the
// backend error text (err.Error()), or, on the cloud resource page selection,
// answered a stale PostgreSQL reader with a 500 instead of the retryable 503;
// and every one answered a client cancel as a server fault. Not parallel: it
// swaps queryHandlerTracer.
func TestInfraSpannedRoutesAnswerFixedServerFailures(t *testing.T) {
	const cloudResourcesPath = "/api/v0/cloud/resources"
	routes := []statusFailureRoute{
		{
			route: failure.Route{
				Name: "infra resource search", Method: http.MethodPost, Path: "/api/v0/infra/resources/search",
				Body: `{"query":"api"}`, Message: infraResourceSearchFailedMessage, Handler: infraGraphFails,
			},
			capability: "platform_impact.deployment_chain",
		},
		{
			route: failure.Route{
				Name: "infra relationships", Method: http.MethodPost, Path: "/api/v0/infra/relationships",
				Body: `{"entity_id":"entity-1"}`, Message: infraRelationshipsReadFailedMessage, Handler: infraGraphFails,
			},
			capability: "platform_impact.deployment_chain",
		},
		{
			route: failure.Route{
				Name: "graph summary ecosystem", Method: http.MethodPost, Path: "/api/v0/ecosystem/graph-summary",
				Body: `{}`, Message: graphSummaryEcosystemReadFailedMessage, Handler: infraGraphFails,
			},
			capability: graphSummaryPacketCapability,
		},
		{
			route: failure.Route{
				Name: "graph summary repository", Method: http.MethodPost, Path: "/api/v0/ecosystem/graph-summary",
				Body: `{"repo_id":"repo-1"}`, Message: graphSummaryRepositoryReadFailedMessage, Handler: infraGraphFails,
			},
			capability: graphSummaryPacketCapability,
		},
		{
			route: failure.Route{
				Name: "infra resource count", Path: "/api/v0/infra/resources/count",
				Message: infraResourceCountFailedMessage,
				Handler: func(err error) failure.Mounter {
					return &InfraHandler{Aggregates: failingInfraAggregateStore{err: err}}
				},
			},
			capability: infraResourceAggregateCapability,
		},
		{
			route: failure.Route{
				Name: "infra resource inventory", Path: "/api/v0/infra/resources/inventory",
				Message: infraResourceInventoryFailedMessage,
				Handler: func(err error) failure.Mounter {
					return &InfraHandler{Aggregates: failingInfraAggregateStore{err: err}}
				},
			},
			capability: infraResourceAggregateCapability,
		},
		{
			route: failure.Route{
				Name: "cloud resource page selection", Path: cloudResourcesPath,
				Message: cloudResourcePageSelectionFailedMessage,
				Handler: func(err error) failure.Mounter {
					return &InfraHandler{Neo4j: failingRunSingleGraph{}, CloudResources: &stubCloudResourceListStore{err: err}}
				},
			},
			capability: cloudResourceListCapability,
		},
		{
			route: failure.Route{
				Name: "cloud resource graph hydration", Path: cloudResourcesPath,
				Message: cloudResourceGraphHydrationFailedMessage,
				Handler: func(err error) failure.Mounter {
					return &InfraHandler{Neo4j: failingRunSingleGraph{err: err}, CloudResources: oneCloudResourceIdentity()}
				},
			},
			capability: cloudResourceListCapability,
		},
	}
	suite := failure.Suite{HandlerTracer: &queryHandlerTracer}
	for _, r := range routes {
		suite.Routes = append(suite.Routes, r.route)
	}
	failure.Run(t, suite)
	for _, r := range routes {
		assertFenceCapability(t, suite, r.route, r.capability)
	}
}

// TestInfraUnspannedRoutesAnswerFixedServerFailures covers the infra-family
// graph reads that open no handler span of their own, so the failure is read
// off the request span: the ecosystem overview, the relationships catalog,
// and the relationship edge slice. Each answered 500 with the graph error
// text.
func TestInfraUnspannedRoutesAnswerFixedServerFailures(t *testing.T) {
	routes := []statusFailureRoute{
		{
			route: failure.Route{
				Name: "ecosystem overview", Path: "/api/v0/ecosystem/overview",
				Message: ecosystemOverviewReadFailedMessage, Handler: infraGraphFails,
			},
			capability: "platform_impact.context_overview",
		},
		{
			route: failure.Route{
				Name: "relationships catalog", Method: http.MethodPost, Path: "/api/v0/relationships/catalog",
				Body: `{}`, Message: relationshipsCatalogReadFailedMessage, Handler: infraGraphFails,
			},
			capability: relationshipsCatalogCapability,
		},
		{
			route: failure.Route{
				Name: "relationship edges", Method: http.MethodPost, Path: "/api/v0/relationships/edges",
				Body: `{"verb":"DEPLOYS_FROM"}`, Message: relationshipEdgesReadFailedMessage, Handler: infraGraphFails,
			},
			capability: relationshipsCatalogCapability,
		},
	}
	suite := failure.Suite{}
	for _, r := range routes {
		suite.Routes = append(suite.Routes, r.route)
	}
	failure.Run(t, suite)
	for _, r := range routes {
		assertFenceCapability(t, suite, r.route, r.capability)
	}
}

// TestCloudResourceProjectionMismatchAnswersFixedServerFailure covers the one
// cloud resource failure step that reads nothing: the graph answers a row the
// owner ledger did not select. The step had fixed text already; it now also
// records the mismatch on the handler span. Not parallel: it swaps
// queryHandlerTracer.
func TestCloudResourceProjectionMismatchAnswersFixedServerFailure(t *testing.T) {
	route := failure.Route{
		Name: "cloud resource projection", Path: "/api/v0/cloud/resources",
		Message: cloudResourceGraphProjectionFailedMessage,
	}
	suite := failure.Suite{HandlerTracer: &queryHandlerTracer}
	handler := &InfraHandler{
		Neo4j:          &fakeTagHistoryGraphReader{rows: []map[string]any{{"uid": "not-selected"}}},
		CloudResources: oneCloudResourceIdentity(),
	}
	rec, spans := failure.Serve(suite, route, handler, false)
	if rec.Code != http.StatusInternalServerError || failure.Message(rec.Body.Bytes()) != cloudResourceGraphProjectionFailedMessage {
		t.Fatalf("status = %d, body = %s; want 500 %q", rec.Code, rec.Body.String(), cloudResourceGraphProjectionFailedMessage)
	}
	assertOneSpanException(t, spans, cloudResourceGraphProjectionFailedMessage)
}

// failingInfraAggregateStore fails both aggregate reads with err.
type failingInfraAggregateStore struct{ err error }

func (s failingInfraAggregateStore) CountInfraResources(
	context.Context, InfraResourceAggregateFilter,
) (InfraResourceAggregateCount, error) {
	return InfraResourceAggregateCount{}, s.err
}

func (s failingInfraAggregateStore) InfraResourceInventory(
	context.Context, InfraResourceAggregateFilter, InfraResourceInventoryDimension, int, int,
) ([]InfraResourceInventoryRow, InfraResourceAggregateSource, error) {
	return nil, "", s.err
}

// oneCloudResourceIdentity selects one owner-ledger identity, so the route
// goes on to the graph hydration read.
func oneCloudResourceIdentity() *stubCloudResourceListStore {
	return &stubCloudResourceListStore{rows: []CloudResourceListIdentity{{UID: "uid-1", ResourceType: "aws_s3_bucket"}}}
}
