package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

func TestCanonicalExecutorForGraphBackendUsesConfiguredNornicDBPhaseGroupStatements(t *testing.T) {
	t.Parallel()

	inner := &groupCapableIngesterExecutor{}
	executor := canonicalExecutorForGraphBackend(
		inner,
		runtimecfg.GraphBackendNornicDB,
		0,
		false,
		777,
		defaultNornicDBFilePhaseStatements,
		defaultNornicDBEntityPhaseStatements,
		nil,
		nil,
		nil,
	)
	pge, ok := executor.(nornicDBPhaseGroupExecutor)
	if !ok {
		t.Fatalf("executor type = %T, want nornicDBPhaseGroupExecutor", executor)
	}
	if got, want := pge.maxStatements, 777; got != want {
		t.Fatalf("phase-group max statements = %d, want %d", got, want)
	}
}

func TestCanonicalExecutorForGraphBackendUsesConfiguredNornicDBFilePhaseStatements(t *testing.T) {
	t.Parallel()

	inner := &groupCapableIngesterExecutor{}
	executor := canonicalExecutorForGraphBackend(
		inner,
		runtimecfg.GraphBackendNornicDB,
		0,
		false,
		defaultNornicDBPhaseGroupStatements,
		3,
		defaultNornicDBEntityPhaseStatements,
		nil,
		nil,
		nil,
	)
	pge, ok := executor.(nornicDBPhaseGroupExecutor)
	if !ok {
		t.Fatalf("executor type = %T, want nornicDBPhaseGroupExecutor", executor)
	}
	if got, want := pge.fileMaxStatements, 3; got != want {
		t.Fatalf("file phase max statements = %d, want %d", got, want)
	}
}

func TestCanonicalExecutorForGraphBackendUsesConfiguredNornicDBEntityPhaseStatements(t *testing.T) {
	t.Parallel()

	inner := &groupCapableIngesterExecutor{}
	executor := canonicalExecutorForGraphBackend(
		inner,
		runtimecfg.GraphBackendNornicDB,
		0,
		false,
		defaultNornicDBPhaseGroupStatements,
		defaultNornicDBFilePhaseStatements,
		17,
		nil,
		nil,
		nil,
	)
	pge, ok := executor.(nornicDBPhaseGroupExecutor)
	if !ok {
		t.Fatalf("executor type = %T, want nornicDBPhaseGroupExecutor", executor)
	}
	if got, want := pge.entityMaxStatements, 17; got != want {
		t.Fatalf("entity phase max statements = %d, want %d", got, want)
	}
}

func TestNornicDBPhaseGroupExecutorSplitsChunksByConfiguredStatementLimit(t *testing.T) {
	t.Parallel()

	inner := &recordingGroupChunkExecutor{}
	executor := nornicDBPhaseGroupExecutor{
		inner:         inner,
		maxStatements: 2,
	}

	stmts := []sourcecypher.Statement{
		{Cypher: "RETURN 1"},
		{Cypher: "RETURN 2"},
		{Cypher: "RETURN 3"},
		{Cypher: "RETURN 4"},
		{Cypher: "RETURN 5"},
	}

	if err := executor.ExecutePhaseGroup(context.Background(), stmts); err != nil {
		t.Fatalf("ExecutePhaseGroup() error = %v, want nil", err)
	}
	if got, want := len(inner.groupSizes), 3; got != want {
		t.Fatalf("group call count = %d, want %d", got, want)
	}
	if got, want := inner.groupSizes, []int{2, 2, 1}; !equalIntSlices(got, want) {
		t.Fatalf("group sizes = %v, want %v", got, want)
	}
}

func TestNornicDBPhaseGroupExecutorUsesEntitySpecificStatementLimit(t *testing.T) {
	t.Parallel()

	inner := &recordingGroupChunkExecutor{}
	executor := nornicDBPhaseGroupExecutor{
		inner:               inner,
		maxStatements:       5,
		entityMaxStatements: 2,
	}

	stmts := []sourcecypher.Statement{
		{Cypher: "RETURN 1", Parameters: map[string]any{"_eshu_phase": "entities"}},
		{Cypher: "RETURN 2", Parameters: map[string]any{"_eshu_phase": "entities"}},
		{Cypher: "RETURN 3", Parameters: map[string]any{"_eshu_phase": "entities"}},
		{Cypher: "RETURN 4", Parameters: map[string]any{"_eshu_phase": "entities"}},
		{Cypher: "RETURN 5", Parameters: map[string]any{"_eshu_phase": "entities"}},
	}

	if err := executor.ExecutePhaseGroup(context.Background(), stmts); err != nil {
		t.Fatalf("ExecutePhaseGroup() error = %v, want nil", err)
	}
	if got, want := inner.groupSizes, []int{2, 2, 1}; !equalIntSlices(got, want) {
		t.Fatalf("entity group sizes = %v, want %v", got, want)
	}
}

func TestNornicDBPhaseGroupExecutorUsesFileSpecificStatementLimit(t *testing.T) {
	t.Parallel()

	inner := &recordingGroupChunkExecutor{}
	executor := nornicDBPhaseGroupExecutor{
		inner:             inner,
		maxStatements:     10,
		fileMaxStatements: 3,
	}

	stmts := []sourcecypher.Statement{
		{Cypher: "RETURN 1", Parameters: map[string]any{"_eshu_phase": sourcecypher.CanonicalPhaseFiles}},
		{Cypher: "RETURN 2", Parameters: map[string]any{"_eshu_phase": sourcecypher.CanonicalPhaseFiles}},
		{Cypher: "RETURN 3", Parameters: map[string]any{"_eshu_phase": sourcecypher.CanonicalPhaseFiles}},
		{Cypher: "RETURN 4", Parameters: map[string]any{"_eshu_phase": sourcecypher.CanonicalPhaseFiles}},
		{Cypher: "RETURN 5", Parameters: map[string]any{"_eshu_phase": sourcecypher.CanonicalPhaseFiles}},
		{Cypher: "RETURN 6", Parameters: map[string]any{"_eshu_phase": sourcecypher.CanonicalPhaseFiles}},
		{Cypher: "RETURN 7", Parameters: map[string]any{"_eshu_phase": sourcecypher.CanonicalPhaseFiles}},
	}

	if err := executor.ExecutePhaseGroup(context.Background(), stmts); err != nil {
		t.Fatalf("ExecutePhaseGroup() error = %v, want nil", err)
	}
	if got, want := inner.groupSizes, []int{3, 3, 1}; !equalIntSlices(got, want) {
		t.Fatalf("group sizes = %v, want %v", got, want)
	}
}

func TestNornicDBPhaseGroupExecutorUsesEntityLabelSpecificStatementLimit(t *testing.T) {
	t.Parallel()

	inner := &recordingGroupChunkExecutor{}
	executor := nornicDBPhaseGroupExecutor{
		inner:               inner,
		maxStatements:       5,
		entityMaxStatements: 4,
		entityLabelMaxStatements: map[string]int{
			"Function": 2,
		},
	}

	stmts := []sourcecypher.Statement{
		{
			Cypher: "RETURN class1",
			Parameters: map[string]any{
				sourcecypher.StatementMetadataPhaseKey:       sourcecypher.CanonicalPhaseEntities,
				sourcecypher.StatementMetadataEntityLabelKey: "Class",
			},
		},
		{
			Cypher: "RETURN class2",
			Parameters: map[string]any{
				sourcecypher.StatementMetadataPhaseKey:       sourcecypher.CanonicalPhaseEntities,
				sourcecypher.StatementMetadataEntityLabelKey: "Class",
			},
		},
		{
			Cypher: "RETURN class3",
			Parameters: map[string]any{
				sourcecypher.StatementMetadataPhaseKey:       sourcecypher.CanonicalPhaseEntities,
				sourcecypher.StatementMetadataEntityLabelKey: "Class",
			},
		},
		{
			Cypher: "RETURN function1",
			Parameters: map[string]any{
				sourcecypher.StatementMetadataPhaseKey:       sourcecypher.CanonicalPhaseEntities,
				sourcecypher.StatementMetadataEntityLabelKey: "Function",
			},
		},
		{
			Cypher: "RETURN function2",
			Parameters: map[string]any{
				sourcecypher.StatementMetadataPhaseKey:       sourcecypher.CanonicalPhaseEntities,
				sourcecypher.StatementMetadataEntityLabelKey: "Function",
			},
		},
		{
			Cypher: "RETURN function3",
			Parameters: map[string]any{
				sourcecypher.StatementMetadataPhaseKey:       sourcecypher.CanonicalPhaseEntities,
				sourcecypher.StatementMetadataEntityLabelKey: "Function",
			},
		},
	}

	if err := executor.ExecutePhaseGroup(context.Background(), stmts); err != nil {
		t.Fatalf("ExecutePhaseGroup() error = %v, want nil", err)
	}
	if got, want := inner.groupSizes, []int{3, 2, 1}; !equalIntSlices(got, want) {
		t.Fatalf("entity label group sizes = %v, want %v", got, want)
	}
}

func TestNornicDBPhaseGroupExecutorUsesEntityLimitsForContainmentPhase(t *testing.T) {
	t.Parallel()

	inner := &recordingGroupChunkExecutor{}
	executor := nornicDBPhaseGroupExecutor{
		inner:               inner,
		maxStatements:       5,
		entityMaxStatements: 4,
		entityLabelMaxStatements: map[string]int{
			"Function": 2,
		},
	}

	stmts := []sourcecypher.Statement{
		{
			Cypher: "RETURN function-containment-1",
			Parameters: map[string]any{
				sourcecypher.StatementMetadataPhaseKey:       sourcecypher.CanonicalPhaseEntityContainment,
				sourcecypher.StatementMetadataEntityLabelKey: "Function",
			},
		},
		{
			Cypher: "RETURN function-containment-2",
			Parameters: map[string]any{
				sourcecypher.StatementMetadataPhaseKey:       sourcecypher.CanonicalPhaseEntityContainment,
				sourcecypher.StatementMetadataEntityLabelKey: "Function",
			},
		},
		{
			Cypher: "RETURN function-containment-3",
			Parameters: map[string]any{
				sourcecypher.StatementMetadataPhaseKey:       sourcecypher.CanonicalPhaseEntityContainment,
				sourcecypher.StatementMetadataEntityLabelKey: "Function",
			},
		},
	}

	if err := executor.ExecutePhaseGroup(context.Background(), stmts); err != nil {
		t.Fatalf("ExecutePhaseGroup() error = %v, want nil", err)
	}
	if got, want := inner.groupSizes, []int{2, 1}; !equalIntSlices(got, want) {
		t.Fatalf("entity containment group sizes = %v, want %v", got, want)
	}
}

func TestNornicDBPhaseGroupExecutorExecutesRetractStatementsSequentially(t *testing.T) {
	t.Parallel()

	inner := &recordingGroupChunkExecutor{}
	executor := nornicDBPhaseGroupExecutor{
		inner:         inner,
		maxStatements: 5,
	}

	stmts := []sourcecypher.Statement{
		{
			Operation: sourcecypher.OperationCanonicalRetract,
			Cypher:    "MATCH (n) DELETE n",
			Parameters: map[string]any{
				sourcecypher.StatementMetadataSummaryKey: "label=File retract",
			},
		},
		{
			Operation: sourcecypher.OperationCanonicalRetract,
			Cypher:    "MATCH (m) DELETE m",
			Parameters: map[string]any{
				sourcecypher.StatementMetadataSummaryKey: "label=Entity retract",
			},
		},
	}

	if err := executor.ExecutePhaseGroup(context.Background(), stmts); err != nil {
		t.Fatalf("ExecutePhaseGroup() error = %v, want nil", err)
	}
	if got := len(inner.groupSizes); got != 0 {
		t.Fatalf("group call count = %d, want 0", got)
	}
	if got, want := len(inner.executeParams), 2; got != want {
		t.Fatalf("execute params count = %d, want %d", got, want)
	}
}

func TestNornicDBPhaseGroupExecutorRequiresAllStatementsToBeRetracts(t *testing.T) {
	t.Parallel()

	inner := &recordingGroupChunkExecutor{}
	executor := nornicDBPhaseGroupExecutor{
		inner:         inner,
		maxStatements: 5,
	}

	stmts := []sourcecypher.Statement{
		{
			Operation: sourcecypher.OperationCanonicalRetract,
			Cypher:    "MATCH (n) DELETE n",
		},
		{
			Cypher: "RETURN grouped",
		},
	}

	if err := executor.ExecutePhaseGroup(context.Background(), stmts); err != nil {
		t.Fatalf("ExecutePhaseGroup() error = %v, want nil", err)
	}
	if got, want := inner.groupSizes, []int{2}; !equalIntSlices(got, want) {
		t.Fatalf("group sizes = %v, want %v", got, want)
	}
	if got := len(inner.executeParams); got != 0 {
		t.Fatalf("execute params count = %d, want 0", got)
	}
}

func TestNornicDBPhaseGroupExecutorExecutesEntitySingletonFallbackOutsideGroup(t *testing.T) {
	t.Parallel()

	inner := &recordingGroupChunkExecutor{}
	executor := nornicDBPhaseGroupExecutor{
		inner:               inner,
		maxStatements:       5,
		entityMaxStatements: 2,
	}

	stmts := []sourcecypher.Statement{
		{
			Cypher: "RETURN grouped1",
			Parameters: map[string]any{
				"_eshu_phase": "entities",
				"rows":        []map[string]any{{"entity_id": "one"}},
			},
		},
		{
			Cypher: "RETURN fallback",
			Parameters: map[string]any{
				"_eshu_phase":             "entities",
				"_eshu_phase_group_mode":  "execute_only",
				"_eshu_statement_summary": "label=Function rows=1 entity_id=fallback fallback=singleton_parameterized",
				"entity_id":               "fallback",
				"props":                   map[string]any{"name": "fallback"},
			},
		},
		{
			Cypher: "RETURN grouped2",
			Parameters: map[string]any{
				"_eshu_phase": "entities",
				"rows":        []map[string]any{{"entity_id": "two"}},
			},
		},
	}

	if err := executor.ExecutePhaseGroup(context.Background(), stmts); err != nil {
		t.Fatalf("ExecutePhaseGroup() error = %v, want nil", err)
	}
	if got, want := inner.groupSizes, []int{1, 1}; !equalIntSlices(got, want) {
		t.Fatalf("group sizes = %v, want %v", got, want)
	}
	if got, want := len(inner.executeParams), 1; got != want {
		t.Fatalf("execute params count = %d, want %d", got, want)
	}
	if _, ok := inner.executeParams[0]["_eshu_phase_group_mode"]; ok {
		t.Fatalf("execute params include group-mode diagnostic: %#v", inner.executeParams[0])
	}
	if got, want := inner.executeParams[0]["entity_id"], "fallback"; got != want {
		t.Fatalf("execute entity_id = %#v, want %#v", got, want)
	}
}

func TestNornicDBPhaseGroupExecutorGroupsEligibleSingletonFallbacks(t *testing.T) {
	t.Parallel()

	inner := &recordingGroupChunkExecutor{}
	executor := nornicDBPhaseGroupExecutor{
		inner:               inner,
		maxStatements:       5,
		entityMaxStatements: 5,
	}

	stmts := []sourcecypher.Statement{
		{
			Cypher: "RETURN grouped1",
			Parameters: map[string]any{
				"_eshu_phase":        "entities",
				"_eshu_entity_label": "TerraformVariable",
				"rows": []map[string]any{
					{"entity_id": "one"},
				},
			},
		},
		{
			Cypher: "RETURN fallback",
			Parameters: map[string]any{
				"_eshu_phase":             "entities",
				"_eshu_entity_label":      "TerraformVariable",
				"_eshu_phase_group_mode":  sourcecypher.PhaseGroupModeGroupedSingleton,
				"_eshu_statement_summary": "label=TerraformVariable rows=1 entity_id=fallback fallback=grouped_singleton",
				"entity_id":               "fallback",
				"props":                   map[string]any{"name": "fallback"},
			},
		},
		{
			Cypher: "RETURN grouped2",
			Parameters: map[string]any{
				"_eshu_phase":        "entities",
				"_eshu_entity_label": "TerraformVariable",
				"rows": []map[string]any{
					{"entity_id": "two"},
				},
			},
		},
	}

	if err := executor.ExecutePhaseGroup(context.Background(), stmts); err != nil {
		t.Fatalf("ExecutePhaseGroup() error = %v, want nil", err)
	}
	if got, want := inner.groupSizes, []int{3}; !equalIntSlices(got, want) {
		t.Fatalf("group sizes = %v, want %v", got, want)
	}
	if got := len(inner.executeParams); got != 0 {
		t.Fatalf("execute params count = %d, want 0", got)
	}
	for _, stmt := range inner.groupStatements {
		if _, ok := stmt.Parameters["_eshu_phase_group_mode"]; ok {
			t.Fatalf("grouped params include group-mode diagnostic: %#v", stmt.Parameters)
		}
	}
}

func TestNornicDBPhaseGroupExecutorWrapsChunkFailureDetails(t *testing.T) {
	t.Parallel()

	inner := &recordingGroupChunkExecutor{
		failAtCall: 2,
		err:        errors.New("context canceled"),
	}
	executor := nornicDBPhaseGroupExecutor{
		inner:         inner,
		maxStatements: 2,
	}

	stmts := []sourcecypher.Statement{
		{Cypher: "RETURN 1"},
		{Cypher: "RETURN 2"},
		{Cypher: "RETURN 3"},
	}

	err := executor.ExecutePhaseGroup(context.Background(), stmts)
	if err == nil {
		t.Fatal("ExecutePhaseGroup() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "phase-group chunk 2/2") {
		t.Fatalf("ExecutePhaseGroup() error = %q, want chunk ordinal context", err.Error())
	}
	if !strings.Contains(err.Error(), "statements 3-3 of 3") {
		t.Fatalf("ExecutePhaseGroup() error = %q, want statement range context", err.Error())
	}
	if !strings.Contains(err.Error(), `first_statement="RETURN 3"`) {
		t.Fatalf("ExecutePhaseGroup() error = %q, want first statement summary", err.Error())
	}
	if !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("ExecutePhaseGroup() error = %q, want inner error context", err.Error())
	}
}
