// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const (
	callGraphMetricsCapability    = "call_graph.metrics"
	CallGraphMetricsEdgeScanLimit = 50000
	// callGraphMetricsEdgeScanLimit is the pre-move spelling of
	// CallGraphMetricsEdgeScanLimit. The queryplan-pinned
	// CallGraphMetricsEdgesCypher body names the bare const, and its
	// source_sha256 digest covers the declaration text, so the alias keeps
	// the moved body byte-identical instead of re-freezing the digest.
	callGraphMetricsEdgeScanLimit = CallGraphMetricsEdgeScanLimit
)

// callGraphMetricsRequest is the pre-move spelling of
// codemodel.CallGraphMetricsRequest, kept for the digest-pinned
// CallGraphMetricsData body below.
type callGraphMetricsRequest = codemodel.CallGraphMetricsRequest

// callGraphMetricsResponse forwards to codemodel.CallGraphMetricsResponse so
// CallGraphMetricsData keeps its pre-move declaration bytes (Go has no
// function aliases).
func callGraphMetricsResponse(req callGraphMetricsRequest, rows []map[string]any) map[string]any {
	return codemodel.CallGraphMetricsResponse(req, rows)
}

// callGraphMetricsEdgesCypher forwards to CallGraphMetricsEdgesCypher so
// CallGraphMetricsData keeps its pre-move declaration bytes.
func callGraphMetricsEdgesCypher(repoID string) (string, map[string]any) {
	return CallGraphMetricsEdgesCypher(repoID)
}

// callGraphMetricsRowsWithStats forwards to
// codemodel.CallGraphMetricsRowsWithStats for the same digest-pinned body.
func callGraphMetricsRowsWithStats(req callGraphMetricsRequest, edgeRows []map[string]any) ([]map[string]any, codemodel.CallGraphMetricsStats) {
	return codemodel.CallGraphMetricsRowsWithStats(req, edgeRows)
}

var (
	errCallGraphMetricsScopeTooBroad = errors.New("call graph metrics scope exceeds internal edge scan limit")
	errCallGraphMetricsUnavailable   = errors.New("call graph metrics are unavailable")
)

// codemodel.CallGraphMetricsRequest aliases the leaf-owned request type so the staying
// handler and data reader keep their signatures; see family_code_shim.go.

func (h *CodeHandler) handleCallGraphMetrics(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryCallGraphMetrics,
		"POST /api/v0/code/call-graph/metrics",
		callGraphMetricsCapability,
	)
	defer span.End()

	var req codemodel.CallGraphMetricsRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if querycontract.CapabilityUnsupported(h.profile(), callGraphMetricsCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"call graph metrics require a supported query profile",
			ErrorCodeUnsupportedCapability,
			callGraphMetricsCapability,
			h.profile(),
			querycontract.RequiredProfile(callGraphMetricsCapability),
		)
		return
	}
	if err := req.Validate(); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !h.applyRepositorySelectorForCapability(w, r, &req.RepoID, callGraphMetricsCapability) {
		return
	}

	data, err := h.CallGraphMetricsData(r.Context(), req)
	if err != nil {
		span.RecordError(err)
		if errors.Is(err, errCallGraphMetricsUnavailable) {
			WriteError(w, http.StatusServiceUnavailable, err.Error())
			return
		}
		if errors.Is(err, errCallGraphMetricsScopeTooBroad) {
			WriteError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		if WriteGraphReadError(w, r, err, callGraphMetricsCapability) {
			return
		}
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	WriteSuccess(
		w,
		r,
		http.StatusOK,
		data,
		BuildTruthEnvelope(h.profile(), callGraphMetricsCapability, TruthBasisAuthoritativeGraph, "resolved from bounded call graph metrics lookup"),
	)
}

// The codemodel.CallGraphMetricsRequest type, its methods, and the metric-type helpers
// moved to codemodel/code_call_graph_metrics_aggregation.go (#6060 lane A
// L1); the request validation and accessors run there now.

// CallGraphMetricsData resolves the bounded call-graph hub/recursive metrics
// read for req against the graph, applying the repository access filter from
// ctx before any Cypher runs. It is exported so route-level tests can prove
// the auth-grant short-circuit that the registered HTTP route hides.
//
// It enforces the grant itself rather than relying on its caller. The HTTP
// route rejects an ungranted repo_id before reaching here, but an exported
// method is callable without that route, so both the grantless case and the
// granted-but-not-this-repository case are refused below.
func (h *CodeHandler) CallGraphMetricsData(ctx context.Context, req callGraphMetricsRequest) (map[string]any, error) {
	if h == nil || h.Neo4j == nil {
		return nil, errCallGraphMetricsUnavailable
	}
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.String("eshu.query.call_graph.metric_type", req.EffectiveMetricType()),
		attribute.Int("eshu.query.call_graph.edge_scan_limit", callGraphMetricsEdgeScanLimit),
	)
	// #5167 code family: this route is grant-bound by its mandatory repo_id.
	// applyRepositorySelectorForCapability resolves that selector against the
	// caller's grant and rejects an ungranted one with 400 before the handler
	// body runs. That guard is the HTTP route's, though, and exporting this
	// method (#6060) put callers on the other side of it, so the bounds are
	// re-checked here rather than assumed. Two cases must never reach the
	// graph: a grantless scoped caller, and a scoped caller asking for a
	// repository its grant does not include. Both answer as the empty read
	// does, so an ungranted repository is indistinguishable from one with no
	// metrics -- the caller learns nothing about a repository it cannot see.
	//
	// It binds codeGrantAccessFilter, the grant the selector resolution above
	// already used, rather than the raw context filter. A token granted only a
	// git ingestion scope carries no canonical repository id, so the raw filter
	// refuses the very repo_id the selector just resolved for it and the caller
	// reads an empty page from a repository it holds -- the scope-versus-
	// canonical mismatch #5052 fixed and codeGrantAccessFilter exists to keep
	// fixed. TestCallGraphMetricsResolvesAScopeOnlyGrantToItsRepository pins it.
	access := codeGrantAccessFilter(ctx)
	if access.Empty() {
		return callGraphMetricsResponse(req, nil), nil
	}
	if access.Scoped() && !access.AllowsRepositoryID(req.RepoID) {
		return callGraphMetricsResponse(req, nil), nil
	}
	cypher, params := callGraphMetricsEdgesCypher(req.RepoID)
	edges, err := h.Neo4j.Run(ctx, cypher, params)
	if err != nil {
		return nil, err
	}
	scanOverflow := len(edges) > callGraphMetricsEdgeScanLimit
	span.SetAttributes(
		attribute.Int("eshu.query.call_graph.expanded_edge_count", len(edges)),
		attribute.Bool("eshu.query.call_graph.scan_overflow", scanOverflow),
	)
	if scanOverflow {
		return nil, fmt.Errorf(
			"%w: reached the %d-edge sentinel; maximum exact scope is %d",
			errCallGraphMetricsScopeTooBroad,
			len(edges),
			callGraphMetricsEdgeScanLimit,
		)
	}
	rows, stats := callGraphMetricsRowsWithStats(req, edges)
	data := callGraphMetricsResponse(req, rows)
	span.SetAttributes(
		attribute.Int("eshu.query.call_graph.expanded_node_count", stats.ExpandedNodes),
		attribute.Int("eshu.query.call_graph.result_count", IntVal(data, "count")),
		attribute.Bool("eshu.query.call_graph.truncated", BoolVal(data, "truncated")),
	)
	return data, nil
}

// CallGraphMetricsEdgesCypher builds the single indexed edge pass behind the
// hub-function and recursive-function metrics, and behind the graph-summary
// packet's hot-entity ranking.
//
// Every caller runs this exact text. Both of its routes are bound to the
// caller's grant before the read -- call-graph metrics by its mandatory,
// selector-resolved repo_id, graph-summary by the not-found it answers for an
// out-of-grant repo_id -- so a grant predicate here would be redundant by
// construction and would give this hot read a second shape with no plan behind
// it. One text is what keeps the queryplan manifest's cypher_sha256 for
// QP-CALL-GRAPH-HUBS and QP-CALL-GRAPH-RECURSIVE, plan claim included,
// describing what production emits.
func CallGraphMetricsEdgesCypher(repoID string) (string, map[string]any) {
	return `MATCH (source:Function {repo_id: $repo_id})-[call:CALLS]->(target:Function {repo_id: $repo_id})
RETURN source.uid AS source_uid,
       coalesce(source.id, source.uid) AS source_id,
       source.relative_path AS source_path,
       source.language AS source_language,
       source.name AS source_name,
       source.start_line AS source_start_line,
       source.end_line AS source_end_line,
       target.uid AS target_uid,
       coalesce(target.id, target.uid) AS target_id,
       target.relative_path AS target_path,
       target.language AS target_language,
       target.name AS target_name,
       target.start_line AS target_start_line,
       target.end_line AS target_end_line
LIMIT $edge_scan_limit`, map[string]any{
			"edge_scan_limit": callGraphMetricsEdgeScanLimit + 1,
			"repo_id":         strings.TrimSpace(repoID),
		}
}
