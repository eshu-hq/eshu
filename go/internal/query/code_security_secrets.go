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
	hardcodedSecretCapability   = "security.hardcoded_secrets"
	hardcodedSecretDefaultLimit = 25
	hardcodedSecretMaxLimit     = 200
	hardcodedSecretMaxOffset    = 10000
)

var (
	errHardcodedSecretBackendUnavailable = errors.New("hardcoded secret investigation backend is unavailable")
	hardcodedSecretAllowedKinds          = []string{
		"api_token",
		"aws_access_key",
		"password_literal",
		"private_key",
		"secret_literal",
		"slack_token",
	}
	secretValuePattern       = regexp.MustCompile(`(?i)(password|passwd|pwd|api[_-]?key|apikey|token|secret|client[_-]?secret|private[_-]?key|authorization)([[:space:]]*[:=][[:space:]]*["']?)([^"',;[:space:]]{6,})`)
	secretStandalonePatterns = []*regexp.Regexp{
		regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
		regexp.MustCompile(`(?i)sk_live_[A-Za-z0-9]{8,}`),
		regexp.MustCompile(`xox[baprs]-[A-Za-z0-9-]{10,}`),
		regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`),
	}
)

type hardcodedSecretInvestigationRequest struct {
	RepoID            string   `json:"repo_id"`
	Language          string   `json:"language"`
	FindingKinds      []string `json:"finding_kinds"`
	IncludeSuppressed bool     `json:"include_suppressed"`
	Limit             int      `json:"limit"`
	Offset            int      `json:"offset"`
}

type hardcodedSecretFindingRow struct {
	RepoID       string
	RelativePath string
	Language     string
	LineNumber   int
	LineText     string
	FindingKind  string
	Confidence   string
	Severity     string
	Suppressed   bool
	Suppressions []string
}

type hardcodedSecretInvestigator interface {
	investigateHardcodedSecrets(context.Context, hardcodedSecretInvestigationRequest) ([]hardcodedSecretFindingRow, error)
}

func init() {
	capabilityMatrix[hardcodedSecretCapability] = capabilitySupport{
		LocalLightweightMax:   &truthDerived,
		LocalAuthoritativeMax: &truthDerived,
		LocalFullStackMax:     &truthDerived,
		ProductionMax:         &truthDerived,
	}
}

func (h *CodeHandler) handleHardcodedSecretInvestigation(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(r, telemetry.SpanQueryHardcodedSecretInvestigation, "POST /api/v0/code/security/secrets/investigate", hardcodedSecretCapability)
	defer span.End()

	var req hardcodedSecretInvestigationRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if capabilityUnsupported(h.profile(), hardcodedSecretCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"hardcoded secret investigation requires a supported query profile",
			ErrorCodeUnsupportedCapability,
			hardcodedSecretCapability,
			h.profile(),
			requiredProfile(hardcodedSecretCapability),
		)
		return
	}
	if req.Offset < 0 {
		WriteError(w, http.StatusBadRequest, "offset must be >= 0")
		return
	}
	if req.Offset > hardcodedSecretMaxOffset {
		WriteError(w, http.StatusBadRequest, "offset must be <= 10000")
		return
	}
	if !h.applyRepositorySelector(w, r, &req.RepoID) {
		return
	}
	var err error
	req.FindingKinds, err = normalizeHardcodedSecretKinds(req.FindingKinds)
	if err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.Limit = normalizeHardcodedSecretLimit(req.Limit)

	rows, err := h.hardcodedSecretRows(r.Context(), req)
	if err != nil {
		if errors.Is(err, errHardcodedSecretBackendUnavailable) {
			WriteError(w, http.StatusServiceUnavailable, err.Error())
			return
		}
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	data := hardcodedSecretResponse(req, rows)
	WriteSuccess(
		w,
		r,
		http.StatusOK,
		data,
		BuildTruthEnvelope(h.profile(), hardcodedSecretCapability, TruthBasisContentIndex, "resolved from bounded content-index hardcoded secret investigation with redacted findings"),
	)
}

func (h *CodeHandler) hardcodedSecretRows(ctx context.Context, req hardcodedSecretInvestigationRequest) ([]hardcodedSecretFindingRow, error) {
	if h == nil || h.Content == nil {
		return nil, errHardcodedSecretBackendUnavailable
	}
	investigator, ok := h.Content.(hardcodedSecretInvestigator)
	if !ok {
		return nil, errHardcodedSecretBackendUnavailable
	}
	probeReq := req
	probeReq.Limit = req.Limit + 1
	rows, err := investigator.investigateHardcodedSecrets(ctx, probeReq)
	if err != nil {
		return nil, fmt.Errorf("investigate hardcoded secrets: %w", err)
	}
	return rows, nil
}

func normalizeHardcodedSecretLimit(limit int) int {
	switch {
	case limit <= 0:
		return hardcodedSecretDefaultLimit
	case limit > hardcodedSecretMaxLimit:
		return hardcodedSecretMaxLimit
	default:
		return limit
	}
}

func normalizeHardcodedSecretKinds(kinds []string) ([]string, error) {
	normalized := make([]string, 0, len(kinds))
	seen := map[string]struct{}{}
	for _, kind := range kinds {
		kind = strings.TrimSpace(strings.ToLower(kind))
		if kind == "" {
			continue
		}
		if !slices.Contains(hardcodedSecretAllowedKinds, kind) {
			return nil, fmt.Errorf("unsupported finding_kind %q", kind)
		}
		if _, exists := seen[kind]; exists {
			continue
		}
		seen[kind] = struct{}{}
		normalized = append(normalized, kind)
	}
	return normalized, nil
}

func hardcodedSecretResponse(req hardcodedSecretInvestigationRequest, rows []hardcodedSecretFindingRow) map[string]any {
	suppressedCount := 0
	for _, row := range rows {
		if row.Suppressed {
			suppressedCount++
		}
	}
	visibleRows := rows
	truncated := len(visibleRows) > req.Limit
	if truncated {
		visibleRows = visibleRows[:req.Limit]
	}

	findings := make([]map[string]any, 0, len(visibleRows))
	for index, row := range visibleRows {
		findings = append(findings, hardcodedSecretFinding(row, index+1))
	}
	return map[string]any{
		"scope":                  hardcodedSecretScope(req),
		"finding_kinds":          req.FindingKinds,
		"findings":               findings,
		"recommended_next_calls": hardcodedSecretRecommendedNextCalls(visibleRows),
		"count":                  len(findings),
		"limit":                  req.Limit,
		"offset":                 req.Offset,
		"truncated":              truncated,
		"source_backend":         "postgres_content_store",
		"coverage": map[string]any{
			"query_shape":         "content_secret_investigation",
			"returned_count":      len(findings),
			"suppressed_count":    suppressedCount,
			"include_suppressed":  req.IncludeSuppressed,
			"limit":               req.Limit,
			"offset":              req.Offset,
			"truncated":           truncated,
			"redaction":           "secret_values_replaced_with_redacted_marker",
			"empty":               len(findings) == 0,
			"searched_all_kinds":  len(req.FindingKinds) == 0,
			"requires_repo_scope": false,
		},
	}
}

func hardcodedSecretFinding(row hardcodedSecretFindingRow, rank int) map[string]any {
	return map[string]any{
		"rank":              rank,
		"repo_id":           row.RepoID,
		"relative_path":     row.RelativePath,
		"language":          row.Language,
		"line_number":       row.LineNumber,
		"finding_kind":      row.FindingKind,
		"confidence":        row.Confidence,
		"severity":          row.Severity,
		"redacted_excerpt":  redactHardcodedSecretLine(row.LineText),
		"suppressed":        row.Suppressed,
		"suppression_notes": row.Suppressions,
		"source_handle": map[string]any{
			"repo_id":       row.RepoID,
			"relative_path": row.RelativePath,
			"start_line":    row.LineNumber,
			"end_line":      row.LineNumber,
		},
	}
}

func hardcodedSecretScope(req hardcodedSecretInvestigationRequest) map[string]any {
	return map[string]any{
		"repo_id":  strings.TrimSpace(req.RepoID),
		"language": strings.TrimSpace(req.Language),
	}
}

func hardcodedSecretRecommendedNextCalls(rows []hardcodedSecretFindingRow) []map[string]any {
	if len(rows) == 0 {
		return []map[string]any{
			{"tool": "investigate_code_topic", "reason": "broaden the security investigation when no redacted secret candidates were found"},
		}
	}
	first := rows[0]
	return []map[string]any{
		{
			"tool":   "build_evidence_citation_packet",
			"reason": "hydrate exact redacted source context for selected findings",
			"args": map[string]any{
				"handles": []map[string]any{
					{
						"kind":            "file",
						"repo_id":         first.RepoID,
						"relative_path":   first.RelativePath,
						"start_line":      first.LineNumber,
						"end_line":        first.LineNumber,
						"evidence_family": "security",
						"reason":          "hardcoded secret candidate",
					},
				},
			},
		},
	}
}

func redactHardcodedSecretLine(line string) string {
	redacted := secretValuePattern.ReplaceAllString(line, `${1}${2}[REDACTED]`)
	for _, pattern := range secretStandalonePatterns {
		redacted = pattern.ReplaceAllString(redacted, "[REDACTED]")
	}
	return redacted
}
