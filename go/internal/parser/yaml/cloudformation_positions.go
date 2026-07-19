// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package yaml

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/cloudformation"

	yamlv3 "gopkg.in/yaml.v3"
)

// cloudformationSections are the top-level CloudFormation/SAM template keys
// this package walks for real per-entity line positions. Anchoring is
// strictly at the document root's own top-level pairs -- never by searching
// for a key name anywhere in the tree -- so a resource's Properties block
// that happens to contain a nested "Resources" or "Outputs" key (for example
// AWS::CloudFormation::Stack's nested template body) is never mistaken for a
// template section.
var cloudformationSections = []string{"Parameters", "Conditions", "Resources", "Outputs"}

// cloudformationPositionFallback records one degraded-position event: a
// CloudFormation entity (or an entire section) whose real per-entity
// line_number/end_line the node walk could not resolve, so
// cloudformation.ParseWithPositions fell back to the section header line or
// the document-root line instead. The collector layer turns these into the
// CloudFormationPositionFallbacks counter (issue #5328); the entity itself is
// never dropped -- only its precision degrades.
type cloudformationPositionFallback struct {
	Section string
	Reason  string
	Line    int
}

// decodeDocumentNodes decodes source into per-document raw *yamlv3.Node
// section roots, applying the identical empty-document skip rule
// DecodeDocuments uses so the two functions' results stay index-aligned when
// run against the same source. It exists because DecodeDocuments flattens
// each document to map[string]any via yamlNodeToAny, discarding every node's
// real Line except a single document-root capture; the CloudFormation
// position walk needs the raw node tree to read each entity key's own Line.
func decodeDocumentNodes(source string) ([]*yamlv3.Node, error) {
	decoder := yamlv3.NewDecoder(strings.NewReader(source))
	nodes := make([]*yamlv3.Node, 0)
	for {
		var node yamlv3.Node
		err := decoder.Decode(&node)
		if err != nil {
			if err.Error() == "EOF" {
				return nodes, nil
			}
			return nil, err
		}
		if len(node.Content) == 0 {
			continue
		}
		nodes = append(nodes, node.Content[0])
	}
}

// cloudformationPositionsFromRoot walks root -- one document's raw node tree,
// as returned by decodeDocumentNodes -- and returns the real per-entity
// Positions for a CloudFormation/SAM document, plus any degraded-position
// fallback events. document is the same document already flattened by
// DecodeDocuments; it is used to know which entity names actually exist (so
// a name present in the document but missing from the node walk's result
// still gets a recorded fallback instead of silently keeping a stale
// position). root == nil (the raw node tree was unavailable) degrades every
// section to the document-root lineNumber, matching Parse's original
// behavior, and records one fallback event.
func cloudformationPositionsFromRoot(root *yamlv3.Node, document map[string]any) (cloudformation.Positions, []cloudformationPositionFallback) {
	if root == nil {
		return cloudformation.Positions{}, []cloudformationPositionFallback{
			{Section: "document", Reason: "root_node_unavailable"},
		}
	}

	doc := atlantisDocumentMapping(root)
	if doc == nil {
		return cloudformation.Positions{}, []cloudformationPositionFallback{
			{Section: "document", Reason: "root_not_mapping"},
		}
	}

	var positions cloudformation.Positions
	var fallbacks []cloudformationPositionFallback
	for _, section := range cloudformationSections {
		flatSection, present := document[section].(map[string]any)
		if !present {
			continue
		}

		keyNode, valueNode := cloudformationSectionNodes(doc, section)
		sectionPositions := cloudformation.SectionPositions{}
		if keyNode != nil {
			sectionPositions.FallbackLine = keyNode.Line
		}

		seen := map[*yamlv3.Node]bool{}
		mapping := resolveAliasMapping(valueNode, seen)
		if mapping == nil {
			fallbacks = append(fallbacks, cloudformationPositionFallback{
				Section: section,
				Reason:  "unresolved_section_mapping",
				Line:    sectionPositions.FallbackLine,
			})
		} else {
			entries := make(map[string]cloudformation.EntityPosition, len(flatSection))
			cloudformationWalkMappingEntries(mapping, entries, seen, true)
			sectionPositions.Entries = entries
			for name := range flatSection {
				if _, ok := entries[name]; !ok {
					fallbacks = append(fallbacks, cloudformationPositionFallback{
						Section: section,
						Reason:  "entity_position_missing",
						Line:    sectionPositions.FallbackLine,
					})
				}
			}
		}

		switch section {
		case "Parameters":
			positions.Parameters = sectionPositions
		case "Conditions":
			positions.Conditions = sectionPositions
		case "Resources":
			positions.Resources = sectionPositions
		case "Outputs":
			positions.Outputs = sectionPositions
		}
	}
	return positions, fallbacks
}

// cloudformationSectionNodes returns the key and value nodes of a top-level
// section pair (for example "Resources") in the document mapping doc, or nil
// nil when absent. Only doc's own top-level pairs are scanned -- this is the
// document-root anchor that keeps a nested same-named key (a resource's
// Properties containing its own "Resources" or "Outputs" map) from being
// mistaken for a template section.
func cloudformationSectionNodes(doc *yamlv3.Node, key string) (keyNode *yamlv3.Node, valueNode *yamlv3.Node) {
	if doc == nil {
		return nil, nil
	}
	for index := 0; index+1 < len(doc.Content); index += 2 {
		if doc.Content[index].Value == key {
			return doc.Content[index], doc.Content[index+1]
		}
	}
	return nil, nil
}

// resolveAliasMapping follows a chain of yaml.v3 AliasNode indirection (for
// example "Resources: *sharedResources") down to the first MappingNode, or
// nil when node -- after resolving any alias -- is not a mapping. seen guards
// against alias cycles: a malformed anchor graph must degrade to the
// documented fallback, not hang or crash the parser.
func resolveAliasMapping(node *yamlv3.Node, seen map[*yamlv3.Node]bool) *yamlv3.Node {
	if node == nil {
		return nil
	}
	if node.Kind == yamlv3.AliasNode {
		if seen[node] {
			return nil
		}
		seen[node] = true
		defer delete(seen, node)
		return resolveAliasMapping(node.Alias, seen)
	}
	if node.Kind != yamlv3.MappingNode {
		return nil
	}
	return node
}

// cloudformationWalkMappingEntries records one EntityPosition per key in
// mapping into positions: StartLine is the key node's own physical Line;
// EndLine is the highest Line touched by the key's value subtree (see
// cloudformationMaxLine). A `<<: *base` / `<<: [*a, *b]` merge-key entry is
// expanded via cloudformationExpandMerge instead of becoming an entity
// itself, attributing each injected entity to its own key's physical line
// (wherever the anchor was defined). overwrite controls precedence to mirror
// node_decode.go's yamlMappingNodeToAny/mergeYAMLMappingValues merge
// semantics exactly: an explicit key in mapping always wins (overwrite=true
// for mapping's own direct entries), while a name injected by a merge only
// fills a name not already present (overwrite=false, via
// cloudformationExpandMerge) so an explicit definition elsewhere in the same
// mapping is never clobbered by a merged one, regardless of which is walked
// first.
func cloudformationWalkMappingEntries(
	mapping *yamlv3.Node,
	positions map[string]cloudformation.EntityPosition,
	seen map[*yamlv3.Node]bool,
	overwrite bool,
) {
	for index := 0; index+1 < len(mapping.Content); index += 2 {
		keyNode := mapping.Content[index]
		valueNode := mapping.Content[index+1]
		if isYAMLMergeKey(keyNode) {
			cloudformationExpandMerge(valueNode, positions, seen)
			continue
		}
		name := yamlScalarString(keyNode)
		if name == "" {
			continue
		}
		if !overwrite {
			if _, exists := positions[name]; exists {
				continue
			}
		}
		positions[name] = cloudformation.EntityPosition{
			StartLine: keyNode.Line,
			EndLine:   cloudformationMaxLine(valueNode, keyNode.Line),
		}
	}
}

// cloudformationExpandMerge injects entity positions from a `<<:` merge-key
// value into positions. valueNode is either a single alias/mapping (`<<:
// *base`) or a sequence of them (`<<: [*a, *b]`). Injected entries never
// overwrite a name already present, matching YAML merge-key precedence
// (explicit keys always win over merged ones). seen guards the alias
// dereference against cycles.
func cloudformationExpandMerge(valueNode *yamlv3.Node, positions map[string]cloudformation.EntityPosition, seen map[*yamlv3.Node]bool) {
	if valueNode == nil {
		return
	}
	switch valueNode.Kind {
	case yamlv3.AliasNode:
		if seen[valueNode] {
			return
		}
		seen[valueNode] = true
		defer delete(seen, valueNode)
		cloudformationExpandMerge(valueNode.Alias, positions, seen)
	case yamlv3.MappingNode:
		cloudformationWalkMappingEntries(valueNode, positions, seen, false)
	case yamlv3.SequenceNode:
		for _, item := range valueNode.Content {
			cloudformationExpandMerge(item, positions, seen)
		}
	case yamlv3.DocumentNode, yamlv3.ScalarNode:
		// A malformed `<<: <scalar>` or a stray DocumentNode (which never
		// actually occurs as a mapping value) injects nothing; the merge is
		// silently a no-op rather than treated as an entity.
	}
}

// firstPositiveInt returns the first value greater than zero, or the last
// value when none are positive. It resolves the line_number recorded on a
// cloudformation_position_fallbacks row: the section header line when known,
// else the document-root lineNumber Parse always has.
func firstPositiveInt(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	if len(values) == 0 {
		return 0
	}
	return values[len(values)-1]
}

// cloudformationMaxLine returns the highest Line reached while walking node's
// subtree, or fallback when node is nil. An AliasNode contributes only its
// own Line and is never followed into its anchor target -- an entity whose
// whole value is `*anchor` must not have its end_line inflated (or
// mis-measured) by the anchor's own definition, which may live anywhere else
// in the file. Because this walk only ever follows Content (a strict tree,
// never an Alias edge), it cannot cycle and needs no seen-guard.
func cloudformationMaxLine(node *yamlv3.Node, fallback int) int {
	max := fallback
	var walk func(n *yamlv3.Node)
	walk = func(n *yamlv3.Node) {
		if n == nil {
			return
		}
		if n.Line > max {
			max = n.Line
		}
		if n.Kind == yamlv3.AliasNode {
			return
		}
		for _, child := range n.Content {
			walk(child)
		}
	}
	walk(node)
	return max
}
