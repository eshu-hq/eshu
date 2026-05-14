package query

import (
	"fmt"
	"sort"
	"strings"
)

type environmentResourceComparison struct {
	shared         []map[string]any
	leftDedicated  []map[string]any
	rightDedicated []map[string]any
}

func environmentCompareResponse(
	req compareEnvironmentsRequest,
	workload map[string]any,
	leftSnap map[string]any,
	rightSnap map[string]any,
	changed []map[string]any,
	confidence float64,
	reason string,
	limit int,
	leftTruncated bool,
	rightTruncated bool,
) map[string]any {
	if changed == nil {
		changed = []map[string]any{}
	}
	comparison := compareEnvironmentResources(leftSnap, rightSnap, req.Left, req.Right)
	summary := environmentCompareSummary(workload, leftSnap, rightSnap, comparison, changed)
	coverage := environmentCompareCoverage(workload, leftSnap, rightSnap, limit, leftTruncated, rightTruncated)
	resp := map[string]any{
		"workload": workload,
		"left":     leftSnap,
		"right":    rightSnap,
		"changed": map[string]any{
			"cloud_resources": changed,
		},
		"confidence": confidence,
		"reason":     reason,
		"limit":      limit,
		"truncated":  leftTruncated || rightTruncated,
		"story":      environmentCompareStoryText(workload, req, summary),
		"summary":    summary,
		"shared": map[string]any{
			"cloud_resources": comparison.shared,
		},
		"dedicated": map[string]any{
			"left": map[string]any{
				"environment":     req.Left,
				"cloud_resources": comparison.leftDedicated,
			},
			"right": map[string]any{
				"environment":     req.Right,
				"cloud_resources": comparison.rightDedicated,
			},
		},
		"evidence":               environmentCompareEvidence(workload, leftSnap, rightSnap, comparison),
		"limitations":            environmentCompareLimitations(leftSnap, rightSnap, leftTruncated, rightTruncated),
		"recommended_next_calls": environmentCompareNextCalls(workload, req, comparison, leftTruncated || rightTruncated),
		"coverage":               coverage,
	}
	return resp
}

func missingEnvironmentSnapshot(environment string) map[string]any {
	return map[string]any{
		"environment":     environment,
		"status":          "missing",
		"instance":        nil,
		"cloud_resources": []map[string]any{},
		"provenance":      []map[string]any{},
		"reason":          "workload not found; environment comparison was not attempted",
	}
}

func compareEnvironmentResources(leftSnap, rightSnap map[string]any, leftEnv, rightEnv string) environmentResourceComparison {
	if compareStringVal(leftSnap, "status") != "present" || compareStringVal(rightSnap, "status") != "present" {
		return environmentResourceComparison{
			shared:         []map[string]any{},
			leftDedicated:  []map[string]any{},
			rightDedicated: []map[string]any{},
		}
	}

	leftByKey := resourceRowsByStoryKey(compareMapSlice(leftSnap, "cloud_resources"))
	rightByKey := resourceRowsByStoryKey(compareMapSlice(rightSnap, "cloud_resources"))
	result := environmentResourceComparison{
		shared:         make([]map[string]any, 0),
		leftDedicated:  make([]map[string]any, 0),
		rightDedicated: make([]map[string]any, 0),
	}

	for key, left := range leftByKey {
		if right, ok := rightByKey[key]; ok {
			result.shared = append(result.shared, sharedEnvironmentResource(left, right, leftEnv, rightEnv))
			continue
		}
		result.leftDedicated = append(result.leftDedicated, dedicatedEnvironmentResource(left, leftEnv))
	}
	for key, right := range rightByKey {
		if _, ok := leftByKey[key]; ok {
			continue
		}
		result.rightDedicated = append(result.rightDedicated, dedicatedEnvironmentResource(right, rightEnv))
	}

	sortStoryRows(result.shared)
	sortStoryRows(result.leftDedicated)
	sortStoryRows(result.rightDedicated)
	return result
}

func resourceRowsByStoryKey(rows []map[string]any) map[string]map[string]any {
	byKey := make(map[string]map[string]any, len(rows))
	for _, row := range rows {
		key := resourceStoryKey(row)
		if key == "" {
			continue
		}
		byKey[key] = row
	}
	return byKey
}

func resourceStoryKey(row map[string]any) string {
	if id := compareStringVal(row, "id"); id != "" {
		return "id:" + id
	}
	parts := []string{
		strings.ToLower(compareStringVal(row, "provider")),
		strings.ToLower(compareStringVal(row, "kind")),
		strings.ToLower(compareStringVal(row, "name")),
	}
	if strings.Join(parts, "|") == "||" {
		return ""
	}
	return "descriptor:" + strings.Join(parts, "|")
}

func sharedEnvironmentResource(left, right map[string]any, leftEnv, rightEnv string) map[string]any {
	return map[string]any{
		"id":                coalesceString(compareStringVal(left, "id"), compareStringVal(right, "id")),
		"name":              coalesceString(compareStringVal(left, "name"), compareStringVal(right, "name")),
		"kind":              coalesceString(compareStringVal(left, "kind"), compareStringVal(right, "kind")),
		"provider":          coalesceString(compareStringVal(left, "provider"), compareStringVal(right, "provider")),
		"left_environment":  leftEnv,
		"right_environment": rightEnv,
		"confidence":        maxCompareFloat(floatVal(left, "confidence"), floatVal(right, "confidence")),
		"reason":            coalesceString(compareStringVal(left, "reason"), compareStringVal(right, "reason")),
	}
}

func dedicatedEnvironmentResource(row map[string]any, environment string) map[string]any {
	return map[string]any{
		"id":          compareStringVal(row, "id"),
		"name":        compareStringVal(row, "name"),
		"kind":        compareStringVal(row, "kind"),
		"provider":    compareStringVal(row, "provider"),
		"environment": environment,
		"confidence":  floatVal(row, "confidence"),
		"reason":      compareStringVal(row, "reason"),
	}
}

func environmentCompareSummary(
	workload map[string]any,
	leftSnap map[string]any,
	rightSnap map[string]any,
	comparison environmentResourceComparison,
	changed []map[string]any,
) map[string]any {
	comparisonState := "identical"
	leftStatus := compareStringVal(leftSnap, "status")
	rightStatus := compareStringVal(rightSnap, "status")
	switch {
	case workload == nil:
		comparisonState = "missing_workload"
	case leftStatus == "unsupported" || rightStatus == "unsupported" || leftStatus == "missing" || rightStatus == "missing":
		comparisonState = "unsupported"
	case leftStatus != "present" || rightStatus != "present":
		comparisonState = "partial"
	case len(changed) > 0 || len(comparison.leftDedicated) > 0 || len(comparison.rightDedicated) > 0:
		comparisonState = "different"
	}
	return map[string]any{
		"comparison":                     comparisonState,
		"left_status":                    leftStatus,
		"right_status":                   rightStatus,
		"shared_resource_count":          len(comparison.shared),
		"left_dedicated_resource_count":  len(comparison.leftDedicated),
		"right_dedicated_resource_count": len(comparison.rightDedicated),
		"changed_resource_count":         len(changed),
	}
}

func environmentCompareCoverage(
	workload map[string]any,
	leftSnap map[string]any,
	rightSnap map[string]any,
	limit int,
	leftTruncated bool,
	rightTruncated bool,
) map[string]any {
	leftStatus := compareStringVal(leftSnap, "status")
	rightStatus := compareStringVal(rightSnap, "status")
	basis := "materialized_cloud_resources"
	switch {
	case workload == nil:
		basis = "missing_workload"
	case leftStatus == "unsupported" || rightStatus == "unsupported" || leftStatus == "missing" || rightStatus == "missing":
		basis = "unsupported"
	case leftStatus != "present" || rightStatus != "present":
		basis = "inferred_environment_evidence"
	}
	return map[string]any{
		"query_shape":      "workload_environment_cloud_resource_story",
		"comparison_basis": basis,
		"freshness_state":  string(FreshnessFresh),
		"left_status":      leftStatus,
		"right_status":     rightStatus,
		"left_truncated":   leftTruncated,
		"right_truncated":  rightTruncated,
		"limit":            limit,
		"truncated":        leftTruncated || rightTruncated,
	}
}

func environmentCompareEvidence(
	workload map[string]any,
	leftSnap map[string]any,
	rightSnap map[string]any,
	comparison environmentResourceComparison,
) []map[string]any {
	rows := make([]map[string]any, 0)
	if workload != nil {
		rows = append(rows, map[string]any{
			"kind":        "workload",
			"source":      "graph",
			"workload_id": compareStringVal(workload, "id"),
			"name":        compareStringVal(workload, "name"),
			"repo_id":     compareStringVal(workload, "repo_id"),
		})
	}
	rows = append(rows, snapshotEvidenceRows("left", leftSnap)...)
	rows = append(rows, snapshotEvidenceRows("right", rightSnap)...)
	rows = append(rows, resourceEvidenceRows("shared_cloud_resource", comparison.shared)...)
	rows = append(rows, resourceEvidenceRows("dedicated_cloud_resource", comparison.leftDedicated)...)
	rows = append(rows, resourceEvidenceRows("dedicated_cloud_resource", comparison.rightDedicated)...)
	return rows
}

func snapshotEvidenceRows(side string, snap map[string]any) []map[string]any {
	rows := make([]map[string]any, 0)
	for _, row := range compareMapSlice(snap, "provenance") {
		copy := cloneCompareStoryMap(row)
		copy["side"] = side
		copy["environment"] = compareStringVal(snap, "environment")
		rows = append(rows, copy)
	}
	return rows
}

func resourceEvidenceRows(kind string, resources []map[string]any) []map[string]any {
	rows := make([]map[string]any, 0, len(resources))
	for _, resource := range resources {
		rows = append(rows, map[string]any{
			"kind":              kind,
			"source":            "graph",
			"resource_id":       compareStringVal(resource, "id"),
			"resource_name":     compareStringVal(resource, "name"),
			"resource_kind":     compareStringVal(resource, "kind"),
			"provider":          compareStringVal(resource, "provider"),
			"environment":       compareStringVal(resource, "environment"),
			"left_environment":  compareStringVal(resource, "left_environment"),
			"right_environment": compareStringVal(resource, "right_environment"),
			"confidence":        floatVal(resource, "confidence"),
			"reason":            compareStringVal(resource, "reason"),
		})
	}
	return rows
}

func environmentCompareLimitations(leftSnap, rightSnap map[string]any, leftTruncated, rightTruncated bool) []map[string]any {
	limitations := make([]map[string]any, 0)
	limitations = appendSnapshotLimitations(limitations, "left", leftSnap)
	limitations = appendSnapshotLimitations(limitations, "right", rightSnap)
	if leftTruncated || rightTruncated {
		limitations = append(limitations, map[string]any{
			"kind":            "result_truncated",
			"left_truncated":  leftTruncated,
			"right_truncated": rightTruncated,
			"reason":          "one or both environment resource lists exceeded the requested limit",
		})
	}
	if compareStringVal(leftSnap, "status") == "present" && compareStringVal(rightSnap, "status") == "present" {
		limitations = append(limitations,
			map[string]any{
				"kind":   "configuration_diff_not_materialized",
				"reason": "this response compares materialized cloud resources; config key/value drift is not materialized in this contract yet",
			},
			map[string]any{
				"kind":   "runtime_setting_diff_not_materialized",
				"reason": "this response compares materialized cloud resources; runtime setting drift is not materialized in this contract yet",
			},
		)
	}
	return limitations
}

func appendSnapshotLimitations(limitations []map[string]any, side string, snap map[string]any) []map[string]any {
	status := compareStringVal(snap, "status")
	switch status {
	case "unsupported", "missing":
		return append(limitations, map[string]any{
			"kind":        "missing_environment_evidence",
			"side":        side,
			"environment": compareStringVal(snap, "environment"),
			"reason":      compareStringVal(snap, "reason"),
		})
	case "inferred":
		return append(limitations, map[string]any{
			"kind":        "inferred_environment_only",
			"side":        side,
			"environment": compareStringVal(snap, "environment"),
			"reason":      compareStringVal(snap, "reason"),
		})
	default:
		return limitations
	}
}

func environmentCompareNextCalls(
	workload map[string]any,
	req compareEnvironmentsRequest,
	comparison environmentResourceComparison,
	truncated bool,
) []map[string]any {
	calls := make([]map[string]any, 0, 3)
	if workload != nil {
		calls = append(calls, map[string]any{
			"tool":   "get_workload_story",
			"reason": "inspect the workload ownership, deployment, and dependency story before drilling into drift",
			"arguments": map[string]any{
				"workload_id": compareStringVal(workload, "id"),
			},
		})
	}
	if resourceID := firstResourceID(comparison.leftDedicated, comparison.rightDedicated, comparison.shared); resourceID != "" {
		calls = append(calls, map[string]any{
			"tool":   "trace_resource_to_code",
			"reason": "trace the most relevant changed or shared resource back to code evidence",
			"arguments": map[string]any{
				"start": resourceID,
				"limit": 25,
			},
		})
	}
	if truncated {
		if nextLimit := nextEnvironmentCompareLimit(req.Limit); nextLimit > 0 {
			calls = append(calls, map[string]any{
				"tool":   "compare_environments",
				"reason": "page deeper because this response hit the requested resource limit",
				"arguments": map[string]any{
					"workload_id": req.WorkloadID,
					"left":        req.Left,
					"right":       req.Right,
					"limit":       nextLimit,
				},
			})
		}
	}
	return calls
}

func environmentCompareStoryText(workload map[string]any, req compareEnvironmentsRequest, summary map[string]any) string {
	workloadName := req.WorkloadID
	if workload != nil && compareStringVal(workload, "name") != "" {
		workloadName = compareStringVal(workload, "name")
	}
	switch summary["comparison"] {
	case "different":
		return fmt.Sprintf("%s differs between %s and %s: %d shared resources, %d dedicated to %s, and %d dedicated to %s.",
			workloadName,
			req.Left,
			req.Right,
			summary["shared_resource_count"],
			summary["left_dedicated_resource_count"],
			req.Left,
			summary["right_dedicated_resource_count"],
			req.Right,
		)
	case "partial":
		return fmt.Sprintf("%s has only partial environment evidence for %s versus %s; materialized cloud-resource drift cannot be answered yet.", workloadName, req.Left, req.Right)
	case "unsupported", "missing_workload":
		return fmt.Sprintf("%s cannot be compared between %s and %s because required workload or environment evidence is missing.", workloadName, req.Left, req.Right)
	default:
		return fmt.Sprintf("%s has no materialized cloud-resource differences between %s and %s.", workloadName, req.Left, req.Right)
	}
}

func firstResourceID(groups ...[]map[string]any) string {
	for _, group := range groups {
		for _, row := range group {
			if id := compareStringVal(row, "id"); id != "" {
				return id
			}
		}
	}
	return ""
}

func sortStoryRows(rows []map[string]any) {
	sort.Slice(rows, func(i, j int) bool {
		leftName := compareStringVal(rows[i], "name")
		rightName := compareStringVal(rows[j], "name")
		if leftName != rightName {
			return leftName < rightName
		}
		return compareStringVal(rows[i], "id") < compareStringVal(rows[j], "id")
	})
}

func cloneCompareStoryMap(row map[string]any) map[string]any {
	clone := make(map[string]any, len(row)+2)
	for key, value := range row {
		clone[key] = value
	}
	return clone
}

func coalesceString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func maxCompareFloat(left, right float64) float64 {
	if left > right {
		return left
	}
	return right
}

func nextEnvironmentCompareLimit(requested int) int {
	current := normalizeImpactListLimit(requested)
	if current >= impactMaxListLimit {
		return 0
	}
	next := current * 2
	if next > impactMaxListLimit {
		return impactMaxListLimit
	}
	return next
}
