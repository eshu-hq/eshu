// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
)

const (
	codeFlowTaintPathCapability   = "code_flow.taint_path"
	codeFlowReachingDefCapability = "code_flow.reaching_def"
	codeFlowCFGSummaryCapability  = "code_flow.cfg_summary"
	codeFlowPDGSummaryCapability  = "code_flow.pdg_summary"

	codeFlowDefaultLimit = 25
	codeFlowMaxLimit     = 100
)

// CodeFlowKind selects one bounded code-flow read surface.
type CodeFlowKind string

const (
	CodeFlowKindTaintPath   CodeFlowKind = "taint_path"
	CodeFlowKindReachingDef CodeFlowKind = "reaching_def"
	CodeFlowKindCFGSummary  CodeFlowKind = "cfg_summary"
	CodeFlowKindPDGSummary  CodeFlowKind = "pdg_summary"
)

// CodeFlowStore loads bounded active-generation parser and reducer evidence for
// API/MCP code-flow readbacks.
type CodeFlowStore interface {
	ListCodeFlow(context.Context, CodeFlowFilter) (CodeFlowReadModel, error)
}

// CodeFlowFilter is the scoped query contract passed to the code-flow store.
type CodeFlowFilter struct {
	Kind     CodeFlowKind
	RepoID   string
	Language string
	Symbol   string
	FilePath string
	Line     int
	Limit    int
}

// CodeFlowReadModel is the store-neutral active-generation code-flow snapshot.
type CodeFlowReadModel struct {
	Functions       []CodeFlowFunction
	TaintPaths      []CodeFlowTaintPath
	Freshness       FreshnessState
	FreshnessDetail string
}

// CodeFlowFunction is one exact parser dataflow record for a function.
type CodeFlowFunction struct {
	RepoID              string
	RelativePath        string
	FunctionName        string
	FunctionUID         string
	Language            string
	LineNumber          int
	CFGBlocks           []any
	CFGEdges            []any
	DefUse              []map[string]any
	ControlDependencies []map[string]any
	Overflow            bool
	OverflowReason      string
	EvidenceHandle      string
	SourceGenerationID  string
	SourceObservedAt    time.Time
}

// CodeFlowTaintPath is one reducer-owned taint evidence path.
type CodeFlowTaintPath struct {
	RepoID             string
	RelativePath       string
	FunctionName       string
	Language           string
	SourceKind         string
	SinkKind           string
	SourceLine         int
	SinkLine           int
	Confidence         float64
	EvidenceHandle     string
	SourceGenerationID string
	SourceObservedAt   time.Time
}

type codeFlowRequest struct {
	RepoID   string `json:"repo_id"`
	Language string `json:"language"`
	Symbol   string `json:"symbol"`
	FilePath string `json:"file_path"`
	Line     int    `json:"line"`
	Limit    int    `json:"limit"`
}

func (h *CodeHandler) handleTaintPath(w http.ResponseWriter, r *http.Request) {
	h.handleCodeFlow(w, r, CodeFlowKindTaintPath, codeFlowTaintPathCapability)
}

func (h *CodeHandler) handleReachingDef(w http.ResponseWriter, r *http.Request) {
	h.handleCodeFlow(w, r, CodeFlowKindReachingDef, codeFlowReachingDefCapability)
}

func (h *CodeHandler) handleCFGSummary(w http.ResponseWriter, r *http.Request) {
	h.handleCodeFlow(w, r, CodeFlowKindCFGSummary, codeFlowCFGSummaryCapability)
}

func (h *CodeHandler) handlePDGSummary(w http.ResponseWriter, r *http.Request) {
	h.handleCodeFlow(w, r, CodeFlowKindPDGSummary, codeFlowPDGSummaryCapability)
}

func (h *CodeHandler) handleCodeFlow(
	w http.ResponseWriter,
	r *http.Request,
	kind CodeFlowKind,
	capability string,
) {
	var req codeFlowRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if capabilityUnsupported(h.profile(), capability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"code-flow reads require a supported query profile",
			ErrorCodeUnsupportedCapability,
			capability,
			h.profile(),
			requiredProfile(capability),
		)
		return
	}
	req.normalize()
	if req.RepoID == "" {
		WriteError(w, http.StatusBadRequest, "repo_id is required")
		return
	}
	if !h.applyRepositorySelectorForCapability(w, r, &req.RepoID, capability) {
		return
	}
	if state, ok := unsupportedCodeFlowLanguage(req); ok {
		h.writeCodeFlowResponse(w, r, capability, kind, req, CodeFlowReadModel{}, codeFlowPayload{
			coverage: map[string]any{
				"state":               "unsupported",
				"language":            req.Language,
				"supported_languages": supportedCodeFlowLanguages(),
				"reason":              "no fixture-backed value-flow parser support is claimed for this language",
			},
			extra: state,
		})
		return
	}
	if h == nil || h.CodeFlow == nil {
		WriteError(w, http.StatusServiceUnavailable, "code-flow store is unavailable")
		return
	}

	filter := CodeFlowFilter{
		Kind:     kind,
		RepoID:   req.RepoID,
		Language: req.Language,
		Symbol:   req.Symbol,
		FilePath: req.FilePath,
		Line:     req.Line,
		Limit:    req.Limit + 1,
	}
	model, err := h.CodeFlow.ListCodeFlow(r.Context(), filter)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			WriteError(w, http.StatusRequestTimeout, err.Error())
			return
		}
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	payload := buildCodeFlowPayload(kind, req, model)
	h.writeCodeFlowResponse(w, r, capability, kind, req, model, payload)
}

type codeFlowPayload struct {
	coverage map[string]any
	extra    map[string]any
}

func buildCodeFlowPayload(kind CodeFlowKind, req codeFlowRequest, model CodeFlowReadModel) codeFlowPayload {
	truncated := false
	coverageState := "exact"
	coverageReason := "active parser dataflow facts matched the requested scope"
	functions := model.Functions
	paths := model.TaintPaths
	if len(functions) > req.Limit {
		truncated = true
		functions = functions[:req.Limit]
	}
	if len(paths) > req.Limit {
		truncated = true
		paths = paths[:req.Limit]
	}
	if model.Freshness == FreshnessStale || model.Freshness == FreshnessBuilding {
		coverageState = "partial"
		coverageReason = "active generation freshness is not fresh"
	}
	if kind == CodeFlowKindTaintPath {
		coverageState = "derived"
		coverageReason = "taint paths are reducer-owned evidence derived from parser value-flow facts"
	}
	if len(model.Functions) == 0 && len(model.TaintPaths) == 0 {
		coverageState = "partial"
		coverageReason = "no active code-flow evidence matched the requested scope"
	}
	ambiguity := codeFlowAmbiguity(functions, req)
	if BoolVal(ambiguity, "ambiguous") {
		coverageState = "partial"
		coverageReason = "symbol matched multiple function candidates"
	}

	extra := map[string]any{}
	switch kind {
	case CodeFlowKindTaintPath:
		extra["paths"] = codeFlowPathPayloads(paths)
	case CodeFlowKindReachingDef:
		extra["definitions"] = codeFlowReachingPayloads(functions)
	case CodeFlowKindCFGSummary:
		extra["functions"] = codeFlowFunctionPayloads(functions)
	case CodeFlowKindPDGSummary:
		extra["summaries"] = codeFlowPDGPayloads(functions)
	}
	extra["ambiguity"] = ambiguity
	extra["bounds"] = map[string]any{
		"limit":     req.Limit,
		"count":     codeFlowResultCount(kind, functions, paths),
		"truncated": truncated,
	}

	return codeFlowPayload{
		coverage: map[string]any{
			"state":    coverageState,
			"reason":   coverageReason,
			"language": req.Language,
		},
		extra: extra,
	}
}

func (h *CodeHandler) writeCodeFlowResponse(
	w http.ResponseWriter,
	r *http.Request,
	capability string,
	kind CodeFlowKind,
	req codeFlowRequest,
	model CodeFlowReadModel,
	payload codeFlowPayload,
) {
	data := map[string]any{
		"query": map[string]any{
			"kind":      string(kind),
			"repo_id":   req.RepoID,
			"language":  req.Language,
			"symbol":    req.Symbol,
			"file_path": req.FilePath,
			"line":      req.Line,
		},
		"coverage": payload.coverage,
	}
	for key, value := range payload.extra {
		data[key] = value
	}
	truth := BuildTruthEnvelope(
		h.profile(),
		capability,
		codeFlowTruthBasis(kind),
		"resolved from bounded code-flow parser facts and reducer-owned evidence",
	)
	if model.Freshness != "" && model.Freshness != FreshnessFresh {
		truth.Freshness.State = model.Freshness
		truth.Freshness.Detail = model.FreshnessDetail
	}
	WriteSuccess(w, r, http.StatusOK, data, truth)
}

func (r *codeFlowRequest) normalize() {
	r.RepoID = strings.TrimSpace(r.RepoID)
	r.Language = normalizeCodeFlowLanguage(r.Language)
	r.Symbol = strings.TrimSpace(r.Symbol)
	r.FilePath = strings.TrimSpace(r.FilePath)
	if r.Limit <= 0 {
		r.Limit = codeFlowDefaultLimit
	}
	if r.Limit > codeFlowMaxLimit {
		r.Limit = codeFlowMaxLimit
	}
	if r.Line < 0 {
		r.Line = 0
	}
}

func codeFlowFunctionPayloads(functions []CodeFlowFunction) []map[string]any {
	out := make([]map[string]any, 0, len(functions))
	for _, function := range functions {
		row := codeFlowFunctionBasePayload(function)
		row["fact_label"] = "exact_parser_fact"
		row["cfg"] = map[string]any{
			"blocks": function.CFGBlocks,
			"edges":  function.CFGEdges,
		}
		row["def_use_count"] = len(function.DefUse)
		row["control_dependence_count"] = len(function.ControlDependencies)
		row["overflow"] = function.Overflow
		if function.OverflowReason != "" {
			row["overflow_reason"] = function.OverflowReason
		}
		out = append(out, row)
	}
	return out
}

func codeFlowReachingPayloads(functions []CodeFlowFunction) []map[string]any {
	out := make([]map[string]any, 0, len(functions))
	for _, function := range functions {
		row := codeFlowFunctionBasePayload(function)
		row["fact_label"] = "exact_parser_fact"
		row["def_use"] = function.DefUse
		row["overflow"] = function.Overflow
		out = append(out, row)
	}
	return out
}

func codeFlowPDGPayloads(functions []CodeFlowFunction) []map[string]any {
	out := make([]map[string]any, 0, len(functions))
	for _, function := range functions {
		row := codeFlowFunctionBasePayload(function)
		row["fact_label"] = "partial_derived_summary"
		row["cfg_block_count"] = len(function.CFGBlocks)
		row["cfg_edge_count"] = len(function.CFGEdges)
		row["def_use_count"] = len(function.DefUse)
		row["control_dependence_count"] = len(function.ControlDependencies)
		row["coverage_state"] = "partial"
		if !function.Overflow {
			row["coverage_state"] = "derived"
		}
		out = append(out, row)
	}
	return out
}

func codeFlowPathPayloads(paths []CodeFlowTaintPath) []map[string]any {
	out := make([]map[string]any, 0, len(paths))
	for _, path := range paths {
		row := map[string]any{
			"repo_id":         path.RepoID,
			"relative_path":   path.RelativePath,
			"function_name":   path.FunctionName,
			"language":        path.Language,
			"source_kind":     path.SourceKind,
			"sink_kind":       path.SinkKind,
			"source_line":     path.SourceLine,
			"sink_line":       path.SinkLine,
			"confidence":      path.Confidence,
			"evidence_handle": path.EvidenceHandle,
			"fact_label":      "derived_reducer_evidence",
		}
		if path.SourceGenerationID != "" {
			row["generation_id"] = path.SourceGenerationID
		}
		if !path.SourceObservedAt.IsZero() {
			row["observed_at"] = path.SourceObservedAt.UTC().Format(time.RFC3339Nano)
		}
		out = append(out, row)
	}
	return out
}

func codeFlowFunctionBasePayload(function CodeFlowFunction) map[string]any {
	row := map[string]any{
		"repo_id":         function.RepoID,
		"relative_path":   function.RelativePath,
		"function_name":   function.FunctionName,
		"function_uid":    function.FunctionUID,
		"language":        function.Language,
		"line_number":     function.LineNumber,
		"evidence_handle": function.EvidenceHandle,
	}
	if function.SourceGenerationID != "" {
		row["generation_id"] = function.SourceGenerationID
	}
	if !function.SourceObservedAt.IsZero() {
		row["observed_at"] = function.SourceObservedAt.UTC().Format(time.RFC3339Nano)
	}
	return row
}

func codeFlowAmbiguity(functions []CodeFlowFunction, req codeFlowRequest) map[string]any {
	ambiguous := req.Symbol != "" && req.FilePath == "" && req.Line == 0 && len(functions) > 1
	candidates := make([]map[string]any, 0)
	if ambiguous {
		candidates = make([]map[string]any, 0, len(functions))
		for _, function := range functions {
			candidates = append(candidates, map[string]any{
				"relative_path": function.RelativePath,
				"function_name": function.FunctionName,
				"function_uid":  function.FunctionUID,
				"line_number":   function.LineNumber,
			})
		}
	}
	return map[string]any{
		"ambiguous":  ambiguous,
		"candidates": candidates,
	}
}

func codeFlowResultCount(kind CodeFlowKind, functions []CodeFlowFunction, paths []CodeFlowTaintPath) int {
	if kind == CodeFlowKindTaintPath {
		return len(paths)
	}
	return len(functions)
}

func codeFlowTruthBasis(kind CodeFlowKind) TruthBasis {
	if kind == CodeFlowKindTaintPath || kind == CodeFlowKindPDGSummary {
		return TruthBasisHybrid
	}
	return TruthBasisContentIndex
}

func unsupportedCodeFlowLanguage(req codeFlowRequest) (map[string]any, bool) {
	language := req.Language
	if language == "" {
		return nil, false
	}
	for _, supported := range supportedCodeFlowLanguages() {
		if language == supported {
			return nil, false
		}
	}
	return map[string]any{
		"functions":   []any{},
		"definitions": []any{},
		"paths":       []any{},
		"summaries":   []any{},
		"ambiguity":   map[string]any{"ambiguous": false, "candidates": []any{}},
		"bounds":      map[string]any{"limit": req.Limit, "count": 0, "truncated": false},
	}, true
}

func supportedCodeFlowLanguages() []string {
	return []string{"csharp", "go", "javascript", "python", "typescript"}
}

func normalizeCodeFlowLanguage(language string) string {
	language = strings.ToLower(strings.TrimSpace(language))
	switch language {
	case "c#", "cs":
		return "csharp"
	case "js":
		return "javascript"
	case "ts":
		return "typescript"
	default:
		return language
	}
}
