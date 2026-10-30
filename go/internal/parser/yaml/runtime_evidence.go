// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package yaml

import (
	"github.com/eshu-hq/eshu/go/internal/parser/cloudformation"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	yamlv3 "gopkg.in/yaml.v3"
)

// isStackFile reports whether filename is a Stack manifest. Stack reads only
// the exact name stack.yaml, so stack.yaml.lock, cabal.project and freeze
// files never match.
func isStackFile(filename string) bool {
	return filename == "stack.yaml"
}

// stackEvidenceFromDecoded reads the root resolver from the single shared
// decode the generic YAML path already produced, so native parsing never
// pays for a second pass over the same source. ok is false when the file is
// a CloudFormation template that happens to carry the stack.yaml name, so
// the generic handling keeps owning it. Only a plain string scalar declares
// a resolver: a custom-snapshot mapping or any other shape is a counted
// rejection, never a version. A file without a resolver is absent, distinct
// from a malformed file, which stays a decode error for the caller. Source
// positions come from that shared decode.
func stackEvidenceFromDecoded(source []byte, documents []any, nodes []*yamlv3.Node) (evidence shared.RuntimeEvidence, ok bool) {
	evidence = shared.NewRuntimeEvidence(source)
	if evidence.State == "over_cap" || len(documents) == 0 {
		return evidence, true
	}
	object, isMap := documents[0].(map[string]any)
	if !isMap {
		evidence.State = "unavailable"
		return evidence, true
	}
	if cloudformation.IsTemplate(object) {
		return shared.RuntimeEvidence{}, false
	}
	var root *yamlv3.Node
	if len(nodes) > 0 {
		root = nodes[0]
	}
	if root == nil || root.Kind != yamlv3.MappingNode {
		evidence.State = "unavailable"
		return evidence, true
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		key, value := root.Content[i], root.Content[i+1]
		if key.Value != "resolver" {
			continue
		}
		if value.Kind != yamlv3.ScalarNode || value.Tag != "!!str" {
			evidence.Rejected++
			continue
		}
		evidence.Add("stack_yaml_resolver", "haskell", value.Value, value.Line)
	}
	return evidence, true
}

// ExtractStackRuntimeEvidence reads a supplied stack.yaml body for
// content-only callers: one decode, the same scalar-only resolver rule, and
// a CloudFormation template carrying the stack.yaml name reads as absent.
// Native parsing uses stackEvidenceFromDecoded on its existing decode.
func ExtractStackRuntimeEvidence(source []byte) shared.RuntimeEvidence {
	evidence := shared.NewRuntimeEvidence(source)
	if evidence.State == "over_cap" {
		return evidence
	}
	var document yamlv3.Node
	if err := yamlv3.Unmarshal(source, &document); err != nil || len(document.Content) == 0 {
		if err != nil {
			evidence.State = "unavailable"
		}
		return evidence
	}
	root := document.Content[0]
	if root.Kind != yamlv3.MappingNode {
		evidence.State = "unavailable"
		return evidence
	}
	if value, err := yamlNodeToAny(root); err == nil {
		if object, isMap := value.(map[string]any); isMap && cloudformation.IsTemplate(object) {
			return shared.NewRuntimeEvidence(source)
		}
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		key, value := root.Content[i], root.Content[i+1]
		if key.Value != "resolver" {
			continue
		}
		if value.Kind != yamlv3.ScalarNode || value.Tag != "!!str" {
			evidence.Rejected++
			continue
		}
		evidence.Add("stack_yaml_resolver", "haskell", value.Value, value.Line)
	}
	return evidence
}

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
