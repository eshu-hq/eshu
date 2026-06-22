package kotlin

import (
	"errors"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

var (
	errNilParser = errors.New("parse kotlin tree: nil parser")
	errNilTree   = errors.New("parse kotlin tree: parser returned nil tree")
)

// kotlinPackageNameFromTree returns the file package name from the AST.
func kotlinPackageNameFromTree(root *tree_sitter.Node, source []byte) string {
	if root == nil {
		return ""
	}
	cursor := root.Walk()
	defer cursor.Close()
	for _, child := range root.NamedChildren(cursor) {
		child := child
		if child.Kind() != "package_header" {
			continue
		}
		return strings.TrimSpace(shared.NodeText(child.ChildByFieldName("name"), source))
	}
	return ""
}

// collectDeclarations runs a structural pre-pass that records declared type
// names, type parameters, class property types, implemented interfaces,
// interface method sets, import aliases, and this file's own function return
// types. It mirrors the previous parser's whole-file context map without
// depending on source order.
func (w *astWalker) collectDeclarations(root *tree_sitter.Node) {
	w.collectDeclarationsIn(root, "")
}

func (w *astWalker) collectDeclarationsIn(node *tree_sitter.Node, currentType string) {
	if node == nil {
		return
	}

	nextType := currentType
	switch node.Kind() {
	case "import":
		if alias := w.importAlias(node); alias != "" {
			w.knownTypeNames[alias] = struct{}{}
		}
	case "class_declaration", "object_declaration", "companion_object":
		name := w.declarationName(node)
		if name != "" {
			nextType = name
			if node.Kind() == "companion_object" && currentType != "" {
				nextType = currentType
			}
			w.knownTypeNames[name] = struct{}{}
			w.localTypeNames[name] = struct{}{}
			if params := w.declaredTypeParameters(node); len(params) > 0 {
				w.classTypeParameters[name] = params
			}
			if implemented := w.implementedTypes(node); len(implemented) > 0 {
				set := make(map[string]struct{}, len(implemented))
				for _, implementedType := range implemented {
					set[implementedType] = struct{}{}
				}
				w.classInterfaces[name] = set
			}
			w.collectPrimaryConstructorProperties(name, node)
			if w.isInterfaceDeclaration(node) {
				w.collectInterfaceMethods(name, node)
			}
		}
	case "function_declaration":
		w.recordFunctionReturnType(node, currentType)
	}

	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		w.collectDeclarationsIn(&child, nextType)
	}
}

// collectPrimaryConstructorProperties records the property name -> type map
// declared in a class primary constructor (`class C(val x: T)`).
func (w *astWalker) collectPrimaryConstructorProperties(className string, node *tree_sitter.Node) {
	if className == "" {
		return
	}
	primary := w.childByKind(node, "primary_constructor")
	if primary == nil {
		return
	}
	shared.WalkNamed(primary, func(child *tree_sitter.Node) {
		if child.Kind() != "class_parameter" {
			return
		}
		if !w.classParameterIsProperty(child) {
			return
		}
		name := strings.TrimSpace(shared.NodeText(w.childByKind(child, "identifier"), w.source))
		userType := w.childByKind(child, "user_type")
		if userType == nil {
			if nullable := w.childByKind(child, "nullable_type"); nullable != nil {
				userType = w.childByKind(nullable, "user_type")
			}
		}
		if name == "" || userType == nil {
			return
		}
		typ := kotlinCanonicalTypeReference(shared.NodeText(userType, w.source))
		if typ == "" {
			return
		}
		w.setClassPropertyType(className, name, typ)
	})
}

// classParameterIsProperty reports whether a class parameter declares a
// property (carries a `val`/`var` keyword), as opposed to a plain constructor
// argument.
func (w *astWalker) classParameterIsProperty(node *tree_sitter.Node) bool {
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil || child.IsNamed() {
			continue
		}
		if child.Kind() == "val" || child.Kind() == "var" {
			return true
		}
	}
	return false
}

// recordFunctionReturnType stores a function's declared return type keyed by
// receiver/class context for later receiver inference.
func (w *astWalker) recordFunctionReturnType(node *tree_sitter.Node, currentType string) {
	functionName := strings.TrimSpace(shared.NodeText(node.ChildByFieldName("name"), w.source))
	returnType := w.functionReturnType(node)
	if functionName == "" || returnType == "" {
		return
	}
	key := functionName
	if extension := w.extensionReceiver(node); extension != "" {
		key = extension + "." + functionName
	} else if currentType != "" {
		key = currentType + "." + functionName
	}
	kotlinStoreFunctionReturnType(w.functionReturnTypes, w.packageName, key, returnType)
}

// functionReturnType returns the canonical declared return type of a function
// declaration (`fun f(): Type`), or "".
func (w *astWalker) functionReturnType(node *tree_sitter.Node) string {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return ""
	}
	nameEnd := nameNode.EndByte()
	bodyStart := uint(len(w.source))
	if body := w.childByKind(node, "function_body"); body != nil {
		bodyStart = body.StartByte()
	}
	var typeNode *tree_sitter.Node
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}
		if child.StartByte() < nameEnd || child.StartByte() >= bodyStart {
			continue
		}
		switch child.Kind() {
		case "user_type", "nullable_type":
			typeNode = shared.CloneNode(child)
		}
	}
	if typeNode == nil {
		return ""
	}
	return kotlinCanonicalTypeReference(shared.NodeText(typeNode, w.source))
}

// collectReturnTypesIn records function return types under their type scope for
// a sibling file. It is the bounded subset of collectDeclarations used when
// only return-type evidence is needed.
func (w *astWalker) collectReturnTypesIn(node *tree_sitter.Node, currentType string) {
	if node == nil {
		return
	}
	nextType := currentType
	switch node.Kind() {
	case "class_declaration", "object_declaration", "companion_object":
		if name := w.declarationName(node); name != "" {
			nextType = name
			if node.Kind() == "companion_object" && currentType != "" {
				nextType = currentType
			}
		}
	case "function_declaration":
		w.recordFunctionReturnType(node, currentType)
	}
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		w.collectReturnTypesIn(&child, nextType)
	}
}

// declarationName returns the declared name of a type node, defaulting an
// anonymous companion object to "Companion".
func (w *astWalker) declarationName(node *tree_sitter.Node) string {
	name := strings.TrimSpace(shared.NodeText(node.ChildByFieldName("name"), w.source))
	if name == "" && node.Kind() == "companion_object" {
		return "Companion"
	}
	return name
}

// isInterfaceDeclaration reports whether a class_declaration node uses the
// `interface` keyword. The grammar models interfaces as class_declaration with
// an anonymous `interface` child instead of `class`.
func (w *astWalker) isInterfaceDeclaration(node *tree_sitter.Node) bool {
	if node.Kind() != "class_declaration" {
		return false
	}
	return w.hasAnonymousChild(node, "interface")
}

func (w *astWalker) hasAnonymousChild(node *tree_sitter.Node, kind string) bool {
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		if !child.IsNamed() && child.Kind() == kind {
			return true
		}
	}
	return false
}

func (w *astWalker) childByKind(node *tree_sitter.Node, kind string) *tree_sitter.Node {
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		if child.Kind() == kind {
			return shared.CloneNode(&child)
		}
	}
	return nil
}

// handleTypeDeclaration emits one class/object/companion/interface/enum row and
// recurses into its body under the new type scope.
func (w *astWalker) handleTypeDeclaration(node *tree_sitter.Node, f frame) {
	name := w.declarationName(node)
	if name == "" {
		w.walkChildren(node, f)
		return
	}

	annotations := w.annotations(node)
	kind := "class"
	bucket := "classes"
	if w.isInterfaceDeclaration(node) {
		kind = "interface"
		bucket = "interfaces"
	}

	item := map[string]any{
		"name":        name,
		"line_number": shared.NodeLine(node),
		"end_line":    shared.NodeLine(node),
		"lang":        "kotlin",
	}
	if rootKinds := kotlinTypeDeadCodeRootKinds(annotations, kind); len(rootKinds) > 0 {
		item["dead_code_root_kinds"] = rootKinds
	}
	shared.AppendBucket(w.payload, bucket, item)

	// A companion object's members belong to the enclosing class for
	// class_context purposes, so recurse keeping the outer type scope rather
	// than switching to the companion's own name.
	childFrame := f.withClass(name)
	if node.Kind() == "companion_object" && f.classContext != "" {
		childFrame = f
	}
	w.walkChildren(node, childFrame)
}

// handleImport emits one import/alias row.
func (w *astWalker) handleImport(node *tree_sitter.Node) {
	source := strings.TrimSpace(shared.NodeText(node.ChildByFieldName("name"), w.source))
	if source == "" {
		source = strings.TrimSpace(shared.NodeText(w.childByKind(node, "qualified_identifier"), w.source))
	}
	if source == "" {
		return
	}

	alias := kotlinImportAlias(source)
	importType := "import"
	full := "import " + source
	if explicit := w.importAlias(node); explicit != "" {
		alias = explicit
		importType = "alias"
		full = "import " + source + " as " + explicit
	}

	shared.AppendBucket(w.payload, "imports", map[string]any{
		"name":             source,
		"source":           source,
		"alias":            alias,
		"full_import_name": full,
		"import_type":      importType,
		"line_number":      shared.NodeLine(node),
		"lang":             "kotlin",
	})
}

// importAlias returns the explicit `as` alias of an import node, or "".
func (w *astWalker) importAlias(node *tree_sitter.Node) string {
	if node.Kind() != "import" {
		return ""
	}
	sawAs := false
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		if !child.IsNamed() && child.Kind() == "as" {
			sawAs = true
			continue
		}
		if sawAs && child.Kind() == "identifier" {
			return strings.TrimSpace(shared.NodeText(child, w.source))
		}
	}
	return ""
}
