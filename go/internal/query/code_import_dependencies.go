// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
)

const (
	importDependencyCapability = "symbol_graph.import_dependencies"
	// importDependencyNoBackendRead is the source_backend an empty-grant page
	// carries. importDependencyResponse hard-codes "graph" for every other
	// response on this route, which is true of all of them except this one.
	//
	// The value reuses the "unavailable" sentinel OUTSIDE its documented
	// meaning: sourceBackendForTruthBasis has no no-read case, and the public
	// table in language-query-dsl.md documents "unavailable" as the fallback
	// for an unrecognized basis. Nothing on the wire means "no backend was
	// read", just as no TruthBasis member does, and naming a backend that was
	// never read would be worse than reusing the one value that claims
	// nothing. The public table carries this second meaning so a caller is not
	// misled. Shares languageQueryNoBackendRead's wording for the same reason
	// both routes share the reason string.
	importDependencyNoBackendRead = "unavailable"
	importDependencyDefaultLimit  = 25
	importDependencyMaxLimit      = 200
	importDependencyMaxOffset     = 10000
)

var errImportDependencyUnavailable = errors.New("import dependency graph is unavailable")

type importDependencyRequest struct {
	QueryType    string `json:"query_type"`
	RepoID       string `json:"repo_id"`
	Language     string `json:"language"`
	SourceFile   string `json:"source_file"`
	TargetFile   string `json:"target_file"`
	SourceModule string `json:"source_module"`
	TargetModule string `json:"target_module"`
	Limit        int    `json:"limit"`
	Offset       int    `json:"offset"`

	// access is the caller's repository grant, set by the handler from the
	// request's AuthContext and never decoded from the body (it is unexported,
	// so encoding/json cannot reach it). It travels on the request because all
	// seven builders take one, so the grant reaches every one of them without a
	// seventh parameter on each signature.
	access repositoryAccessFilter
}

func (h *CodeHandler) handleImportDependencyInvestigation(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryImportDependencyInvestigation,
		"POST /api/v0/code/imports/investigate",
		importDependencyCapability,
	)
	defer span.End()

	var req importDependencyRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if capabilityUnsupported(h.profile(), importDependencyCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"import dependency investigation requires a supported query profile",
			ErrorCodeUnsupportedCapability,
			importDependencyCapability,
			h.profile(),
			requiredProfile(importDependencyCapability),
		)
		return
	}
	if err := req.validate(); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !h.applyRepositorySelectorForCapability(w, r, &req.RepoID, importDependencyCapability) {
		return
	}
	span.SetAttributes(attribute.String("eshu.import_dependencies.query_type", req.queryType()))

	// repo_id is optional here -- source_file, target_file, source_module or
	// target_module each satisfy req.validate() on their own -- so a scoped
	// caller who omits it reaches every builder corpus-wide. codeContentGrantScope
	// is the front gate for the grantless case; req.access is what the builders
	// bind for everyone else.
	if _, blocked := codeContentGrantScope(r.Context(), req.RepoID); blocked {
		// This page is produced without reading anything, so it must not
		// describe itself as an authoritative graph read. It reports the same
		// basis the language-query empty page reports, and importDependencyResponse's
		// blanket "graph" source_backend is replaced with the vocabulary
		// sourceBackendForTruthBasis uses when nothing served a read.
		//
		// Neither value is a perfect fit: the TruthBasis enum has no "no read
		// happened" member, so content_index is the lowest-claim value
		// available rather than a description of what occurred. The reason
		// string is what actually says it, and it is the field a caller should
		// read here -- which is why it denies every backend rather than only
		// the graph. "No graph read was issued" beside a content_index basis
		// would invite the reader to infer a content read that also never
		// happened. It is the same sentence language-query's empty page uses.
		emptyPage := importDependencyResponse(req, nil)
		emptyPage["source_backend"] = importDependencyNoBackendRead
		WriteSuccess(
			w,
			r,
			http.StatusOK,
			emptyPage,
			BuildTruthEnvelope(h.profile(), importDependencyCapability, TruthBasisContentIndex, "the caller's grant admits no repository, so no backend was read"),
		)
		return
	}
	req.access = codeGrantAccessFilter(r.Context())

	data, err := h.importDependencyData(r.Context(), req)
	if err != nil {
		span.RecordError(err)
		if errors.Is(err, errImportDependencyUnavailable) {
			WriteError(w, http.StatusServiceUnavailable, err.Error())
			return
		}
		if errors.Is(err, errImportDependencyScopeTooBroad) {
			span.SetAttributes(attribute.Bool("eshu.import_dependencies.scan_overflow", true))
			WriteError(
				w,
				http.StatusUnprocessableEntity,
				fmt.Sprintf("%v; narrow the repository, file, or module scope", err),
			)
			return
		}
		if WriteGraphReadError(w, r, err, importDependencyCapability) {
			return
		}
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	span.SetAttributes(
		attribute.Int("eshu.import_dependencies.result_count", IntVal(data, "count")),
		attribute.Bool("eshu.import_dependencies.truncated", BoolVal(data, "truncated")),
		attribute.Bool("eshu.import_dependencies.scan_overflow", false),
	)
	WriteSuccess(
		w,
		r,
		http.StatusOK,
		data,
		BuildTruthEnvelope(h.profile(), importDependencyCapability, TruthBasisAuthoritativeGraph, "resolved from bounded graph import dependency lookup"),
	)
}

func (r importDependencyRequest) validate() error {
	if _, ok := importDependencyQueryTypes()[r.queryType()]; !ok {
		return fmt.Errorf("query_type must be one of: %s", strings.Join(importDependencyQueryTypeNames(), ", "))
	}
	if r.Limit > importDependencyMaxLimit {
		return fmt.Errorf("limit must be <= 200")
	}
	if r.Limit < 0 {
		return fmt.Errorf("limit must be >= 0")
	}
	if r.Offset < 0 {
		return fmt.Errorf("offset must be >= 0")
	}
	if r.Offset > importDependencyMaxOffset {
		return fmt.Errorf("offset must be <= 10000")
	}
	if !r.hasScopeFilter() {
		return fmt.Errorf("one of repo_id, source_file, target_file, source_module, or target_module is required")
	}
	if strings.TrimSpace(r.TargetFile) != "" && r.queryType() != "file_import_cycles" && r.queryType() != "cross_module_calls" {
		return fmt.Errorf("target_file is supported only for file_import_cycles and cross_module_calls")
	}
	if r.queryType() == "file_import_cycles" {
		language := r.normalizedLanguage()
		if language != "" && language != "python" {
			return fmt.Errorf("file_import_cycles currently supports python module-name cycle detection")
		}
	}
	return nil
}

func (r importDependencyRequest) queryType() string {
	queryType := strings.ToLower(strings.TrimSpace(r.QueryType))
	if queryType == "" {
		return "imports_by_file"
	}
	return queryType
}

func (r importDependencyRequest) normalizedLanguage() string {
	return strings.ToLower(strings.TrimSpace(r.Language))
}

func (r importDependencyRequest) normalizedLimit() int {
	switch {
	case r.Limit <= 0:
		return importDependencyDefaultLimit
	case r.Limit > importDependencyMaxLimit:
		return importDependencyMaxLimit
	default:
		return r.Limit
	}
}

func (r importDependencyRequest) queryLimit() int {
	return r.normalizedLimit() + 1
}

func (r importDependencyRequest) hasScopeFilter() bool {
	for _, value := range []string{r.RepoID, r.SourceFile, r.TargetFile, r.SourceModule, r.TargetModule} {
		if strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}

func importDependencyQueryTypes() map[string]struct{} {
	return map[string]struct{}{
		"imports_by_file":     {},
		"importers":           {},
		"module_dependencies": {},
		"package_imports":     {},
		"file_import_cycles":  {},
		"cross_module_calls":  {},
	}
}

func importDependencyQueryTypeNames() []string {
	return []string{"imports_by_file", "importers", "module_dependencies", "package_imports", "file_import_cycles", "cross_module_calls"}
}

func (h *CodeHandler) importDependencyData(ctx context.Context, req importDependencyRequest) (map[string]any, error) {
	if h == nil || h.Neo4j == nil {
		return nil, errImportDependencyUnavailable
	}
	rows, err := h.importDependencyRows(ctx, req)
	if err != nil {
		return nil, err
	}
	return importDependencyResponse(req, rows), nil
}

func importDependencyParams(req importDependencyRequest) map[string]any {
	params := map[string]any{
		"limit":  req.queryLimit(),
		"offset": req.Offset,
	}
	if repoID := strings.TrimSpace(req.RepoID); repoID != "" {
		params["repo_id"] = repoID
	}
	if language := req.normalizedLanguage(); language != "" {
		params["language"] = language
	}
	params = req.access.GraphParams(params)
	if sourceFile := strings.TrimSpace(req.SourceFile); sourceFile != "" {
		params["source_file"] = sourceFile
	}
	if targetFile := strings.TrimSpace(req.TargetFile); targetFile != "" {
		params["target_file"] = targetFile
	}
	if sourceModule := strings.TrimSpace(req.SourceModule); sourceModule != "" {
		params["source_module"] = sourceModule
	}
	if targetModule := strings.TrimSpace(req.TargetModule); targetModule != "" {
		params["target_module"] = targetModule
	}
	return params
}
