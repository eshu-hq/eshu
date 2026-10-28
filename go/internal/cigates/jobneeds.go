// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"gopkg.in/yaml.v3"
)

// jobNeeds decodes a job's needs entry in either YAML shape. A single need
// arrives as a scalar string; several arrive as a sequence; an absent entry
// decodes to no needs.
func jobNeeds(node yaml.Node) []string {
	switch node.Kind {
	case yaml.ScalarNode:
		return []string{node.Value}
	case yaml.SequenceNode:
		var needs []string
		for _, item := range node.Content {
			needs = append(needs, item.Value)
		}
		return needs
	default:
		return nil
	}
}
