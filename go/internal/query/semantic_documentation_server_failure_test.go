// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"testing"

	"go.opentelemetry.io/otel/codes"

	"github.com/eshu-hq/eshu/go/internal/query/testutil/failure"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// semanticDocumentationFailureRoutes lists every route #7674 slice 14 moved
// onto the reader fence and the server-failure helper: the two semantic
// evidence lists and the six documentation reads. Each store reads through
// the guarded PostgreSQL reader (the content reader and the documentation
// finding aggregate store are both built on it in cmd/api and
// cmd/mcp-server), so each route answers the fence 503 first. Before the
// slice each one wrote a fixed 500 through its own error writer with no
// fence and no cancel handling.
func semanticDocumentationFailureRoutes() []statusFailureRoute {
	semantic := func(name, path, capability string) statusFailureRoute {
		return statusFailureRoute{
			route: failure.Route{
				Name: name, Path: path, Message: semanticEvidenceReadFailedMessage,
				Handler: func(err error) failure.Mounter {
					return &SemanticEvidenceHandler{Content: failingSemanticEvidenceStore{err: err}}
				},
			},
			capability: capability,
		}
	}
	content := func(name, path, capability string) statusFailureRoute {
		return statusFailureRoute{
			route: failure.Route{
				Name: name, Path: path, Message: documentationReadFailedMessage,
				Handler: func(err error) failure.Mounter {
					return &DocumentationHandler{Content: failingDocumentationContentStore{err: err}}
				},
			},
			capability: capability,
		}
	}
	aggregate := func(name, path string) statusFailureRoute {
		return statusFailureRoute{
			route: failure.Route{
				Name: name, Path: path, Message: documentationReadFailedMessage,
				Handler: func(err error) failure.Mounter {
					return &DocumentationHandler{Aggregates: failingDocumentationAggregateStore{err: err}}
				},
			},
			capability: documentationFindingAggregateCapability,
		}
	}
	return []statusFailureRoute{
		semantic("semantic documentation observations",
			"/api/v0/semantic/documentation-observations?scope_id=scope-1", semanticDocumentationObservationsCapability),
		semantic("semantic code hints",
			"/api/v0/semantic/code-hints?scope_id=scope-1", semanticCodeHintsCapability),
		content("documentation findings", "/api/v0/documentation/findings", documentationFindingsCapability),
		content("documentation facts", "/api/v0/documentation/facts?scope_id=scope-1", documentationFactsCapability),
		content("documentation evidence packet",
			"/api/v0/documentation/findings/finding-1/evidence-packet", documentationEvidencePacketCapability),
		content("documentation packet freshness",
			"/api/v0/documentation/evidence-packets/packet-1/freshness", documentationPacketFreshnessCapability),
		aggregate("documentation findings count", "/api/v0/documentation/findings/count"),
		aggregate("documentation findings inventory", "/api/v0/documentation/findings/inventory"),
	}
}

// TestSemanticAndDocumentationRoutesAnswerFixedServerFailures is the #7674
// slice 14 regression. Each route answered a stale or timed-out reader with
// a 500 instead of the retryable 503, and answered a client cancel as a
// server fault with nothing on the handler span. Every route opens a handler
// span through queryHandlerTracer. Not parallel: it swaps queryHandlerTracer.
func TestSemanticAndDocumentationRoutesAnswerFixedServerFailures(t *testing.T) {
	routes := semanticDocumentationFailureRoutes()
	suite := failure.Suite{HandlerTracer: &queryHandlerTracer}
	for _, r := range routes {
		suite.Routes = append(suite.Routes, r.route)
	}
	failure.Run(t, suite)
	for _, r := range routes {
		assertFenceCapability(t, suite, r.route, r.capability)
	}
}

// failingSemanticEvidenceStore fails the semantic evidence list with err,
// unconditionally, so a client cancel still reaches the failure step.
type failingSemanticEvidenceStore struct{ err error }

func (s failingSemanticEvidenceStore) SemanticEvidence(
	context.Context, semanticEvidenceFilter,
) (semanticEvidenceListReadModel, error) {
	return semanticEvidenceListReadModel{}, s.err
}

// failingDocumentationContentStore fails the four documentation content reads
// the documentation routes make with err.
type failingDocumentationContentStore struct {
	fakePortContentStore
	err error
}

func (s failingDocumentationContentStore) DocumentationFindings(
	context.Context, documentationFindingFilter,
) (documentationFindingListReadModel, error) {
	return documentationFindingListReadModel{}, s.err
}

func (s failingDocumentationContentStore) DocumentationFacts(
	context.Context, documentationFactFilter,
) (documentationFactListReadModel, error) {
	return documentationFactListReadModel{}, s.err
}

func (s failingDocumentationContentStore) DocumentationEvidencePacketWithFilter(
	context.Context, documentationEvidencePacketFilter,
) (documentationEvidencePacketReadModel, error) {
	return documentationEvidencePacketReadModel{}, s.err
}

func (s failingDocumentationContentStore) DocumentationEvidencePacketFreshnessWithFilter(
	context.Context, documentationEvidencePacketFreshnessFilter,
) (documentationEvidencePacketFreshnessReadModel, error) {
	return documentationEvidencePacketFreshnessReadModel{}, s.err
}

// failingDocumentationAggregateStore fails both documentation finding
// aggregate reads with err.
type failingDocumentationAggregateStore struct{ err error }

func (s failingDocumentationAggregateStore) CountDocumentationFindings(
	context.Context, DocumentationFindingAggregateFilter,
) (DocumentationFindingAggregateCount, error) {
	return DocumentationFindingAggregateCount{}, s.err
}

func (s failingDocumentationAggregateStore) DocumentationFindingInventory(
	context.Context, DocumentationFindingAggregateFilter, DocumentationFindingInventoryDimension, int, int,
) ([]DocumentationFindingInventoryRow, error) {
	return nil, s.err
}

// TestDocumentationAggregatesUnconfiguredLeaveTheSpanUnmarked pins that the
// configuration 503 the count and inventory routes answer with no aggregate
// store is a plain write: the handler span stays Unset with no exception and
// no client-cancel event, unlike the server-failure helper's 500. The
// telemetry traces page states this. Not parallel: it swaps
// queryHandlerTracer.
func TestDocumentationAggregatesUnconfiguredLeaveTheSpanUnmarked(t *testing.T) {
	suite := failure.Suite{HandlerTracer: &queryHandlerTracer}
	for _, path := range []string{"/api/v0/documentation/findings/count", "/api/v0/documentation/findings/inventory"} {
		route := failure.Route{
			Name: path, Path: path,
			Handler: func(error) failure.Mounter { return &DocumentationHandler{} },
		}
		rec, ended := failure.Serve(suite, route, route.Handler(nil), false)
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s: status = %d, want 503", path, rec.Code)
		}
		var handlerSpans int
		for _, span := range ended {
			if span.Name() != telemetry.SpanQueryDocumentationAggregate {
				continue
			}
			handlerSpans++
			if span.Status().Code != codes.Unset {
				t.Errorf("%s: span status = %v, want Unset", path, span.Status().Code)
			}
			if events := len(span.Events()); events != 0 {
				t.Errorf("%s: span events = %d, want 0", path, events)
			}
		}
		if handlerSpans != 1 {
			t.Errorf("%s: handler spans = %d, want 1", path, handlerSpans)
		}
	}
}
