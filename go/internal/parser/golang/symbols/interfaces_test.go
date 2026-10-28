// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package symbols

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func TestInterfaceTargetModeled(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		target InterfaceTarget
		want   bool
	}{
		{"zero value is unmodeled", InterfaceTarget{}, false},
		{"local interface is modeled", InterfaceTarget{LocalInterface: "reader"}, true},
		{"imported interface is modeled", InterfaceTarget{Imported: true}, true},
		{"methods alone do not model", InterfaceTarget{ImportedMethods: []string{"close"}}, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.target.Modeled(); got != tc.want {
				t.Errorf("Modeled() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestInterfaceMethodNamesCollectsDirectMethods(t *testing.T) {
	t.Parallel()

	source := []byte("package p\n\ntype Store interface {\n\tGet(key string) string\n\tClose() error\n\tio.Closer\n}\n")
	tree := parseGoLocalVariableTypesTestSource(t, source)
	defer tree.Close()

	var ifaceNode *tree_sitter.Node
	shared.WalkNamed(tree.RootNode(), func(node *tree_sitter.Node) {
		if ifaceNode == nil && node.Kind() == "interface_type" {
			ifaceNode = node
		}
	})
	if ifaceNode == nil {
		t.Fatal("no interface_type in fixture")
	}

	want := []string{"get", "close"}
	if got := InterfaceMethodNames(ifaceNode, source); !reflect.DeepEqual(got, want) {
		t.Errorf("InterfaceMethodNames() = %#v, want %#v", got, want)
	}
	if got := InterfaceMethodNames(nil, source); len(got) != 0 {
		t.Errorf("InterfaceMethodNames(nil) = %#v, want empty", got)
	}
}

func TestAppendUniqueMethodsSkipsBlanksAndDuplicates(t *testing.T) {
	t.Parallel()

	got := AppendUniqueMethods([]string{"close"}, []string{"  Read  ", "", "close", "READ"})
	want := []string{"close", "read"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("AppendUniqueMethods() = %#v, want %#v", got, want)
	}
}

func TestKnownImportedInterfaceMethodsAllowlist(t *testing.T) {
	t.Parallel()

	cases := []struct {
		typeName string
		want     []string
	}{
		{"io.Closer", []string{"close"}},
		{"sourcecypher.Executor", []string{"execute", "executegroup"}},
		{"cypher.GroupExecutor", []string{"executegroup"}},
		{"  io.Closer  ", []string{"close"}},
		{"unknown.Widget", nil},
		{"", nil},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.typeName, func(t *testing.T) {
			t.Parallel()
			if got := KnownImportedInterfaceMethods(tc.typeName); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("KnownImportedInterfaceMethods(%q) = %#v, want %#v", tc.typeName, got, tc.want)
			}
		})
	}
}

func TestReferencedLocalInterfacesOnlyMatchesKnown(t *testing.T) {
	t.Parallel()

	source := []byte("package p\n\ntype Holder struct {\n\tStore Store\n\tOther Other\n}\n")
	tree := parseGoLocalVariableTypesTestSource(t, source)
	defer tree.Close()

	methods := map[string][]string{"store": {"get"}}
	got := ReferencedLocalInterfaces(tree.RootNode(), source, methods)
	if !reflect.DeepEqual(got, []string{"store"}) {
		t.Errorf("ReferencedLocalInterfaces() = %#v, want [store]", got)
	}
	if got := ReferencedLocalInterfaces(nil, source, methods); len(got) != 0 {
		t.Errorf("ReferencedLocalInterfaces(nil) = %#v, want empty", got)
	}
}

func TestInterfaceTargetFromTypeNodeClassifiesEveryKind(t *testing.T) {
	t.Parallel()

	source := []byte("package p\n\ntype Store interface{ Get() }\n\nfunc useLocal(s Store) {}\n\nfunc useImported(c io.Closer) {}\n\nfunc usePlain(n int) {}\n")
	tree := parseGoLocalVariableTypesTestSource(t, source)
	defer tree.Close()
	root := tree.RootNode()
	methods := map[string][]string{"store": {"get"}}

	targets := map[string]InterfaceTarget{}
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "function_declaration" {
			return
		}
		name := string(source[node.ChildByFieldName("name").StartByte():node.ChildByFieldName("name").EndByte()])
		params := node.ChildByFieldName("parameters")
		if params == nil {
			return
		}
		WalkDirectNamed(params, func(param *tree_sitter.Node) {
			if param.Kind() == "parameter_declaration" {
				targets[name] = InterfaceTargetFromTypeNode(param.ChildByFieldName("type"), source, methods)
			}
		})
	})

	if got := targets["useLocal"]; got.LocalInterface != "store" || got.Imported {
		t.Errorf("useLocal target = %#v, want local store", got)
	}
	if got := targets["useImported"]; !got.Imported || len(got.ImportedMethods) == 0 || got.AllowExportedMethods {
		t.Errorf("useImported target = %#v, want imported with known methods", got)
	}
	if got := targets["usePlain"]; got.Modeled() {
		t.Errorf("usePlain target = %#v, want unmodeled", got)
	}
	if got := InterfaceTargetFromTypeNode(nil, source, methods); got.Modeled() {
		t.Errorf("nil node target = %#v, want unmodeled", got)
	}
}

func TestInterfaceTargetFromTypeNodeUnknownImportAllowsExported(t *testing.T) {
	t.Parallel()

	source := []byte("package p\n\nfunc useRemote(w widget.Thing) {}\n")
	tree := parseGoLocalVariableTypesTestSource(t, source)
	defer tree.Close()

	var typeNode *tree_sitter.Node
	shared.WalkNamed(tree.RootNode(), func(node *tree_sitter.Node) {
		if typeNode == nil && node.Kind() == "parameter_declaration" {
			typeNode = node.ChildByFieldName("type")
		}
	})
	if typeNode == nil {
		t.Fatal("no parameter type in fixture")
	}

	got := InterfaceTargetFromTypeNode(typeNode, source, map[string][]string{})
	if !got.Imported || len(got.ImportedMethods) != 0 || !got.AllowExportedMethods {
		t.Errorf("unknown import target = %#v, want imported with AllowExportedMethods", got)
	}
}

func TestFunctionParamInterfaceTargetsMapsPositions(t *testing.T) {
	t.Parallel()

	source := []byte("package p\n\ntype Store interface{ Get() }\n\nfunc handle(s Store, c io.Closer, n int, t Store, u Store) {}\n\nfunc plain(a int) {}\n")
	tree := parseGoLocalVariableTypesTestSource(t, source)
	defer tree.Close()
	root := tree.RootNode()
	methods := map[string][]string{"store": {"get"}}

	targets := FunctionParamInterfaceTargets(root, source, methods)
	byIndex, ok := targets["handle"]
	if !ok {
		t.Fatalf("no targets for handle: %#v", targets)
	}
	for index, wantLocal := range map[int]string{0: "store", 3: "store", 4: "store"} {
		got, ok := byIndex[index]
		if !ok || got.LocalInterface != wantLocal {
			t.Errorf("handle param %d = %#v, want local %q", index, got, wantLocal)
		}
	}
	if got, ok := byIndex[1]; !ok || !got.Imported {
		t.Errorf("handle param 1 = %#v, want imported", got)
	}
	if _, ok := byIndex[2]; ok {
		t.Errorf("handle param 2 (int) has a target, want none")
	}
	if _, ok := targets["plain"]; ok {
		t.Errorf("plain has targets %#v, want none", targets["plain"])
	}
}

func TestFunctionParamImportedInterfaceMethodsReducesToKnown(t *testing.T) {
	t.Parallel()

	source := []byte("package p\n\nfunc handle(c io.Closer, w widget.Thing, n int) {}\n")
	tree := parseGoLocalVariableTypesTestSource(t, source)
	defer tree.Close()

	got := FunctionParamImportedInterfaceMethods(tree.RootNode(), source)
	byIndex, ok := got["handle"]
	if !ok {
		t.Fatalf("no imported methods for handle: %#v", got)
	}
	if methods := byIndex[0]; !reflect.DeepEqual(methods, []string{"close"}) {
		t.Errorf("handle param 0 methods = %#v, want [close]", methods)
	}
	if _, ok := byIndex[1]; ok {
		t.Errorf("unknown import param has methods, want none (empty method set is skipped)")
	}
}

func TestTypeParameterConstraintCandidates(t *testing.T) {
	t.Parallel()

	cases := []struct {
		text string
		want []string
	}{
		{"T io.Reader | Store", []string{"io.reader", "store"}},
		{"T any", nil},
		{"T comparable", nil},
		{"T *Store | ~string", []string{"store", "string"}},
		{"T", nil},
		{"T Store | Store", []string{"store"}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.text, func(t *testing.T) {
			t.Parallel()
			got := TypeParameterConstraintCandidates(tc.text)
			if len(got) == 0 && len(tc.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("TypeParameterConstraintCandidates(%q) = %#v, want %#v", tc.text, got, tc.want)
			}
		})
	}
}
