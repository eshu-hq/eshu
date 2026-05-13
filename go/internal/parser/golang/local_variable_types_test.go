package golang

import (
	"reflect"
	"testing"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_go "github.com/tree-sitter/tree-sitter-go/bindings/go"
)

func TestWalkPackageScopeImportedVariableDeclarationsSkipsFunctionBodies(t *testing.T) {
	t.Parallel()

	source := []byte(`package main

import "example.com/pkg"

var packageClient pkg.Client

func run() {
	var localClient pkg.Client
	_ = localClient
}
`)
	tree := parseGoLocalVariableTypesTestSource(t, source)
	defer tree.Close()

	var visitedVarNames []string
	walkPackageScopeImportedVariableDeclarations(tree.RootNode(), func(node *tree_sitter.Node) {
		if node.Kind() != "var_spec" {
			return
		}
		name := node.ChildByFieldName("name")
		if name == nil {
			return
		}
		visitedVarNames = append(visitedVarNames, string(source[name.StartByte():name.EndByte()]))
	})

	want := []string{"packageClient"}
	if !reflect.DeepEqual(visitedVarNames, want) {
		t.Fatalf("visited var_spec names = %#v, want %#v", visitedVarNames, want)
	}
}

func TestGoKnownImportedVariableTypesOnlyCapturesPackageScope(t *testing.T) {
	t.Parallel()

	source := []byte(`package main

import "example.com/pkg"

var packageClient pkg.Client

func run() {
	var localClient pkg.Client
	_ = localClient
}
`)
	tree := parseGoLocalVariableTypesTestSource(t, source)
	defer tree.Close()

	got := goKnownImportedVariableTypes(
		tree.RootNode(),
		source,
		map[string][]string{"example.com/pkg": []string{"pkg"}},
		goBuildParentLookup(tree.RootNode()),
	)

	want := map[string]string{"packageclient": "example.com/pkg.client"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("goKnownImportedVariableTypes() = %#v, want %#v", got, want)
	}
}

func parseGoLocalVariableTypesTestSource(t *testing.T, source []byte) *tree_sitter.Tree {
	t.Helper()

	parser := tree_sitter.NewParser()
	if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_go.Language())); err != nil {
		t.Fatalf("SetLanguage(go) error = %v", err)
	}
	tree := parser.Parse(source, nil)
	if tree == nil {
		t.Fatal("Parse() returned nil tree")
	}
	return tree
}
