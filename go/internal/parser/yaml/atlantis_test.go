// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package yaml

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestParseAtlantisConfigEmitsProjectRows proves a repo-level atlantis.yaml is
// detected by filename and each project entry becomes one atlantis_projects row
// carrying the governance fields the AtlantisProject node and its MANAGES /
// DEPENDS_ON edges are built from.
func TestParseAtlantisConfigEmitsProjectRows(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "atlantis.yaml")
	source := `version: 3
automerge: true
parallel_plan: true
projects:
  - name: network
    dir: terraform/network
    workspace: default
    terraform_version: v1.7.5
    autoplan:
      enabled: true
      when_modified:
        - "*.tf"
        - "../modules/**/*.tf"
    apply_requirements:
      - approved
      - mergeable
    execution_order_group: 1
  - name: app
    dir: terraform/app
    workflow: custom
    depends_on:
      - network
    autoplan:
      enabled: false
`
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatalf("write atlantis.yaml: %v", err)
	}

	payload, err := Parse(path, false, Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	rows, ok := payload["atlantis_projects"].([]map[string]any)
	if !ok {
		t.Fatalf("payload[atlantis_projects] type = %T, want []map[string]any; payload keys=%v", payload["atlantis_projects"], keysOf(payload))
	}
	if len(rows) != 2 {
		t.Fatalf("atlantis_projects len = %d, want 2; rows=%+v", len(rows), rows)
	}

	// Rows are sorted by line then name; network is declared first in the source.
	network := rows[0]
	app := rows[1]
	if got := app["name"]; got != "app" {
		t.Fatalf("rows[0].name = %v, want app", got)
	}
	if got := app["dir"]; got != "terraform/app" {
		t.Fatalf("app.dir = %v, want terraform/app", got)
	}
	if got := app["workflow"]; got != "custom" {
		t.Fatalf("app.workflow = %v, want custom", got)
	}
	if got := app["depends_on"]; got != "network" {
		t.Fatalf("app.depends_on = %v, want network", got)
	}
	if got := app["autoplan_enabled"]; got != false {
		t.Fatalf("app.autoplan_enabled = %v, want false", got)
	}
	if got := app["workspace"]; got != "default" {
		t.Fatalf("app.workspace = %v, want default (defaulted)", got)
	}

	if got := network["name"]; got != "network" {
		t.Fatalf("rows[1].name = %v, want network", got)
	}
	if got := network["dir"]; got != "terraform/network" {
		t.Fatalf("network.dir = %v, want terraform/network", got)
	}
	if got := network["terraform_version"]; got != "v1.7.5" {
		t.Fatalf("network.terraform_version = %v, want v1.7.5", got)
	}
	if got := network["apply_requirements"]; got != "approved,mergeable" {
		t.Fatalf("network.apply_requirements = %v, want approved,mergeable", got)
	}
	if got := network["autoplan_when_modified"]; got != "*.tf,../modules/**/*.tf" {
		t.Fatalf("network.autoplan_when_modified = %v, want *.tf,../modules/**/*.tf", got)
	}
	if got := network["execution_order_group"]; got != 1 {
		t.Fatalf("network.execution_order_group = %v (%T), want int 1", got, got)
	}
	if got := network["autoplan_enabled"]; got != true {
		t.Fatalf("network.autoplan_enabled = %v, want true", got)
	}
	if got := network["path"]; got != path {
		t.Fatalf("network.path = %v, want %v", got, path)
	}
}

// TestParseAtlantisConfigIgnoredForNonAtlantisYAML proves a generic YAML file is
// not treated as an Atlantis config (the bucket stays empty).
func TestParseAtlantisConfigIgnoredForNonAtlantisYAML(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "values.yaml")
	if err := os.WriteFile(path, []byte("projects:\n  - dir: x\n"), 0o600); err != nil {
		t.Fatalf("write yaml: %v", err)
	}

	payload, err := Parse(path, false, Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	rows, _ := payload["atlantis_projects"].([]map[string]any)
	if len(rows) != 0 {
		t.Fatalf("atlantis_projects len = %d for non-atlantis file, want 0", len(rows))
	}
}

// TestParseAtlantisConfigDistinctIdentityForSameDirWorkspaces proves the
// canonical "one dir, multiple workspaces, no explicit name" Atlantis pattern
// produces two DISTINCT project rows. Node identity is (name, path, line_number)
// and every project shares the file's top line, so without qualifying the
// name-fallback by workspace the second workspace's node would collide and be
// silently dropped (governance data loss).
func TestParseAtlantisConfigDistinctIdentityForSameDirWorkspaces(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "atlantis.yaml")
	source := `version: 3
projects:
  - dir: terraform/infra
    workspace: staging
    apply_requirements:
      - approved
  - dir: terraform/infra
    workspace: production
    apply_requirements:
      - approved
      - mergeable
`
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatalf("write atlantis.yaml: %v", err)
	}

	payload, err := Parse(path, false, Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	rows, _ := payload["atlantis_projects"].([]map[string]any)
	if len(rows) != 2 {
		t.Fatalf("atlantis_projects len = %d, want 2 distinct rows; rows=%+v", len(rows), rows)
	}
	names := map[string]bool{}
	for _, row := range rows {
		names[fmt.Sprint(row["name"])] = true
	}
	if len(names) != 2 {
		t.Fatalf("project names = %v, want 2 distinct (workspace-qualified) names", names)
	}
	if !names["terraform/infra:staging"] || !names["terraform/infra:production"] {
		t.Fatalf("expected workspace-qualified fallback names, got %v", names)
	}
}

// TestParseAtlantisConfigResolvesAnchorsAndMergeKeys proves the common
// real-world pattern where projects DRY shared config via a YAML anchor
// (`- &template ...`) and merge keys (`- <<: *template`) resolves correctly: the
// merged project inherits the template's fields (workflow, autoplan) while its
// own keys override. The node-walked document path drops aliases, so this
// exercises the source-decoded path.
func TestParseAtlantisConfigResolvesAnchorsAndMergeKeys(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "atlantis.yaml")
	source := `version: 3
abort_on_execution_order_fail: true
projects:
  - &template
    workflow: shared
    name: network
    dir: stacks/network
    autoplan:
      enabled: true
      when_modified:
        - "../../shared/**/*.tf"
        - "*.tf"
  - <<: *template
    name: app
    dir: stacks/app
    execution_order_group: 2
    depends_on:
      - network
`
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatalf("write atlantis.yaml: %v", err)
	}

	payload, err := Parse(path, false, Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	rows, _ := payload["atlantis_projects"].([]map[string]any)
	if len(rows) != 2 {
		t.Fatalf("atlantis_projects len = %d, want 2; rows=%+v", len(rows), rows)
	}
	byName := map[string]map[string]any{}
	for _, row := range rows {
		byName[fmt.Sprint(row["name"])] = row
	}

	app, ok := byName["app"]
	if !ok {
		t.Fatalf("missing merged 'app' project; rows=%+v", rows)
	}
	if got := app["workflow"]; got != "shared" {
		t.Fatalf("app.workflow = %v, want shared (merged from anchor)", got)
	}
	if got := app["autoplan_enabled"]; got != true {
		t.Fatalf("app.autoplan_enabled = %v, want true (merged from anchor)", got)
	}
	if got := app["autoplan_when_modified"]; got != "../../shared/**/*.tf,*.tf" {
		t.Fatalf("app.autoplan_when_modified = %v, want merged list from anchor", got)
	}
	if got := app["dir"]; got != "stacks/app" {
		t.Fatalf("app.dir = %v, want stacks/app (own override)", got)
	}
	if got := app["depends_on"]; got != "network" {
		t.Fatalf("app.depends_on = %v, want network", got)
	}
	if got := app["execution_order_group"]; got != 2 {
		t.Fatalf("app.execution_order_group = %v, want 2", got)
	}

	network := byName["network"]
	if got := network["dir"]; got != "stacks/network" {
		t.Fatalf("network.dir = %v, want stacks/network", got)
	}
	if got := network["workflow"]; got != "shared" {
		t.Fatalf("network.workflow = %v, want shared", got)
	}
	// Each project element has its own line number → distinct identity.
	if app["line_number"] == network["line_number"] {
		t.Fatalf("app and network share line_number %v; want distinct per-project lines", app["line_number"])
	}
}

// TestParseAtlantisConfigToleratesMalformedShapes proves malformed YAML shapes
// (scalar where a list/map is expected, a non-map project, projects not a list)
// are skipped without panicking and without fabricating fields.
func TestParseAtlantisConfigToleratesMalformedShapes(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"projects_not_a_list": "version: 3\nprojects: oops\n",
		"scalar_project":      "version: 3\nprojects:\n  - just-a-string\n",
		"scalar_autoplan_and_depends_on": `version: 3
projects:
  - dir: a
    autoplan: "yes"
    depends_on: b
    execution_order_group: not-an-int
`,
		"name_and_dir_missing": "version: 3\nprojects:\n  - workspace: default\n",
	}
	for name, source := range cases {
		source := source
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, "atlantis.yaml")
			if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
				t.Fatalf("write atlantis.yaml: %v", err)
			}
			payload, err := Parse(path, false, Options{})
			if err != nil {
				t.Fatalf("Parse() error = %v, want nil (malformed input must not fail the parse)", err)
			}
			rows, _ := payload["atlantis_projects"].([]map[string]any)
			switch name {
			case "scalar_autoplan_and_depends_on":
				// The project has a usable dir, so one row materializes; the
				// scalar fields are tolerated (absent), not fabricated.
				if len(rows) != 1 {
					t.Fatalf("rows len = %d, want 1; rows=%+v", len(rows), rows)
				}
				if _, ok := rows[0]["depends_on"]; ok {
					t.Fatalf("scalar depends_on must be skipped, got %v", rows[0]["depends_on"])
				}
				if _, ok := rows[0]["autoplan_enabled"]; ok {
					t.Fatalf("scalar autoplan must be skipped, got %v", rows[0]["autoplan_enabled"])
				}
				if _, ok := rows[0]["execution_order_group"]; ok {
					t.Fatalf("non-int execution_order_group must be skipped, got %v", rows[0]["execution_order_group"])
				}
			default:
				if len(rows) != 0 {
					t.Fatalf("rows len = %d, want 0 for %s; rows=%+v", len(rows), name, rows)
				}
			}
		})
	}
}

func keysOf(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	return keys
}
