// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package yaml

import (
	"regexp"
	"sort"
	"strings"

	yamlv3 "gopkg.in/yaml.v3"
)

// helmValuesReferenceRegexp matches a Helm Go-template `.Values.<dotted.path>`
// expression. Templates are NOT valid YAML (they carry Go-template control
// syntax), so the usage extractor scans line-by-line with this regexp rather
// than decoding the manifest. The path segment captures one or more
// dot-separated identifiers (letters, digits, underscores, hyphens), which
// covers the leaf-key form that maps to a flattened values.yaml definition.
//
// Index/range forms (`.Values.list[0]`, `range .Values.items`) and function
// pipelines beyond the dotted path are intentionally truncated at the first
// non-identifier character: only the stable dotted prefix is captured so the
// usage resolves to the values.yaml leaf it ultimately reads.
var helmValuesReferenceRegexp = regexp.MustCompile(`\.Values\.([A-Za-z0-9_-]+(?:\.[A-Za-z0-9_-]+)*)`)

// parseHelmTemplateValueUsages scans one Helm template manifest body for
// `{{ .Values.<dotted.path> }}` references and returns one row per distinct
// dotted path, carrying the path (name) and the 1-based source line of its first
// occurrence. The rows feed the HelmTemplateValueUsage content entity, whose
// REFERENCES edge to the HelmValueDefinition node is resolved in the projector
// structural-edge phase by matching the dotted path within the same chart.
//
// Deduplication keeps the first occurrence so the node line points at where the
// value first enters the rendered manifest; later uses of the same value do not
// create duplicate nodes.
func parseHelmTemplateValueUsages(source []byte) []map[string]any {
	lines := strings.Split(string(source), "\n")
	firstLineByPath := make(map[string]int)
	var order []string

	for index, line := range lines {
		// Only scan inside Go-template actions; a bare ".Values.x" outside {{ }}
		// is data, not a template reference.
		if !strings.Contains(line, "{{") {
			continue
		}
		for _, match := range helmValuesReferenceRegexp.FindAllStringSubmatch(line, -1) {
			path := strings.TrimSpace(match[1])
			if path == "" {
				continue
			}
			if _, seen := firstLineByPath[path]; seen {
				continue
			}
			firstLineByPath[path] = index + 1
			order = append(order, path)
		}
	}

	if len(order) == 0 {
		return nil
	}

	sort.Strings(order)
	rows := make([]map[string]any, 0, len(order))
	for _, path := range order {
		rows = append(rows, map[string]any{
			"name":        path,
			"line_number": firstLineByPath[path],
			"lang":        "yaml",
		})
	}
	return rows
}

// parseHelmValueDefinitions flattens a Helm values.yaml body into one row per
// leaf key, carrying the dotted path (name) and the 1-based source line of the
// leaf scalar. Intermediate mapping keys are not emitted; only referenceable
// leaves (scalars and sequence values) become HelmValueDefinition nodes so a
// template `{{ .Values.<dotted.path> }}` usage can resolve to exactly one
// definition.
//
// The body is decoded through the raw yaml.v3 node tree (not DecodeDocuments) so
// each leaf carries its own source line. Sequence leaves use the dotted path of
// the containing key, so `image.tag` and `ports` (a list) both resolve.
func parseHelmValueDefinitions(source []byte) []map[string]any {
	var root yamlv3.Node
	if err := yamlv3.Unmarshal(source, &root); err != nil {
		return nil
	}
	if len(root.Content) == 0 {
		return nil
	}
	document := root.Content[0]
	if document.Kind != yamlv3.MappingNode {
		return nil
	}

	lineByPath := make(map[string]int)
	var order []string
	collectHelmValueLeaves(document, "", lineByPath, &order)

	if len(order) == 0 {
		return nil
	}

	sort.Strings(order)
	rows := make([]map[string]any, 0, len(order))
	for _, path := range order {
		rows = append(rows, map[string]any{
			"name":        path,
			"line_number": lineByPath[path],
			"lang":        "yaml",
		})
	}
	return rows
}

// collectHelmValueLeaves walks a values.yaml mapping/sequence node tree, emitting
// the dotted path and source line of each leaf value (scalar or a sequence). A
// mapping recurses into its children; a sequence or scalar is a leaf keyed by the
// accumulated dotted prefix. The first leaf seen for a path wins on line.
func collectHelmValueLeaves(node *yamlv3.Node, prefix string, lineByPath map[string]int, order *[]string) {
	switch node.Kind {
	case yamlv3.MappingNode:
		for index := 0; index+1 < len(node.Content); index += 2 {
			keyNode := node.Content[index]
			valueNode := node.Content[index+1]
			key := strings.TrimSpace(keyNode.Value)
			if key == "" {
				continue
			}
			childPrefix := key
			if prefix != "" {
				childPrefix = prefix + "." + key
			}
			if valueNode.Kind == yamlv3.MappingNode {
				collectHelmValueLeaves(valueNode, childPrefix, lineByPath, order)
				continue
			}
			// Scalars and sequences are referenceable leaves. The line is the
			// value node's line (the scalar position, or the first sequence item /
			// the key line for an empty/flow sequence).
			line := valueNode.Line
			if line <= 0 {
				line = keyNode.Line
			}
			recordHelmValueLeaf(childPrefix, line, lineByPath, order)
		}
	case yamlv3.SequenceNode, yamlv3.ScalarNode:
		if prefix == "" {
			return
		}
		line := node.Line
		recordHelmValueLeaf(prefix, line, lineByPath, order)
	case yamlv3.DocumentNode:
		// A document node wraps a single content node; descend into it so a
		// top-level document passed directly still flattens.
		if len(node.Content) > 0 {
			collectHelmValueLeaves(node.Content[0], prefix, lineByPath, order)
		}
	case yamlv3.AliasNode:
		// Aliases resolve to their anchored node; follow the alias so a merged or
		// referenced value is still recorded as a leaf.
		if node.Alias != nil {
			collectHelmValueLeaves(node.Alias, prefix, lineByPath, order)
		}
	}
}

func recordHelmValueLeaf(path string, line int, lineByPath map[string]int, order *[]string) {
	if path == "" {
		return
	}
	if _, seen := lineByPath[path]; seen {
		return
	}
	if line <= 0 {
		line = 1
	}
	lineByPath[path] = line
	*order = append(*order, path)
}
