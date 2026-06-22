package python

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// pythonFastAPIRouteMethods is the set of FastAPI/APIRouter HTTP method
// decorator attributes recognized on a route decorator (`@app.get(...)`).
var pythonFastAPIRouteMethods = map[string]struct{}{
	"get":     {},
	"post":    {},
	"put":     {},
	"patch":   {},
	"delete":  {},
	"options": {},
	"head":    {},
}

// pythonFastAPIServerConstructors names the call targets that create a FastAPI
// application server symbol via a module-level assignment.
var pythonFastAPIServerConstructors = map[string]struct{}{
	"FastAPI": {},
}

// pythonFlaskServerConstructors names the call targets that create a Flask
// application server symbol via a module-level assignment.
var pythonFlaskServerConstructors = map[string]struct{}{
	"Flask":      {},
	"create_app": {},
}

// buildPythonFrameworkSemantics derives FastAPI and Flask route semantics from
// the parsed module AST. Server symbols come from assignment nodes whose
// right-hand side calls a framework constructor; routes come from decorator
// nodes that call an HTTP-method or `.route` attribute on a server symbol.
func buildPythonFrameworkSemantics(root *tree_sitter.Node, source []byte) map[string]any {
	fastAPI := detectPythonFastAPISemantics(root, source)
	flask := detectPythonFlaskSemantics(root, source)
	frameworks := make([]string, 0, 2)
	semantics := map[string]any{
		"frameworks": []string{},
	}
	if fastAPI != nil {
		frameworks = append(frameworks, "fastapi")
		semantics["fastapi"] = fastAPI
	}
	if flask != nil {
		frameworks = append(frameworks, "flask")
		semantics["flask"] = flask
	}
	semantics["frameworks"] = frameworks
	return semantics
}

// pythonRouteDecorator is one route decorator resolved from the AST: the server
// symbol it targets, the HTTP method (uppercased), the route path literal, the
// optional `methods=[...]` list, and the bound handler function name (empty when
// the decorator has no following def, e.g. an orphaned syntax-error decorator).
type pythonRouteDecorator struct {
	symbol  string
	method  string
	path    string
	methods []string
	handler string
}

func detectPythonFastAPISemantics(root *tree_sitter.Node, source []byte) map[string]any {
	appSymbols, routerPrefixes := pythonFastAPIServerSymbols(root, source)
	serverSymbols := make([]string, 0, len(appSymbols)+len(routerPrefixes))
	serverSymbols = append(serverSymbols, appSymbols...)
	for _, symbol := range pythonRouterSymbolsInOrder(root, source) {
		serverSymbols = appendUniqueString(serverSymbols, symbol)
	}

	decorators := pythonFastAPIRouteDecorators(root, source)
	if len(serverSymbols) == 0 || len(decorators) == 0 {
		return nil
	}

	methods := make([]string, 0, len(decorators))
	paths := make([]string, 0, len(decorators))
	entries := make([]map[string]string, 0, len(decorators))
	for _, decorator := range decorators {
		path := decorator.path
		if prefix := routerPrefixes[decorator.symbol]; prefix != "" {
			path = prefix + path
		}
		methods = appendUniqueString(methods, decorator.method)
		paths = appendUniqueString(paths, path)
		entries = append(entries, routeEntry(decorator.method, path, decorator.handler))
	}

	return map[string]any{
		"route_methods":  methods,
		"route_paths":    paths,
		"route_entries":  entries,
		"server_symbols": serverSymbols,
	}
}

func detectPythonFlaskSemantics(root *tree_sitter.Node, source []byte) map[string]any {
	serverSymbols := pythonFlaskServerSymbols(root, source)
	if len(serverSymbols) == 0 {
		return nil
	}

	decorators := pythonFlaskRouteDecorators(root, source)
	if len(decorators) == 0 {
		return nil
	}

	allowed := make(map[string]struct{}, len(serverSymbols))
	for _, symbol := range serverSymbols {
		allowed[symbol] = struct{}{}
	}

	methods := make([]string, 0, len(decorators))
	paths := make([]string, 0, len(decorators))
	entries := make([]map[string]string, 0, len(decorators))
	for _, decorator := range decorators {
		if _, ok := allowed[decorator.symbol]; !ok {
			continue
		}
		paths = appendUniqueString(paths, decorator.path)
		routeMethods := decorator.methods
		if len(routeMethods) == 0 {
			routeMethods = []string{"GET"}
		}
		// The handler def is shared by every method the route declares; it is
		// resolved once from the decorated definition and reused.
		for _, method := range routeMethods {
			methods = appendUniqueString(methods, method)
			entries = append(entries, routeEntry(method, decorator.path, decorator.handler))
		}
	}
	if len(paths) == 0 {
		return nil
	}

	return map[string]any{
		"route_methods":  methods,
		"route_paths":    paths,
		"route_entries":  entries,
		"server_symbols": serverSymbols,
	}
}

// pythonFastAPIServerSymbols returns FastAPI() app symbols in source order plus
// a map from APIRouter symbol to its `prefix=` value.
func pythonFastAPIServerSymbols(root *tree_sitter.Node, source []byte) ([]string, map[string]string) {
	appSymbols := make([]string, 0)
	prefixes := make(map[string]string)
	pythonWalkServerAssignments(root, source, func(symbol string, call *tree_sitter.Node, callee string) {
		switch callee {
		case "APIRouter":
			prefixes[symbol] = pythonKeywordArgumentString(call, "prefix", source)
		default:
			if _, ok := pythonFastAPIServerConstructors[callee]; ok {
				appSymbols = appendUniqueString(appSymbols, symbol)
			}
		}
	})
	return appSymbols, prefixes
}

// pythonRouterSymbolsInOrder returns APIRouter() symbols in source order so the
// server-symbol list stays app-symbols-first, then routers.
func pythonRouterSymbolsInOrder(root *tree_sitter.Node, source []byte) []string {
	symbols := make([]string, 0)
	pythonWalkServerAssignments(root, source, func(symbol string, _ *tree_sitter.Node, callee string) {
		if callee == "APIRouter" {
			symbols = appendUniqueString(symbols, symbol)
		}
	})
	return symbols
}

func pythonFlaskServerSymbols(root *tree_sitter.Node, source []byte) []string {
	symbols := make([]string, 0)
	pythonWalkServerAssignments(root, source, func(symbol string, _ *tree_sitter.Node, callee string) {
		if _, ok := pythonFlaskServerConstructors[callee]; ok {
			symbols = appendUniqueString(symbols, symbol)
		}
	})
	return symbols
}

// pythonWalkServerAssignments visits every `name = Call(...)` assignment whose
// left-hand side is a bare identifier, invoking visit with the assigned symbol,
// the call node, and the bare callee name. Annotated assignments
// (`app: FastAPI = FastAPI()`) are included because the grammar still exposes a
// `right` call field on the assignment node.
func pythonWalkServerAssignments(
	root *tree_sitter.Node,
	source []byte,
	visit func(symbol string, call *tree_sitter.Node, callee string),
) {
	walkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "assignment" {
			return
		}
		left := node.ChildByFieldName("left")
		if left == nil || left.Kind() != "identifier" {
			return
		}
		right := node.ChildByFieldName("right")
		if right == nil || right.Kind() != "call" {
			return
		}
		callee := pythonCallSimpleName(right.ChildByFieldName("function"), source)
		if callee == "" {
			return
		}
		symbol := strings.TrimSpace(nodeText(left, source))
		if symbol == "" {
			return
		}
		visit(symbol, right, callee)
	})
}

// pythonFastAPIRouteDecorators returns FastAPI/APIRouter route decorators in
// source order with bound handlers resolved from the decorated definition.
func pythonFastAPIRouteDecorators(root *tree_sitter.Node, source []byte) []pythonRouteDecorator {
	decorators := make([]pythonRouteDecorator, 0)
	pythonWalkRouteDecorators(root, source, func(decorator *tree_sitter.Node, symbol string, attribute string, call *tree_sitter.Node) {
		if _, ok := pythonFastAPIRouteMethods[attribute]; !ok {
			return
		}
		path := pythonFirstPositionalString(call, source)
		if path == "" {
			return
		}
		decorators = append(decorators, pythonRouteDecorator{
			symbol:  symbol,
			method:  strings.ToUpper(attribute),
			path:    path,
			handler: pythonDecoratorHandlerName(decorator, source),
		})
	})
	return decorators
}

// pythonFlaskRouteDecorators returns Flask `.route(...)` decorators in source
// order with their declared methods and bound handlers.
func pythonFlaskRouteDecorators(root *tree_sitter.Node, source []byte) []pythonRouteDecorator {
	decorators := make([]pythonRouteDecorator, 0)
	pythonWalkRouteDecorators(root, source, func(decorator *tree_sitter.Node, symbol string, attribute string, call *tree_sitter.Node) {
		if attribute != "route" {
			return
		}
		path := pythonFirstPositionalString(call, source)
		if path == "" {
			return
		}
		decorators = append(decorators, pythonRouteDecorator{
			symbol:  symbol,
			path:    path,
			methods: pythonKeywordArgumentStringList(call, "methods", source),
			handler: pythonDecoratorHandlerName(decorator, source),
		})
	})
	return decorators
}

// pythonWalkRouteDecorators visits every decorator node whose decorator
// expression is a call on an `obj.attribute(...)` form, passing the decorator
// node, the receiver symbol, the attribute name, and the call node. It covers
// decorators inside a decorated_definition and orphaned decorators that
// tree-sitter parks under an ERROR node when no def follows.
func pythonWalkRouteDecorators(
	root *tree_sitter.Node,
	source []byte,
	visit func(decorator *tree_sitter.Node, symbol string, attribute string, call *tree_sitter.Node),
) {
	walkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "decorator" {
			return
		}
		call := pythonDecoratorCall(node)
		if call == nil {
			return
		}
		function := call.ChildByFieldName("function")
		if function == nil || function.Kind() != "attribute" {
			return
		}
		object := function.ChildByFieldName("object")
		if object == nil || object.Kind() != "identifier" {
			return
		}
		symbol := strings.TrimSpace(nodeText(object, source))
		attribute := strings.TrimSpace(nodeText(function.ChildByFieldName("attribute"), source))
		if symbol == "" || attribute == "" {
			return
		}
		visit(node, symbol, attribute, call)
	})
}

// buildPythonORMTableMappings derives SQLAlchemy `__tablename__` and Django
// `Meta.db_table` table mappings from class_definition AST nodes.
func buildPythonORMTableMappings(root *tree_sitter.Node, source []byte) []map[string]any {
	mappings := make([]map[string]any, 0)
	walkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "class_definition" {
			return
		}
		className := strings.TrimSpace(nodeText(node.ChildByFieldName("name"), source))
		if className == "" {
			return
		}
		classLine := nodeLine(node)
		body := node.ChildByFieldName("body")
		if body == nil {
			return
		}
		mappings = pythonAppendClassTableMappings(body, source, className, classLine, mappings)
	})
	return mappings
}

// pythonAppendClassTableMappings scans the direct statements of a class body for
// a `__tablename__` assignment (SQLAlchemy) and a nested `class Meta:` body for a
// `db_table` assignment (Django), appending one mapping row per match.
func pythonAppendClassTableMappings(
	body *tree_sitter.Node,
	source []byte,
	className string,
	classLine int,
	mappings []map[string]any,
) []map[string]any {
	cursor := body.Walk()
	defer cursor.Close()
	for _, statement := range body.NamedChildren(cursor) {
		statement := statement
		if assignment := pythonClassBodyAssignment(&statement); assignment != nil {
			if name, value, line, ok := pythonStringAssignment(assignment, source); ok && name == "__tablename__" {
				mappings = append(mappings, map[string]any{
					"class_name":        className,
					"class_line_number": classLine,
					"table_name":        value,
					"framework":         "sqlalchemy",
					"line_number":       line,
				})
			}
			continue
		}
		if statement.Kind() == "class_definition" &&
			strings.TrimSpace(nodeText(statement.ChildByFieldName("name"), source)) == "Meta" {
			mappings = pythonAppendDjangoMetaMappings(&statement, source, className, classLine, mappings)
		}
	}
	return mappings
}

// pythonAppendDjangoMetaMappings scans a Django `class Meta:` body for the
// `db_table` string assignment and appends the django mapping row when present.
func pythonAppendDjangoMetaMappings(
	meta *tree_sitter.Node,
	source []byte,
	className string,
	classLine int,
	mappings []map[string]any,
) []map[string]any {
	body := meta.ChildByFieldName("body")
	if body == nil {
		return mappings
	}
	cursor := body.Walk()
	defer cursor.Close()
	for _, statement := range body.NamedChildren(cursor) {
		statement := statement
		assignment := pythonClassBodyAssignment(&statement)
		if assignment == nil {
			continue
		}
		if name, value, line, ok := pythonStringAssignment(assignment, source); ok && name == "db_table" {
			mappings = append(mappings, map[string]any{
				"class_name":        className,
				"class_line_number": classLine,
				"table_name":        value,
				"framework":         "django",
				"line_number":       line,
			})
		}
	}
	return mappings
}

// pythonClassBodyAssignment returns the assignment node inside a class-body
// statement, unwrapping the expression_statement wrapper, or nil when the
// statement is not a simple assignment.
func pythonClassBodyAssignment(statement *tree_sitter.Node) *tree_sitter.Node {
	if statement.Kind() != "expression_statement" {
		return nil
	}
	cursor := statement.Walk()
	defer cursor.Close()
	for _, child := range statement.NamedChildren(cursor) {
		child := child
		if child.Kind() == "assignment" {
			return shared.CloneNode(&child)
		}
	}
	return nil
}

// pythonStringAssignment returns the left identifier name, the right string
// literal value, and the 1-based assignment line for a `name = "literal"`
// assignment node. The boolean is false when either side is not the expected
// shape.
func pythonStringAssignment(assignment *tree_sitter.Node, source []byte) (string, string, int, bool) {
	left := assignment.ChildByFieldName("left")
	right := assignment.ChildByFieldName("right")
	if left == nil || left.Kind() != "identifier" || right == nil || right.Kind() != "string" {
		return "", "", 0, false
	}
	name := strings.TrimSpace(nodeText(left, source))
	if name == "" {
		return "", "", 0, false
	}
	return name, pythonStringLiteralValue(right, source), nodeLine(assignment), true
}
