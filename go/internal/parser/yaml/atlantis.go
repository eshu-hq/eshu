// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package yaml

import (
	"sort"
	"strconv"
	"strings"

	yamlv3 "gopkg.in/yaml.v3"
)

// atlantisWorkflowStages are the Atlantis custom-workflow stages.
var atlantisWorkflowStages = []string{"plan", "apply", "import", "policy_check"}

// isAtlantisConfig reports whether filename is a repo-level Atlantis config
// (atlantis.yaml / atlantis.yml). Atlantis configs are plain YAML with no
// apiVersion/kind, so they are dispatched by filename rather than by the
// Kubernetes-style document discriminator the other YAML configs use.
func isAtlantisConfig(filename string) bool {
	switch strings.ToLower(strings.TrimSpace(filename)) {
	case "atlantis.yaml", "atlantis.yml":
		return true
	default:
		return false
	}
}

// parseAtlantisProjectsFromSource extracts one row per project entry in a
// repo-level atlantis.yaml. Each row becomes an AtlantisProject content entity.
// The apply / autoplan fields are governance properties on the node; dir feeds
// the (AtlantisProject)-[:MANAGES]->(Directory) edge and depends_on /
// execution_order_group feed (AtlantisProject)-[:DEPENDS_ON]->(AtlantisProject).
//
// It decodes from the raw source (not the parent node-walked document) because
// real-world atlantis.yaml routinely DRYs project definitions with YAML anchors
// and merge keys (`- &template ...` / `- <<: *template`). Decoding each project
// element node with yamlv3.Node.Decode resolves those merges and aliases (the
// parent walker drops them), and the element's own line number gives every
// project a distinct identity. Malformed entries (a non-map project, or a field
// whose YAML shape does not match) are tolerantly skipped rather than failing
// the parse. The caller sorts the bucket deterministically (by line then name).
func parseAtlantisProjectsFromSource(source []byte, path string) ([]map[string]any, error) {
	var root yamlv3.Node
	if err := yamlv3.Unmarshal(source, &root); err != nil {
		return nil, err
	}
	projectsNode := atlantisProjectsNode(&root)
	if projectsNode == nil {
		return nil, nil
	}

	rows := make([]map[string]any, 0, len(projectsNode.Content))
	for _, elem := range projectsNode.Content {
		var project map[string]any
		// Node.Decode resolves merge keys (<<) and aliases (*anchor) for this
		// element's subtree; a scalar/sequence element fails to decode into a
		// map and is skipped.
		if err := elem.Decode(&project); err != nil || project == nil {
			continue
		}
		if row := atlantisProjectRow(project, path, elem.Line); row != nil {
			rows = append(rows, row)
		}
	}
	return rows, nil
}

// atlantisDocumentMapping returns the top-level mapping node of a decoded
// atlantis.yaml document, or nil when it is not a mapping.
func atlantisDocumentMapping(root *yamlv3.Node) *yamlv3.Node {
	doc := root
	if doc.Kind == yamlv3.DocumentNode {
		if len(doc.Content) == 0 {
			return nil
		}
		doc = doc.Content[0]
	}
	if doc.Kind != yamlv3.MappingNode {
		return nil
	}
	return doc
}

// atlantisMappingValue returns the value node for key in a mapping node.
func atlantisMappingValue(doc *yamlv3.Node, key string) *yamlv3.Node {
	if doc == nil {
		return nil
	}
	for index := 0; index+1 < len(doc.Content); index += 2 {
		if doc.Content[index].Value == key {
			return doc.Content[index+1]
		}
	}
	return nil
}

// atlantisProjectsNode returns the `projects` sequence node of a decoded
// atlantis.yaml document, or nil when absent or not a sequence.
func atlantisProjectsNode(root *yamlv3.Node) *yamlv3.Node {
	value := atlantisMappingValue(atlantisDocumentMapping(root), "projects")
	if value != nil && value.Kind == yamlv3.SequenceNode {
		return value
	}
	return nil
}

// parseAtlantisWorkflowsFromSource extracts AtlantisWorkflow rows from a
// repo-level atlantis.yaml: one row per workflow defined in the `workflows:` map
// (source=defined, with the ordered step kinds of each defined stage), plus one
// source=referenced row per workflow named by a project's `workflow:` that has no
// in-file definition (Atlantis allows server-side workflow definitions, which is
// the common real-world case). Run-step command bodies are intentionally NOT
// captured as node properties — recording the step KIND (`run`) is truthful
// without fabricating semantics for an opaque shell command.
func parseAtlantisWorkflowsFromSource(source []byte, path string) ([]map[string]any, error) {
	var root yamlv3.Node
	if err := yamlv3.Unmarshal(source, &root); err != nil {
		return nil, err
	}
	doc := atlantisDocumentMapping(&root)
	if doc == nil {
		return nil, nil
	}

	defined := map[string]bool{}
	var rows []map[string]any

	if wfNode := atlantisMappingValue(doc, "workflows"); wfNode != nil && wfNode.Kind == yamlv3.MappingNode {
		for index := 0; index+1 < len(wfNode.Content); index += 2 {
			name := strings.TrimSpace(wfNode.Content[index].Value)
			if name == "" {
				continue
			}
			var body map[string]any
			if err := wfNode.Content[index+1].Decode(&body); err != nil || body == nil {
				continue
			}
			rows = append(rows, atlantisWorkflowRow(name, body, path, wfNode.Content[index].Line))
			defined[name] = true
		}
	}

	// Referenced-but-undefined workflows: a project names a workflow defined
	// server-side. Emit a stub node so USES_WORKFLOW has a target.
	referenced := map[string]int{}
	if projectsNode := atlantisProjectsNode(&root); projectsNode != nil {
		for _, elem := range projectsNode.Content {
			var project map[string]any
			if err := elem.Decode(&project); err != nil || project == nil {
				continue
			}
			ref := cleanYAMLString(project["workflow"])
			if ref == "" || defined[ref] {
				continue
			}
			if _, seen := referenced[ref]; !seen {
				referenced[ref] = elem.Line
			}
		}
	}
	refNames := make([]string, 0, len(referenced))
	for name := range referenced {
		refNames = append(refNames, name)
	}
	sort.Strings(refNames)
	for _, name := range refNames {
		rows = append(rows, map[string]any{
			"name":        name,
			"line_number": referenced[name],
			"path":        path,
			"lang":        "yaml",
			"source":      "referenced",
		})
	}
	return rows, nil
}

// atlantisWorkflowRow builds one defined AtlantisWorkflow row from a workflow
// body map (its plan/apply/import/policy_check stages and their steps).
func atlantisWorkflowRow(name string, body map[string]any, path string, line int) map[string]any {
	row := map[string]any{
		"name":        name,
		"line_number": line,
		"path":        path,
		"lang":        "yaml",
		"source":      "defined",
	}
	stages := make([]string, 0, len(atlantisWorkflowStages))
	for _, stage := range atlantisWorkflowStages {
		stageBody, ok := body[stage].(map[string]any)
		if !ok {
			continue
		}
		stages = append(stages, stage)
		if kinds := atlantisStepKinds(stageBody["steps"]); kinds != "" {
			row[stage+"_step_kinds"] = kinds
		}
	}
	if len(stages) > 0 {
		sort.Strings(stages)
		row["defined_stages"] = strings.Join(stages, ",")
	}
	return row
}

// atlantisStepKinds returns the ordered step kinds of one workflow stage. A step
// is either a bare string (init/plan/apply/import/policy_check) or a single-key
// map (run/env/multienv/...); the kind is that string or map key. The opaque
// body of a run/env step is deliberately not captured.
func atlantisStepKinds(steps any) string {
	items, ok := steps.([]any)
	if !ok {
		return ""
	}
	kinds := make([]string, 0, len(items))
	for _, item := range items {
		switch typed := item.(type) {
		case string:
			if kind := strings.TrimSpace(typed); kind != "" {
				kinds = append(kinds, kind)
			}
		case map[string]any:
			keys := make([]string, 0, len(typed))
			for key := range typed {
				keys = append(keys, key)
			}
			if len(keys) > 0 {
				sort.Strings(keys)
				kinds = append(kinds, keys[0])
			}
		}
	}
	return strings.Join(kinds, ",")
}

// atlantisProjectRow builds one AtlantisProject row from a decoded project map,
// or nil when the project cannot be identified (no name and no dir).
func atlantisProjectRow(project map[string]any, path string, lineNumber int) map[string]any {
	dir := cleanYAMLString(project["dir"])
	workspace := cleanYAMLString(project["workspace"])
	if workspace == "" {
		workspace = "default"
	}

	name := cleanYAMLString(project["name"])
	if name == "" {
		// Atlantis project name is optional; it is required only to disambiguate
		// projects that share a dir+workspace. When omitted we derive a stable
		// name from the dir, qualified by a non-default workspace so the
		// canonical "same dir, multiple workspaces" pattern produces distinct
		// node identities ((name, path, line_number) is the node key).
		name = dir
		if workspace != "default" {
			name = dir + ":" + workspace
		}
	}
	if name == "" {
		// A project with neither name nor dir cannot be addressed.
		return nil
	}

	row := map[string]any{
		"name":        name,
		"line_number": lineNumber,
		"path":        path,
		"lang":        "yaml",
		"dir":         dir,
		"workspace":   workspace,
	}
	setAtlantisString(row, "terraform_version", project["terraform_version"])
	setAtlantisString(row, "terraform_distribution", project["terraform_distribution"])
	setAtlantisString(row, "workflow", project["workflow"])
	if group, ok := atlantisIntValue(project["execution_order_group"]); ok {
		row["execution_order_group"] = group
	}
	if autoplan, ok := project["autoplan"].(map[string]any); ok {
		row["autoplan_enabled"] = boolValueDefault(autoplan["enabled"], true)
		if mods := joinAtlantisStringList(autoplan["when_modified"]); mods != "" {
			row["autoplan_when_modified"] = mods
		}
	}
	setAtlantisJoined(row, "apply_requirements", project["apply_requirements"])
	setAtlantisJoined(row, "plan_requirements", project["plan_requirements"])
	setAtlantisJoined(row, "import_requirements", project["import_requirements"])
	setAtlantisJoined(row, "depends_on", project["depends_on"])
	if locks, ok := project["repo_locks"].(map[string]any); ok {
		setAtlantisString(row, "repo_locks_mode", locks["mode"])
	}
	return row
}

// setAtlantisString sets key to the cleaned scalar value when it is non-empty.
func setAtlantisString(row map[string]any, key string, value any) {
	if cleaned := cleanYAMLString(value); cleaned != "" {
		row[key] = cleaned
	}
}

// setAtlantisJoined sets key to the comma-joined list value when non-empty.
func setAtlantisJoined(row map[string]any, key string, value any) {
	if joined := joinAtlantisStringList(value); joined != "" {
		row[key] = joined
	}
}

// joinAtlantisStringList comma-joins a YAML string sequence, preserving order
// and dropping blank entries. The split is reversed by the edge builders that
// consume depends_on, so order and exact tokens are retained.
func joinAtlantisStringList(value any) string {
	items, ok := value.([]any)
	if !ok {
		return ""
	}
	cleaned := make([]string, 0, len(items))
	for _, item := range items {
		if entry := cleanYAMLString(item); entry != "" {
			cleaned = append(cleaned, entry)
		}
	}
	return strings.Join(cleaned, ",")
}

// atlantisIntValue extracts an integer from a YAML scalar (decoded as a string
// by the line-number-preserving decoder, or already numeric).
func atlantisIntValue(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case float64:
		return int(typed), true
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(typed))
		if err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}

// boolValueDefault returns the boolean value, or fallback when value is absent
// (nil). Atlantis autoplan.enabled defaults to true when the block is present
// without an explicit enabled field.
func boolValueDefault(value any, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return boolValue(value)
}
