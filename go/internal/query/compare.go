package query

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
)

// CompareHandler provides environment comparison endpoints.
type CompareHandler struct {
	Neo4j   GraphQuery
	Content serviceEvidenceReader
	Profile QueryProfile
}

// Mount registers comparison routes on the given mux.
func (h *CompareHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v0/compare/environments", h.compareEnvironments)
}

func (h *CompareHandler) profile() QueryProfile {
	if h == nil {
		return ProfileProduction
	}
	return NormalizeQueryProfile(string(h.Profile))
}

// compareEnvironmentsRequest is the JSON request body.
type compareEnvironmentsRequest struct {
	WorkloadID string `json:"workload_id"`
	Left       string `json:"left"`
	Right      string `json:"right"`
	Limit      int    `json:"limit"`
}

// compareEnvironments handles POST /api/v0/compare/environments.
func (h *CompareHandler) compareEnvironments(w http.ResponseWriter, r *http.Request) {
	if capabilityUnsupported(h.profile(), "platform_impact.environment_compare") {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"environment comparison requires authoritative environment truth",
			"unsupported_capability",
			"platform_impact.environment_compare",
			h.profile(),
			requiredProfile("platform_impact.environment_compare"),
		)
		return
	}

	var req compareEnvironmentsRequest
	if err := readCompareJSON(r, &req); err != nil {
		writeCompareError(w, http.StatusBadRequest, err.Error())
		return
	}

	if req.WorkloadID == "" {
		writeCompareError(w, http.StatusBadRequest, "workload_id is required")
		return
	}
	if req.Left == "" {
		writeCompareError(w, http.StatusBadRequest, "left environment is required")
		return
	}
	if req.Right == "" {
		writeCompareError(w, http.StatusBadRequest, "right environment is required")
		return
	}

	ctx := r.Context()
	limit := normalizeImpactListLimit(req.Limit)

	// Fetch workload
	workload, err := h.fetchWorkload(ctx, req.WorkloadID)
	if err != nil {
		writeCompareError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// If workload not found, return missing response
	if workload == nil {
		resp := map[string]any{
			"workload": nil,
			"left": map[string]any{
				"environment":     req.Left,
				"status":          "missing",
				"instance":        nil,
				"cloud_resources": []any{},
			},
			"right": map[string]any{
				"environment":     req.Right,
				"status":          "missing",
				"instance":        nil,
				"cloud_resources": []any{},
			},
			"changed": map[string]any{
				"cloud_resources": []any{},
			},
			"confidence": 0.0,
			"reason":     "Workload '" + req.WorkloadID + "' not found",
		}
		WriteSuccess(w, r, http.StatusOK, resp, BuildTruthEnvelope(h.profile(), "platform_impact.environment_compare", TruthBasisHybrid, "compared environment state from workload and cloud-resource evidence"))
		return
	}

	// Fetch environment snapshots
	serviceEvidence, err := h.loadServiceEvidence(ctx, workload)
	if err != nil {
		writeCompareError(w, http.StatusInternalServerError, err.Error())
		return
	}

	leftSnap, leftTruncated, err := h.environmentSnapshot(ctx, workload, req.Left, serviceEvidence, limit)
	if err != nil {
		writeCompareError(w, http.StatusInternalServerError, err.Error())
		return
	}
	rightSnap, rightTruncated, err := h.environmentSnapshot(ctx, workload, req.Right, serviceEvidence, limit)
	if err != nil {
		writeCompareError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Compute diff
	changed := []map[string]any{}
	if compareStringVal(leftSnap, "status") == "present" && compareStringVal(rightSnap, "status") == "present" {
		leftResources := compareMapSlice(leftSnap, "cloud_resources")
		rightResources := compareMapSlice(rightSnap, "cloud_resources")
		changed = diffCloudResources(leftResources, rightResources, req.Left, req.Right)
	}

	// Compute overall confidence
	confidence, reason := computeConfidence(leftSnap, rightSnap, changed)

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
		"coverage": map[string]any{
			"left_truncated":  leftTruncated,
			"right_truncated": rightTruncated,
		},
	}

	WriteSuccess(w, r, http.StatusOK, resp, BuildTruthEnvelope(h.profile(), "platform_impact.environment_compare", TruthBasisHybrid, "compared environment state from workload and cloud-resource evidence"))
}

// fetchWorkload queries Neo4j for the workload by ID.
func (h *CompareHandler) fetchWorkload(ctx context.Context, workloadID string) (map[string]any, error) {
	cypher := `
		MATCH (w:Workload) WHERE w.id = $workload_id
		RETURN w.id as id, w.name as name, w.kind as kind, w.repo_id as repo_id
	`
	row, err := h.Neo4j.RunSingle(ctx, cypher, map[string]any{"workload_id": workloadID})
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, nil
	}
	return map[string]any{
		"id":      compareStringVal(row, "id"),
		"name":    compareStringVal(row, "name"),
		"kind":    compareStringVal(row, "kind"),
		"repo_id": compareStringVal(row, "repo_id"),
	}, nil
}

// environmentSnapshot fetches the instance and cloud resources for one environment.
func (h *CompareHandler) environmentSnapshot(
	ctx context.Context,
	workload map[string]any,
	environment string,
	serviceEvidence ServiceQueryEvidence,
	limit int,
) (map[string]any, bool, error) {
	// Find the workload instance for this environment
	instanceCypher := `
		MATCH (i:WorkloadInstance)
		WHERE i.workload_id = $workload_id
		  AND i.environment = $environment
		RETURN i.id as id, i.name as name, i.kind as kind,
		       i.environment as environment, i.workload_id as workload_id
		LIMIT 1
	`
	instanceRow, err := h.Neo4j.RunSingle(ctx, instanceCypher, map[string]any{
		"workload_id": compareStringVal(workload, "id"),
		"environment": environment,
	})

	if err != nil {
		return nil, false, err
	}
	if instanceRow == nil {
		provenance := inferredEnvironmentProvenance(environment, serviceEvidence)
		if len(provenance) > 0 {
			return map[string]any{
				"environment":     environment,
				"status":          "inferred",
				"instance":        nil,
				"cloud_resources": []map[string]any{},
				"provenance":      provenance,
				"reason":          "environment inferred from service evidence; no materialized workload instance found",
			}, false, nil
		}
		return map[string]any{
			"environment":     environment,
			"status":          "unsupported",
			"instance":        nil,
			"cloud_resources": []map[string]any{},
			"provenance":      []map[string]any{},
			"reason":          "no materialized workload instance or inferable service evidence found for environment",
		}, false, nil
	}

	instance := map[string]any{
		"id":          compareStringVal(instanceRow, "id"),
		"name":        compareStringVal(instanceRow, "name"),
		"kind":        compareStringVal(instanceRow, "kind"),
		"environment": compareStringVal(instanceRow, "environment"),
		"workload_id": compareStringVal(instanceRow, "workload_id"),
	}

	// Fetch cloud resources
	resourcesCypher := `
		MATCH (i:WorkloadInstance)-[r:USES]->(c:CloudResource)
		WHERE i.id = $instance_id
		RETURN c.id as id, c.name as name, c.environment as environment,
		       c.kind as kind, c.provider as provider,
		       r.confidence as confidence, r.reason as reason
		ORDER BY c.name, c.id
		LIMIT $limit
	`
	resourceRows, err := h.Neo4j.Run(ctx, resourcesCypher, map[string]any{
		"instance_id": compareStringVal(instanceRow, "id"),
		"limit":       limit + 1,
	})

	if err != nil {
		return nil, false, err
	}

	resourceRows, truncated := trimImpactRows(resourceRows, limit)
	cloudResources := make([]map[string]any, 0, len(resourceRows))
	for _, row := range resourceRows {
		cloudResources = append(cloudResources, map[string]any{
			"id":          compareStringVal(row, "id"),
			"name":        compareStringVal(row, "name"),
			"environment": compareStringVal(row, "environment"),
			"kind":        compareStringVal(row, "kind"),
			"provider":    compareStringVal(row, "provider"),
			"confidence":  floatVal(row, "confidence"),
			"reason":      compareStringVal(row, "reason"),
		})
	}

	return map[string]any{
		"environment":     environment,
		"status":          "present",
		"instance":        instance,
		"cloud_resources": cloudResources,
		"provenance": []map[string]any{
			{
				"kind":   "materialized_workload_instance",
				"source": "graph",
				"value":  compareStringVal(instance, "id"),
				"reason": "materialized_workload_instance",
			},
		},
		"reason": "materialized workload instance found for environment",
	}, truncated, nil
}

// diffCloudResources computes added/removed resources between two snapshots.
func diffCloudResources(left, right []map[string]any, leftEnv, rightEnv string) []map[string]any {
	// Build lookup maps by resource ID
	leftMap := make(map[string]map[string]any)
	for _, r := range left {
		leftMap[compareStringVal(r, "id")] = r
	}

	rightMap := make(map[string]map[string]any)
	for _, r := range right {
		rightMap[compareStringVal(r, "id")] = r
	}

	changed := make([]map[string]any, 0)

	// Resources in right but not left = added
	for id, r := range rightMap {
		if _, exists := leftMap[id]; !exists {
			changed = append(changed, map[string]any{
				"id":          compareStringVal(r, "id"),
				"name":        compareStringVal(r, "name"),
				"kind":        compareStringVal(r, "kind"),
				"provider":    compareStringVal(r, "provider"),
				"confidence":  floatVal(r, "confidence"),
				"reason":      compareStringVal(r, "reason"),
				"change":      "added",
				"environment": rightEnv,
			})
		}
	}

	// Resources in left but not right = removed
	for id, r := range leftMap {
		if _, exists := rightMap[id]; !exists {
			changed = append(changed, map[string]any{
				"id":          compareStringVal(r, "id"),
				"name":        compareStringVal(r, "name"),
				"kind":        compareStringVal(r, "kind"),
				"provider":    compareStringVal(r, "provider"),
				"confidence":  floatVal(r, "confidence"),
				"reason":      compareStringVal(r, "reason"),
				"change":      "removed",
				"environment": leftEnv,
			})
		}
	}

	// Sort by confidence descending, then by name
	sort.Slice(changed, func(i, j int) bool {
		confI := floatVal(changed[i], "confidence")
		confJ := floatVal(changed[j], "confidence")
		if confI != confJ {
			return confI > confJ
		}
		return compareStringVal(changed[i], "name") < compareStringVal(changed[j], "name")
	})

	return changed
}

// computeConfidence calculates overall confidence and reason for the comparison.
func computeConfidence(left, right map[string]any, changed []map[string]any) (float64, string) {
	leftStatus := compareStringVal(left, "status")
	rightStatus := compareStringVal(right, "status")

	// If either environment cannot be materialized, the comparison is not yet
	// supported for this workload/environment pair.
	if leftStatus == "unsupported" || rightStatus == "unsupported" {
		return 0.0, "Comparison unsupported: one or both environments lack materialized instances and inferable environment evidence"
	}
	if leftStatus != "present" || rightStatus != "present" {
		return 0.35, "Comparison limited to inferred environment evidence; cloud resource differences require materialized workload instances"
	}

	// If no changes, confidence is 1.0
	if len(changed) == 0 {
		return 1.0, "Environments are identical"
	}

	// Calculate average confidence from changed resources
	var sum float64
	for _, c := range changed {
		sum += floatVal(c, "confidence")
	}
	avgConfidence := sum / float64(len(changed))

	return avgConfidence, "Comparison based on materialized cloud resource differences"
}

func (h *CompareHandler) loadServiceEvidence(ctx context.Context, workload map[string]any) (ServiceQueryEvidence, error) {
	if h.Content == nil {
		return ServiceQueryEvidence{}, nil
	}
	repoID := compareStringVal(workload, "repo_id")
	serviceName := compareStringVal(workload, "name")
	if repoID == "" || serviceName == "" {
		return ServiceQueryEvidence{}, nil
	}
	return loadServiceQueryEvidence(ctx, h.Content, repoID, serviceName)
}

// floatVal safely extracts a float64 from a map value.
func floatVal(row map[string]any, key string) float64 {
	v, ok := row[key]
	if !ok || v == nil {
		return 0.0
	}
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int64:
		return float64(n)
	case int:
		return float64(n)
	default:
		return 0.0
	}
}

func compareMapSlice(value map[string]any, key string) []map[string]any {
	if len(value) == 0 {
		return nil
	}
	raw, ok := value[key]
	if !ok || raw == nil {
		return nil
	}
	if typed, ok := raw.([]map[string]any); ok {
		return typed
	}
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		row, ok := item.(map[string]any)
		if ok {
			result = append(result, row)
		}
	}
	return result
}

func compareStringVal(row map[string]any, key string) string {
	value, ok := row[key]
	if !ok || value == nil {
		return ""
	}
	if typed, ok := value.(string); ok {
		return typed
	}
	return fmt.Sprintf("%v", value)
}

func writeCompareJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(value)
}

func writeCompareError(w http.ResponseWriter, status int, message string) {
	writeCompareJSON(w, status, map[string]any{
		"error":  http.StatusText(status),
		"detail": message,
	})
}

func readCompareJSON(r *http.Request, value any) error {
	if r.Body == nil {
		return fmt.Errorf("request body is required")
	}
	defer func() { _ = r.Body.Close() }()
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(value); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	return nil
}
