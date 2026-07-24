// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func (h *ImpactHandler) developerChangePlan(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryChangeSurfaceInvestigation,
		"POST /api/v0/impact/developer-change-plan",
		developerChangePlanCapability,
	)
	defer span.End()

	if capabilityUnsupported(h.profile(), developerChangePlanCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"developer change plan requires authoritative platform truth",
			ErrorCodeUnsupportedCapability,
			developerChangePlanCapability,
			h.profile(),
			requiredProfile(developerChangePlanCapability),
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
	impactData, err := h.preChangeImpactResponse(r, normalized)
	if err != nil {
		if WriteGraphReadError(w, r, err, developerChangePlanCapability) {
			return
		}
		WriteError(w, preChangeImpactErrorStatus(err), err.Error())
		return
	}
	truth := BuildTruthEnvelope(
		h.profile(),
		developerChangePlanCapability,
		TruthBasisHybrid,
		"planned from normalized changed-file input and bounded pre-change impact evidence",
	)
	WriteSuccess(w, r, http.StatusOK, developerChangePlanAnswerData(developerChangePlanData(normalized, impactData), truth), truth)
}

func developerChangePlanData(req preChangeImpactRequest, impactData map[string]any) map[string]any {
	data := map[string]any{
		"schema_version":        "developer_change_plan.v1",
		"workflow":              "developer_change_plan",
		"read_only":             true,
		"developer_intent":      req.DeveloperIntent,
		"change_set":            mapValue(impactData, "change_set"),
		"changed_files":         mapSliceValue(impactData, "changed_files"),
		"changed_file_count":    IntVal(impactData, "changed_file_count"),
		"coverage":              mapValue(impactData, "coverage"),
		"missing_evidence":      mapSliceValue(impactData, "missing_evidence"),
		"affected_entities":     developerPlanAffectedEntities(impactData),
		"recommended_tests":     developerPlanTests(impactData),
		"bounded_next_calls":    developerPlanNextCalls(req, impactData),
		"pre_change_summary":    preChangeSummary(impactData),
		"pre_change_impact_ref": "eshu://api-result/impact/pre-change",
	}
	data["actions"] = developerPlanActions(req, data)
	data["patch_guidance"] = developerPlanPatchGuidance(data)
	data["blocked"] = len(mapSliceValue(data, "missing_evidence")) > 0
	data["truncated"] = BoolVal(impactData, "truncated")
	return attachAnswerMetadata(data)
}

func developerPlanActions(req preChangeImpactRequest, plan map[string]any) []map[string]any {
	actions := []map[string]any{{
		"order":             1,
		"kind":              "inspect_changed_symbols",
		"risk":              "medium",
		"title":             "Inspect changed symbols before editing",
		"rationale":         "Start from bounded content evidence so patch scope follows indexed symbols.",
		"affected_entities": mapSliceValue(plan, "affected_entities"),
		"follow_up_calls":   []string{"POST /api/v0/impact/pre-change"},
	}}
	if strings.TrimSpace(req.DeveloperIntent) != "" {
		actions = append(actions, map[string]any{
			"order":     len(actions) + 1,
			"kind":      "confirm_developer_intent",
			"risk":      "low",
			"title":     "Confirm the requested intent matches the affected files",
			"rationale": req.DeveloperIntent,
		})
	}
	for _, file := range mapSliceValue(plan, "changed_files") {
		status := StringVal(file, "status")
		if status == "renamed" || status == "copied" {
			actions = append(actions, map[string]any{
				"order":     len(actions) + 1,
				"kind":      "rename_safety_check",
				"risk":      "high",
				"title":     "Verify old and new path evidence before refactor guidance",
				"rationale": "Renamed and copied files preserve old_path but current impact evidence resolves only the new path.",
				"file":      file,
			})
		}
	}
	if len(mapSliceValue(plan, "missing_evidence")) > 0 {
		actions = append(actions, map[string]any{
			"order":            len(actions) + 1,
			"kind":             "block_unsafe_recommendation",
			"risk":             "high",
			"title":            "Block unsafe patch guidance until missing evidence is resolved",
			"rationale":        "Missing, stale, or deleted-path evidence can hide affected symbols or owners.",
			"missing_evidence": mapSliceValue(plan, "missing_evidence"),
			"follow_up_calls":  []string{"get_generation_lifecycle", "eshu change impact"},
		})
	}
	actions = append(actions, map[string]any{
		"order":             len(actions) + 1,
		"kind":              "run_recommended_tests",
		"risk":              "medium",
		"title":             "Run the focused verification ladder",
		"rationale":         "Use tests after evidence gaps are handled so correctness proof follows graph-aware scope.",
		"recommended_tests": mapSliceValue(plan, "recommended_tests"),
	})
	return actions
}

func developerPlanAffectedEntities(impactData map[string]any) []map[string]any {
	symbols := mapSliceValue(mapValue(impactData, "code_surface"), "touched_symbols")
	entities := make([]map[string]any, 0, len(symbols))
	for _, symbol := range symbols {
		entities = append(entities, map[string]any{
			"kind":          StringVal(symbol, "entity_type"),
			"name":          StringVal(symbol, "name"),
			"relative_path": StringVal(symbol, "relative_path"),
			"language":      StringVal(symbol, "language"),
		})
	}
	return entities
}

func developerPlanTests(impactData map[string]any) []map[string]any {
	tests := []map[string]any{{
		"command": "git diff --check",
		"reason":  "repository hygiene before review",
	}}
	seen := map[string]struct{}{"git diff --check": {}}
	for _, entity := range developerPlanAffectedEntities(impactData) {
		switch strings.ToLower(StringVal(entity, "language")) {
		case "go":
			addDeveloperPlanTest(&tests, seen, "cd go && go test ./... -count=1", "Go symbol changed")
		case "typescript", "javascript", "tsx", "jsx":
			addDeveloperPlanTest(&tests, seen, "npm test", "frontend symbol changed")
		}
	}
	if len(tests) == 1 {
		addDeveloperPlanTest(&tests, seen, "rerun the focused package or route gate for the touched surface", "no language-specific test was inferred")
	}
	return tests
}

func addDeveloperPlanTest(tests *[]map[string]any, seen map[string]struct{}, command string, reason string) {
	if _, ok := seen[command]; ok {
		return
	}
	seen[command] = struct{}{}
	*tests = append(*tests, map[string]any{"command": command, "reason": reason})
}

func developerPlanNextCalls(req preChangeImpactRequest, impactData map[string]any) []map[string]any {
	calls := []map[string]any{
		{"kind": "api", "target": "POST /api/v0/impact/pre-change", "reason": "regenerate bounded impact evidence"},
		{"kind": "cli", "target": "eshu change impact", "reason": "re-run local diff mapping from the CLI"},
		{"kind": "mcp", "target": "analyze_pre_change_impact", "reason": "let an agent inspect the same bounded evidence"},
	}
	if preChangeGraphTarget(req) != "" {
		calls = append(calls, map[string]any{
			"kind":   "api",
			"target": "POST /api/v0/impact/change-surface/investigate",
			"reason": "drill into target-specific code and graph surface",
		})
	}
	if len(mapSliceValue(impactData, "missing_evidence")) > 0 {
		calls = append(calls, map[string]any{
			"kind":   "mcp",
			"target": "get_generation_lifecycle",
			"reason": "check freshness and prior-generation evidence before patching",
		})
	}
	return calls
}

func developerPlanPatchGuidance(plan map[string]any) []map[string]any {
	guidance := make([]map[string]any, 0, len(mapSliceValue(plan, "changed_files")))
	for _, file := range mapSliceValue(plan, "changed_files") {
		status := StringVal(file, "status")
		row := map[string]any{
			"status":        status,
			"relative_path": StringVal(file, "path"),
			"guidance":      developerPlanPatchGuidanceText(status),
			"safe":          status != "deleted" && len(mapSliceValue(plan, "missing_evidence")) == 0,
		}
		if oldPath := StringVal(file, "old_path"); oldPath != "" {
			row["old_path"] = oldPath
		}
		guidance = append(guidance, row)
	}
	return guidance
}

func developerPlanPatchGuidanceText(status string) string {
	switch status {
	case "renamed", "copied":
		return "Treat old and new paths as separate evidence anchors; inspect old_path before broad refactor guidance."
	case "deleted":
		return "Do not infer deleted symbol impact without prior-generation evidence."
	case "added":
		return "Verify new symbols have owners, tests, and bounded follow-up calls before expanding scope."
	default:
		return "Patch the indexed symbol in place and keep verification scoped to affected packages first."
	}
}

func developerChangePlanAnswerData(data map[string]any, truth *TruthEnvelope) map[string]any {
	metadata, _ := AnswerMetadataFromData(data)
	envelope := &ResponseEnvelope{Data: data, Truth: truth, Error: nil}
	data["answer_packet"] = NewAnswerPacketFromMetadata(AnswerPacketInput{
		PromptFamily: "developer.change_plan",
		Question:     "What safe, read-only action plan should guide this change?",
		PrimaryTool:  "plan_developer_change",
		PrimaryRoute: "/api/v0/impact/developer-change-plan",
		Summary:      developerPlanSummary(data),
		ResultRef:    "eshu://api-result/impact/developer-change-plan",
		Envelope:     envelope,
	}, metadata)
	return data
}

func developerPlanSummary(data map[string]any) string {
	return fmt.Sprintf(
		"Built %d read-only action(s) for %d changed file(s), with blocked=%t and missing_evidence=%d.",
		len(mapSliceValue(data, "actions")),
		IntVal(data, "changed_file_count"),
		BoolVal(data, "blocked"),
		len(mapSliceValue(data, "missing_evidence")),
	)
}
