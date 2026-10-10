// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"errors"
	"net/http"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/query/tracing"
)

// writeCodeReadFailure answers a failed code read that the route's own 503
// verdicts and 4xx sentinels did not claim. A stale or timed-out PostgreSQL
// reader, a graph outage, or a graph deadline gets its shared verdict from
// WriteGraphReadError (503 with Retry-After, or 504). Anything else answers
// message through tracing.WriteServerFailure: 500 with err recorded on the
// request span, or 499 with only the client-cancel event when the caller
// canceled the request.
func writeCodeReadFailure(w http.ResponseWriter, r *http.Request, err error, capability, message string) {
	if WriteGraphReadError(w, r, err, capability) {
		return
	}
	tracing.WriteServerFailure(w, r, err, http.StatusInternalServerError, message)
}

// writeCodeUnavailable answers a code read that failed because a backend the
// route needs is not configured or not ready, and reports whether err wraps
// sentinel. A graph or reader verdict from WriteGraphReadError still wins, and
// a client cancel answers 499. Otherwise the answer is 503 with sentinel's own
// fixed text, never the wrapped err, and no Retry-After: these sentinels name
// a configuration state, not a transient fault. err is recorded on the
// request span with Error status, as tracing.WriteServerFailure does for a
// 500; that helper cannot answer 503. It writes nothing and reports false when
// err does not wrap sentinel.
func writeCodeUnavailable(w http.ResponseWriter, r *http.Request, err, sentinel error, capability string) bool {
	if !errors.Is(err, sentinel) {
		return false
	}
	if WriteGraphReadError(w, r, err, capability) {
		return true
	}
	message := sentinel.Error()
	if tracing.ClientCanceled(r.Context(), err) {
		tracing.WriteServerFailure(w, r, err, http.StatusInternalServerError, message)
		return true
	}
	span := trace.SpanFromContext(r.Context())
	span.RecordError(err)
	span.SetStatus(codes.Error, message)
	WriteError(w, http.StatusServiceUnavailable, message)
	return true
}

// Fixed client-facing messages for code-family read failures (#7674). Each
// route step that answers a server failure has its own constant, and the
// response body is exactly that text: a backend error quotes SQL, Cypher,
// hosts, and credentials, so err.Error() never reaches a 4xx or 5xx body.
// The cause is recorded on the request span instead.
const (
	codeSearchContentIndexFailedMessage = "code search failed to read the content entity-name index"
	codeSearchGraphFailedMessage        = "code search failed to read the graph"
	codeSearchContentFailedMessage      = "code search failed to read the content store"

	symbolSearchFailedMessage        = "symbol search failed to read its backend"
	structuralInventoryFailedMessage = "structural inventory read failed"
	codeTopicFailedMessage           = "code topic investigation read failed"
	hardcodedSecretFailedMessage     = "hardcoded secret investigation read failed"
	importDependencyFailedMessage    = "import dependency investigation read failed"
	callGraphMetricsFailedMessage    = "call graph metrics read failed"
	codeFlowFailedMessage            = "code-flow read failed"
	codeQualityFailedMessage         = "code quality inspection read failed"

	relationshipTargetFailedMessage               = "relationship target resolution failed"
	relationshipGraphFailedMessage                = "relationship graph read failed"
	relationshipContentFallbackFailedMessage      = "relationship content fallback read failed"
	relationshipRepositoryFailedMessage           = "relationship repository identity read failed"
	relationshipMetadataFailedMessage             = "relationship content metadata read failed"
	transitiveRelationshipGraphFailedMessage      = "transitive relationship graph read failed"
	transitiveRelationshipRepositoryFailedMessage = "transitive relationship repository identity read failed"
	transitiveRelationshipMetadataFailedMessage   = "transitive relationship content metadata read failed"

	relationshipStoryTargetFailedMessage    = "relationship story target resolution failed"
	relationshipStoryFailedMessage          = "relationship story read failed"
	relationshipStoryHierarchyFailedMessage = "relationship story class hierarchy read failed"
	relationshipStoryOverrideFailedMessage  = "relationship story override read failed"

	complexityRankingFailedMessage  = "complexity ranking read failed"
	complexityLookupFailedMessage   = "complexity lookup read failed"
	complexityMetadataFailedMessage = "complexity content metadata read failed"

	callChainEndpointFailedMessage    = "call chain endpoint resolution failed"
	callChainGraphFailedMessage       = "call chain graph read failed"
	comparePathsEndpointFailedMessage = "code path comparison endpoint resolution failed"
	comparePathsGraphFailedMessage    = "code path comparison graph read failed"

	routeToCallerRouteFailedMessage        = "route-to-caller route read failed"
	routeToCallerRelationshipFailedMessage = "route-to-caller relationship read failed"
	routeToCallerImpactFailedMessage       = "route-to-caller impact read failed"

	divergenceFindingsFailedMessage      = "divergence findings read failed"
	divergenceReportFailedMessage        = "divergence report read failed"
	divergenceWrapperBypassFailedMessage = "divergence wrapper-bypass investigation read failed"
	divergenceOutlierFailedMessage       = "divergence convention-outlier investigation read failed"
	divergenceDriftedFailedMessage       = "divergence drifted finding read failed"
	divergenceMembersFailedMessage       = "divergence finding members read failed"

	readOnlyCypherFailedMessage     = "read-only Cypher query failed"
	visualizationQueryFailedMessage = "visualization graph query failed"
	bundleSearchFailedMessage       = "registry bundle search read failed"
	bundleVersionCountFailedMessage = "registry bundle version count read failed"
)
