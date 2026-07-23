// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"net/http"
	"path"
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	preChangeImpactCapability     = "platform_impact.pre_change"
	developerChangePlanCapability = "platform_impact.developer_change_plan"
)

type preChangeImpactRequest struct {
	RepoID          string                `json:"repo_id"`
	DeveloperIntent string                `json:"developer_intent"`
	BaseRef         string                `json:"base_ref"`
	HeadRef         string                `json:"head_ref"`
	ChangedPaths    []string              `json:"changed_paths"`
	Changes         []preChangeFileChange `json:"changes"`
	Target          string                `json:"target"`
	TargetType      string                `json:"target_type"`
	ServiceName     string                `json:"service_name"`
	WorkloadID      string                `json:"workload_id"`
	ResourceID      string                `json:"resource_id"`
	ModuleID        string                `json:"module_id"`
	Topic           string                `json:"topic"`
	Query           string                `json:"query"`
	Environment     string                `json:"environment"`
	MaxDepth        int                   `json:"max_depth"`
	Limit           int                   `json:"limit"`
	Offset          int                   `json:"offset"`

	changedPathsProvided bool
	changesProvided      bool
}

type preChangeFileChange struct {
	Path    string `json:"path"`
	OldPath string `json:"old_path"`
	Status  string `json:"status"`
}

func (h *ImpactHandler) preChangeImpact(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryChangeSurfaceInvestigation,
		"POST /api/v0/impact/pre-change",
		preChangeImpactCapability,
	)
	defer span.End()

	if capabilityUnsupported(h.profile(), preChangeImpactCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"pre-change impact requires authoritative platform truth",
			ErrorCodeUnsupportedCapability,
			preChangeImpactCapability,
			h.profile(),
			requiredProfile(preChangeImpactCapability),
		)
		return
	}

	var req preChangeImpactRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	normalized, err := normalizePreChangeImpactRequest(req)
	if err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	resp, err := h.preChangeImpactResponse(r, normalized)
	if err != nil {
		if WriteGraphReadError(w, r, err, preChangeImpactCapability) {
			return
		}
		WriteError(w, preChangeImpactErrorStatus(err), err.Error())
		return
	}
	truth := BuildTruthEnvelope(
		h.profile(),
		preChangeImpactCapability,
		TruthBasisHybrid,
		"resolved from normalized changed-file input and bounded change-surface evidence",
	)
	WriteSuccess(w, r, http.StatusOK, preChangeImpactAnswerData(resp, truth), truth)
}

func normalizePreChangeImpactRequest(req preChangeImpactRequest) (preChangeImpactRequest, error) {
	req.RepoID = strings.TrimSpace(req.RepoID)
	req.DeveloperIntent = strings.TrimSpace(req.DeveloperIntent)
	req.BaseRef = strings.TrimSpace(req.BaseRef)
	req.HeadRef = strings.TrimSpace(req.HeadRef)
	req.Target = strings.TrimSpace(req.Target)
	req.TargetType = normalizeChangeSurfaceTargetType(req.TargetType)
	req.ServiceName = strings.TrimSpace(req.ServiceName)
	req.WorkloadID = strings.TrimSpace(req.WorkloadID)
	req.ResourceID = strings.TrimSpace(req.ResourceID)
	req.ModuleID = strings.TrimSpace(req.ModuleID)
	req.Topic = strings.TrimSpace(firstNonEmptyString(req.Topic, req.Query))
	req.Query = ""
	req.Environment = strings.TrimSpace(req.Environment)
	if req.Limit <= 0 {
		req.Limit = changeSurfaceInvestigationDefaultLimit
	}
	if req.Limit > changeSurfaceInvestigationMaxLimit {
		req.Limit = changeSurfaceInvestigationMaxLimit
	}
	if req.Offset < 0 {
		return preChangeImpactRequest{}, fmt.Errorf("offset must be >= 0")
	}
	if req.Offset > changeSurfaceInvestigationMaxOffset {
		return preChangeImpactRequest{}, fmt.Errorf("offset must be <= %d", changeSurfaceInvestigationMaxOffset)
	}
	if req.MaxDepth <= 0 {
		req.MaxDepth = changeSurfaceInvestigationDefaultDepth
	}
	if req.MaxDepth > changeSurfaceInvestigationMaxDepth {
		req.MaxDepth = changeSurfaceInvestigationMaxDepth
	}
	req.Changes = normalizePreChangeFileChanges(req.Changes, req.ChangedPaths)
	for i := range req.Changes {
		normalizedPath, err := normalizePreChangePath(req.Changes[i].Path)
		if err != nil {
			return preChangeImpactRequest{}, err
		}
		req.Changes[i].Path = normalizedPath
		if req.Changes[i].OldPath != "" {
			normalizedOldPath, err := normalizePreChangePath(req.Changes[i].OldPath)
			if err != nil {
				return preChangeImpactRequest{}, err
			}
			req.Changes[i].OldPath = normalizedOldPath
		}
	}
	req.Changes = dedupePreChangeFileChanges(req.Changes)
	req.ChangedPaths = preChangeCurrentPaths(req.Changes)
	if len(req.ChangedPaths) > 0 && req.RepoID == "" {
		return preChangeImpactRequest{}, fmt.Errorf("repo_id is required when changed files are provided")
	}
	if refsProvidedWithoutChangedInput(req) && req.Topic == "" && preChangeGraphTarget(req) == "" {
		return preChangeImpactRequest{}, fmt.Errorf("changed_paths or changes are required when refs are provided")
	}
	if len(req.ChangedPaths) == 0 && req.BaseRef == "" && req.HeadRef == "" && req.Topic == "" && preChangeGraphTarget(req) == "" {
		return preChangeImpactRequest{}, fmt.Errorf("changed files, refs, topic, or target is required")
	}
	return req, nil
}

func refsProvidedWithoutChangedInput(req preChangeImpactRequest) bool {
	return (req.BaseRef != "" || req.HeadRef != "") &&
		len(req.Changes) == 0 &&
		!req.changedPathsProvided &&
		!req.changesProvided
}

func normalizePreChangeFileChanges(changes []preChangeFileChange, paths []string) []preChangeFileChange {
	out := make([]preChangeFileChange, 0, len(changes)+len(paths))
	seen := map[string]struct{}{}
	for _, path := range normalizeChangedPaths(paths) {
		change := preChangeFileChange{Path: path, Status: "modified"}
		key := preChangeFileKey(change)
		seen[key] = struct{}{}
		out = append(out, change)
	}
	for _, change := range changes {
		change.Path = strings.TrimSpace(change.Path)
		change.OldPath = strings.TrimSpace(change.OldPath)
		change.Status = normalizePreChangeStatus(change.Status)
		if change.Path == "" && change.OldPath != "" {
			change.Path = change.OldPath
		}
		if change.Path == "" {
			continue
		}
		key := preChangeFileKey(change)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, change)
	}
	slices.SortFunc(out, func(a, b preChangeFileChange) int {
		return strings.Compare(preChangeFileKey(a), preChangeFileKey(b))
	})
	return out
}

func dedupePreChangeFileChanges(changes []preChangeFileChange) []preChangeFileChange {
	out := make([]preChangeFileChange, 0, len(changes))
	seen := map[string]struct{}{}
	for _, change := range changes {
		key := preChangeFileKey(change)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, change)
	}
	slices.SortFunc(out, func(a, b preChangeFileChange) int {
		return strings.Compare(preChangeFileKey(a), preChangeFileKey(b))
	})
	return out
}

func normalizePreChangePath(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("changed file path is required")
	}
	if strings.HasPrefix(raw, "/") {
		return "", fmt.Errorf("changed file path %q must be repo-relative", raw)
	}
	for _, segment := range strings.Split(raw, "/") {
		if segment == ".." {
			return "", fmt.Errorf("changed file path %q must not contain parent traversal", raw)
		}
	}
	cleaned := path.Clean(raw)
	if cleaned == "." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("changed file path %q must be repo-relative", raw)
	}
	return cleaned, nil
}

func normalizePreChangeStatus(status string) string {
	status = strings.ToLower(strings.TrimSpace(status))
	switch {
	case status == "", status == "m", status == "modified", status == "changed":
		return "modified"
	case status == "a", status == "added", status == "add":
		return "added"
	case status == "d", status == "deleted", status == "delete", status == "removed":
		return "deleted"
	case status == "r", strings.HasPrefix(status, "r"), status == "renamed", status == "rename":
		return "renamed"
	case status == "c", strings.HasPrefix(status, "c"), status == "copied", status == "copy":
		return "copied"
	default:
		return status
	}
}

func preChangeFileKey(change preChangeFileChange) string {
	return strings.Join([]string{change.Path, change.OldPath, change.Status}, "\x00")
}

func preChangeCurrentPaths(changes []preChangeFileChange) []string {
	paths := make([]string, 0, len(changes))
	for _, change := range changes {
		if change.Path != "" {
			paths = append(paths, change.Path)
		}
	}
	return normalizeChangedPaths(paths)
}

func preChangeGraphTarget(req preChangeImpactRequest) string {
	switch {
	case req.ServiceName != "":
		return req.ServiceName
	case req.WorkloadID != "":
		return req.WorkloadID
	case req.ResourceID != "":
		return req.ResourceID
	case req.ModuleID != "":
		return req.ModuleID
	default:
		return req.Target
	}
}

func (h *ImpactHandler) preChangeImpactResponse(
	r *http.Request,
	req preChangeImpactRequest,
) (map[string]any, error) {
	changeReq := preChangeAsChangeSurfaceRequest(req)
	codeSurface, err := h.changeSurfaceCodeSurface(r.Context(), changeReq)
	if err != nil {
		return nil, preChangeCodeSurfaceError{err: err}
	}

	var selected *changeSurfaceTargetCandidate
	resolution := changeSurfaceNoTargetResolution(changeReq)
	if changeReq.graphTarget() != "" {
		selected, resolution, err = h.resolveChangeSurfaceTarget(r.Context(), changeReq)
		if err != nil {
			return nil, err
		}
	}

	impactRows := []map[string]any(nil)
	graphTruncated := false
	if selected != nil {
		impactRows, graphTruncated, err = h.changeSurfaceImpactRows(r.Context(), changeReq, *selected)
		if err != nil {
			return nil, err
		}
	}
	surface := h.changeSurfaceResponse(changeReq, resolution, codeSurface, impactRows, graphTruncated)
	return preChangeImpactData(req, surface), nil
}

func preChangeAsChangeSurfaceRequest(req preChangeImpactRequest) changeSurfaceInvestigationRequest {
	return changeSurfaceInvestigationRequest{
		Target:       req.Target,
		TargetType:   req.TargetType,
		ServiceName:  req.ServiceName,
		WorkloadID:   req.WorkloadID,
		ResourceID:   req.ResourceID,
		ModuleID:     req.ModuleID,
		RepoID:       req.RepoID,
		Topic:        req.Topic,
		ChangedPaths: append([]string(nil), req.ChangedPaths...),
		Environment:  req.Environment,
		MaxDepth:     req.MaxDepth,
		Limit:        req.Limit,
		Offset:       req.Offset,
	}
}

func preChangeImpactData(req preChangeImpactRequest, surface map[string]any) map[string]any {
	data := copyMap(surface)
	data["workflow"] = "pre_change_impact"
	data["change_set"] = map[string]any{
		"repo_id":  req.RepoID,
		"base_ref": req.BaseRef,
		"head_ref": req.HeadRef,
		"mode":     preChangeMode(req),
	}
	data["changed_files"] = preChangeFileMaps(req.RepoID, req.Changes)
	data["changed_file_count"] = len(req.Changes)
	data["missing_evidence"] = preChangeMissingEvidence(req, mapValue(surface, "code_surface"))
	data["coverage"] = preChangeCoverage(req, surface)
	data["truncated"] = boolMapValue(data["coverage"].(map[string]any), "truncated")
	data["recommended_next_calls"] = preChangeNextCalls(data)
	return attachAnswerMetadata(data)
}

func preChangeMode(req preChangeImpactRequest) string {
	switch {
	case req.BaseRef != "" || req.HeadRef != "":
		return "ref_diff"
	case len(req.Changes) > 0:
		return "file_list"
	default:
		return "target_or_topic"
	}
}

func preChangeFileMaps(repoID string, changes []preChangeFileChange) []map[string]any {
	files := make([]map[string]any, 0, len(changes))
	for _, change := range changes {
		row := map[string]any{
			"repo_id": repoID,
			"path":    change.Path,
			"status":  change.Status,
			"source_handle": map[string]any{
				"repo_id":       repoID,
				"relative_path": change.Path,
			},
		}
		if change.OldPath != "" {
			row["old_path"] = change.OldPath
		}
		files = append(files, row)
	}
	return files
}

func preChangeMissingEvidence(req preChangeImpactRequest, codeSurface map[string]any) []map[string]any {
	matched := map[string]struct{}{}
	for _, symbol := range mapSliceValue(codeSurface, "touched_symbols") {
		if path := StringVal(symbol, "relative_path"); path != "" {
			matched[path] = struct{}{}
		}
	}
	missing := make([]map[string]any, 0)
	for _, change := range req.Changes {
		if _, ok := matched[change.Path]; ok {
			continue
		}
		reason := "changed_path_no_symbol_evidence"
		if change.Status == "deleted" {
			reason = "deleted_path_requires_prior_generation"
		}
		missing = append(missing, map[string]any{
			"kind":          "file",
			"repo_id":       req.RepoID,
			"relative_path": change.Path,
			"status":        change.Status,
			"reason":        reason,
		})
	}
	return missing
}

func preChangeCoverage(req preChangeImpactRequest, surface map[string]any) map[string]any {
	coverage := copyMap(mapValue(surface, "coverage"))
	coverage["query_shape"] = "pre_change_impact_over_change_surface"
	coverage["changed_file_count"] = len(req.Changes)
	coverage["changed_path_count"] = len(req.ChangedPaths)
	coverage["truncated"] = BoolVal(surface, "truncated")
	if len(req.Changes) == 0 {
		coverage["state"] = "empty_diff"
	} else if len(preChangeMissingEvidence(req, mapValue(surface, "code_surface"))) > 0 {
		coverage["state"] = "partial"
	} else {
		coverage["state"] = "supported"
	}
	return coverage
}

func preChangeNextCalls(data map[string]any) []map[string]any {
	calls := mapSliceValue(data, "recommended_next_calls")
	if len(calls) == 0 && IntVal(data, "changed_file_count") > 0 {
		calls = append(calls, map[string]any{
			"tool":   "investigate_change_surface",
			"reason": "inspect the normalized changed-file surface",
		})
	}
	if len(mapSliceValue(data, "missing_evidence")) > 0 {
		calls = append(calls, map[string]any{
			"tool":   "get_generation_lifecycle",
			"reason": "check generation freshness for missing or deleted path evidence",
		})
	}
	return calls
}

func preChangeImpactAnswerData(data map[string]any, truth *TruthEnvelope) map[string]any {
	metadata, _ := AnswerMetadataFromData(data)
	envelope := &ResponseEnvelope{Data: data, Truth: truth, Error: nil}
	data["answer_packet"] = NewAnswerPacketFromMetadata(AnswerPacketInput{
		PromptFamily: "pre_change.impact",
		Question:     "What does this change affect, what evidence proves it, and what should I inspect next?",
		PrimaryTool:  "analyze_pre_change_impact",
		PrimaryRoute: "/api/v0/impact/pre-change",
		Summary:      preChangeSummary(data),
		ResultRef:    "eshu://api-result/impact/pre-change",
		Envelope:     envelope,
	}, metadata)
	return data
}

func preChangeSummary(data map[string]any) string {
	summary := mapValue(data, "impact_summary")
	return fmt.Sprintf(
		"Mapped %d changed file(s) to %d touched symbol(s), %d direct impact row(s), and %d transitive impact row(s).",
		IntVal(data, "changed_file_count"),
		IntVal(mapValue(data, "code_surface"), "symbol_count"),
		IntVal(summary, "direct_count"),
		IntVal(summary, "transitive_count"),
	)
}
