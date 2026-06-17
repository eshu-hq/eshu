package golang

import (
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// blankIdentifier is Go's write-only sink; it is never a meaningful definition
// or use for value-flow purposes.
const blankIdentifier = "_"

// goFunctionParamNames returns the receiver and parameter binding names of a
// function or method declaration, in declaration order. Anonymous parameters
// (a type with no name) contribute nothing.
func goFunctionParamNames(node *tree_sitter.Node, source []byte) []string {
	var names []string
	names = append(names, goParameterListNames(node.ChildByFieldName("receiver"), source)...)
	names = append(names, goParameterListNames(node.ChildByFieldName("parameters"), source)...)
	return names
}

// goParameterListNames collects the identifier names declared in a parameter
// list, skipping the type identifiers that share the list.
func goParameterListNames(list *tree_sitter.Node, source []byte) []string {
	if list == nil {
		return nil
	}
	var names []string
	cursor := list.Walk()
	defer cursor.Close()
	for _, decl := range list.NamedChildren(cursor) {
		if decl.Kind() != "parameter_declaration" && decl.Kind() != "variadic_parameter_declaration" {
			continue
		}
		declCursor := decl.Walk()
		for _, field := range decl.NamedChildren(declCursor) {
			if field.Kind() == "identifier" {
				if name := nodeText(&field, source); name != "" && name != blankIdentifier {
					names = append(names, name)
				}
			}
		}
		declCursor.Close()
	}
	return names
}

// goStmtDefsUses returns the bindings a statement defines and the bindings it
// uses for reaching-definition purposes. It handles the definition-bearing Go
// statement kinds; callers route other kinds through goExprUses.
func goStmtDefsUses(node *tree_sitter.Node, source []byte) (defs, uses []string) {
	switch node.Kind() {
	case "short_var_declaration":
		if left := node.ChildByFieldName("left"); left != nil {
			defs = goAssignTargets(left, source)
		}
		if right := node.ChildByFieldName("right"); right != nil {
			uses = goExprUses(right, source)
		}
	case "assignment_statement":
		defs, uses = goAssignmentDefsUses(node, source)
	case "var_declaration", "const_declaration":
		defs, uses = goSpecDefsUses(node, source)
	case "inc_statement", "dec_statement":
		// x++ / x-- both read and write the operand.
		if operand := firstNamedChild(node); operand != nil && operand.Kind() == "identifier" {
			if name := nodeText(operand, source); name != "" && name != blankIdentifier {
				defs = []string{name}
				uses = []string{name}
			}
		}
	}
	return defs, uses
}

// goAssignmentDefsUses splits an assignment into defined and used bindings. A
// plain identifier target is a definition; a selector or index target reads its
// base (field/element flow is modeled later). A compound operator (for example
// +=) also reads the target.
func goAssignmentDefsUses(node *tree_sitter.Node, source []byte) (defs, uses []string) {
	left := node.ChildByFieldName("left")
	right := node.ChildByFieldName("right")
	compound := goIsCompoundAssign(node, source)

	if left != nil {
		cursor := left.Walk()
		defer cursor.Close()
		for _, target := range left.NamedChildren(cursor) {
			target := target
			if target.Kind() == "identifier" {
				name := nodeText(&target, source)
				if name == "" || name == blankIdentifier {
					continue
				}
				defs = append(defs, name)
				if compound {
					uses = append(uses, name)
				}
				continue
			}
			// Non-identifier target (a.b, a[i]): the base is read, not defined.
			uses = append(uses, goExprUses(&target, source)...)
		}
	}
	if right != nil {
		uses = append(uses, goExprUses(right, source)...)
	}
	return defs, uses
}

// goSpecDefsUses collects definitions and uses from a var or const declaration's
// specs.
func goSpecDefsUses(node *tree_sitter.Node, source []byte) (defs, uses []string) {
	walkNamed(node, func(child *tree_sitter.Node) {
		if child.Kind() != "var_spec" && child.Kind() != "const_spec" {
			return
		}
		if name := child.ChildByFieldName("name"); name != nil {
			if text := nodeText(name, source); text != "" && text != blankIdentifier {
				defs = append(defs, text)
			}
		}
		// Multiple names: var a, b = f()
		cursor := child.Walk()
		for _, field := range child.NamedChildren(cursor) {
			if field.Kind() == "identifier" {
				if text := nodeText(&field, source); text != "" && text != blankIdentifier {
					defs = appendUnique(defs, text)
				}
			}
		}
		cursor.Close()
		if value := child.ChildByFieldName("value"); value != nil {
			uses = append(uses, goExprUses(value, source)...)
		}
	})
	return defs, uses
}

// goAssignTargets returns the identifier targets on the left of an assignment or
// short variable declaration, skipping the blank identifier.
func goAssignTargets(left *tree_sitter.Node, source []byte) []string {
	var targets []string
	cursor := left.Walk()
	defer cursor.Close()
	for _, child := range left.NamedChildren(cursor) {
		if child.Kind() != "identifier" {
			continue
		}
		if name := nodeText(&child, source); name != "" && name != blankIdentifier {
			targets = append(targets, name)
		}
	}
	return targets
}

// goExprUses returns the identifier names read within an expression subtree. It
// does not descend into nested function literals, so a closure's captured
// variables are not attributed to the enclosing function (closures are modeled
// later). The blank identifier is never a use.
func goExprUses(node *tree_sitter.Node, source []byte) []string {
	if node == nil {
		return nil
	}
	var uses []string
	var visit func(*tree_sitter.Node)
	visit = func(current *tree_sitter.Node) {
		if current == nil || current.Kind() == "func_literal" {
			return
		}
		if current.Kind() == "identifier" {
			if name := nodeText(current, source); name != "" && name != blankIdentifier {
				uses = append(uses, name)
			}
		}
		cursor := current.Walk()
		defer cursor.Close()
		for _, child := range current.NamedChildren(cursor) {
			child := child
			visit(&child)
		}
	}
	visit(node)
	return uses
}

// goIsCompoundAssign reports whether an assignment uses a compound operator such
// as += so the target is also read.
func goIsCompoundAssign(node *tree_sitter.Node, source []byte) bool {
	if op := node.ChildByFieldName("operator"); op != nil {
		text := nodeText(op, source)
		return text != "" && text != "="
	}
	// Fallback: the operator is the unnamed token sitting between the left and
	// right field nodes. Compare by byte range since tree-sitter nodes are not
	// directly comparable.
	left := node.ChildByFieldName("left")
	right := node.ChildByFieldName("right")
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.Children(cursor) {
		child := child
		if child.IsNamed() || sameSpan(&child, left) || sameSpan(&child, right) {
			continue
		}
		op := nodeText(&child, source)
		if op != "" && op != "=" {
			return true
		}
	}
	return false
}

// sameSpan reports whether two nodes cover the same byte range.
func sameSpan(a, b *tree_sitter.Node) bool {
	if a == nil || b == nil {
		return false
	}
	return a.StartByte() == b.StartByte() && a.EndByte() == b.EndByte()
}

// firstNamedChild returns the first named child of a node, or nil.
func firstNamedChild(node *tree_sitter.Node) *tree_sitter.Node {
	if node == nil {
		return nil
	}
	cursor := node.Walk()
	defer cursor.Close()
	children := node.NamedChildren(cursor)
	if len(children) == 0 {
		return nil
	}
	first := children[0]
	return &first
}

// appendUnique appends value when it is not already present, preserving order.
func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
