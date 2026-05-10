package postgres

import (
	"strings"
	"testing"
)

func TestWorkflowControlSchemaIncludesExpectedTables(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS workflow_runs",
		"CREATE TABLE IF NOT EXISTS workflow_work_items",
		"CREATE TABLE IF NOT EXISTS workflow_claims",
		"source_system TEXT NOT NULL",
		"acceptance_unit_id TEXT NOT NULL",
		"source_run_id TEXT NOT NULL",
		"workflow_work_items_phase_tuple_idx",
		"workflow_work_items_tfstate_candidate_nonterminal_idx",
		"generation_id LIKE 'terraform_state_candidate:%'",
		"current_fencing_token BIGINT NOT NULL DEFAULT 0",
		"UNIQUE (work_item_id, fencing_token)",
	} {
		if !strings.Contains(workflowControlSchemaSQL, want) {
			t.Fatalf("workflowControlSchemaSQL missing %q", want)
		}
	}
}
