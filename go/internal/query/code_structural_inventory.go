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
	structuralInventoryCapability   = "code_inventory.structural"
	structuralInventoryDefaultLimit = 25
	structuralInventoryMaxLimit     = 200
	structuralInventoryMaxOffset    = 10000
)

var errStructuralInventoryUnavailable = errors.New("structural inventory content index is unavailable")

type structuralInventoryRequest struct {
	RepoID        string `json:"repo_id"`
	Language      string `json:"language"`
	InventoryKind string `json:"inventory_kind"`
	EntityKind    string `json:"entity_kind"`
	FilePath      string `json:"file_path"`
	Symbol        string `json:"symbol"`
	Decorator     string `json:"decorator"`
	MethodName    string `json:"method_name"`
	ClassName     string `json:"class_name"`
	Limit         int    `json:"limit"`
	Offset        int    `json:"offset"`
}

type structuralInventoryContentStore interface {
	InspectStructuralInventory(context.Context, structuralInventoryRequest) ([]EntityContent, error)
	CountStructuralInventoryByFile(context.Context, structuralInventoryRequest) ([]StructuralInventoryFileCount, error)
}

// StructuralInventoryFileCount is an aggregated structural inventory row for
// one repo-relative file.
type StructuralInventoryFileCount struct {
	RepoID        string `json:"repo_id"`
	RelativePath  string `json:"relative_path"`
	Language      string `json:"language,omitempty"`
	FunctionCount int    `json:"function_count"`
	SourceBackend string `json:"source_backend"`
	SourceHandle  any    `json:"source_handle"`
	MatchedKind   string `json:"match_kind"`
}

func (h *CodeHandler) handleStructuralInventory(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryCodeStructuralInventory,
		"POST /api/v0/code/structure/inventory",
		structuralInventoryCapability,
	)
	defer span.End()

	var req structuralInventoryRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if capabilityUnsupported(h.profile(), structuralInventoryCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"structural code inventory requires a supported query profile",
			ErrorCodeUnsupportedCapability,
			structuralInventoryCapability,
			h.profile(),
			requiredProfile(structuralInventoryCapability),
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

	data, err := h.structuralInventoryData(r.Context(), req)
	if err != nil {
		if errors.Is(err, errStructuralInventoryUnavailable) {
			WriteError(w, http.StatusServiceUnavailable, err.Error())
			return
		}
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	limit := req.normalizedLimit()
	results := data.results
	WriteSuccess(
		w,
		r,
		http.StatusOK,
		map[string]any{
			"repo_id":        req.RepoID,
			"language":       strings.TrimSpace(req.Language),
			"inventory_kind": req.kind(),
			"entity_kind":    req.entityType(),
			"file_path":      strings.TrimSpace(req.FilePath),
			"symbol":         strings.TrimSpace(req.Symbol),
			"decorator":      strings.TrimSpace(req.Decorator),
			"method_name":    strings.TrimSpace(req.MethodName),
			"class_name":     strings.TrimSpace(req.ClassName),
			"limit":          limit,
			"offset":         req.Offset,
			"results":        results,
			"matches":        results,
			"count":          len(results),
			"truncated":      data.truncated,
			"next_offset":    nextStructuralInventoryOffset(req.Offset, len(results), data.truncated),
			"source_backend": "postgres_content_store",
		},
		BuildTruthEnvelope(h.profile(), structuralInventoryCapability, TruthBasisContentIndex, "resolved from bounded content-index structural inventory"),
	)
}

type structuralInventoryData struct {
	results   []map[string]any
	truncated bool
}

func (h *CodeHandler) structuralInventoryData(
	ctx context.Context,
	req structuralInventoryRequest,
) (structuralInventoryData, error) {
	if h == nil || h.Content == nil {
		return structuralInventoryData{}, errStructuralInventoryUnavailable
	}
	reader, ok := h.Content.(structuralInventoryContentStore)
	if !ok {
		return structuralInventoryData{}, errStructuralInventoryUnavailable
	}
	displayLimit := req.normalizedLimit()
	queryReq := req
	queryReq.Limit = displayLimit + 1
	if req.kind() == "function_count_by_file" {
		rows, err := reader.CountStructuralInventoryByFile(ctx, queryReq)
		if err != nil {
			return structuralInventoryData{}, err
		}
		truncated := len(rows) > displayLimit
		if truncated {
			rows = rows[:displayLimit]
		}
		return structuralInventoryData{results: structuralInventoryFileCountResults(rows), truncated: truncated}, nil
	}
	rows, err := reader.InspectStructuralInventory(ctx, queryReq)
	if err != nil {
		return structuralInventoryData{}, err
	}
	truncated := len(rows) > displayLimit
	if truncated {
		rows = rows[:displayLimit]
	}
	return structuralInventoryData{results: structuralInventoryResults(rows, req.kind()), truncated: truncated}, nil
}

func (r structuralInventoryRequest) validate() error {
	if r.Limit > structuralInventoryMaxLimit {
		return fmt.Errorf("limit must be <= 200")
	}
	if r.Offset < 0 {
		return fmt.Errorf("offset must be >= 0")
	}
	if r.Offset > structuralInventoryMaxOffset {
		return fmt.Errorf("offset must be <= 10000")
	}
	if _, ok := structuralInventoryKinds()[r.kind()]; !ok {
		return fmt.Errorf("inventory_kind must be one of: %s", strings.Join(structuralInventoryKindNames(), ", "))
	}
	if r.kind() == "class_with_method" && strings.TrimSpace(r.MethodName) == "" {
		return fmt.Errorf("method_name is required for class_with_method inventory")
	}
	if r.kind() == "function_count_by_file" &&
		strings.TrimSpace(r.EntityKind) != "" &&
		contentEntityTypeForResolve(strings.ToLower(strings.TrimSpace(r.EntityKind))) != "Function" {
		return fmt.Errorf("entity_kind must be function for function_count_by_file inventory")
	}
	if !r.hasScopeFilter() {
		return fmt.Errorf("one of repo_id, file_path, language, entity_kind, or symbol is required")
	}
	return nil
}

func (r structuralInventoryRequest) normalizedLimit() int {
	switch {
	case r.Limit <= 0:
		return structuralInventoryDefaultLimit
	case r.Limit > structuralInventoryMaxLimit:
		return structuralInventoryMaxLimit
	default:
		return r.Limit
	}
}

func (r structuralInventoryRequest) kind() string {
	kind := strings.ToLower(strings.TrimSpace(r.InventoryKind))
	if kind == "" {
		return "entity"
	}
	return kind
}

func (r structuralInventoryRequest) entityType() string {
	entityKind := strings.TrimSpace(r.EntityKind)
	switch r.kind() {
	case "dataclass":
		return "Class"
	case "documented_function", "class_with_method", "function_count_by_file":
		return "Function"
	}
	if entityKind == "" {
		return ""
	}
	return contentEntityTypeForResolve(strings.ToLower(entityKind))
}

func (r structuralInventoryRequest) hasScopeFilter() bool {
	for _, value := range []string{r.RepoID, r.FilePath, r.Language, r.EntityKind, r.Symbol} {
		if strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}

func structuralInventoryKinds() map[string]struct{} {
	return map[string]struct{}{
		"entity":                 {},
		"top_level":              {},
		"dataclass":              {},
		"documented":             {},
		"documented_function":    {},
		"decorated":              {},
		"class_with_method":      {},
		"super_call":             {},
		"function_count_by_file": {},
	}
}

func structuralInventoryKindNames() []string {
	return []string{"entity", "top_level", "dataclass", "documented", "documented_function", "decorated", "class_with_method", "super_call", "function_count_by_file"}
}

func structuralInventoryResults(entities []EntityContent, matchKind string) []map[string]any {
	results := make([]map[string]any, 0, len(entities))
	for index, entity := range entities {
		result := map[string]any{
			"entity_id":      entity.EntityID,
			"name":           entity.EntityName,
			"entity_name":    entity.EntityName,
			"entity_type":    entity.EntityType,
			"file_path":      entity.RelativePath,
			"relative_path":  entity.RelativePath,
			"repo_id":        entity.RepoID,
			"language":       entity.Language,
			"start_line":     entity.StartLine,
			"end_line":       entity.EndLine,
			"source_cache":   entity.SourceCache,
			"metadata":       entity.Metadata,
			"match_kind":     matchKind,
			"rank":           index + 1,
			"source_backend": "postgres_content_store",
			"source_handle":  structuralInventorySourceHandle(entity),
		}
		if className := structuralInventoryClassName(entity.Metadata); className != "" {
			result["class_name"] = className
		}
		if decorators := stringSliceFromAny(entity.Metadata["decorators"]); len(decorators) > 0 {
			result["decorators"] = decorators
		}
		if docstring := metadataString(entity.Metadata, "docstring"); docstring != "" {
			result["docstring_present"] = true
		}
		attachSemanticSummary(result)
		results = append(results, result)
	}
	return results
}

func structuralInventoryFileCountResults(rows []StructuralInventoryFileCount) []map[string]any {
	results := make([]map[string]any, 0, len(rows))
	for index, row := range rows {
		result := map[string]any{
			"repo_id":        row.RepoID,
			"file_path":      row.RelativePath,
			"relative_path":  row.RelativePath,
			"language":       row.Language,
			"function_count": row.FunctionCount,
			"match_kind":     "function_count_by_file",
			"rank":           index + 1,
			"source_backend": "postgres_content_store",
			"source_handle":  row.SourceHandle,
		}
		results = append(results, result)
	}
	return results
}

func structuralInventoryClassName(metadata map[string]any) string {
	for _, key := range []string{"class_context", "context", "impl_context"} {
		if value := metadataString(metadata, key); value != "" {
			return value
		}
	}
	return ""
}

func structuralInventorySourceHandle(entity EntityContent) map[string]any {
	return map[string]any{
		"repo_id":        entity.RepoID,
		"file_path":      entity.RelativePath,
		"relative_path":  entity.RelativePath,
		"start_line":     entity.StartLine,
		"end_line":       entity.EndLine,
		"entity_id":      entity.EntityID,
		"entity_type":    entity.EntityType,
		"entity_name":    entity.EntityName,
		"content_tool":   "get_file_lines",
		"drilldown_tool": "get_entity_context",
	}
}

func nextStructuralInventoryOffset(offset, count int, truncated bool) any {
	if !truncated {
		return nil
	}
	return offset + count
}
