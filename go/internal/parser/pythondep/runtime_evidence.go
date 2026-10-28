// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pythondep

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/pelletier/go-toml/v2/unstable"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	"github.com/eshu-hq/eshu/go/internal/parser/shared/toml/nesting"
)

const pythonRuntimeMaxBytes = 1 << 20

// PythonRuntimeValue is a validated string declaration and its original line.
type PythonRuntimeValue struct {
	SourceKind string
	Value      string
	Line       int
}

// PythonRuntimeEvidence is parser-private, bounded evidence for one TOML file.
// State distinguishes an empty valid document, rejected values, a token-cap
// skip and a failed parse; callers must not interpret an absent transport as
// an empty document. TokenCount records bound setup.py keyword declarations.
type PythonRuntimeEvidence struct {
	Values     []PythonRuntimeValue
	Rejected   int
	State      string
	ByteSize   int
	TokenCount int
	Digest     string
}

type pythonTOMLDocument struct {
	sections []*tomlSection
	decoded  map[string]any
	evidence PythonRuntimeEvidence
}

// ExtractPythonRuntimeEvidence validates a TOML file and extracts only its
// interpreter declarations. It is also the legacy content-only entrypoint.
func ExtractPythonRuntimeEvidence(filename string, source []byte) (PythonRuntimeEvidence, error) {
	doc, err := parsePythonTOML(filename, source)
	return doc.evidence, err
}

func parsePythonTOML(filename string, source []byte) (pythonTOMLDocument, error) {
	evidence := PythonRuntimeEvidence{State: "unavailable", ByteSize: len(source), Digest: PythonRuntimeDigest(source)}
	overRuntimeCap := len(source) > pythonRuntimeMaxBytes
	if err := nesting.CheckBounds(source, nesting.Bounds{Depth: 64, KeyWork: 25_000_000}); err != nil {
		return pythonTOMLDocument{evidence: evidence}, err
	}
	var decoded map[string]any
	if err := toml.Unmarshal(source, &decoded); err != nil {
		return pythonTOMLDocument{evidence: evidence}, err
	}
	sections, positions, err := pythonTOMLStructure(source, decoded)
	if err != nil {
		return pythonTOMLDocument{evidence: evidence}, err
	}
	// The syntax tree supplies source order and positions only. Both the
	// dependency rows and runtime evidence take values from this one validated
	// semantic document; escaped TOML strings cannot diverge between them.
	if overRuntimeCap {
		// The dependency parser keeps its own source-read contract; only
		// runtime evidence is capped at the declaration manifest limit.
		evidence.State = "over_cap"
		return pythonTOMLDocument{sections: sections, decoded: decoded, evidence: evidence}, nil
	}
	evidence.State = "parsed_empty"
	for _, field := range pythonRuntimeFields(filename) {
		value, exists := pythonTOMLAt(decoded, field.path)
		if !exists {
			continue
		}
		valueString, ok := value.(string)
		if !ok || !shared.SafeVersionSpec(strings.TrimSpace(valueString)) {
			evidence.Rejected++
			continue
		}
		line := positions[strings.Join(field.path, "\x00")]
		if line < 1 {
			return pythonTOMLDocument{evidence: PythonRuntimeEvidence{State: "unavailable", ByteSize: len(source), Digest: evidence.Digest}}, fmt.Errorf("runtime value has no TOML source position")
		}
		evidence.Values = append(evidence.Values, PythonRuntimeValue{SourceKind: field.kind, Value: strings.Clone(strings.TrimSpace(valueString)), Line: line})
	}
	if len(evidence.Values) > 0 || evidence.Rejected > 0 {
		evidence.State = "parsed_values"
	}
	return pythonTOMLDocument{sections: sections, decoded: decoded, evidence: evidence}, nil
}

func pythonTOMLValue(value any) tomlValue {
	switch typed := value.(type) {
	case string:
		return tomlValue{IsString: true, StringValue: typed, Scalar: typed}
	case []any:
		array := make([]string, 0, len(typed))
		for _, element := range typed {
			text, ok := element.(string)
			if !ok {
				return tomlValue{}
			}
			array = append(array, text)
		}
		return tomlValue{IsArray: true, Array: array}
	case map[string]any:
		inline := make(map[string]tomlValue, len(typed))
		for key, element := range typed {
			inline[key] = pythonTOMLValue(element)
		}
		return tomlValue{IsInline: true, InlineTable: inline}
	default:
		return tomlValue{}
	}
}

type pythonRuntimeField struct {
	path []string
	kind string
}

func pythonRuntimeFields(filename string) []pythonRuntimeField {
	switch strings.ToLower(filename) {
	case "pyproject.toml":
		return []pythonRuntimeField{
			{[]string{"project", "requires-python"}, "pyproject_requires_python"},
			{[]string{"tool", "poetry", "dependencies", "python"}, "poetry_python"},
		}
	case "pipfile":
		return []pythonRuntimeField{
			{[]string{"requires", "python_version"}, "pipfile_python_version"},
			{[]string{"requires", "python_full_version"}, "pipfile_python_full_version"},
		}
	default:
		return nil
	}
}

func pythonTOMLAt(document map[string]any, path []string) (any, bool) {
	var current any = document
	for _, part := range path {
		table, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = table[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func pythonTOMLStructure(source []byte, decoded map[string]any) ([]*tomlSection, map[string]int, error) {
	root := &tomlSection{Header: "", RawValues: map[string]string{}, Values: map[string]tomlValue{}}
	sections := []*tomlSection{root}
	current := root
	positions := make(map[string]int)
	var parser unstable.Parser
	parser.Reset(source)
	var table []string
	for parser.NextExpression() {
		node := parser.Expression()
		switch node.Kind {
		case unstable.Table, unstable.ArrayTable:
			table = pythonTOMLNodeKeys(node)
			current = &tomlSection{
				Header: strings.Join(table, "."), IsArray: node.Kind == unstable.ArrayTable,
				StartLine: parser.Shape(node.Raw).Start.Line,
				RawValues: map[string]string{}, Values: map[string]tomlValue{},
			}
			sections = append(sections, current)
		case unstable.KeyValue:
			key := pythonTOMLNodeKeys(node)
			if len(key) == 0 {
				continue
			}
			path := append(append([]string(nil), table...), key...)
			line := parser.Shape(node.Raw).Start.Line
			positions[strings.Join(path, "\x00")] = line
			name := strings.Join(key, ".")
			value, found := pythonTOMLAt(decoded, path)
			if !found {
				continue
			}
			parsed := pythonTOMLValue(value)
			parsed.LineNumber = line
			current.Keys = append(current.Keys, name)
			current.Values[name] = parsed
		default:
			continue
		}
	}
	return sections, positions, parser.Error()
}

func pythonTOMLNodeKeys(node *unstable.Node) []string {
	keys := make([]string, 0, 4)
	iterator := node.Key()
	for iterator.Next() {
		keys = append(keys, string(iterator.Node().Data))
	}
	return keys
}

// PythonRuntimeDigest binds private parser evidence to exact source bytes.
func PythonRuntimeDigest(source []byte) string {
	sum := sha256.Sum256(source)
	return hex.EncodeToString(sum[:])
}
