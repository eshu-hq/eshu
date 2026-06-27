// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package yaml

import (
	"strings"

	yamlv3 "gopkg.in/yaml.v3"
)

// gitlabPipelineName is the stable node name for the single GitlabPipeline entity
// emitted per .gitlab-ci.yml. The file has no inherent name, and (name, path,
// line_number) is the canonical node key, so a constant name plus the file path
// keeps the pipeline identity stable across runs.
const gitlabPipelineName = ".gitlab-ci.yml"

// gitlabReservedKeys are the top-level GitLab CI keywords that configure the
// pipeline globally rather than declaring a job. A top-level mapping key that is
// one of these is never treated as a GitlabJob.
var gitlabReservedKeys = map[string]bool{
	"stages":        true,
	"variables":     true,
	"include":       true,
	"default":       true,
	"image":         true,
	"services":      true,
	"before_script": true,
	"after_script":  true,
	"cache":         true,
	"workflow":      true,
}

// isGitlabCIConfig reports whether filename is a GitLab CI pipeline definition
// (.gitlab-ci.yml / .gitlab-ci.yaml). Like atlantis.yaml these files carry no
// apiVersion/kind discriminator, so they are dispatched by filename rather than
// by the Kubernetes-style document walker the other YAML configs use.
func isGitlabCIConfig(filename string) bool {
	switch strings.ToLower(strings.TrimSpace(filename)) {
	case ".gitlab-ci.yml", ".gitlab-ci.yaml":
		return true
	default:
		return false
	}
}

// parseGitlabCIFromSource extracts the GitlabPipeline row and the GitlabJob rows
// from a .gitlab-ci.yml. The pipeline row carries the ordered stage list and the
// global variable count; each job row carries its stage, when, image, and the
// resolved needs/dependencies job names plus a script line count.
//
// It decodes directly from the raw source (not the parent node-walked document)
// so YAML anchors and merge keys — heavily used in real .gitlab-ci.yml files to
// DRY job definitions (`.template: &t ...` / `<<: *t`) — resolve against each
// job's own subtree. The job key's own line number gives every job a distinct
// node identity. Hidden/template jobs (keys starting with ".") and the reserved
// global keywords are excluded from the job rows. Malformed entries are
// tolerantly skipped rather than failing the parse.
func parseGitlabCIFromSource(source []byte, path string) (pipeline map[string]any, jobs []map[string]any, err error) {
	var root yamlv3.Node
	if uerr := yamlv3.Unmarshal(source, &root); uerr != nil {
		return nil, nil, uerr
	}
	doc := atlantisDocumentMapping(&root)
	if doc == nil {
		return nil, nil, nil
	}

	stages := gitlabStageList(doc)
	pipeline = map[string]any{
		"name":           gitlabPipelineName,
		"line_number":    gitlabPipelineLine(doc),
		"path":           path,
		"lang":           "yaml",
		"variable_count": gitlabVariableCount(doc),
	}
	if len(stages) > 0 {
		pipeline["stages"] = strings.Join(stages, ",")
	}

	jobs = gitlabJobRows(doc, path)
	return pipeline, jobs, nil
}

// gitlabPipelineLine returns the line number anchoring the GitlabPipeline node.
// The top-level document mapping's first key line is a stable anchor; falling
// back to 1 when the mapping is empty.
func gitlabPipelineLine(doc *yamlv3.Node) int {
	if doc != nil && len(doc.Content) > 0 && doc.Content[0].Line > 0 {
		return doc.Content[0].Line
	}
	return 1
}

// gitlabStageList returns the ordered `stages:` sequence of a .gitlab-ci.yml.
// Order is significant (it is the pipeline execution order) and is preserved.
func gitlabStageList(doc *yamlv3.Node) []string {
	node := atlantisMappingValue(doc, "stages")
	if node == nil || node.Kind != yamlv3.SequenceNode {
		return nil
	}
	stages := make([]string, 0, len(node.Content))
	for _, elem := range node.Content {
		if name := strings.TrimSpace(elem.Value); name != "" {
			stages = append(stages, name)
		}
	}
	return stages
}

// gitlabVariableCount returns the number of entries in the top-level
// `variables:` mapping, or 0 when absent. The values are deliberately not
// captured as node properties (they may carry secrets); only the count is.
func gitlabVariableCount(doc *yamlv3.Node) int {
	node := atlantisMappingValue(doc, "variables")
	if node == nil || node.Kind != yamlv3.MappingNode {
		return 0
	}
	return len(node.Content) / 2
}

// gitlabJobRows builds one GitlabJob row per top-level job mapping, skipping the
// reserved global keywords and hidden/template jobs (keys starting with "."). The
// caller sorts the bucket deterministically (by line then name).
func gitlabJobRows(doc *yamlv3.Node, path string) []map[string]any {
	var rows []map[string]any
	for index := 0; index+1 < len(doc.Content); index += 2 {
		keyNode := doc.Content[index]
		name := strings.TrimSpace(keyNode.Value)
		if name == "" || strings.HasPrefix(name, ".") || gitlabReservedKeys[name] {
			continue
		}
		valueNode := doc.Content[index+1]
		if valueNode.Kind != yamlv3.MappingNode {
			continue
		}
		var body map[string]any
		// Node.Decode resolves merge keys (<<) and aliases (*anchor) for this
		// job's subtree; a job whose body is not a map is skipped above.
		if derr := valueNode.Decode(&body); derr != nil || body == nil {
			continue
		}
		rows = append(rows, gitlabJobRow(name, body, path, keyNode.Line))
	}
	return rows
}

// gitlabJobRow builds one GitlabJob row from a decoded job body map. The job's
// stage, when, and image scalars become node properties; needs/dependencies feed
// the (GitlabJob)-[:NEEDS]->(GitlabJob) edge as a comma-joined name list; the
// script bodies are NOT captured — only a line count — so an opaque shell command
// is never given fabricated semantics.
func gitlabJobRow(name string, body map[string]any, path string, line int) map[string]any {
	row := map[string]any{
		"name":        name,
		"line_number": line,
		"path":        path,
		"lang":        "yaml",
	}
	setGitlabString(row, "job_stage", body["stage"])
	setGitlabString(row, "job_when", body["when"])
	setGitlabString(row, "image", gitlabImageName(body["image"]))
	if needs := gitlabNeedsNames(body); needs != "" {
		row["needs"] = needs
	}
	if count := gitlabScriptLineCount(body); count > 0 {
		row["script_line_count"] = count
	}
	return row
}

// gitlabImageName extracts the image reference from a job's `image:` value, which
// is either a bare string or a mapping with a `name:` key.
func gitlabImageName(value any) any {
	switch typed := value.(type) {
	case string:
		return typed
	case map[string]any:
		return typed["name"]
	default:
		return nil
	}
}

// gitlabNeedsNames returns the comma-joined, ordered job names a job depends on,
// drawn from `needs:` (preferred) or `dependencies:`. A needs entry is either a
// bare job name or a mapping with a `job:` key; non-job needs (e.g. cross-project
// pipeline artifacts without a local job) are skipped. The split is reversed by
// the NEEDS edge builder, so order and exact tokens are retained.
func gitlabNeedsNames(body map[string]any) string {
	items := gitlabSequence(body["needs"])
	if items == nil {
		items = gitlabSequence(body["dependencies"])
	}
	names := make([]string, 0, len(items))
	for _, item := range items {
		switch typed := item.(type) {
		case string:
			if name := strings.TrimSpace(typed); name != "" {
				names = append(names, name)
			}
		case map[string]any:
			if name := cleanYAMLString(typed["job"]); name != "" {
				names = append(names, name)
			}
		}
	}
	return strings.Join(names, ",")
}

// gitlabScriptLineCount sums the entries across a job's before_script, script,
// and after_script blocks. A script block is a single string (one line) or a
// sequence of strings. Only the count is recorded; the command bodies are not.
func gitlabScriptLineCount(body map[string]any) int {
	total := 0
	for _, key := range []string{"before_script", "script", "after_script"} {
		switch typed := body[key].(type) {
		case string:
			if strings.TrimSpace(typed) != "" {
				total++
			}
		case []any:
			for _, item := range typed {
				if cleanYAMLString(item) != "" {
					total++
				}
			}
		}
	}
	return total
}

// gitlabSequence returns value as a []any when it is a YAML sequence, else nil.
func gitlabSequence(value any) []any {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	return items
}

// setGitlabString sets key to the cleaned scalar value when it is non-empty.
func setGitlabString(row map[string]any, key string, value any) {
	if cleaned := cleanYAMLString(value); cleaned != "" {
		row[key] = cleaned
	}
}
