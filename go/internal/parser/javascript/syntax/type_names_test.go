// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package syntax

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func TestTypeReferenceLeafNameTakesFinalSegment(t *testing.T) {
	t.Parallel()

	for full, want := range map[string]string{
		"Service": "Service", "ns.Service": "Service", "a.b.C": "C",
		"ns:Service": "Service", "  spaced  ": "spaced", "": "",
	} {
		if got := TypeReferenceLeafName(full); got != want {
			t.Errorf("TypeReferenceLeafName(%q) = %q, want %q", full, got, want)
		}
	}
}

func TestAppendTypeReferenceCallsSkipsBuiltinsAndLang(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseTSRootForTest(t, `import { Service } from "./svc";
function run(svc: Service, count: number): Promise<Service> {
  const cast = svc as Service;
  return svc;
}
`)
	defer closeFn()

	payload := map[string]any{}
	AppendTypeReferenceCalls(payload, root, source, "typescript")
	calls, _ := payload["function_calls"].([]map[string]any)
	names := map[string]int{}
	for _, call := range calls {
		name, _ := call["name"].(string)
		kind, _ := call["call_kind"].(string)
		lang, _ := call["lang"].(string)
		if kind != "typescript.type_reference" || lang != "typescript" {
			t.Errorf("call row = %v, want type-reference kind and lang", call)
		}
		names[name]++
	}
	if names["Service"] == 0 {
		t.Errorf("no Service references recorded: %v", payload)
	}
	for _, builtin := range []string{"number", "Promise", "string"} {
		if names[builtin] != 0 {
			t.Errorf("builtin %s recorded as a reference: %v", builtin, payload)
		}
	}

	plain := map[string]any{}
	AppendTypeReferenceCalls(plain, root, source, "javascript")
	if len(plain) != 0 {
		t.Errorf("javascript lang payload = %v, want empty", plain)
	}
	unknown := map[string]any{}
	AppendTypeReferenceCalls(unknown, root, source, "ruby")
	if len(unknown) != 0 {
		t.Errorf("unknown lang payload = %v, want empty", unknown)
	}
}

func TestFunctionNameReadsIdentifierKinds(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseRootForTest(t, "function named() {}\nconst o = { key: 1 };\n")
	defer closeFn()

	var funcName, pairKey *tree_sitter.Node
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "function_declaration":
			if funcName == nil {
				funcName = node.ChildByFieldName("name")
			}
		case "pair":
			if pairKey == nil {
				pairKey = node.ChildByFieldName("key")
			}
		}
	})
	if got := FunctionName(funcName, source); got != "named" {
		t.Errorf("function name = %q, want named", got)
	}
	if got := FunctionName(pairKey, source); got != "key" {
		t.Errorf("property key name = %q, want key", got)
	}
	if got := FunctionName(nil, source); got != "" {
		t.Errorf("nil name = %q, want empty", got)
	}
}

func TestTrimQuotesTable(t *testing.T) {
	t.Parallel()

	for text, want := range map[string]struct {
		unquoted string
		ok       bool
	}{
		`"abc"`: {"abc", true}, `'abc'`: {"abc", true}, "`abc`": {"abc", true},
		`"a"`: {"a", true}, `""`: {"", true}, `"ab'`: {`"ab'`, false},
		`x`: {`x`, false}, ``: {``, false}, `"`: {`"`, false},
	} {
		unquoted, ok := TrimQuotes(text)
		if unquoted != want.unquoted || ok != want.ok {
			t.Errorf("TrimQuotes(%q) = (%q, %v), want (%q, %v)", text, unquoted, ok, want.unquoted, want.ok)
		}
	}
}

func TestParameterCountCountsNamedChildren(t *testing.T) {
	t.Parallel()

	root, _, closeFn := parseRootForTest(t, "function none() {}\nfunction two(a, b) {}\n")
	defer closeFn()

	counts := []int{}
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() == "formal_parameters" {
			counts = append(counts, ParameterCount(node, nil))
		}
	})
	if len(counts) != 2 || counts[0] != 0 || counts[1] != 2 {
		t.Errorf("parameter counts = %v, want [0 2]", counts)
	}
	if got := ParameterCount(nil, nil); got != 0 {
		t.Errorf("nil node = %d, want 0", got)
	}
}

func TestParentLookupParentOfRootIsNil(t *testing.T) {
	t.Parallel()

	root, _, closeFn := parseRootForTest(t, "const x = 1;\n")
	defer closeFn()
	lookup := BuildParentLookup(root)

	if got := lookup.Parent(root); got != nil {
		t.Errorf("root parent = %v, want nil", got)
	}
	var ident *tree_sitter.Node
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if ident == nil && node.Kind() == "identifier" {
			ident = node
		}
	})
	parent := lookup.Parent(ident)
	if parent == nil || parent.Kind() != "variable_declarator" {
		t.Errorf("identifier parent = %v, want the declarator", parent)
	}
	if got := lookup.Parent(nil); got != nil {
		t.Errorf("nil node parent = %v, want nil", got)
	}
}
