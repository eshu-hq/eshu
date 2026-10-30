// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rust

import (
	"strings"

	"github.com/pelletier/go-toml/v2/unstable"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// cargoRuntimeEvidence reads the decoded package's rust-version and locates its
// source line in the same validated Cargo TOML body.
func cargoRuntimeEvidence(source []byte, document map[string]any) shared.RuntimeEvidence {
	evidence := shared.NewRuntimeEvidence(source)
	packageTable, ok := document["package"].(map[string]any)
	if !ok {
		return evidence
	}
	value, exists := packageTable["rust-version"]
	if !exists {
		return evidence
	}
	version, ok := value.(string)
	if !ok {
		evidence.Rejected++
		return evidence
	}
	if evidence.State == "over_cap" {
		return evidence
	}
	var parser unstable.Parser
	parser.Reset(source)
	var table []string
	for parser.NextExpression() {
		node := parser.Expression()
		switch node.Kind {
		case unstable.Table, unstable.ArrayTable:
			table = cargoNodeKeys(node)
		case unstable.KeyValue:
			key := cargoNodeKeys(node)
			path := append(append([]string(nil), table...), key...)
			if len(path) == 2 && path[0] == "package" && path[1] == "rust-version" {
				evidence.Add("cargo_package_rust_version", "rust", strings.TrimSpace(version), parser.Shape(node.Raw).Start.Line)
				return evidence
			}
		default:
			continue
		}
	}
	// A decoded value without a source position cannot be emitted as truth.
	evidence.State = "unavailable"
	return evidence
}

func cargoNodeKeys(node *unstable.Node) []string {
	var keys []string
	iterator := node.Key()
	for iterator.Next() {
		keys = append(keys, string(iterator.Node().Data))
	}
	return keys
}

// ExtractCargoRuntimeEvidence reads a supplied Cargo body for content-only
// callers. Native parsing uses cargoRuntimeEvidence on its existing decode.
func ExtractCargoRuntimeEvidence(source []byte) shared.RuntimeEvidence {
	evidence := shared.NewRuntimeEvidence(source)
	if evidence.State == "over_cap" {
		return evidence
	}
	var document map[string]any
	if err := decodeCargoTOML(source, &document, nil); err != nil {
		evidence.State = "unavailable"
		return evidence
	}
	return cargoRuntimeEvidence(source, document)
}
