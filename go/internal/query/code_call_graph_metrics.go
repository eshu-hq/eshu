package query

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	callGraphMetricsCapability   = "call_graph.metrics"
	callGraphMetricsDefaultLimit = 25
	callGraphMetricsMaxLimit     = 200
	callGraphMetricsMaxOffset    = 10000
)

var errCallGraphMetricsUnavailable = errors.New("call graph metrics are unavailable")

type callGraphMetricsRequest struct {
	MetricType string `json:"metric_type"`
	RepoID     string `json:"repo_id"`
	Language   string `json:"language"`
	Limit      *int   `json:"limit"`
	Offset     int    `json:"offset"`
}

func (h *CodeHandler) handleCallGraphMetrics(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryCallGraphMetrics,
		"POST /api/v0/code/call-graph/metrics",
		callGraphMetricsCapability,
	)
	defer span.End()

	var req callGraphMetricsRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if capabilityUnsupported(h.profile(), callGraphMetricsCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"call graph metrics require a supported query profile",
			ErrorCodeUnsupportedCapability,
			callGraphMetricsCapability,
			h.profile(),
			requiredProfile(callGraphMetricsCapability),
		)
		return
	}
	if err := req.validate(); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !h.applyRepositorySelector(w, r, &req.RepoID) {
		return
	}

	data, err := h.callGraphMetricsData(r.Context(), req)
	if err != nil {
		if errors.Is(err, errCallGraphMetricsUnavailable) {
			WriteError(w, http.StatusServiceUnavailable, err.Error())
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

func (r callGraphMetricsRequest) validate() error {
	if strings.TrimSpace(r.RepoID) == "" {
		return fmt.Errorf("repo_id is required")
	}
	if _, ok := callGraphMetricTypes()[r.metricType()]; !ok {
		return fmt.Errorf("metric_type must be one of: %s", strings.Join(callGraphMetricTypeNames(), ", "))
	}
	if r.Offset < 0 {
		return fmt.Errorf("offset must be >= 0")
	}
	if r.Offset > callGraphMetricsMaxOffset {
		return fmt.Errorf("offset must be <= 10000")
	}
	if r.Limit == nil {
		return nil
	}
	if *r.Limit > callGraphMetricsMaxLimit {
		return fmt.Errorf("limit must be <= 200")
	}
	if *r.Limit < 1 {
		return fmt.Errorf("limit must be >= 1")
	}
	return nil
}

func (r callGraphMetricsRequest) metricType() string {
	metricType := strings.ToLower(strings.TrimSpace(r.MetricType))
	if metricType == "" {
		return "hub_functions"
	}
	return metricType
}

func (r callGraphMetricsRequest) normalizedLanguage() string {
	return strings.ToLower(strings.TrimSpace(r.Language))
}

func (r callGraphMetricsRequest) normalizedLimit() int {
	if r.Limit == nil {
		return callGraphMetricsDefaultLimit
	}
	switch {
	case *r.Limit > callGraphMetricsMaxLimit:
		return callGraphMetricsMaxLimit
	default:
		return *r.Limit
	}
}

func (r callGraphMetricsRequest) queryLimit() int {
	return r.normalizedLimit() + 1
}

func callGraphMetricTypes() map[string]struct{} {
	return map[string]struct{}{
		"hub_functions":       {},
		"recursive_functions": {},
	}
}

func callGraphMetricTypeNames() []string {
	return []string{"hub_functions", "recursive_functions"}
}

func (h *CodeHandler) callGraphMetricsData(ctx context.Context, req callGraphMetricsRequest) (map[string]any, error) {
	if h == nil || h.Neo4j == nil {
		return nil, errCallGraphMetricsUnavailable
	}
	cypher, params := callGraphMetricsCypher(req)
	rows, err := h.Neo4j.Run(ctx, cypher, params)
	if err != nil {
		return nil, err
	}
	return callGraphMetricsResponse(req, rows), nil
}

func callGraphMetricsCypher(req callGraphMetricsRequest) (string, map[string]any) {
	params := map[string]any{
		"repo_id": strings.TrimSpace(req.RepoID),
		"limit":   req.queryLimit(),
		"offset":  req.Offset,
	}
	if language := req.normalizedLanguage(); language != "" {
		params["language"] = language
	}
	if req.metricType() == "recursive_functions" {
		return recursiveFunctionsCypher(req), params
	}
	return hubFunctionsCypher(req), params
}

func hubFunctionsCypher(req callGraphMetricsRequest) string {
	var cypher strings.Builder
	cypher.WriteString(`MATCH (repo:Repository {id: $repo_id})-[:REPO_CONTAINS]->(source_file:File)-[:CONTAINS]->(fn:Function)
`)
	if req.normalizedLanguage() != "" {
		cypher.WriteString("WHERE coalesce(fn.language, source_file.language) = $language\n")
	}
	cypher.WriteString(`OPTIONAL MATCH (repo)-[:REPO_CONTAINS]->(:File)-[:CONTAINS]->(caller:Function)-[:CALLS]->(fn)
WITH repo, source_file, fn, count(DISTINCT caller) AS incoming_calls
OPTIONAL MATCH (repo)-[:REPO_CONTAINS]->(:File)-[:CONTAINS]->(callee:Function)<-[:CALLS]-(fn)
WITH repo, source_file, fn, incoming_calls, count(DISTINCT callee) AS outgoing_calls
WITH repo, source_file, fn, incoming_calls, outgoing_calls, incoming_calls + outgoing_calls AS total_degree
WHERE total_degree > 0
RETURN repo.id as repo_id,
       source_file.relative_path as file_path,
       coalesce(fn.language, source_file.language) as language,
       coalesce(fn.id, fn.uid) as function_id,
       fn.name as function_name,
       fn.start_line as start_line,
       fn.end_line as end_line,
       incoming_calls as incoming_calls,
       outgoing_calls as outgoing_calls,
       total_degree as total_degree
ORDER BY total_degree DESC, incoming_calls DESC, outgoing_calls DESC, source_file.relative_path, fn.start_line, fn.name
SKIP $offset
LIMIT $limit`)
	return cypher.String()
}

func recursiveFunctionsCypher(req callGraphMetricsRequest) string {
	var cypher strings.Builder
	cypher.WriteString(`MATCH (repo:Repository {id: $repo_id})-[:REPO_CONTAINS]->(source_file:File)-[:CONTAINS]->(fn:Function)
MATCH (fn)-[:CALLS]->(partner:Function)
MATCH (partner)-[:CALLS]->(fn)
MATCH (repo)-[:REPO_CONTAINS]->(partner_file:File)-[:CONTAINS]->(partner)
WITH repo, source_file, fn, partner_file, partner,
     coalesce(fn.id, fn.uid, fn.name) AS source_key,
     coalesce(partner.id, partner.uid, partner.name) AS partner_key
WHERE source_key <= partner_key`)
	if req.normalizedLanguage() != "" {
		cypher.WriteString("\n  AND coalesce(fn.language, source_file.language) = $language")
		cypher.WriteString("\n  AND coalesce(partner.language, partner_file.language) = $language")
	}
	cypher.WriteString(`
RETURN repo.id as repo_id,
       source_file.relative_path as file_path,
       coalesce(fn.language, source_file.language) as language,
       coalesce(fn.id, fn.uid) as function_id,
       fn.name as function_name,
       fn.start_line as start_line,
       fn.end_line as end_line,
       partner_file.relative_path as partner_file,
       coalesce(partner.id, partner.uid) as partner_id,
       partner.name as partner_name,
       partner.start_line as partner_start_line,
       partner.end_line as partner_end_line
ORDER BY source_file.relative_path, fn.start_line, fn.name, partner_file.relative_path, partner.start_line, partner.name
SKIP $offset
LIMIT $limit`)
	return cypher.String()
}
