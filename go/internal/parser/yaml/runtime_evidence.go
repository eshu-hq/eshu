// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package yaml

import (
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	yamlv3 "gopkg.in/yaml.v3"
)

// helmRuntimeEvidence reads kubeVersion from the root of the same chart bytes.
func helmRuntimeEvidence(source []byte) shared.RuntimeEvidence {
	evidence := shared.NewRuntimeEvidence(source)
	if evidence.State == "over_cap" {
		return evidence
	}
	var document yamlv3.Node
	if err := yamlv3.Unmarshal(source, &document); err != nil || len(document.Content) != 1 {
		evidence.State = "unavailable"
		return evidence
	}
	root := document.Content[0]
	if root.Kind != yamlv3.MappingNode {
		evidence.State = "unavailable"
		return evidence
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		key, value := root.Content[i], root.Content[i+1]
		if key.Value != "kubeVersion" {
			continue
		}
		if value.Kind != yamlv3.ScalarNode || value.Tag != "!!str" {
			evidence.Rejected++
			continue
		}
		evidence.Add("helm_chart_kube_version", "kubernetes", value.Value, value.Line)
	}
	return evidence
}
