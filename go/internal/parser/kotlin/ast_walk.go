// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kotlin

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// astWalker holds the mutable state threaded through one Kotlin file walk.
//
// The walker replaces the previous line-scan and regex extraction: it descends
// the tree-sitter AST in source order, carrying type scope (current class,
// current function, local-variable types, smart-cast flow) through recursion so
// nested classes, lambdas, and guarded blocks resolve receivers correctly.
type astWalker struct {
	source      []byte
	payload     map[string]any
	packageName string
	indexSource bool

	// classTypeParameters maps a declared class/interface name to its type
	// parameter names, used for generic return-type substitution.
	classTypeParameters map[string][]string
	// classPropertyTypes maps class name -> property name -> type.
	classPropertyTypes map[string]map[string]string
	// interfaceMethods maps interface name -> method names declared on it.
	interfaceMethods map[string]map[string]struct{}
	// classInterfaces maps class name -> implemented type names.
	classInterfaces map[string]map[string]struct{}
	// knownTypeNames holds declared type names and import aliases used to
	// recognize constructor calls.
	knownTypeNames map[string]struct{}
	// localTypeNames holds only types declared in this file. Imported aliases
	// are excluded because Kotlin imports do not distinguish a function from a
	// type, so an imported bare call must still emit a call edge.
	localTypeNames map[string]struct{}
	// functionReturnTypes maps function keys to declared return types, seeded
	// with package-bounded sibling returns and grown during the walk.
	functionReturnTypes map[string]string

	// localVariableTypes maps function key -> variable name -> inferred type.
	localVariableTypes map[string]map[string]string
	// localVariableCallKinds maps function key -> variable name -> call kind.
	localVariableCallKinds map[string]map[string]string
	// seenVariables dedupes the variables bucket by name across the file.
	seenVariables map[string]struct{}
}

// frame carries the lexical scope a node is visited under. It is passed by
// value so each recursion level sees its own enclosing class and function
// without mutating siblings.
type frame struct {
	classContext    string
	functionContext string
	// smartCastTypes overlays narrowed variable types active in this scope
	// (if/when `is` checks). It is keyed by variable name.
	smartCastTypes map[string]string
}

func (f frame) withClass(name string) frame {
	f.classContext = name
	return f
}

func (f frame) withFunction(name string) frame {
	f.functionContext = name
	return f
}

// withSmartCasts returns a frame whose smart-cast overlay is the union of the
// current overlay and the provided narrowings. The map is copied so sibling
// scopes are unaffected.
func (f frame) withSmartCasts(narrowed map[string]string) frame {
	if len(narrowed) == 0 {
		return f
	}
	merged := make(map[string]string, len(f.smartCastTypes)+len(narrowed))
	for name, typ := range f.smartCastTypes {
		merged[name] = typ
	}
	for name, typ := range narrowed {
		if strings.TrimSpace(name) == "" || strings.TrimSpace(typ) == "" {
			continue
		}
		merged[name] = typ
	}
	f.smartCastTypes = merged
	return f
}

// effectiveVariableTypes returns the variable-type view for the current
// function with smart-cast narrowings layered on top.
func (w *astWalker) effectiveVariableTypes(f frame) map[string]string {
	base := w.localVariableTypes[f.functionContext]
	if len(f.smartCastTypes) == 0 {
		return base
	}
	merged := make(map[string]string, len(base)+len(f.smartCastTypes))
	for name, typ := range base {
		merged[name] = typ
	}
	for name, typ := range f.smartCastTypes {
		merged[name] = typ
	}
	return merged
}

// walkFile parses one Kotlin file into the parser payload by walking the AST.
func walkFile(repoRoot, path string, isDependency bool, options shared.Options, parser *tree_sitter.Parser) (map[string]any, error) {
	source, err := shared.ReadSource(path)
	if err != nil {
		return nil, err
	}
	if parser == nil {
		return nil, errNilParser
	}
	tree := parser.Parse(source, nil)
	if tree == nil {
		return nil, errNilTree
	}
	defer tree.Close()

	packageName := kotlinPackageNameFromTree(tree.RootNode(), source)

	siblingReturns, err := kotlinCollectSiblingFunctionReturnTypes(repoRoot, path, packageName, parser)
	if err != nil {
		return nil, err
	}

	w := &astWalker{
		source:                 source,
		payload:                shared.BasePayload(path, "kotlin", isDependency),
		packageName:            packageName,
		indexSource:            options.IndexSource,
		classTypeParameters:    make(map[string][]string),
		classPropertyTypes:     make(map[string]map[string]string),
		interfaceMethods:       make(map[string]map[string]struct{}),
		classInterfaces:        make(map[string]map[string]struct{}),
		knownTypeNames:         make(map[string]struct{}),
		localTypeNames:         make(map[string]struct{}),
		functionReturnTypes:    make(map[string]string, len(siblingReturns)),
		localVariableTypes:     make(map[string]map[string]string),
		localVariableCallKinds: make(map[string]map[string]string),
		seenVariables:          make(map[string]struct{}),
	}
	w.payload["interfaces"] = []map[string]any{}
	for key, returnType := range siblingReturns {
		w.functionReturnTypes[key] = returnType
	}

	// Pre-pass: collect declared type names, type parameters, class property
	// types, implemented interfaces, interface method sets, and the current
	// file's own function return types so receiver inference has full
	// file-level context regardless of declaration order.
	w.collectDeclarations(tree.RootNode())

	// Main pass: emit declarations, variables, and calls in source order.
	w.walkNode(tree.RootNode(), frame{})
	if semantics := kotlinSpringFrameworkSemantics(tree.RootNode(), source); semantics != nil {
		w.payload["framework_semantics"] = semantics
	}

	shared.SortNamedBucket(w.payload, "functions")
	shared.SortNamedBucket(w.payload, "classes")
	shared.SortNamedBucket(w.payload, "interfaces")
	shared.SortNamedBucket(w.payload, "variables")
	shared.SortNamedBucket(w.payload, "imports")
	shared.SortNamedBucket(w.payload, "function_calls")

	return w.payload, nil
}

// walkNode dispatches one node and recurses into its named children under an
// updated frame. Declaration nodes set scope; expression nodes emit calls.
func (w *astWalker) walkNode(node *tree_sitter.Node, f frame) {
	if node == nil {
		return
	}

	switch node.Kind() {
	case "import":
		w.handleImport(node)
		return
	case "class_declaration", "object_declaration", "companion_object":
		w.handleTypeDeclaration(node, f)
		return
	case "function_declaration", "secondary_constructor":
		w.handleFunctionDeclaration(node, f)
		return
	case "property_declaration":
		w.handlePropertyDeclaration(node, f)
		return
	case "call_expression", "infix_expression":
		w.handleCall(node, f)
		w.walkChildren(node, f)
		return
	case "if_expression":
		w.handleIfExpression(node, f)
		return
	case "when_expression":
		w.handleWhenExpression(node, f)
		return
	}

	w.walkChildren(node, f)
}

// walkChildren recurses into the named children of a node under frame f.
func (w *astWalker) walkChildren(node *tree_sitter.Node, f frame) {
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		w.walkNode(&child, f)
	}
}
