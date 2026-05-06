package reducer

import (
	"slices"
	"testing"
)

func TestBuildPlatformInfraIntentRowsEmptyInputs(t *testing.T) {
	t.Parallel()

	result := BuildPlatformInfraIntentRows(nil, nil)
	if len(result) != 0 {
		t.Fatalf("BuildPlatformInfraIntentRows(nil, nil) len = %d, want 0", len(result))
	}

	result = BuildPlatformInfraIntentRows([]map[string]any{}, []ExistingPlatformEdge{})
	if len(result) != 0 {
		t.Fatalf("BuildPlatformInfraIntentRows(empty, empty) len = %d, want 0", len(result))
	}
}

func TestBuildPlatformInfraIntentRowsAllUpserts(t *testing.T) {
	t.Parallel()

	descriptorRows := []map[string]any{
		{
			"repo_id":     "repo:infra-eks",
			"platform_id": "platform:kubernetes:aws:prod-cluster",
			"scope":       "cluster",
		},
		{
			"repo_id":     "repo:infra-ecs",
			"platform_id": "platform:ecs:aws:payments-cluster",
			"scope":       "service",
		},
	}

	result := BuildPlatformInfraIntentRows(descriptorRows, nil)

	if got, want := len(result), 2; got != want {
		t.Fatalf("len(result) = %d, want %d", got, want)
	}

	// All rows should be upserts.
	for i, row := range result {
		if got, want := row.Action, IntentActionUpsert; got != want {
			t.Errorf("result[%d].Action = %q, want %q", i, got, want)
		}
	}

	// Check first row.
	if got, want := result[0].RepoID, "repo:infra-eks"; got != want {
		t.Errorf("result[0].RepoID = %q, want %q", got, want)
	}
	if got, want := result[0].PlatformID, "platform:kubernetes:aws:prod-cluster"; got != want {
		t.Errorf("result[0].PlatformID = %q, want %q", got, want)
	}
	if got, want := result[0].Extra["scope"], "cluster"; got != want {
		t.Errorf("result[0].Extra[scope] = %q, want %q", got, want)
	}

	// Check second row.
	if got, want := result[1].RepoID, "repo:infra-ecs"; got != want {
		t.Errorf("result[1].RepoID = %q, want %q", got, want)
	}
	if got, want := result[1].PlatformID, "platform:ecs:aws:payments-cluster"; got != want {
		t.Errorf("result[1].PlatformID = %q, want %q", got, want)
	}
}

func TestBuildPlatformInfraIntentRowsAllRetracts(t *testing.T) {
	t.Parallel()

	existing := []ExistingPlatformEdge{
		{
			RepoID:             "repo:old-infra",
			ExistingPlatformID: "platform:kubernetes:aws:deprecated-cluster",
		},
		{
			RepoID:             "repo:old-ecs",
			ExistingPlatformID: "platform:ecs:aws:old-cluster",
		},
	}

	result := BuildPlatformInfraIntentRows(nil, existing)

	if got, want := len(result), 2; got != want {
		t.Fatalf("len(result) = %d, want %d", got, want)
	}

	// All rows should be retracts.
	for i, row := range result {
		if got, want := row.Action, IntentActionRetract; got != want {
			t.Errorf("result[%d].Action = %q, want %q", i, got, want)
		}
	}

	// Check first retract.
	if got, want := result[0].RepoID, "repo:old-infra"; got != want {
		t.Errorf("result[0].RepoID = %q, want %q", got, want)
	}
	if got, want := result[0].PlatformID, "platform:kubernetes:aws:deprecated-cluster"; got != want {
		t.Errorf("result[0].PlatformID = %q, want %q", got, want)
	}

	// Check second retract.
	if got, want := result[1].RepoID, "repo:old-ecs"; got != want {
		t.Errorf("result[1].RepoID = %q, want %q", got, want)
	}
	if got, want := result[1].PlatformID, "platform:ecs:aws:old-cluster"; got != want {
		t.Errorf("result[1].PlatformID = %q, want %q", got, want)
	}
}

func TestBuildPlatformInfraIntentRowsMixOfUpsertsAndRetracts(t *testing.T) {
	t.Parallel()

	descriptorRows := []map[string]any{
		{
			"repo_id":     "repo:infra-eks",
			"platform_id": "platform:kubernetes:aws:prod-cluster",
		},
		{
			"repo_id":     "repo:infra-ecs",
			"platform_id": "platform:ecs:aws:new-cluster",
		},
	}

	existing := []ExistingPlatformEdge{
		{
			RepoID:             "repo:infra-eks",
			ExistingPlatformID: "platform:kubernetes:aws:prod-cluster", // Should NOT retract (in desired).
		},
		{
			RepoID:             "repo:infra-ecs",
			ExistingPlatformID: "platform:ecs:aws:old-cluster", // Should retract (not in desired).
		},
		{
			RepoID:             "repo:old-infra",
			ExistingPlatformID: "platform:kubernetes:aws:deprecated", // Should retract (not in desired).
		},
	}

	result := BuildPlatformInfraIntentRows(descriptorRows, existing)

	// Should have 2 upserts + 2 retracts = 4 rows.
	if got, want := len(result), 4; got != want {
		t.Fatalf("len(result) = %d, want %d", got, want)
	}

	// Count actions.
	upsertCount := 0
	retractCount := 0
	for _, row := range result {
		switch row.Action {
		case IntentActionUpsert:
			upsertCount++
		case IntentActionRetract:
			retractCount++
		}
	}

	if got, want := upsertCount, 2; got != want {
		t.Errorf("upsert count = %d, want %d", got, want)
	}
	if got, want := retractCount, 2; got != want {
		t.Errorf("retract count = %d, want %d", got, want)
	}

	// Verify specific retracts exist.
	retractPairs := make(map[string]string)
	for _, row := range result {
		if row.Action == IntentActionRetract {
			retractPairs[row.RepoID] = row.PlatformID
		}
	}

	expectedRetracts := map[string]string{
		"repo:infra-ecs": "platform:ecs:aws:old-cluster",
		"repo:old-infra": "platform:kubernetes:aws:deprecated",
	}

	for repo, platform := range expectedRetracts {
		if got, ok := retractPairs[repo]; !ok {
			t.Errorf("missing retract for repo %q", repo)
		} else if got != platform {
			t.Errorf("retract for repo %q has platform %q, want %q", repo, got, platform)
		}
	}
}

func TestBuildPlatformInfraIntentRowsSkipsEmptyPairs(t *testing.T) {
	t.Parallel()

	existing := []ExistingPlatformEdge{
		{
			RepoID:             "",
			ExistingPlatformID: "platform:kubernetes:aws:prod",
		},
		{
			RepoID:             "repo:infra",
			ExistingPlatformID: "",
		},
		{
			RepoID:             "repo:valid",
			ExistingPlatformID: "platform:valid",
		},
	}

	result := BuildPlatformInfraIntentRows(nil, existing)

	// Only the valid pair should create a retract.
	if got, want := len(result), 1; got != want {
		t.Fatalf("len(result) = %d, want %d", got, want)
	}

	if got, want := result[0].RepoID, "repo:valid"; got != want {
		t.Errorf("result[0].RepoID = %q, want %q", got, want)
	}
	if got, want := result[0].PlatformID, "platform:valid"; got != want {
		t.Errorf("result[0].PlatformID = %q, want %q", got, want)
	}
}

func TestBuildPlatformInfraIntentRowsPreservesExtraFields(t *testing.T) {
	t.Parallel()

	descriptorRows := []map[string]any{
		{
			"repo_id":     "repo:infra",
			"platform_id": "platform:k8s",
			"scope":       "cluster",
			"provider":    "aws",
			"region":      "us-east-1",
		},
	}

	result := BuildPlatformInfraIntentRows(descriptorRows, nil)

	if got, want := len(result), 1; got != want {
		t.Fatalf("len(result) = %d, want %d", got, want)
	}

	row := result[0]
	if got, want := row.Extra["scope"], "cluster"; got != want {
		t.Errorf("Extra[scope] = %v, want %v", got, want)
	}
	if got, want := row.Extra["provider"], "aws"; got != want {
		t.Errorf("Extra[provider] = %v, want %v", got, want)
	}
	if got, want := row.Extra["region"], "us-east-1"; got != want {
		t.Errorf("Extra[region] = %v, want %v", got, want)
	}
}

func TestSharedPlatformProjectionMetricsEmptyInputs(t *testing.T) {
	t.Parallel()

	result := SharedPlatformProjectionMetrics(nil, nil)
	if len(result) != 0 {
		t.Fatalf("SharedPlatformProjectionMetrics(nil, nil) len = %d, want 0", len(result))
	}

	result = SharedPlatformProjectionMetrics([]PlatformInfraIntentRow{}, nil)
	if len(result) != 0 {
		t.Fatalf("SharedPlatformProjectionMetrics(empty, nil) len = %d, want 0", len(result))
	}

	result = SharedPlatformProjectionMetrics(
		[]PlatformInfraIntentRow{{RepoID: "repo:test"}},
		nil,
	)
	if len(result) != 0 {
		t.Fatalf("SharedPlatformProjectionMetrics with nil context map should return empty, got len=%d", len(result))
	}
}

func TestSharedPlatformProjectionMetricsSingleGenerationID(t *testing.T) {
	t.Parallel()

	intentRows := []PlatformInfraIntentRow{
		{RepoID: "repo:infra-eks", PlatformID: "platform:k8s", Action: IntentActionUpsert},
		{RepoID: "repo:infra-eks", PlatformID: "platform:k8s-old", Action: IntentActionRetract},
	}

	contextByRepoID := map[string]ProjectionContext{
		"repo:infra-eks": {
			SourceRunID:  "run-123",
			GenerationID: "gen-456",
		},
	}

	result := SharedPlatformProjectionMetrics(intentRows, contextByRepoID)

	if got, want := len(result), 3; got != want {
		t.Fatalf("len(result) = %d, want %d", got, want)
	}

	// Check authoritative_domains.
	domains, ok := result["authoritative_domains"].([]string)
	if !ok {
		t.Fatalf("authoritative_domains type = %T, want []string", result["authoritative_domains"])
	}
	if !slices.Equal(domains, []string{"platform_infra"}) {
		t.Errorf("authoritative_domains = %v, want [platform_infra]", domains)
	}

	// Check accepted_generation_id.
	genID, ok := result["accepted_generation_id"].(string)
	if !ok {
		t.Fatalf("accepted_generation_id type = %T, want string", result["accepted_generation_id"])
	}
	if got, want := genID, "gen-456"; got != want {
		t.Errorf("accepted_generation_id = %q, want %q", got, want)
	}

	// Check intent_count.
	count, ok := result["intent_count"].(int)
	if !ok {
		t.Fatalf("intent_count type = %T, want int", result["intent_count"])
	}
	if got, want := count, 2; got != want {
		t.Errorf("intent_count = %d, want %d", got, want)
	}
}

func TestSharedPlatformProjectionMetricsMultipleGenerationIDs(t *testing.T) {
	t.Parallel()

	intentRows := []PlatformInfraIntentRow{
		{RepoID: "repo:infra-eks", PlatformID: "platform:k8s", Action: IntentActionUpsert},
		{RepoID: "repo:infra-ecs", PlatformID: "platform:ecs", Action: IntentActionUpsert},
	}

	contextByRepoID := map[string]ProjectionContext{
		"repo:infra-eks": {
			SourceRunID:  "run-123",
			GenerationID: "gen-456",
		},
		"repo:infra-ecs": {
			SourceRunID:  "run-789",
			GenerationID: "gen-999", // Different generation ID.
		},
	}

	result := SharedPlatformProjectionMetrics(intentRows, contextByRepoID)

	if got, want := len(result), 3; got != want {
		t.Fatalf("len(result) = %d, want %d", got, want)
	}

	// Check authoritative_domains.
	domains, ok := result["authoritative_domains"].([]string)
	if !ok {
		t.Fatalf("authoritative_domains type = %T, want []string", result["authoritative_domains"])
	}
	if !slices.Equal(domains, []string{"platform_infra"}) {
		t.Errorf("authoritative_domains = %v, want [platform_infra]", domains)
	}

	// Check accepted_generation_id is nil when multiple generation IDs.
	if result["accepted_generation_id"] != nil {
		t.Errorf("accepted_generation_id = %v, want nil (multiple generation IDs)", result["accepted_generation_id"])
	}

	// Check intent_count.
	count, ok := result["intent_count"].(int)
	if !ok {
		t.Fatalf("intent_count type = %T, want int", result["intent_count"])
	}
	if got, want := count, 2; got != want {
		t.Errorf("intent_count = %d, want %d", got, want)
	}
}

func TestSharedPlatformProjectionMetricsSkipsMissingRepoContext(t *testing.T) {
	t.Parallel()

	intentRows := []PlatformInfraIntentRow{
		{RepoID: "repo:infra-eks", PlatformID: "platform:k8s", Action: IntentActionUpsert},
		{RepoID: "repo:infra-ecs", PlatformID: "platform:ecs", Action: IntentActionUpsert},
	}

	contextByRepoID := map[string]ProjectionContext{
		"repo:infra-eks": {
			SourceRunID:  "run-123",
			GenerationID: "gen-456",
		},
		// repo:infra-ecs is missing from context.
	}

	result := SharedPlatformProjectionMetrics(intentRows, contextByRepoID)

	// Should only collect generation ID from repo:infra-eks.
	genID, ok := result["accepted_generation_id"].(string)
	if !ok {
		t.Fatalf("accepted_generation_id type = %T, want string", result["accepted_generation_id"])
	}
	if got, want := genID, "gen-456"; got != want {
		t.Errorf("accepted_generation_id = %q, want %q", got, want)
	}
}
