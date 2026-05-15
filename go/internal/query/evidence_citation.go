package query

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	evidenceCitationCapability      = "evidence_citation.packet"
	evidenceCitationDefaultLimit    = 10
	evidenceCitationMaxLimit        = 50
	evidenceCitationMaxExcerptLines = 40
)

type evidenceCitationRequest struct {
	Subject  map[string]any           `json:"subject,omitempty"`
	Question string                   `json:"question,omitempty"`
	Handles  []evidenceCitationHandle `json:"handles"`
	Limit    int                      `json:"limit,omitempty"`
}

type evidenceCitationHandle struct {
	Kind           string `json:"kind,omitempty"`
	RepoID         string `json:"repo_id,omitempty"`
	RelativePath   string `json:"relative_path,omitempty"`
	EntityID       string `json:"entity_id,omitempty"`
	EvidenceFamily string `json:"evidence_family,omitempty"`
	Reason         string `json:"reason,omitempty"`
	StartLine      int    `json:"start_line,omitempty"`
	EndLine        int    `json:"end_line,omitempty"`
}

type evidenceCitationResponse struct {
	Subject              map[string]any           `json:"subject,omitempty"`
	Question             string                   `json:"question,omitempty"`
	Citations            []evidenceCitation       `json:"citations"`
	MissingHandles       []evidenceCitationHandle `json:"missing_handles"`
	Coverage             evidenceCitationCoverage `json:"coverage"`
	RecommendedNextCalls []map[string]any         `json:"recommended_next_calls"`
}

type evidenceCitation struct {
	CitationID     string `json:"citation_id"`
	Rank           int    `json:"rank"`
	Kind           string `json:"kind"`
	EvidenceFamily string `json:"evidence_family"`
	Reason         string `json:"reason,omitempty"`
	RepoID         string `json:"repo_id,omitempty"`
	RelativePath   string `json:"relative_path,omitempty"`
	EntityID       string `json:"entity_id,omitempty"`
	EntityType     string `json:"entity_type,omitempty"`
	EntityName     string `json:"entity_name,omitempty"`
	StartLine      int    `json:"start_line,omitempty"`
	EndLine        int    `json:"end_line,omitempty"`
	Language       string `json:"language,omitempty"`
	ArtifactType   string `json:"artifact_type,omitempty"`
	ContentHash    string `json:"content_hash,omitempty"`
	CommitSHA      string `json:"commit_sha,omitempty"`
	Excerpt        string `json:"excerpt"`
}

type evidenceCitationCoverage struct {
	QueryShape       string `json:"query_shape"`
	InputHandleCount int    `json:"input_handle_count"`
	ResolvedCount    int    `json:"resolved_count"`
	MissingCount     int    `json:"missing_count"`
	Limit            int    `json:"limit"`
	Truncated        bool   `json:"truncated"`
	SourceBackend    string `json:"source_backend"`
}

type evidenceCitationFileLookup struct {
	RepoID       string
	RelativePath string
}

type evidenceCitationFileKey struct {
	repoID       string
	relativePath string
}

type evidenceCitationFileStore interface {
	evidenceCitationFiles(context.Context, []evidenceCitationFileLookup) (map[evidenceCitationFileKey]FileContent, error)
}

type evidenceCitationEntityBatchStore interface {
	GetEntityContents(context.Context, []string) (map[string]*EntityContent, error)
}

func (h *EvidenceHandler) buildEvidenceCitations(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryEvidenceCitationPacket,
		"POST /api/v0/evidence/citations",
		evidenceCitationCapability,
	)
	defer span.End()

	if h.Content == nil {
		WriteError(w, http.StatusNotImplemented, "evidence citation packet requires the Postgres content store")
		return
	}

	var req evidenceCitationRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	handles, limit, truncated, err := normalizeEvidenceCitationRequest(req)
	if err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	files, err := h.evidenceCitationFileContents(r.Context(), handles)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	entities, err := h.evidenceCitationEntityContents(r.Context(), handles)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	citations := make([]evidenceCitation, 0, len(handles))
	missing := make([]evidenceCitationHandle, 0)
	for _, handle := range handles {
		switch handle.Kind {
		case "file":
			file, ok := files[evidenceCitationFileKey{repoID: handle.RepoID, relativePath: handle.RelativePath}]
			if !ok {
				missing = append(missing, handle)
				continue
			}
			citations = append(citations, citationFromFile(len(citations)+1, handle, file))
		case "entity":
			entity, ok := entities[handle.EntityID]
			if !ok || entity == nil {
				missing = append(missing, handle)
				continue
			}
			citations = append(citations, citationFromEntity(len(citations)+1, handle, *entity))
		}
	}

	response := evidenceCitationResponse{
		Subject:        req.Subject,
		Question:       strings.TrimSpace(req.Question),
		Citations:      citations,
		MissingHandles: missing,
		Coverage: evidenceCitationCoverage{
			QueryShape:       "bounded_evidence_citation_packet",
			InputHandleCount: len(req.Handles),
			ResolvedCount:    len(citations),
			MissingCount:     len(missing),
			Limit:            limit,
			Truncated:        truncated,
			SourceBackend:    "postgres_content_store",
		},
		RecommendedNextCalls: evidenceCitationNextCalls(missing, truncated),
	}

	WriteSuccess(w, r, http.StatusOK, response, BuildTruthEnvelope(
		h.profile(),
		evidenceCitationCapability,
		TruthBasisContentIndex,
		"resolved from bounded Postgres content handles without graph traversal",
	))
}

func normalizeEvidenceCitationRequest(
	req evidenceCitationRequest,
) ([]evidenceCitationHandle, int, bool, error) {
	limit := req.Limit
	if limit <= 0 {
		limit = evidenceCitationDefaultLimit
	}
	if limit > evidenceCitationMaxLimit {
		limit = evidenceCitationMaxLimit
	}

	handles := make([]evidenceCitationHandle, 0, len(req.Handles))
	seen := make(map[string]struct{}, len(req.Handles))
	for _, handle := range req.Handles {
		normalized, ok := normalizeEvidenceCitationHandle(handle)
		if !ok {
			return nil, limit, false, fmt.Errorf("each handle must include either repo_id plus relative_path or entity_id")
		}
		key := normalized.Kind + "\x00" + normalized.RepoID + "\x00" + normalized.RelativePath + "\x00" + normalized.EntityID
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		handles = append(handles, normalized)
	}
	if len(handles) == 0 {
		return nil, limit, false, fmt.Errorf("at least one evidence handle is required")
	}
	truncated := len(handles) > limit
	if truncated {
		handles = handles[:limit]
	}
	return handles, limit, truncated, nil
}

func normalizeEvidenceCitationHandle(handle evidenceCitationHandle) (evidenceCitationHandle, bool) {
	handle.Kind = strings.ToLower(strings.TrimSpace(handle.Kind))
	handle.RepoID = strings.TrimSpace(handle.RepoID)
	handle.RelativePath = strings.TrimSpace(filepath.ToSlash(handle.RelativePath))
	handle.EntityID = strings.TrimSpace(handle.EntityID)
	handle.EvidenceFamily = strings.ToLower(strings.TrimSpace(handle.EvidenceFamily))
	handle.Reason = strings.TrimSpace(handle.Reason)
	if handle.Kind == "" {
		if handle.EntityID != "" {
			handle.Kind = "entity"
		} else if handle.RepoID != "" && handle.RelativePath != "" {
			handle.Kind = "file"
		}
	}
	switch handle.Kind {
	case "file":
		return handle, handle.RepoID != "" && handle.RelativePath != ""
	case "entity":
		return handle, handle.EntityID != ""
	default:
		return evidenceCitationHandle{}, false
	}
}

func (h *EvidenceHandler) evidenceCitationFileContents(
	ctx context.Context,
	handles []evidenceCitationHandle,
) (map[evidenceCitationFileKey]FileContent, error) {
	lookups := make([]evidenceCitationFileLookup, 0)
	seen := make(map[evidenceCitationFileKey]struct{}, len(handles))
	for _, handle := range handles {
		if handle.Kind != "file" {
			continue
		}
		key := evidenceCitationFileKey{repoID: handle.RepoID, relativePath: handle.RelativePath}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		lookups = append(lookups, evidenceCitationFileLookup{RepoID: handle.RepoID, RelativePath: handle.RelativePath})
	}
	if len(lookups) == 0 {
		return map[evidenceCitationFileKey]FileContent{}, nil
	}
	if store, ok := h.Content.(evidenceCitationFileStore); ok {
		return store.evidenceCitationFiles(ctx, lookups)
	}

	results := make(map[evidenceCitationFileKey]FileContent, len(lookups))
	for _, lookup := range lookups {
		file, err := h.Content.GetFileContent(ctx, lookup.RepoID, lookup.RelativePath)
		if err != nil {
			return nil, err
		}
		if file == nil {
			continue
		}
		results[evidenceCitationFileKey{repoID: lookup.RepoID, relativePath: lookup.RelativePath}] = *file
	}
	return results, nil
}

func (h *EvidenceHandler) evidenceCitationEntityContents(
	ctx context.Context,
	handles []evidenceCitationHandle,
) (map[string]*EntityContent, error) {
	entityIDs := make([]string, 0)
	seen := make(map[string]struct{}, len(handles))
	for _, handle := range handles {
		if handle.Kind != "entity" {
			continue
		}
		if _, exists := seen[handle.EntityID]; exists {
			continue
		}
		seen[handle.EntityID] = struct{}{}
		entityIDs = append(entityIDs, handle.EntityID)
	}
	if len(entityIDs) == 0 {
		return map[string]*EntityContent{}, nil
	}
	if store, ok := h.Content.(evidenceCitationEntityBatchStore); ok {
		return store.GetEntityContents(ctx, entityIDs)
	}

	results := make(map[string]*EntityContent, len(entityIDs))
	for _, entityID := range entityIDs {
		entity, err := h.Content.GetEntityContent(ctx, entityID)
		if err != nil {
			return nil, err
		}
		if entity != nil {
			results[entityID] = entity
		}
	}
	return results, nil
}

func citationFromFile(rank int, handle evidenceCitationHandle, file FileContent) evidenceCitation {
	excerpt, startLine, endLine := boundedLineExcerpt(file.Content, handle.StartLine, handle.EndLine)
	return evidenceCitation{
		CitationID:     evidenceCitationID("file", file.RepoID, file.RelativePath, startLine, endLine),
		Rank:           rank,
		Kind:           "file",
		EvidenceFamily: evidenceFamily(handle.EvidenceFamily, file.RelativePath, file.ArtifactType, "file"),
		Reason:         handle.Reason,
		RepoID:         file.RepoID,
		RelativePath:   file.RelativePath,
		StartLine:      startLine,
		EndLine:        endLine,
		Language:       file.Language,
		ArtifactType:   file.ArtifactType,
		ContentHash:    file.ContentHash,
		CommitSHA:      file.CommitSHA,
		Excerpt:        excerpt,
	}
}

func citationFromEntity(rank int, handle evidenceCitationHandle, entity EntityContent) evidenceCitation {
	excerpt, offsetStart, offsetEnd := boundedLineExcerpt(entity.SourceCache, 1, 0)
	startLine := entity.StartLine
	endLine := entity.EndLine
	if excerpt != "" && startLine > 0 {
		endLine = startLine + offsetEnd - offsetStart
	}
	return evidenceCitation{
		CitationID:     evidenceCitationID("entity", entity.EntityID, entity.RepoID, entity.RelativePath),
		Rank:           rank,
		Kind:           "entity",
		EvidenceFamily: evidenceFamily(handle.EvidenceFamily, entity.RelativePath, "", "entity"),
		Reason:         handle.Reason,
		RepoID:         entity.RepoID,
		RelativePath:   entity.RelativePath,
		EntityID:       entity.EntityID,
		EntityType:     entity.EntityType,
		EntityName:     entity.EntityName,
		StartLine:      startLine,
		EndLine:        endLine,
		Language:       entity.Language,
		Excerpt:        excerpt,
	}
}

func boundedLineExcerpt(content string, startLine, endLine int) (string, int, int) {
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return "", 0, 0
	}
	if startLine < 1 {
		startLine = 1
	}
	if endLine < startLine || endLine > len(lines) {
		endLine = len(lines)
	}
	if endLine-startLine+1 > evidenceCitationMaxExcerptLines {
		endLine = startLine + evidenceCitationMaxExcerptLines - 1
	}
	if startLine > len(lines) {
		return "", startLine, startLine - 1
	}
	return strings.Join(lines[startLine-1:endLine], "\n"), startLine, endLine
}

func evidenceFamily(explicit, relativePath, artifactType, kind string) string {
	if explicit != "" {
		return explicit
	}
	path := strings.ToLower(relativePath)
	artifact := strings.ToLower(artifactType)
	switch {
	case strings.Contains(artifact, "doc") || strings.HasSuffix(path, ".md") || strings.Contains(path, "readme"):
		return "documentation"
	case strings.Contains(artifact, "manifest") || strings.HasSuffix(path, ".yaml") ||
		strings.HasSuffix(path, ".yml") || strings.HasSuffix(path, ".tf") ||
		strings.Contains(path, "chart") || strings.Contains(path, "kustomization"):
		return "deployment"
	case kind == "entity":
		return "source"
	default:
		return "source"
	}
}

func evidenceCitationID(parts ...any) string {
	hash := sha1.New()
	for _, part := range parts {
		_, _ = fmt.Fprintf(hash, "%v\x00", part)
	}
	return "citation:" + hex.EncodeToString(hash.Sum(nil))[:16]
}

func evidenceCitationNextCalls(missing []evidenceCitationHandle, truncated bool) []map[string]any {
	calls := make([]map[string]any, 0, 2)
	if truncated {
		calls = append(calls, map[string]any{
			"tool":   "build_evidence_citation_packet",
			"reason": "increase limit or send the next handle batch to continue citation hydration",
		})
	}
	if len(missing) > 0 {
		calls = append(calls, map[string]any{
			"tool":   "search_file_content",
			"reason": "refresh or rediscover missing file/entity handles before citation hydration",
		})
	}
	return calls
}
