// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/queryspan"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func (h *ImpactHandler) developerChangePlan(w http.ResponseWriter, r *http.Request) {
	r, span := queryspan.StartHandlerSpanWith(queryspan.HandlerTracer(),
		r,
		telemetry.SpanQueryChangeSurfaceInvestigation,
		"POST /api/v0/impact/developer-change-plan",
		developerChangePlanCapability,
	)
	defer span.End()

	if querycontract.CapabilityUnsupported(h.profile(), developerChangePlanCapability) {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"developer change plan requires authoritative platform truth",
			querycontract.ErrorCodeUnsupportedCapability,
			developerChangePlanCapability,
			h.profile(),
			querycontract.RequiredProfile(developerChangePlanCapability),
		)
		return
	}

	var req preChangeImpactRequest
	if err := querycontract.ReadJSON(r, &req); err != nil {
		querycontract.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	normalized, err := normalizePreChangeImpactRequest(req)
	if err != nil {
		querycontract.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	impactData, err := h.PreChangeImpactResponse(r, normalized)
	if err != nil {
		if querycontract.WriteGraphReadError(w, r, err, developerChangePlanCapability) {
			return
		}
		querycontract.WriteError(w, preChangeImpactErrorStatus(err), err.Error())
		return
	}
	truth := querycontract.BuildTruthEnvelope(
		h.profile(),
		developerChangePlanCapability,
		querycontract.TruthBasisHybrid,
		"planned from normalized changed-file input and bounded pre-change impact evidence",
	)
	data := developerChangePlanData(normalized, impactData)
	querycontract.WriteSuccess(w, r, http.StatusOK, AttachDeveloperChangePlanPacket(data, developerPlanSummary(data), truth), truth)
}

func developerChangePlanData(req preChangeImpactRequest, impactData map[string]any) map[string]any {
	data := map[string]any{
		"schema_version":        "developer_change_plan.v1",
		"workflow":              "developer_change_plan",
		"read_only":             true,
		"developer_intent":      req.DeveloperIntent,
		"change_set":            querycontract.MapValue(impactData, "change_set"),
		"changed_files":         querycontract.MapSliceValue(impactData, "changed_files"),
		"changed_file_count":    querycontract.IntVal(impactData, "changed_file_count"),
		"coverage":              querycontract.MapValue(impactData, "coverage"),
		"missing_evidence":      querycontract.MapSliceValue(impactData, "missing_evidence"),
		"affected_entities":     developerPlanAffectedEntities(impactData),
		"recommended_tests":     developerPlanTests(impactData),
		"bounded_next_calls":    developerPlanNextCalls(req, impactData),
		"pre_change_summary":    preChangeSummary(impactData),
		"pre_change_impact_ref": "eshu://api-result/impact/pre-change",
	}
	data["actions"] = developerPlanActions(req, data)
	data["patch_guidance"] = developerPlanPatchGuidance(data)
	data["blocked"] = len(querycontract.MapSliceValue(data, "missing_evidence")) > 0
	data["truncated"] = querycontract.BoolVal(impactData, "truncated")
	return querycontract.AttachAnswerMetadata(data)
}

func developerPlanActions(req preChangeImpactRequest, plan map[string]any) []map[string]any {
	actions := []map[string]any{{
		"order":             1,
		"kind":              "inspect_changed_symbols",
		"risk":              "medium",
		"title":             "Inspect changed symbols before editing",
		"rationale":         "Start from bounded content evidence so patch scope follows indexed symbols.",
		"affected_entities": querycontract.MapSliceValue(plan, "affected_entities"),
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
	for _, file := range querycontract.MapSliceValue(plan, "changed_files") {
		status := querycontract.StringVal(file, "status")
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
	if len(querycontract.MapSliceValue(plan, "missing_evidence")) > 0 {
		actions = append(actions, map[string]any{
			"order":            len(actions) + 1,
			"kind":             "block_unsafe_recommendation",
			"risk":             "high",
			"title":            "Block unsafe patch guidance until missing evidence is resolved",
			"rationale":        "Missing, stale, or deleted-path evidence can hide affected symbols or owners.",
			"missing_evidence": querycontract.MapSliceValue(plan, "missing_evidence"),
			"follow_up_calls":  []string{"get_generation_lifecycle", "eshu change impact"},
		})
	}
	actions = append(actions, map[string]any{
		"order":             len(actions) + 1,
		"kind":              "run_recommended_tests",
		"risk":              "medium",
		"title":             "Run the focused verification ladder",
		"rationale":         "Use tests after evidence gaps are handled so correctness proof follows graph-aware scope.",
		"recommended_tests": querycontract.MapSliceValue(plan, "recommended_tests"),
	})
	return actions
}

func developerPlanAffectedEntities(impactData map[string]any) []map[string]any {
	symbols := querycontract.MapSliceValue(querycontract.MapValue(impactData, "code_surface"), "touched_symbols")
	entities := make([]map[string]any, 0, len(symbols))
	for _, symbol := range symbols {
		entities = append(entities, map[string]any{
			"kind":          querycontract.StringVal(symbol, "entity_type"),
			"name":          querycontract.StringVal(symbol, "name"),
			"relative_path": querycontract.StringVal(symbol, "relative_path"),
			"language":      querycontract.StringVal(symbol, "language"),
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
		switch strings.ToLower(querycontract.StringVal(entity, "language")) {
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
	if len(querycontract.MapSliceValue(impactData, "missing_evidence")) > 0 {
		calls = append(calls, map[string]any{
			"kind":   "mcp",
			"target": "get_generation_lifecycle",
			"reason": "check freshness and prior-generation evidence before patching",
		})
	}
	return calls
}

func developerPlanPatchGuidance(plan map[string]any) []map[string]any {
	guidance := make([]map[string]any, 0, len(querycontract.MapSliceValue(plan, "changed_files")))
	for _, file := range querycontract.MapSliceValue(plan, "changed_files") {
		status := querycontract.StringVal(file, "status")
		row := map[string]any{
			"status":        status,
			"relative_path": querycontract.StringVal(file, "path"),
			"guidance":      developerPlanPatchGuidanceText(status),
			"safe":          status != "deleted" && len(querycontract.MapSliceValue(plan, "missing_evidence")) == 0,
		}
		if oldPath := querycontract.StringVal(file, "old_path"); oldPath != "" {
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

// AttachDeveloperChangePlanPacket attaches the developer-change-plan answer
// packet to an already-built plan payload. It is a variable (not a method)
// because packet composition names the root AnswerPacket cluster, which
// cannot cross into this package: package query assigns the production
// implementation at init, so the cmd wirings keep working unchanged, and
// tests in this package inject fakes or exercise data shaping only. It
// defaults to identity so a handler built without wiring still returns the
// shaped data. See #6060.
var AttachDeveloperChangePlanPacket = func(data map[string]any, summary string, truth *querycontract.TruthEnvelope) map[string]any {
	return data
}

func developerPlanSummary(data map[string]any) string {
	return fmt.Sprintf(
		"Built %d read-only action(s) for %d changed file(s), with blocked=%t and missing_evidence=%d.",
		len(querycontract.MapSliceValue(data, "actions")),
		querycontract.IntVal(data, "changed_file_count"),
		querycontract.BoolVal(data, "blocked"),
		len(querycontract.MapSliceValue(data, "missing_evidence")),
	)
}
