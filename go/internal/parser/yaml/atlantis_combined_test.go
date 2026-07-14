// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package yaml

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestParseAtlantisConfigSingleParseCoversProjectsAndWorkflows pins the
// combined atlantis_projects + atlantis_workflows output for one representative
// atlantis.yaml (anchors/merge keys, multiple workspaces sharing a dir, a
// defined workflow with all four stages, and a referenced-but-undefined
// workflow) through a single Parse() call. This is the regression pin for
// issue #4846: parseAtlantisProjectsFromSource and
// parseAtlantisWorkflowsFromSource are merged into one source-unmarshal
// (parseAtlantisFromSource), and this test must produce byte-identical rows
// before and after that refactor. It lives in its own file (not
// atlantis_test.go) to keep the per-function test file under the repo's
// 500-line file cap.
func TestParseAtlantisConfigSingleParseCoversProjectsAndWorkflows(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "atlantis.yaml")
	source := `version: 3
automerge: true
parallel_plan: true
abort_on_execution_order_fail: true
projects:
  - &template
    workflow: shared
    name: network
    dir: stacks/network
    workspace: default
    terraform_version: v1.7.5
    terraform_distribution: terraform
    autoplan:
      enabled: true
      when_modified:
        - "../../shared/**/*.tf"
        - "*.tf"
    apply_requirements:
      - approved
      - mergeable
    plan_requirements:
      - approved
    import_requirements:
      - approved
    repo_locks:
      mode: on_plan
  - <<: *template
    name: app
    dir: stacks/app
    execution_order_group: 2
    depends_on:
      - network
  - dir: stacks/infra
    workspace: staging
    apply_requirements:
      - approved
  - dir: stacks/infra
    workspace: production
    apply_requirements:
      - approved
      - mergeable
  - name: legacy
    dir: legacy
    workflow: server_side
    autoplan:
      enabled: false
workflows:
  shared:
    plan:
      steps:
        - init
        - run: terraform fmt -check
        - plan
    apply:
      steps:
        - apply
    import:
      steps:
        - import
    policy_check:
      steps:
        - policy_check
  custom_unused:
    plan:
      steps:
        - init
        - plan
`
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatalf("write atlantis.yaml: %v", err)
	}

	payload, err := Parse(path, false, Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	projects, ok := payload["atlantis_projects"].([]map[string]any)
	if !ok {
		t.Fatalf("payload[atlantis_projects] type = %T, want []map[string]any", payload["atlantis_projects"])
	}
	if len(projects) != 5 {
		t.Fatalf("atlantis_projects len = %d, want 5; rows=%+v", len(projects), projects)
	}
	byProjectName := map[string]map[string]any{}
	for _, row := range projects {
		byProjectName[fmt.Sprint(row["name"])] = row
	}
	if got := byProjectName["app"]["workflow"]; got != "shared" {
		t.Fatalf("app.workflow = %v, want shared (merged from anchor)", got)
	}
	if got := byProjectName["app"]["depends_on"]; got != "network" {
		t.Fatalf("app.depends_on = %v, want network", got)
	}
	if _, ok := byProjectName["stacks/infra:staging"]; !ok {
		t.Fatalf("missing workspace-qualified staging project; rows=%+v", projects)
	}
	if _, ok := byProjectName["stacks/infra:production"]; !ok {
		t.Fatalf("missing workspace-qualified production project; rows=%+v", projects)
	}
	if got := byProjectName["legacy"]["workflow"]; got != "server_side" {
		t.Fatalf("legacy.workflow = %v, want server_side", got)
	}

	workflows, ok := payload["atlantis_workflows"].([]map[string]any)
	if !ok {
		t.Fatalf("payload[atlantis_workflows] type = %T, want []map[string]any", payload["atlantis_workflows"])
	}
	// shared + custom_unused (both defined) + server_side (referenced) = 3.
	if len(workflows) != 3 {
		t.Fatalf("atlantis_workflows len = %d, want 3; rows=%+v", len(workflows), workflows)
	}
	byWorkflowName := map[string]map[string]any{}
	for _, row := range workflows {
		byWorkflowName[fmt.Sprint(row["name"])] = row
	}
	shared, ok := byWorkflowName["shared"]
	if !ok {
		t.Fatalf("missing 'shared' workflow; rows=%+v", workflows)
	}
	if shared["source"] != "defined" {
		t.Fatalf("shared.source = %v, want defined", shared["source"])
	}
	if shared["defined_stages"] != "apply,import,plan,policy_check" {
		t.Fatalf("shared.defined_stages = %v, want apply,import,plan,policy_check", shared["defined_stages"])
	}
	if shared["plan_step_kinds"] != "init,run,plan" {
		t.Fatalf("shared.plan_step_kinds = %v, want init,run,plan", shared["plan_step_kinds"])
	}
	if _, ok := byWorkflowName["custom_unused"]; !ok {
		t.Fatalf("missing 'custom_unused' defined-but-unreferenced workflow; rows=%+v", workflows)
	}
	server, ok := byWorkflowName["server_side"]
	if !ok {
		t.Fatalf("missing referenced 'server_side' workflow; rows=%+v", workflows)
	}
	if server["source"] != "referenced" {
		t.Fatalf("server_side.source = %v, want referenced", server["source"])
	}
}
