// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package python

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// pythonGatheredNodes holds all resolution-candidate nodes gathered during
// a single pre-order walk, replacing the many separate full-tree walks each
// framework detector previously performed. Tree-sitter *tree_sitter.Node
// values point at stack-allocated cursors during the recursive walk;
// every gathered node is cloned (shared.CloneNode) so it survives past
// the walk.
type pythonGatheredNodes struct {
	assignments []*tree_sitter.Node
	decorators  []*tree_sitter.Node
	calls       []*tree_sitter.Node
	functions   []*tree_sitter.Node
	classes     []*tree_sitter.Node
	imports     []*tree_sitter.Node // import_statement + import_from_statement
}

// gatherPythonFrameworkNodes walks root once and collects every node kind
// needed by the framework-semantics and ORM-table-mapping passes. The slices
// are appended in pre-order so iteration reproduces the original per-kind
// walk order.
func gatherPythonFrameworkNodes(root *tree_sitter.Node) pythonGatheredNodes {
	var g pythonGatheredNodes
	walkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "assignment":
			g.assignments = append(g.assignments, shared.CloneNode(node))
		case "decorator":
			g.decorators = append(g.decorators, shared.CloneNode(node))
		case "call":
			g.calls = append(g.calls, shared.CloneNode(node))
		case "function_definition":
			g.functions = append(g.functions, shared.CloneNode(node))
		case "class_definition":
			g.classes = append(g.classes, shared.CloneNode(node))
		case "import_statement", "import_from_statement":
			g.imports = append(g.imports, shared.CloneNode(node))
		}
	})
	return g
}

// --- Gathered-variant walk helpers for framework_routes.go ---

// pythonWalkServerAssignmentsGathered mirrors pythonWalkServerAssignments
// but iterates a pre-gathered slice of assignment nodes instead of walking
// the full tree.
func pythonWalkServerAssignmentsGathered(
	gathered []*tree_sitter.Node,
	source []byte,
	visit func(symbol string, call *tree_sitter.Node, callee string),
) {
	for _, node := range gathered {
		left := node.ChildByFieldName("left")
		if left == nil || left.Kind() != "identifier" {
			continue
		}
		right := node.ChildByFieldName("right")
		if right == nil || right.Kind() != "call" {
			continue
		}
		callee := pythonCallSimpleName(right.ChildByFieldName("function"), source)
		if callee == "" {
			continue
		}
		symbol := nodeText(left, source)
		if symbol == "" {
			continue
		}
		visit(symbol, right, callee)
	}
}

// pythonWalkRouteDecoratorsGathered mirrors pythonWalkRouteDecorators
// but iterates a pre-gathered slice of decorator nodes instead of walking
// the full tree.
func pythonWalkRouteDecoratorsGathered(
	gathered []*tree_sitter.Node,
	source []byte,
	visit func(decorator *tree_sitter.Node, symbol string, attribute string, call *tree_sitter.Node),
) {
	for _, node := range gathered {
		call := pythonDecoratorCall(node)
		if call == nil {
			continue
		}
		function := call.ChildByFieldName("function")
		if function == nil || function.Kind() != "attribute" {
			continue
		}
		object := function.ChildByFieldName("object")
		if object == nil || object.Kind() != "identifier" {
			continue
		}
		symbol := nodeText(object, source)
		attribute := nodeText(function.ChildByFieldName("attribute"), source)
		if symbol == "" || attribute == "" {
			continue
		}
		visit(node, symbol, attribute, call)
	}
}

// --- Gathered-variant framework detectors (in-order: FastAPI, Flask, Django, DRF, aiohttp, Tornado) ---

// detectPythonFastAPISemanticsGathered produces the same output as
// detectPythonFastAPISemantics using pre-gathered assignment and decorator
// nodes instead of separate full-tree walks.
func detectPythonFastAPISemanticsGathered(g pythonGatheredNodes, source []byte) map[string]any {
	appSymbols := make([]string, 0)
	routerPrefixes := make(map[string]string)
	routerSymbols := make([]string, 0)
	pythonWalkServerAssignmentsGathered(g.assignments, source, func(symbol string, call *tree_sitter.Node, callee string) {
		switch callee {
		case "APIRouter":
			routerPrefixes[symbol] = pythonKeywordArgumentString(call, "prefix", source)
			routerSymbols = appendUniqueString(routerSymbols, symbol)
		default:
			if _, ok := pythonFastAPIServerConstructors[callee]; ok {
				appSymbols = appendUniqueString(appSymbols, symbol)
			}
		}
	})

	serverSymbols := make([]string, 0, len(appSymbols)+len(routerPrefixes))
	serverSymbols = append(serverSymbols, appSymbols...)
	for _, symbol := range routerSymbols {
		serverSymbols = appendUniqueString(serverSymbols, symbol)
	}

	decorators := make([]pythonRouteDecorator, 0)
	pythonWalkRouteDecoratorsGathered(g.decorators, source, func(decorator *tree_sitter.Node, symbol string, attribute string, call *tree_sitter.Node) {
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

// detectPythonFlaskSemanticsGathered produces the same output as
// detectPythonFlaskSemantics using pre-gathered assignment and decorator
// nodes instead of separate full-tree walks.
func detectPythonFlaskSemanticsGathered(g pythonGatheredNodes, source []byte) map[string]any {
	serverSymbols := make([]string, 0)
	pythonWalkServerAssignmentsGathered(g.assignments, source, func(symbol string, _ *tree_sitter.Node, callee string) {
		if _, ok := pythonFlaskServerConstructors[callee]; ok {
			serverSymbols = appendUniqueString(serverSymbols, symbol)
		}
	})

	if len(serverSymbols) == 0 {
		return nil
	}

	decorators := make([]pythonRouteDecorator, 0)
	pythonWalkRouteDecoratorsGathered(g.decorators, source, func(decorator *tree_sitter.Node, symbol string, attribute string, call *tree_sitter.Node) {
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

// buildPythonFrameworkSemanticsGathered derives FastAPI, Flask, Django, DRF,
// aiohttp, and Tornado route semantics from pre-gathered nodes instead of
// per-framework full-tree walks.
func buildPythonFrameworkSemanticsGathered(g pythonGatheredNodes, root *tree_sitter.Node, source []byte) map[string]any {
	fastAPI := detectPythonFastAPISemanticsGathered(g, source)
	flask := detectPythonFlaskSemanticsGathered(g, source)
	django := detectPythonDjangoSemanticsGathered(g, source)
	drf := detectPythonDRFSemanticsGathered(g, source)
	aiohttp := detectPythonAioHTTPSemanticsGathered(g, source)
	tornado := detectPythonTornadoSemanticsGathered(g, source)
	frameworks := make([]string, 0, 6)
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
	if django != nil {
		frameworks = append(frameworks, "django")
		semantics["django"] = django
	}
	if drf != nil {
		frameworks = append(frameworks, "drf")
		semantics["drf"] = drf
	}
	if aiohttp != nil {
		frameworks = append(frameworks, "aiohttp")
		semantics["aiohttp"] = aiohttp
	}
	if tornado != nil {
		frameworks = append(frameworks, "tornado")
		semantics["tornado"] = tornado
	}
	semantics["frameworks"] = frameworks
	return semantics
}

// buildPythonORMTableMappingsGathered derives SQLAlchemy __tablename__ and
// Django Meta.db_table mappings from pre-gathered class_definition nodes
// instead of a separate full-tree walk.
func buildPythonORMTableMappingsGathered(classes []*tree_sitter.Node, source []byte) []map[string]any {
	mappings := make([]map[string]any, 0)
	for _, node := range classes {
		className := nodeText(node.ChildByFieldName("name"), source)
		if className == "" {
			continue
		}
		classLine := nodeLine(node)
		body := node.ChildByFieldName("body")
		if body == nil {
			continue
		}
		mappings = pythonAppendClassTableMappings(body, source, className, classLine, mappings)
	}
	return mappings
}
