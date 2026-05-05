package reducer

// PlatformIntentAction represents an upsert or retract action.
const (
	IntentActionUpsert  = "upsert"
	IntentActionRetract = "retract"
)

// ExistingPlatformEdge represents one existing PROVISIONS_PLATFORM edge.
type ExistingPlatformEdge struct {
	RepoID             string
	ExistingPlatformID string
}

// PlatformInfraIntentRow is one authoritative platform-domain intent row with action.
type PlatformInfraIntentRow struct {
	RepoID     string
	PlatformID string
	Action     string // "upsert" or "retract"
	// Additional descriptor fields from the original row.
	Extra map[string]any
}

// BuildPlatformInfraIntentRows diffs desired descriptor rows against existing
// platform edges and returns authoritative upsert and retract intent rows.
func BuildPlatformInfraIntentRows(
	descriptorRows []map[string]any,
	existingRows []ExistingPlatformEdge,
) []PlatformInfraIntentRow {
	// Build set of desired (repo_id, platform_id) pairs from descriptor rows.
	desiredPairs := make(map[[2]string]struct{})
	for _, row := range descriptorRows {
		repoID := anyToString(row["repo_id"])
		platformID := anyToString(row["platform_id"])
		if repoID != "" && platformID != "" {
			desiredPairs[[2]string{repoID, platformID}] = struct{}{}
		}
	}

	// Start with all descriptor rows marked as upserts.
	intentRows := make([]PlatformInfraIntentRow, 0, len(descriptorRows)+len(existingRows))
	for _, row := range descriptorRows {
		repoID := anyToString(row["repo_id"])
		platformID := anyToString(row["platform_id"])

		// Copy all fields except repo_id and platform_id into Extra.
		extra := make(map[string]any)
		for k, v := range row {
			if k != "repo_id" && k != "platform_id" {
				extra[k] = v
			}
		}

		intentRows = append(intentRows, PlatformInfraIntentRow{
			RepoID:     repoID,
			PlatformID: platformID,
			Action:     IntentActionUpsert,
			Extra:      extra,
		})
	}

	// Add retract rows for existing edges not in desired pairs.
	for _, row := range existingRows {
		repoID := row.RepoID
		platformID := row.ExistingPlatformID

		// Skip empty pairs.
		if repoID == "" || platformID == "" {
			continue
		}

		// Skip if this pair is in the desired set.
		pair := [2]string{repoID, platformID}
		if _, ok := desiredPairs[pair]; ok {
			continue
		}

		// Add retract row.
		intentRows = append(intentRows, PlatformInfraIntentRow{
			RepoID:     repoID,
			PlatformID: platformID,
			Action:     IntentActionRetract,
			Extra:      make(map[string]any),
		})
	}

	return intentRows
}

// SharedPlatformProjectionMetrics returns completion-fencing metadata for
// authoritative platform cutover.
func SharedPlatformProjectionMetrics(
	intentRows []PlatformInfraIntentRow,
	contextByRepoID map[string]ProjectionContext,
) map[string]any {
	// Collect touched repository IDs from intent rows.
	touchedRepos := make(map[string]struct{})
	for _, row := range intentRows {
		if row.RepoID != "" {
			touchedRepos[row.RepoID] = struct{}{}
		}
	}

	// Return empty if no touched repositories or no context.
	if len(touchedRepos) == 0 || contextByRepoID == nil {
		return map[string]any{}
	}

	// Collect unique generation IDs for touched repositories.
	generationIDs := make(map[string]struct{})
	for repoID := range touchedRepos {
		if ctx, ok := contextByRepoID[repoID]; ok {
			if ctx.GenerationID != "" {
				generationIDs[ctx.GenerationID] = struct{}{}
			}
		}
	}

	// Build result map.
	result := map[string]any{
		"authoritative_domains": []string{"platform_infra"},
		"intent_count":          len(intentRows),
	}

	// Set accepted_generation_id only if exactly one unique generation ID.
	if len(generationIDs) == 1 {
		for genID := range generationIDs {
			result["accepted_generation_id"] = genID
			break
		}
	} else {
		result["accepted_generation_id"] = nil
	}

	return result
}
