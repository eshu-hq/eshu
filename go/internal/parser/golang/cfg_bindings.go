package golang

import tree_sitter "github.com/tree-sitter/go-tree-sitter"

// blankIdentifier is Go's write-only sink; it is never a meaningful definition
// or use for value-flow purposes.
const blankIdentifier = "_"

// goDefaultAccessPathParts matches cfg.DefaultLimits().MaxAccessPathParts for
// helper callers that do not pass a CFG limit object.
const goDefaultAccessPathParts = 4

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

// goStmtDefsUsesWithAliases returns the bindings a statement defines and the
// bindings it uses for reaching-definition purposes. It handles the
// definition-bearing Go statement kinds; callers route other kinds through
// goExprUsesWithOptions.
func goStmtDefsUsesWithAliases(node *tree_sitter.Node, source []byte, aliases goBindingAliases) (defs, uses []string) {
	return goStmtDefsUsesWithOptions(node, source, aliases, goAccessPathOptions{})
}

func goStmtDefsUsesWithOptions(node *tree_sitter.Node, source []byte, aliases goBindingAliases, options goAccessPathOptions) (defs, uses []string) {
	switch node.Kind() {
	case "short_var_declaration":
		if left := node.ChildByFieldName("left"); left != nil {
			defs = goAssignTargetsWithOptions(left, source, aliases, options)
		}
		if right := node.ChildByFieldName("right"); right != nil {
			uses = goExprUsesWithOptions(right, source, aliases, options)
		}
	case "assignment_statement":
		defs, uses = goAssignmentDefsUsesWithOptions(node, source, aliases, options)
	case "var_declaration", "const_declaration":
		defs, uses = goSpecDefsUsesWithOptions(node, source, aliases, options)
	case "inc_statement", "dec_statement":
		// x++ / x-- both read and write the operand.
		if operand := firstNamedChild(node); operand != nil {
			if name, ok := goAccessPathWithOptions(operand, source, aliases, options); ok && name != blankIdentifier {
				defs = []string{name}
				uses = []string{name}
			}
		}
	}
	return defs, uses
}

// goAssignmentDefsUsesWithOptions splits an assignment into defined and used
// bindings. A plain identifier target is a definition; a selector target is a
// precise access-path definition. A compound operator (for example +=) also
// reads the target.
func goAssignmentDefsUsesWithOptions(node *tree_sitter.Node, source []byte, aliases goBindingAliases, options goAccessPathOptions) (defs, uses []string) {
	left := node.ChildByFieldName("left")
	right := node.ChildByFieldName("right")
	compound := goIsCompoundAssign(node, source)

	if left != nil {
		cursor := left.Walk()
		defer cursor.Close()
		for _, target := range left.NamedChildren(cursor) {
			target := target
			if name, ok := goAccessPathWithOptions(&target, source, aliases, options); ok && name != blankIdentifier {
				defs = append(defs, name)
				if compound {
					uses = append(uses, name)
				}
				continue
			}
			// Unsupported target (for example a[i]) reads its components but is
			// not modeled as a precise definition in this field-sensitive pass.
			uses = append(uses, goExprUsesWithOptions(&target, source, aliases, options)...)
		}
	}
	if right != nil {
		uses = append(uses, goExprUsesWithOptions(right, source, aliases, options)...)
	}
	return defs, uses
}

// goSpecDefsUsesWithOptions collects definitions and uses from a var or const
// declaration's specs.
func goSpecDefsUsesWithOptions(node *tree_sitter.Node, source []byte, aliases goBindingAliases, options goAccessPathOptions) (defs, uses []string) {
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
			uses = append(uses, goExprUsesWithOptions(value, source, aliases, options)...)
		}
	})
	return defs, uses
}

// goAssignTargets returns the identifier targets on the left of an assignment or
// short variable declaration, skipping the blank identifier.
func goAssignTargets(left *tree_sitter.Node, source []byte) []string {
	return goAssignTargetsWithAliases(left, source, nil)
}

func goAssignTargetsWithAliases(left *tree_sitter.Node, source []byte, aliases goBindingAliases) []string {
	return goAssignTargetsWithOptions(left, source, aliases, goAccessPathOptions{})
}

func goAssignTargetsWithOptions(left *tree_sitter.Node, source []byte, aliases goBindingAliases, options goAccessPathOptions) []string {
	var targets []string
	cursor := left.Walk()
	defer cursor.Close()
	for _, child := range left.NamedChildren(cursor) {
		if name, ok := goAssignTargetPathWithOptions(&child, source, aliases, options); ok && name != "" && name != blankIdentifier {
			targets = append(targets, name)
		}
	}
	return targets
}

// goExprUsesWithOptions returns the identifier and selector access-path names
// read within an expression subtree. It does not descend into nested function
// literals, so a closure's captured variables are not attributed to the
// enclosing function (closures are modeled later). The blank identifier is never
// a use.
func goExprUsesWithOptions(node *tree_sitter.Node, source []byte, aliases goBindingAliases, options goAccessPathOptions) []string {
	if node == nil {
		return nil
	}
	var uses []string
	var visit func(*tree_sitter.Node, bool)
	visit = func(current *tree_sitter.Node, includeFuncLiteralCaptures bool) {
		if current == nil {
			return
		}
		if current.Kind() == "func_literal" {
			if includeFuncLiteralCaptures {
				uses = append(uses, goFuncLiteralCaptureUses(current, source, aliases, options)...)
			}
			return
		}
		if name, ok := goAccessPathWithOptions(current, source, aliases, options); ok {
			if name != "" && name != blankIdentifier {
				uses = append(uses, name)
				if current.Kind() == "selector_expression" {
					uses = appendBaseAccessPath(uses, current, source, aliases, options)
				}
			}
			if current.Kind() == "selector_expression" {
				return
			}
		}
		if current.Kind() == "call_expression" {
			cursor := current.Walk()
			defer cursor.Close()
			for _, child := range current.NamedChildren(cursor) {
				child := child
				visit(&child, child.Kind() == "func_literal" || child.Kind() == "argument_list")
			}
			return
		}
		cursor := current.Walk()
		defer cursor.Close()
		for _, child := range current.NamedChildren(cursor) {
			child := child
			visit(&child, includeFuncLiteralCaptures)
		}
	}
	visit(node, false)
	return uses
}

func goFuncLiteralCaptureUses(node *tree_sitter.Node, source []byte, aliases goBindingAliases, options goAccessPathOptions) []string {
	local := map[string]struct{}{}
	for _, name := range goParameterListNames(node.ChildByFieldName("parameters"), source) {
		local[name] = struct{}{}
	}
	body := node.ChildByFieldName("body")
	if body == nil {
		return nil
	}
	walkScopeBindings(body, func(child *tree_sitter.Node) {
		switch child.Kind() {
		case "short_var_declaration", "var_declaration", "const_declaration":
			defs, _ := goStmtDefsUsesWithOptions(child, source, nil, options)
			for _, def := range defs {
				local[def] = struct{}{}
			}
		}
	})
	uses := goExprUsesWithOptions(body, source, aliases, options)
	out := make([]string, 0, len(uses))
	for _, use := range uses {
		if _, shadowed := local[use]; shadowed {
			continue
		}
		out = appendUnique(out, use)
	}
	return out
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
