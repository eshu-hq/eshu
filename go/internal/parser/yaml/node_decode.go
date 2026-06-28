// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package yaml

import (
	"fmt"
	"strings"

	yamlv3 "gopkg.in/yaml.v3"
)

func yamlNodeToAny(node *yamlv3.Node) (any, error) {
	return yamlNodeToAnySeen(node, map[*yamlv3.Node]bool{})
}

func yamlNodeToAnySeen(node *yamlv3.Node, seen map[*yamlv3.Node]bool) (any, error) {
	if node == nil {
		return nil, nil
	}
	if seen[node] {
		return nil, fmt.Errorf("yaml alias cycle at line %d", node.Line)
	}
	seen[node] = true
	defer delete(seen, node)

	if intrinsicKey, ok := cloudFormationYAMLIntrinsicKey(node.Tag); ok {
		value, err := yamlNodeValueSeen(node, seen)
		if err != nil {
			return nil, err
		}
		return map[string]any{intrinsicKey: value}, nil
	}

	switch node.Kind {
	case yamlv3.DocumentNode:
		if len(node.Content) == 0 {
			return nil, nil
		}
		return yamlNodeToAnySeen(node.Content[0], seen)
	case yamlv3.AliasNode:
		return yamlNodeToAnySeen(node.Alias, seen)
	case yamlv3.MappingNode:
		return yamlMappingNodeToAny(node, seen)
	case yamlv3.SequenceNode:
		result := make([]any, 0, len(node.Content))
		for _, child := range node.Content {
			value, err := yamlNodeToAnySeen(child, seen)
			if err != nil {
				return nil, err
			}
			result = append(result, value)
		}
		return result, nil
	case yamlv3.ScalarNode:
		return yamlScalarString(node), nil
	default:
		return nil, nil
	}
}

func yamlMappingNodeToAny(node *yamlv3.Node, seen map[*yamlv3.Node]bool) (map[string]any, error) {
	result := make(map[string]any, len(node.Content)/2)
	for index := 0; index+1 < len(node.Content); index += 2 {
		keyNode := node.Content[index]
		valueNode := node.Content[index+1]
		key := yamlScalarString(keyNode)
		if isYAMLMergeKey(keyNode) {
			if err := mergeYAMLMapping(result, valueNode, seen); err != nil {
				return nil, err
			}
			continue
		}
		value, err := yamlNodeToAnySeen(valueNode, seen)
		if err != nil {
			return nil, err
		}
		result[key] = value
	}
	return result, nil
}

func mergeYAMLMapping(result map[string]any, valueNode *yamlv3.Node, seen map[*yamlv3.Node]bool) error {
	value, err := yamlNodeToAnySeen(valueNode, seen)
	if err != nil {
		return err
	}
	switch mergedValue := value.(type) {
	case map[string]any:
		mergeYAMLMappingValues(result, mergedValue)
	case []any:
		for _, item := range mergedValue {
			merged, ok := item.(map[string]any)
			if !ok {
				return fmt.Errorf("yaml merge sequence contains non-map value at line %d", valueNode.Line)
			}
			mergeYAMLMappingValues(result, merged)
		}
	default:
		return fmt.Errorf("yaml merge value must be a map or sequence of maps at line %d", valueNode.Line)
	}
	return nil
}

func mergeYAMLMappingValues(result map[string]any, merged map[string]any) {
	for key, item := range merged {
		if _, exists := result[key]; !exists {
			result[key] = item
		}
	}
}

func yamlNodeValueSeen(node *yamlv3.Node, seen map[*yamlv3.Node]bool) (any, error) {
	switch node.Kind {
	case yamlv3.DocumentNode:
		if len(node.Content) == 0 {
			return nil, nil
		}
		return yamlNodeToAnySeen(node.Content[0], seen)
	case yamlv3.AliasNode:
		return yamlNodeToAnySeen(node.Alias, seen)
	case yamlv3.MappingNode:
		return yamlMappingNodeToAny(node, seen)
	case yamlv3.SequenceNode:
		result := make([]any, 0, len(node.Content))
		for _, child := range node.Content {
			value, err := yamlNodeToAnySeen(child, seen)
			if err != nil {
				return nil, err
			}
			result = append(result, value)
		}
		return result, nil
	case yamlv3.ScalarNode:
		return yamlScalarString(node), nil
	default:
		return nil, nil
	}
}

func isYAMLMergeKey(node *yamlv3.Node) bool {
	if node == nil || yamlScalarString(node) != "<<" {
		return false
	}
	return node.Tag == "!!merge" || node.Tag == "tag:yaml.org,2002:merge"
}

func cloudFormationYAMLIntrinsicKey(tag string) (string, bool) {
	switch tag {
	case "!And":
		return "Fn::And", true
	case "!Condition":
		return "Condition", true
	case "!Equals":
		return "Fn::Equals", true
	case "!GetAtt":
		return "Fn::GetAtt", true
	case "!If":
		return "Fn::If", true
	case "!ImportValue":
		return "Fn::ImportValue", true
	case "!Join":
		return "Fn::Join", true
	case "!Or":
		return "Fn::Or", true
	case "!Ref":
		return "Ref", true
	case "!Sub":
		return "Fn::Sub", true
	default:
		return "", false
	}
}

func yamlScalarString(node *yamlv3.Node) string {
	if node == nil {
		return ""
	}
	return strings.TrimSpace(node.Value)
}
