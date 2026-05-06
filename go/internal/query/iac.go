package query

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/iacreachability"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	iacDeadCapability   = "iac_quality.dead_iac"
	iacDeadFileLimit    = 10000
	iacDeadDefaultLimit = 100
	iacDeadMaxLimit     = 500
)

// IaCHandler serves infrastructure-as-code quality query routes.
type IaCHandler struct {
	Content      ContentStore
	Reachability IaCReachabilityStore
	Profile      QueryProfile
}

// Mount registers IaC quality routes on the given mux.
func (h *IaCHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v0/iac/dead", h.handleDeadIaC)
}

func (h *IaCHandler) profile() QueryProfile {
	if h == nil {
		return ProfileProduction
	}
	return NormalizeQueryProfile(string(h.Profile))
}

type deadIaCRequest struct {
	RepoID           string   `json:"repo_id"`
	RepoIDs          []string `json:"repo_ids"`
	Families         []string `json:"families"`
	IncludeAmbiguous bool     `json:"include_ambiguous"`
	Limit            int      `json:"limit"`
	Offset           int      `json:"offset"`
}

type deadIaCFinding struct {
	ID           string   `json:"id"`
	Family       string   `json:"family"`
	RepoID       string   `json:"repo_id"`
	RepoName     string   `json:"repo_name,omitempty"`
	Artifact     string   `json:"artifact"`
	Reachability string   `json:"reachability"`
	Finding      string   `json:"finding"`
	Confidence   float64  `json:"confidence"`
	Evidence     []string `json:"evidence"`
	Limitations  []string `json:"limitations,omitempty"`
}

// IaCReachabilityStore reads reducer-materialized IaC cleanup findings.
type IaCReachabilityStore interface {
	ListLatestCleanupFindings(
		ctx context.Context,
		repoIDs []string,
		families []string,
		includeAmbiguous bool,
		limit int,
		offset int,
	) ([]IaCReachabilityFindingRow, error)
	CountLatestCleanupFindings(ctx context.Context, repoIDs []string, families []string, includeAmbiguous bool) (int, error)
	HasLatestRows(ctx context.Context, repoIDs []string, families []string) (bool, error)
}

// IaCReachabilityFindingRow is the query-facing shape of one materialized IaC
// cleanup row.
type IaCReachabilityFindingRow struct {
	ID           string
	Family       string
	RepoID       string
	ArtifactPath string
	Reachability string
	Finding      string
	Confidence   float64
	Evidence     []string
	Limitations  []string
}

func (h *IaCHandler) handleDeadIaC(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(r, telemetry.SpanQueryDeadIaC, "POST /api/v0/iac/dead", iacDeadCapability)
	defer span.End()

	if capabilityUnsupported(h.profile(), iacDeadCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"dead-IaC analysis requires an explicit indexed IaC scope",
			ErrorCodeUnsupportedCapability,
			iacDeadCapability,
			h.profile(),
			requiredProfile(iacDeadCapability),
		)
		return
	}

	var req deadIaCRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	repoIDs := normalizeDeadIaCRepoScope(req)
	if len(repoIDs) == 0 {
		WriteError(w, http.StatusBadRequest, "repo_id or repo_ids is required")
		return
	}
	repoIDs, err := h.resolveRepositoryScope(r.Context(), repoIDs)
	if err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	normalizeDeadIaCPaging(&req)
	families := normalizeDeadIaCFamilies(req.Families)
	if h != nil && h.Reachability != nil {
		totalFindings, err := h.Reachability.CountLatestCleanupFindings(
			r.Context(),
			repoIDs,
			families,
			req.IncludeAmbiguous,
		)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		rows, err := h.Reachability.ListLatestCleanupFindings(
			r.Context(),
			repoIDs,
			families,
			req.IncludeAmbiguous,
			req.Limit,
			req.Offset,
		)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if len(rows) > 0 {
			findings := materializedDeadIaCFindings(rows)
			h.enrichDeadIaCRepoNames(r.Context(), findings)
			writeMaterializedDeadIaC(w, r, h.profile(), repoIDs, findings, deadIaCPage{
				Limit: req.Limit, Offset: req.Offset, Total: totalFindings,
			})
			return
		}
		hasRows, err := h.Reachability.HasLatestRows(r.Context(), repoIDs, families)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if hasRows {
			writeMaterializedDeadIaC(w, r, h.profile(), repoIDs, nil, deadIaCPage{
				Limit: req.Limit, Offset: req.Offset, Total: totalFindings,
			})
			return
		}
	}
	if h == nil || h.Content == nil {
		WriteError(w, http.StatusServiceUnavailable, "content store is required")
		return
	}

	filesByRepo, err := loadIaCDeadFiles(r.Context(), h.Content, repoIDs)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	findings := analyzeDeadIaC(filesByRepo, iacreachability.FamilyFilter(families), req.IncludeAmbiguous)
	totalFindings := len(findings)
	findings = pageDeadIaCFindings(findings, req.Limit, req.Offset)
	h.enrichDeadIaCRepoNames(r.Context(), findings)

	WriteSuccess(w, r, http.StatusOK, map[string]any{
		"repo_ids":             repoIDs,
		"findings":             findings,
		"findings_count":       len(findings),
		"total_findings_count": totalFindings,
		"limit":                req.Limit,
		"offset":               req.Offset,
		"truncated":            deadIaCTruncated(req.Offset, len(findings), totalFindings),
		"next_offset":          deadIaCNextOffset(req.Offset, len(findings), totalFindings),
		"truth_basis":          "content_scope",
		"analysis_status":      "derived_candidate_analysis",
		"limitations": []string{
			"bounded to the requested repository scope",
			"dynamic templates and variable-selected references are reported as ambiguous",
			"exact dead-IaC requires reducer-materialized usage rows",
		},
	}, BuildTruthEnvelope(h.profile(), iacDeadCapability, TruthBasisContentIndex, "derived from bounded IaC content references"))
}

func writeMaterializedDeadIaC(
	w http.ResponseWriter,
	r *http.Request,
	profile QueryProfile,
	repoIDs []string,
	findings []deadIaCFinding,
	page deadIaCPage,
) {
	WriteSuccess(w, r, http.StatusOK, map[string]any{
		"repo_ids":             repoIDs,
		"findings":             findings,
		"findings_count":       len(findings),
		"total_findings_count": page.Total,
		"limit":                page.Limit,
		"offset":               page.Offset,
		"truncated":            deadIaCTruncated(page.Offset, len(findings), page.Total),
		"next_offset":          deadIaCNextOffset(page.Offset, len(findings), page.Total),
		"truth_basis":          "materialized_reducer_rows",
		"analysis_status":      "materialized_reachability",
		"limitations": []string{
			"dynamic templates and variable-selected references are reported as ambiguous",
		},
	}, BuildTruthEnvelope(profile, iacDeadCapability, TruthBasisSemanticFacts, "resolved from reducer-materialized IaC reachability rows"))
}

type deadIaCPage struct {
	Limit  int
	Offset int
	Total  int
}

func materializedDeadIaCFindings(rows []IaCReachabilityFindingRow) []deadIaCFinding {
	findings := make([]deadIaCFinding, 0, len(rows))
	for _, row := range rows {
		findings = append(findings, deadIaCFinding{
			ID:           row.ID,
			Family:       row.Family,
			RepoID:       row.RepoID,
			Artifact:     row.ArtifactPath,
			Reachability: row.Reachability,
			Finding:      row.Finding,
			Confidence:   row.Confidence,
			Evidence:     append([]string(nil), row.Evidence...),
			Limitations:  append([]string(nil), row.Limitations...),
		})
	}
	return findings
}

func (h *IaCHandler) enrichDeadIaCRepoNames(ctx context.Context, findings []deadIaCFinding) {
	if h == nil || h.Content == nil || len(findings) == 0 {
		return
	}
	repositories, err := h.Content.ListRepositories(ctx)
	if err != nil {
		return
	}
	namesByID := make(map[string]string, len(repositories))
	for _, repo := range repositories {
		if strings.TrimSpace(repo.ID) == "" || strings.TrimSpace(repo.Name) == "" {
			continue
		}
		namesByID[repo.ID] = repo.Name
	}
	for i := range findings {
		if name := namesByID[findings[i].RepoID]; name != "" {
			findings[i].RepoName = name
		}
	}
}

func normalizeDeadIaCRepoScope(req deadIaCRequest) []string {
	seen := map[string]struct{}{}
	var repoIDs []string
	add := func(repoID string) {
		repoID = strings.TrimSpace(repoID)
		if repoID == "" {
			return
		}
		if _, ok := seen[repoID]; ok {
			return
		}
		seen[repoID] = struct{}{}
		repoIDs = append(repoIDs, repoID)
	}
	add(req.RepoID)
	for _, repoID := range req.RepoIDs {
		add(repoID)
	}
	sort.Strings(repoIDs)
	return repoIDs
}

func normalizeDeadIaCPaging(req *deadIaCRequest) {
	if req.Limit <= 0 {
		req.Limit = iacDeadDefaultLimit
	}
	if req.Limit > iacDeadMaxLimit {
		req.Limit = iacDeadMaxLimit
	}
	if req.Offset < 0 {
		req.Offset = 0
	}
}

func pageDeadIaCFindings(findings []deadIaCFinding, limit int, offset int) []deadIaCFinding {
	if offset >= len(findings) {
		return nil
	}
	end := offset + limit
	if end > len(findings) {
		end = len(findings)
	}
	return findings[offset:end]
}

func deadIaCTruncated(offset int, returned int, total int) bool {
	return offset+returned < total
}

func deadIaCNextOffset(offset int, returned int, total int) *int {
	if !deadIaCTruncated(offset, returned, total) {
		return nil
	}
	next := offset + returned
	return &next
}

func (h *IaCHandler) resolveRepositoryScope(ctx context.Context, selectors []string) ([]string, error) {
	if h == nil || h.Content == nil {
		return selectors, nil
	}
	resolved := make([]string, 0, len(selectors))
	seen := make(map[string]struct{}, len(selectors))
	for _, selector := range selectors {
		repoID, err := resolveRepositorySelectorExact(ctx, nil, h.Content, selector)
		if err != nil {
			return nil, err
		}
		if _, ok := seen[repoID]; ok {
			continue
		}
		seen[repoID] = struct{}{}
		resolved = append(resolved, repoID)
	}
	sort.Strings(resolved)
	return resolved, nil
}

func normalizeDeadIaCFamilies(raw []string) []string {
	seen := map[string]struct{}{}
	var families []string
	for _, family := range raw {
		family = strings.ToLower(strings.TrimSpace(family))
		if family == "" {
			continue
		}
		if _, ok := seen[family]; ok {
			continue
		}
		seen[family] = struct{}{}
		families = append(families, family)
	}
	sort.Strings(families)
	return families
}

func loadIaCDeadFiles(ctx context.Context, content ContentStore, repoIDs []string) (map[string][]iacreachability.File, error) {
	filesByRepo := make(map[string][]iacreachability.File, len(repoIDs))
	for _, repoID := range repoIDs {
		files, err := content.ListRepoFiles(ctx, repoID, iacDeadFileLimit)
		if err != nil {
			return nil, fmt.Errorf("list IaC files for %q: %w", repoID, err)
		}
		for i, file := range files {
			if strings.TrimSpace(file.Content) != "" || !iacreachability.RelevantFile(file.RelativePath) {
				continue
			}
			loaded, err := content.GetFileContent(ctx, repoID, file.RelativePath)
			if err != nil {
				return nil, fmt.Errorf("get IaC file %q from %q: %w", file.RelativePath, repoID, err)
			}
			if loaded != nil {
				files[i] = *loaded
			}
		}
		filesByRepo[repoID] = queryFilesToIaCFiles(files)
	}
	return filesByRepo, nil
}

func queryFilesToIaCFiles(files []FileContent) []iacreachability.File {
	result := make([]iacreachability.File, 0, len(files))
	for _, file := range files {
		if !iacreachability.RelevantFile(file.RelativePath) {
			continue
		}
		result = append(result, iacreachability.File{
			RepoID:       file.RepoID,
			RelativePath: file.RelativePath,
			Content:      file.Content,
		})
	}
	return result
}

func analyzeDeadIaC(
	filesByRepo map[string][]iacreachability.File,
	families map[string]bool,
	includeAmbiguous bool,
) []deadIaCFinding {
	rows := iacreachability.Analyze(filesByRepo, iacreachability.Options{
		Families:         families,
		IncludeAmbiguous: includeAmbiguous,
	})
	rows = iacreachability.CleanupRows(rows, includeAmbiguous)
	findings := make([]deadIaCFinding, 0, len(rows))
	for _, row := range rows {
		findings = append(findings, deadIaCFinding{
			ID:           row.ID,
			Family:       row.Family,
			RepoID:       row.RepoID,
			Artifact:     row.ArtifactPath,
			Reachability: string(row.Reachability),
			Finding:      string(row.Finding),
			Confidence:   row.Confidence,
			Evidence:     append([]string(nil), row.Evidence...),
			Limitations:  append([]string(nil), row.Limitations...),
		})
	}
	return findings
}
