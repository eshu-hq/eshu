package csharp

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/cfg"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// csharpParameterName returns the identifier bound by a `parameter` node, or
// empty when the parameter has no name (e.g. discards).
func csharpParameterName(param *tree_sitter.Node, source []byte) string {
	if param == nil {
		return ""
	}
	name := param.ChildByFieldName("name")
	if name == nil {
		return ""
	}
	return strings.TrimSpace(shared.NodeText(name, source))
}

// csharpParameterType returns the declared type text of a `parameter` node.
func csharpParameterType(param *tree_sitter.Node, source []byte) string {
	if param == nil {
		return ""
	}
	if typeNode := param.ChildByFieldName("type"); typeNode != nil {
		return strings.TrimSpace(shared.NodeText(typeNode, source))
	}
	return ""
}

// csharpParameterAttributeNames returns the simple attribute names applied to a
// parameter (e.g. "FromQuery" for `[FromQuery]`).
func csharpParameterAttributeNames(param *tree_sitter.Node, source []byte) []string {
	var names []string
	walkDirectNamed(param, func(child *tree_sitter.Node) {
		if child.Kind() != "attribute_list" {
			return
		}
		for _, name := range csharpAttributeNamesFromList(child, source) {
			names = append(names, csharpLastTypeSegment(name))
		}
	})
	return names
}

// csharpDataflowParamNames returns the ordered parameter identifiers of a
// method/constructor for CFG entry seeding.
func csharpDataflowParamNames(node *tree_sitter.Node, source []byte) []string {
	params := node.ChildByFieldName("parameters")
	if params == nil {
		return nil
	}
	var names []string
	walkDirectNamed(params, func(child *tree_sitter.Node) {
		if child.Kind() != "parameter" {
			return
		}
		if name := csharpParameterName(child, source); name != "" {
			names = append(names, name)
		}
	})
	return names
}

// csharpTypeEnv maps in-scope variable names to their declared simple/qualified
// type text. It is the C# substitute for Java's global call-inference index:
// receiver types are resolved locally from parameters and explicitly-typed local
// declarations, never from same-name guesses.
type csharpTypeEnv map[string]string

// lookup returns the declared type text bound to a variable name, or empty when
// the variable's type is unknown (e.g. `var`/implicit_type locals).
func (e csharpTypeEnv) lookup(name string) string {
	if e == nil {
		return ""
	}
	return e[name]
}

// csharpBuildTypeEnv records the declared types of a function's parameters and
// explicitly-typed locals. Implicitly-typed (`var`) locals are intentionally
// omitted: an unknown receiver type must not match a sink, preserving the
// honesty contract over a same-name false positive.
func csharpBuildTypeEnv(funcNode *tree_sitter.Node, source []byte) csharpTypeEnv {
	env := csharpTypeEnv{}
	if params := funcNode.ChildByFieldName("parameters"); params != nil {
		walkDirectNamed(params, func(child *tree_sitter.Node) {
			if child.Kind() != "parameter" {
				return
			}
			name := csharpParameterName(child, source)
			typeText := csharpParameterType(child, source)
			if name != "" && typeText != "" {
				env[name] = typeText
			}
		})
	}
	walkInCSharpFunction(funcNode, func(node *tree_sitter.Node) {
		if node.Kind() != "variable_declaration" {
			return
		}
		typeNode := node.ChildByFieldName("type")
		if typeNode == nil || typeNode.Kind() == "implicit_type" {
			return
		}
		typeText := strings.TrimSpace(shared.NodeText(typeNode, source))
		if typeText == "" {
			return
		}
		walkDirectNamed(node, func(child *tree_sitter.Node) {
			if child.Kind() != "variable_declarator" {
				return
			}
			nameNode := child.ChildByFieldName("name")
			if nameNode == nil {
				return
			}
			name := strings.TrimSpace(shared.NodeText(nameNode, source))
			if name != "" {
				env[name] = typeText
			}
		})
	})
	return env
}

// csharpAssignDefsUses splits an assignment or postfix/prefix update expression
// into defined and used identifiers for def->use tracking.
func csharpAssignDefsUses(node *tree_sitter.Node, source []byte) (defs, uses []string) {
	switch node.Kind() {
	case "assignment_expression":
		left := node.ChildByFieldName("left")
		if left != nil && left.Kind() == "identifier" {
			defs = append(defs, strings.TrimSpace(shared.NodeText(left, source)))
		} else if left != nil {
			uses = append(uses, csharpExprUses(left, source)...)
		}
		if right := node.ChildByFieldName("right"); right != nil {
			uses = append(uses, csharpExprUses(right, source)...)
		}
	case "postfix_unary_expression", "prefix_unary_expression":
		if arg := csharpFirstNamedChild(node); arg != nil && arg.Kind() == "identifier" {
			name := strings.TrimSpace(shared.NodeText(arg, source))
			defs = append(defs, name)
			uses = append(uses, name)
		}
	}
	return defs, uses
}

// csharpFirstNamedChild returns the first immediate named child, or nil.
func csharpFirstNamedChild(node *tree_sitter.Node) *tree_sitter.Node {
	if node == nil {
		return nil
	}
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		return shared.CloneNode(&child)
	}
	return nil
}

// csharpExprUses collects identifier uses in an expression subtree, skipping
// nested function scopes so an inner closure's reads are not attributed outward.
func csharpExprUses(node *tree_sitter.Node, source []byte) []string {
	if node == nil {
		return nil
	}
	var uses []string
	var visit func(*tree_sitter.Node)
	visit = func(current *tree_sitter.Node) {
		if current == nil || csharpIsNestedFunction(current.Kind()) {
			return
		}
		if current.Kind() == "identifier" {
			if name := strings.TrimSpace(shared.NodeText(current, source)); name != "" {
				uses = append(uses, name)
			}
			return
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

// csharpLineIndex maps source lines to the CFG statement IDs that define or use
// bindings on that line, so AST-derived facts can be attached to lowered stmts.
type csharpLineIndex struct {
	defByLine map[int]map[string]int
	useByLine map[int]int
}

// newCSharpLineIndex builds the per-line def/use index from a lowered function.
func newCSharpLineIndex(fn cfg.Function) *csharpLineIndex {
	index := &csharpLineIndex{defByLine: map[int]map[string]int{}, useByLine: map[int]int{}}
	for _, block := range fn.Blocks {
		for _, stmt := range block.Stmts {
			for _, def := range stmt.Defs {
				byBinding := index.defByLine[stmt.Line]
				if byBinding == nil {
					byBinding = map[string]int{}
					index.defByLine[stmt.Line] = byBinding
				}
				if _, exists := byBinding[def]; !exists {
					byBinding[def] = stmt.ID
				}
			}
			if len(stmt.Uses) > 0 {
				if _, exists := index.useByLine[stmt.Line]; !exists {
					index.useByLine[stmt.Line] = stmt.ID
				}
			}
		}
	}
	return index
}

// defStmt returns the statement ID that defines binding on line, if any.
func (l *csharpLineIndex) defStmt(line int, binding string) (int, bool) {
	byBinding, ok := l.defByLine[line]
	if !ok {
		return 0, false
	}
	stmtID, ok := byBinding[binding]
	return stmtID, ok
}

// useStmt returns the first use statement ID on line, if any.
func (l *csharpLineIndex) useStmt(line int) (int, bool) {
	stmtID, ok := l.useByLine[line]
	return stmtID, ok
}
