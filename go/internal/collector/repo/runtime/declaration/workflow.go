// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"gopkg.in/yaml.v3"
)

// workflowVersionSuffix marks a with: input that declares a runtime version:
// node-version, python-version, go-version and any other <tool>-version. The
// runtime is the key prefix, kept open like a .tool-versions tool, so a setup
// action for another runtime reads without a code change.
const workflowVersionSuffix = "-version"

// workflowVersionFileKey is the setup-node pointer input: its value names the
// version file (for example .nvmrc) rather than a version. It emits as one
// alias fact carrying the pointer as written, never resolved.
const workflowVersionFileKey = "node-version-file"

// extractWorkflow reads the with: version inputs of one GitHub Actions
// workflow body: every *-version scalar under a job-level with: (a reusable
// workflow call) or a step-level with: (a setup step) is one declaration, and
// node-version-file records its pointer. rejected counts values the credential
// guard dropped. err is non-nil when the body is not readable YAML, in which
// case no declaration is returned: the part before the error would pass for
// the file, as with package.json.
func extractWorkflow(body string) (declarations []declaration, rejected int, err error) {
	decoder := yaml.NewDecoder(strings.NewReader(body))
	onLine := map[int]int{}
	var out []declaration
	dropped := 0
	for {
		var document yaml.Node
		if err := decoder.Decode(&document); err != nil {
			if errors.Is(err, io.EOF) {
				return out, dropped, nil
			}
			return nil, 0, fmt.Errorf("decode workflow YAML: %w", err)
		}
		root := workflowDocumentRoot(&document)
		if root == nil {
			continue
		}
		declarations, values := extractWorkflowDocument(root, onLine)
		out = append(out, declarations...)
		dropped += values
	}
}

// extractWorkflowDocument reads one decoded workflow document. Jobs and steps
// walk in file order, so the fact stream is deterministic without sorting.
func extractWorkflowDocument(root *yaml.Node, onLine map[int]int) ([]declaration, int) {
	jobs := mappingValueNode(root, "jobs")
	if jobs == nil || resolveAlias(jobs).Kind != yaml.MappingNode {
		return nil, 0
	}
	jobs = resolveAlias(jobs)
	var out []declaration
	rejected := 0
	for i := 0; i+1 < len(jobs.Content); i += 2 {
		job := resolveAlias(jobs.Content[i+1])
		if job == nil || job.Kind != yaml.MappingNode {
			continue
		}
		if with := mappingValueNode(job, "with"); with != nil {
			declarations, dropped := extractWorkflowWithInputs(with, onLine)
			out = append(out, declarations...)
			rejected += dropped
		}
		steps := mappingValueNode(job, "steps")
		if steps = resolveAlias(steps); steps == nil || steps.Kind != yaml.SequenceNode {
			continue
		}
		for _, rawStep := range steps.Content {
			step := resolveAlias(rawStep)
			if step == nil || step.Kind != yaml.MappingNode {
				continue
			}
			if with := mappingValueNode(step, "with"); with != nil {
				declarations, dropped := extractWorkflowWithInputs(with, onLine)
				out = append(out, declarations...)
				rejected += dropped
			}
		}
	}
	return out, rejected
}

// extractWorkflowWithInputs reads one with: mapping. A repeated key behaves
// as a JSON parse does: the last occurrence wins, and only that one emits. A
// merge key (<<) is skipped: values merged in from an anchor are not read.
func extractWorkflowWithInputs(with *yaml.Node, onLine map[int]int) ([]declaration, int) {
	with = resolveAlias(with)
	if with == nil || with.Kind != yaml.MappingNode {
		return nil, 0
	}
	type pending struct {
		line    int
		runtime string
		tool    *string
		raw     string
		whole   bool
	}
	var order []string
	values := map[string]*pending{}
	rejected := 0
	for i := 0; i+1 < len(with.Content); i += 2 {
		key := workflowInputKey(with.Content[i])
		if key == "" || key == "<<" {
			continue
		}
		var runtime string
		var tool *string
		whole := false
		if key == workflowVersionFileKey {
			runtime, whole = "node", true
		} else {
			prefix, ok := strings.CutSuffix(key, workflowVersionSuffix)
			if !ok || prefix == "" {
				continue
			}
			runtime, tool = normalizeRuntime(prefix)
		}
		raw, present, ok := workflowScalarValue(with.Content[i+1])
		if !ok {
			// A mapping, sequence, boolean or null declares no version, the
			// same as a non-string package.json member: absent, not rejected.
			continue
		}
		if !present || !safeSpec(raw) {
			// An empty value declares nothing; a value outside the version
			// grammar may carry file text and never reaches a fact.
			rejected++
			continue
		}
		entry := &pending{line: with.Content[i+1].Line, runtime: runtime, tool: tool, raw: raw, whole: whole}
		if existing, dup := values[key]; dup {
			*existing = *entry
			continue
		}
		order = append(order, key)
		values[key] = entry
	}
	var out []declaration
	for _, key := range order {
		entry := values[key]
		out = append(out, declaration{
			line:     entry.line,
			order:    onLine[entry.line],
			section:  sectionWorkflowSetup,
			runtime:  entry.runtime,
			toolName: entry.tool,
			rawSpec:  entry.raw,
			// A version-file pointer is kept as written: an alias, or
			// unresolved when the pointer itself holds a placeholder.
			whole: entry.whole,
		})
		onLine[entry.line]++
	}
	return out, rejected
}

// workflowDocumentRoot returns the mapping at the root of a parsed YAML
// document, unwrapping the document node wrapper. A non-mapping root (a
// scalar, sequence or empty document) yields nil: it declares no jobs.
func workflowDocumentRoot(node *yaml.Node) *yaml.Node {
	node = resolveAlias(node)
	if node == nil {
		return nil
	}
	if node.Kind == yaml.DocumentNode {
		if len(node.Content) == 0 {
			return nil
		}
		node = resolveAlias(node.Content[0])
	}
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	return node
}

// mappingValueNode returns the value of key in a mapping, or nil. Keys match
// case-insensitively: GitHub Actions input and block names are.
func mappingValueNode(mapping *yaml.Node, key string) *yaml.Node {
	mapping = resolveAlias(mapping)
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if workflowInputKey(mapping.Content[i]) == key {
			return mapping.Content[i+1]
		}
	}
	return nil
}

// workflowInputKey returns the lower-cased text of a scalar mapping key, or
// "" when the key is not a scalar.
func workflowInputKey(node *yaml.Node) string {
	node = resolveAlias(node)
	if node == nil || node.Kind != yaml.ScalarNode {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(node.Value))
}

// workflowScalarValue returns the text of a string or number scalar. ok is
// false for a mapping, sequence, boolean or null, which declares no version.
// present is false for an empty string, which declares nothing.
func workflowScalarValue(node *yaml.Node) (raw string, present bool, ok bool) {
	node = resolveAlias(node)
	if node == nil || node.Kind != yaml.ScalarNode {
		return "", false, false
	}
	switch node.Tag {
	case "!!str", "!!int", "!!float":
		raw = strings.TrimSpace(node.Value)
		return raw, raw != "", true
	default:
		return "", false, false
	}
}

// resolveAlias follows YAML alias nodes to their anchor target.
func resolveAlias(node *yaml.Node) *yaml.Node {
	for node != nil && node.Kind == yaml.AliasNode && node.Alias != nil {
		node = node.Alias
	}
	return node
}
