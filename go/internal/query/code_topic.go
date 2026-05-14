package query

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	codeTopicCapability   = "code_search.topic_investigation"
	codeTopicDefaultLimit = 25
	codeTopicMaxLimit     = 200
	codeTopicMaxOffset    = 10000
	codeTopicMaxTerms     = 16
)

var (
	errCodeTopicBackendUnavailable = errors.New("code topic investigation backend is unavailable")
	codeTopicWordPattern           = regexp.MustCompile(`[a-z0-9]+`)
)

type codeTopicInvestigationRequest struct {
	Topic    string   `json:"topic"`
	Query    string   `json:"query"`
	Intent   string   `json:"intent"`
	RepoID   string   `json:"repo_id"`
	Language string   `json:"language"`
	Terms    []string `json:"terms"`
	Limit    int      `json:"limit"`
	Offset   int      `json:"offset"`
}

type codeTopicEvidenceRow struct {
	SourceKind   string
	RepoID       string
	RelativePath string
	EntityID     string
	EntityName   string
	EntityType   string
	Language     string
	StartLine    int
	EndLine      int
	MatchedTerms []string
	Score        int
}

type codeTopicContentInvestigator interface {
	investigateCodeTopic(context.Context, codeTopicInvestigationRequest) ([]codeTopicEvidenceRow, error)
}

func (h *CodeHandler) handleTopicInvestigation(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(r, telemetry.SpanQueryCodeTopicInvestigation, "POST /api/v0/code/topics/investigate", codeTopicCapability)
	defer span.End()

	var req codeTopicInvestigationRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if capabilityUnsupported(h.profile(), codeTopicCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"code topic investigation requires a supported query profile",
			ErrorCodeUnsupportedCapability,
			codeTopicCapability,
			h.profile(),
			requiredProfile(codeTopicCapability),
		)
		return
	}
	req.Topic = strings.TrimSpace(req.topic())
	if req.Topic == "" {
		WriteError(w, http.StatusBadRequest, "topic is required")
		return
	}
	if req.Offset < 0 {
		WriteError(w, http.StatusBadRequest, "offset must be >= 0")
		return
	}
	if req.Offset > codeTopicMaxOffset {
		WriteError(w, http.StatusBadRequest, "offset must be <= 10000")
		return
	}
	if !h.applyRepositorySelector(w, r, &req.RepoID) {
		return
	}

	req.Limit = req.normalizedLimit()
	req.Terms = codeTopicSearchTerms(req.Topic, req.Intent, req.Terms)
	results, err := h.codeTopicRows(r.Context(), req)
	if err != nil {
		if errors.Is(err, errCodeTopicBackendUnavailable) {
			WriteError(w, http.StatusServiceUnavailable, err.Error())
			return
		}
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	truncated := len(results) > req.Limit
	if truncated {
		results = results[:req.Limit]
	}
	data := codeTopicResponse(req, results, truncated)
	WriteSuccess(
		w,
		r,
		http.StatusOK,
		data,
		BuildTruthEnvelope(h.profile(), codeTopicCapability, TruthBasisContentIndex, "resolved from bounded content-index topic investigation"),
	)
}

func (h *CodeHandler) codeTopicRows(ctx context.Context, req codeTopicInvestigationRequest) ([]codeTopicEvidenceRow, error) {
	if h == nil || h.Content == nil {
		return nil, errCodeTopicBackendUnavailable
	}
	investigator, ok := h.Content.(codeTopicContentInvestigator)
	if !ok {
		return nil, errCodeTopicBackendUnavailable
	}
	probeReq := req
	probeReq.Limit = req.Limit + 1
	rows, err := investigator.investigateCodeTopic(ctx, probeReq)
	if err != nil {
		return nil, fmt.Errorf("investigate code topic: %w", err)
	}
	return rows, nil
}

func (r codeTopicInvestigationRequest) topic() string {
	if topic := strings.TrimSpace(r.Topic); topic != "" {
		return topic
	}
	return strings.TrimSpace(r.Query)
}

func (r codeTopicInvestigationRequest) normalizedLimit() int {
	switch {
	case r.Limit <= 0:
		return codeTopicDefaultLimit
	case r.Limit > codeTopicMaxLimit:
		return codeTopicMaxLimit
	default:
		return r.Limit
	}
}

func codeTopicResponse(req codeTopicInvestigationRequest, rows []codeTopicEvidenceRow, truncated bool) map[string]any {
	groups := make([]map[string]any, 0, len(rows))
	matchedFiles := make([]map[string]any, 0, len(rows))
	matchedSymbols := make([]map[string]any, 0, len(rows))
	callGraphHandles := make([]map[string]any, 0, len(rows))
	for index, row := range rows {
		group := codeTopicEvidenceGroup(row, index+1)
		groups = append(groups, group)
		matchedFiles = appendMatchedFile(matchedFiles, row)
		if row.EntityID != "" {
			symbol := codeTopicSymbol(row, index+1)
			matchedSymbols = append(matchedSymbols, symbol)
			callGraphHandles = append(callGraphHandles, codeTopicCallGraphHandle(row))
		}
	}
	return map[string]any{
		"topic":                  req.Topic,
		"intent":                 strings.TrimSpace(req.Intent),
		"scope":                  codeTopicScope(req),
		"searched_terms":         req.Terms,
		"matched_files":          matchedFiles,
		"matched_symbols":        matchedSymbols,
		"evidence_groups":        groups,
		"call_graph_handles":     callGraphHandles,
		"recommended_next_calls": codeTopicRecommendedNextCalls(req, rows),
		"count":                  len(rows),
		"limit":                  req.Limit,
		"offset":                 req.Offset,
		"truncated":              truncated,
		"source_backend":         "postgres_content_store",
		"coverage": map[string]any{
			"query_shape":         "content_topic_investigation",
			"searched_terms":      req.Terms,
			"searched_term_count": len(req.Terms),
			"returned_count":      len(rows),
			"limit":               req.Limit,
			"offset":              req.Offset,
			"truncated":           truncated,
			"empty":               len(rows) == 0,
		},
	}
}

func codeTopicEvidenceGroup(row codeTopicEvidenceRow, rank int) map[string]any {
	group := map[string]any{
		"rank":                   rank,
		"source_kind":            row.SourceKind,
		"repo_id":                row.RepoID,
		"relative_path":          row.RelativePath,
		"language":               row.Language,
		"matched_terms":          row.MatchedTerms,
		"score":                  row.Score,
		"source_handle":          codeTopicSourceHandle(row),
		"recommended_next_calls": codeTopicRowNextCalls(row),
	}
	if row.EntityID != "" {
		group["entity_id"] = row.EntityID
		group["entity_name"] = row.EntityName
		group["entity_type"] = row.EntityType
		group["start_line"] = row.StartLine
		group["end_line"] = row.EndLine
	}
	return group
}

func appendMatchedFile(files []map[string]any, row codeTopicEvidenceRow) []map[string]any {
	for _, file := range files {
		if file["repo_id"] == row.RepoID && file["relative_path"] == row.RelativePath {
			return files
		}
	}
	return append(files, map[string]any{
		"repo_id":       row.RepoID,
		"relative_path": row.RelativePath,
		"language":      row.Language,
		"source_handle": codeTopicSourceHandle(row),
	})
}

func codeTopicSymbol(row codeTopicEvidenceRow, rank int) map[string]any {
	return map[string]any{
		"rank":          rank,
		"entity_id":     row.EntityID,
		"entity_name":   row.EntityName,
		"entity_type":   row.EntityType,
		"repo_id":       row.RepoID,
		"relative_path": row.RelativePath,
		"language":      row.Language,
		"start_line":    row.StartLine,
		"end_line":      row.EndLine,
		"source_handle": codeTopicSourceHandle(row),
	}
}

func codeTopicSourceHandle(row codeTopicEvidenceRow) map[string]any {
	return map[string]any{
		"repo_id":       row.RepoID,
		"relative_path": row.RelativePath,
		"start_line":    row.StartLine,
		"end_line":      row.EndLine,
	}
}

func codeTopicCallGraphHandle(row codeTopicEvidenceRow) map[string]any {
	return map[string]any{
		"tool": "get_code_relationship_story",
		"args": map[string]any{
			"entity_id": row.EntityID,
			"repo_id":   row.RepoID,
			"direction": "both",
			"limit":     25,
		},
	}
}

func codeTopicRowNextCalls(row codeTopicEvidenceRow) []map[string]any {
	calls := []map[string]any{
		{
			"tool": "get_file_lines",
			"args": map[string]any{
				"repo_id":       row.RepoID,
				"relative_path": row.RelativePath,
				"start_line":    max(1, row.StartLine-5),
				"end_line":      max(row.EndLine+5, row.StartLine+20),
			},
		},
	}
	if row.EntityID != "" {
		calls = append(calls, codeTopicCallGraphHandle(row))
	}
	return calls
}

func codeTopicRecommendedNextCalls(req codeTopicInvestigationRequest, rows []codeTopicEvidenceRow) []map[string]any {
	if len(rows) == 0 {
		return []map[string]any{
			{
				"tool": "find_symbol",
				"args": map[string]any{
					"symbol":     firstCodeTopicTerm(req.Terms),
					"repo_id":    req.RepoID,
					"match_mode": "fuzzy",
					"limit":      25,
				},
			},
			{
				"tool": "search_file_content",
				"args": map[string]any{
					"query":   firstCodeTopicTerm(req.Terms),
					"repo_id": req.RepoID,
					"limit":   25,
				},
			},
		}
	}
	return codeTopicRowNextCalls(rows[0])
}

func codeTopicScope(req codeTopicInvestigationRequest) map[string]any {
	return map[string]any{
		"repo_id":  req.RepoID,
		"language": strings.TrimSpace(req.Language),
		"limit":    req.Limit,
		"offset":   req.Offset,
	}
}

func firstCodeTopicTerm(terms []string) string {
	if len(terms) == 0 {
		return ""
	}
	return terms[0]
}

func codeTopicSearchTerms(topic, intent string, explicit []string) []string {
	terms := make([]string, 0, codeTopicMaxTerms)
	seen := map[string]struct{}{}
	add := func(term string) {
		term = normalizeCodeTopicTerm(term)
		if term == "" {
			return
		}
		if _, ok := seen[term]; ok {
			return
		}
		seen[term] = struct{}{}
		terms = append(terms, term)
	}
	for _, term := range explicit {
		add(term)
	}
	for _, token := range codeTopicWordPattern.FindAllString(strings.ToLower(topic+" "+intent), -1) {
		add(token)
		for _, synonym := range codeTopicSynonyms(token) {
			add(synonym)
		}
		if len(terms) >= codeTopicMaxTerms {
			break
		}
	}
	if len(terms) > codeTopicMaxTerms {
		return terms[:codeTopicMaxTerms]
	}
	slices.SortStableFunc(terms, func(a, b string) int {
		return strings.Compare(a, b)
	})
	return terms
}

func normalizeCodeTopicTerm(term string) string {
	term = strings.ToLower(strings.TrimSpace(term))
	switch {
	case term == "":
		return ""
	case codeTopicStopWords[term]:
		return ""
	case len(term) < 3:
		return ""
	default:
		return term
	}
}

func codeTopicSynonyms(token string) []string {
	switch token {
	case "authentication", "authenticate", "authenticated", "credentials", "credential":
		return []string{"auth", "token"}
	case "github":
		return []string{"installation", "app"}
	case "locking", "locked", "locks":
		return []string{"lock"}
	case "repositories", "repository":
		return []string{"repo"}
	case "synchronization", "synchronise", "synchronize":
		return []string{"sync"}
	default:
		return nil
	}
}

var codeTopicStopWords = map[string]bool{
	"all": true, "and": true, "are": true, "code": true, "for": true,
	"how": true, "in": true, "into": true, "is": true, "of": true,
	"or": true, "responsible": true, "show": true, "the": true,
	"this": true, "to": true, "where": true, "who": true,
}
