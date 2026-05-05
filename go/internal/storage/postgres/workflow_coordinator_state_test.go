package postgres

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestWorkflowControlStoreReconcileCollectorInstancesUpsertsAndDeactivates(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewWorkflowControlStore(db)
	now := time.Date(2026, time.April, 20, 19, 0, 0, 0, time.UTC)

	err := store.ReconcileCollectorInstances(context.Background(), now, []workflow.DesiredCollectorInstance{
		{
			InstanceID:    "collector-git-primary",
			CollectorKind: scope.CollectorGit,
			Mode:          workflow.CollectorModeContinuous,
			Enabled:       true,
			Bootstrap:     true,
			Configuration: `{"provider":"github"}`,
		},
	})
	if err != nil {
		t.Fatalf("ReconcileCollectorInstances() error = %v, want nil", err)
	}
	if got, want := len(db.execs), 2; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	if !strings.Contains(db.execs[0].query, "INSERT INTO collector_instances") {
		t.Fatalf("first query missing collector_instances upsert: %s", db.execs[0].query)
	}
	if !strings.Contains(db.execs[1].query, "UPDATE collector_instances") {
		t.Fatalf("second query missing collector_instances deactivate: %s", db.execs[1].query)
	}
}

func TestWorkflowControlStoreListCollectorInstancesReturnsDurableState(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 20, 19, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{
			rows: [][]any{{
				"collector-git-primary",
				string(scope.CollectorGit),
				string(workflow.CollectorModeContinuous),
				true,
				true,
				false,
				"Primary Git",
				`{"provider":"github"}`,
				now,
				sql.NullTime{},
				now,
				now,
			}},
		}},
	}
	store := NewWorkflowControlStore(db)

	instances, err := store.ListCollectorInstances(context.Background())
	if err != nil {
		t.Fatalf("ListCollectorInstances() error = %v, want nil", err)
	}
	if got, want := len(instances), 1; got != want {
		t.Fatalf("len(instances) = %d, want %d", got, want)
	}
	if got, want := instances[0].InstanceID, "collector-git-primary"; got != want {
		t.Fatalf("InstanceID = %q, want %q", got, want)
	}
}

func TestWorkflowControlStoreUpsertCompletenessStatesExecutesUpsert(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewWorkflowControlStore(db)
	now := time.Date(2026, time.April, 20, 19, 15, 0, 0, time.UTC)

	err := store.UpsertCompletenessStates(context.Background(), []workflow.CompletenessState{{
		RunID:         "run-1",
		CollectorKind: scope.CollectorGit,
		Keyspace:      reducer.GraphProjectionKeyspaceCodeEntitiesUID,
		PhaseName:     "canonical_nodes_committed",
		Required:      true,
		Status:        "ready",
		ObservedAt:    now,
		UpdatedAt:     now,
	}})
	if err != nil {
		t.Fatalf("UpsertCompletenessStates() error = %v, want nil", err)
	}
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	if !strings.Contains(db.execs[0].query, "INSERT INTO workflow_run_completeness") {
		t.Fatalf("query missing workflow_run_completeness upsert: %s", db.execs[0].query)
	}
	if got, want := db.execs[0].args[2], string(reducer.GraphProjectionKeyspaceCodeEntitiesUID); got != want {
		t.Fatalf("keyspace arg = %v, want %q", got, want)
	}
}

func TestWorkflowCoordinatorStateSchemaIncludesExpectedTables(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS collector_instances",
		"CREATE TABLE IF NOT EXISTS workflow_run_completeness",
		"ADD COLUMN IF NOT EXISTS keyspace TEXT",
		"claims_enabled BOOLEAN NOT NULL DEFAULT FALSE",
	} {
		if !strings.Contains(workflowCoordinatorStateSchemaSQL, want) {
			t.Fatalf("workflowCoordinatorStateSchemaSQL missing %q", want)
		}
	}
}

func TestWorkflowCoordinatorStateBootstrapDefinitionRegistered(t *testing.T) {
	t.Parallel()

	var found bool
	for _, def := range BootstrapDefinitions() {
		if def.Name == "workflow_coordinator_state" {
			found = true
			if !strings.Contains(def.SQL, "collector_instances") {
				t.Fatal("definition SQL missing collector_instances")
			}
			break
		}
	}
	if !found {
		t.Fatal("workflow_coordinator_state not found in BootstrapDefinitions()")
	}
}
