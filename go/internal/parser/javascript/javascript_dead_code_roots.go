package javascript

import (
	"path/filepath"
	"slices"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

type javaScriptDeadCodeEvidence struct {
	registeredRootKinds map[string][]string
	typeScriptRootKinds map[string][]string
	fileRootKinds       []string
	hapiControllerFile  bool
	hapiHandlerFile     bool
	hapiPluginFile      bool
	// parents amortizes ancestor traversal so dead-code helpers consult a Go
	// map instead of re-entering cgo via ts_node_parent per declaration node
	// (see #3586). It is built once per Parse() over the file's own tree.
	parents *javaScriptParentLookup
}

func javaScriptDeadCodeRootEvidence(
	repoRoot string,
	path string,
	root *tree_sitter.Node,
	source []byte,
	siblingParser *javaScriptSiblingParser,
	parents *javaScriptParentLookup,
) javaScriptDeadCodeEvidence {
	hapiHandlerFile := javaScriptIsHapiHandlerFile(repoRoot, path, siblingParser)
	registeredRootKinds := javaScriptRegisteredDeadCodeRootKinds(root, source)
	mergeJavaScriptRegisteredRootKinds(
		registeredRootKinds,
		javaScriptHapiPluginRegisterAliasRootKinds(root, source),
	)
	if javaScriptIsHapiPluginFile(repoRoot, path) {
		mergeJavaScriptRegisteredRootKinds(
			registeredRootKinds,
			javaScriptDefaultObjectExportAliasRootKinds(root, source, "register", "javascript.hapi_plugin_register"),
		)
	}
	mergeJavaScriptRegisteredRootKinds(
		registeredRootKinds,
		javaScriptCommonJSDefaultExportAliasRootKinds(root, source),
	)
	mergeJavaScriptRegisteredRootKinds(registeredRootKinds, javaScriptFrameworkRegisteredDeadCodeRootKinds(root, source))
	if hapiHandlerFile {
		mergeJavaScriptRegisteredRootKinds(
			registeredRootKinds,
			javaScriptCommonJSExportAliasRootKinds(root, source, "javascript.hapi_handler_export"),
		)
		mergeJavaScriptRegisteredRootKinds(
			registeredRootKinds,
			javaScriptTypeScriptExportAssignmentAliasRootKinds(root, source, "javascript.hapi_handler_export"),
		)
	}
	return javaScriptDeadCodeEvidence{
		registeredRootKinds: registeredRootKinds,
		typeScriptRootKinds: javaScriptTypeScriptSurfaceRootKinds(repoRoot, path, root, source, siblingParser, parents),
		fileRootKinds:       javaScriptPackageFileRootKinds(repoRoot, path),
		hapiControllerFile:  javaScriptIsHapiControllerFile(repoRoot, path),
		hapiHandlerFile:     hapiHandlerFile,
		hapiPluginFile:      javaScriptIsHapiPluginFile(repoRoot, path),
		parents:             parents,
	}
}

var javaScriptRouteExportNames = map[string]struct{}{
	"GET":     {},
	"POST":    {},
	"PUT":     {},
	"PATCH":   {},
	"DELETE":  {},
	"HEAD":    {},
	"OPTIONS": {},
}

var javaScriptExpressRouteMethods = map[string]struct{}{
	"get":     {},
	"post":    {},
	"put":     {},
	"patch":   {},
	"delete":  {},
	"head":    {},
	"options": {},
}

func javaScriptRegisteredDeadCodeRootKinds(
	root *tree_sitter.Node,
	source []byte,
) map[string][]string {
	registered := make(map[string][]string)
	if root == nil || !javaScriptHasExpressImport(string(source)) {
		return registered
	}

	allowedBases := make(map[string]struct{})
	if express, ok := detectExpressSemantics(root, source); ok {
		for _, symbol := range javaScriptExpressServerSymbols(express) {
			allowedBases[strings.ToLower(strings.TrimSpace(symbol))] = struct{}{}
		}
	}
	if len(allowedBases) == 0 {
		return registered
	}

	walkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "call_expression" {
			return
		}
		functionNode := node.ChildByFieldName("function")
		base, property, ok := javaScriptMemberBaseAndProperty(functionNode, source)
		if !ok {
			return
		}
		if _, ok := allowedBases[strings.ToLower(base)]; !ok {
			return
		}
		if _, ok := javaScriptExpressRouteMethods[strings.ToLower(property)]; !ok {
			return
		}

		argsNode := node.ChildByFieldName("arguments")
		if argsNode == nil {
			return
		}
		args := argsNode.NamedChildren(argsNode.Walk())
		if len(args) < 2 {
			return
		}

		handlerStart := 1
		if javaScriptIsExpressRouteChain(functionNode, source) {
			handlerStart = 0
		}
		for i := handlerStart; i < len(args); i++ {
			for _, handlerName := range javaScriptExpressHandlerNames(&args[i], source) {
				key := strings.ToLower(handlerName)
				registered[key] = appendUniqueString(registered[key], "javascript.express_route_registration")
			}
		}
	})

	return registered
}

// javaScriptExpressServerSymbols extracts the typed server_symbols contract.
func javaScriptExpressServerSymbols(express map[string]any) []string {
	return ExpressServerSymbols(express)
}

// ExpressServerSymbols extracts the typed server_symbols contract from
// JavaScript framework semantics.
func ExpressServerSymbols(express map[string]any) []string {
	if len(express) == 0 {
		return nil
	}
	serverSymbols, ok := express["server_symbols"].([]string)
	if !ok {
		return nil
	}
	return serverSymbols
}

func javaScriptDeadCodeRootKinds(
	path string,
	node *tree_sitter.Node,
	name string,
	source []byte,
	evidence javaScriptDeadCodeEvidence,
) []string {
	rootKinds := append([]string(nil), evidence.registeredRootKinds[strings.ToLower(strings.TrimSpace(name))]...)
	if classNode, className := javaScriptConstructorClass(node, name, source, evidence.parents); classNode != nil {
		rootKinds = appendRegisteredJavaScriptRootKinds(rootKinds, className, evidence)
		for _, rootKind := range evidence.fileRootKinds {
			if rootKind == "javascript.node_package_export" &&
				(javaScriptIsExported(classNode, evidence.parents) || javaScriptIsCommonJSExport(classNode, className, source, evidence.parents)) {
				rootKinds = appendUniqueString(rootKinds, rootKind)
			}
		}
	}
	if javaScriptIsNextJSRouteExport(path, node, name, evidence.parents) {
		rootKinds = appendUniqueString(rootKinds, "javascript.nextjs_route_export")
	}
	if javaScriptIsNextJSAppExport(path, node, evidence.parents) {
		rootKinds = appendUniqueString(rootKinds, "javascript.nextjs_app_export")
	}
	for _, rootKind := range evidence.fileRootKinds {
		switch rootKind {
		case "javascript.node_package_entrypoint":
			if javaScriptIsNodeEntrypointFunctionName(name) {
				rootKinds = appendUniqueString(rootKinds, rootKind)
			}
		case "javascript.node_package_bin":
			if javaScriptIsNodeBinFunctionName(name) {
				rootKinds = appendUniqueString(rootKinds, rootKind)
			}
		case "javascript.node_package_script":
			if javaScriptIsNodeScriptFunctionName(name) {
				rootKinds = appendUniqueString(rootKinds, rootKind)
			}
		case "javascript.node_package_export":
			if javaScriptIsExported(node, evidence.parents) || javaScriptIsCommonJSExport(node, name, source, evidence.parents) {
				rootKinds = appendUniqueString(rootKinds, rootKind)
			}
		}
	}
	if evidence.hapiHandlerFile && (javaScriptIsExported(node, evidence.parents) || javaScriptIsCommonJSExport(node, name, source, evidence.parents)) {
		rootKinds = appendUniqueString(rootKinds, "javascript.hapi_handler_export")
	}
	if javaScriptIsCommonJSMixinExport(node, name, source, evidence.parents) {
		rootKinds = appendUniqueString(rootKinds, "javascript.commonjs_mixin_export")
	}
	if javaScriptMethodInsideCommonJSDefaultExport(node, source, evidence.parents) {
		rootKinds = appendUniqueString(rootKinds, "javascript.commonjs_default_export")
	}
	if javaScriptIsHapiPluginRegister(node, name, source, evidence.hapiPluginFile, evidence.parents) {
		rootKinds = appendUniqueString(rootKinds, "javascript.hapi_plugin_register")
	}
	if javaScriptIsHapiRouteConfigHandler(node, name, source, evidence.parents) {
		rootKinds = appendUniqueString(rootKinds, "javascript.hapi_route_config_handler")
	}
	if javaScriptIsNodeSeedExecute(path, node, name, source, evidence.parents) {
		rootKinds = appendUniqueString(rootKinds, "javascript.node_seed_execute")
	}
	if javaScriptIsNodeMigrationExport(path, node, name, evidence.parents) {
		rootKinds = appendUniqueString(rootKinds, "javascript.node_migration_export")
	}
	if javaScriptIsHapiAMQPConsumer(path, node, name, source, evidence.parents) {
		rootKinds = appendUniqueString(rootKinds, "javascript.hapi_amqp_consumer")
	}
	if javaScriptIsHapiProxyCallback(node, name, source, evidence.parents) {
		rootKinds = appendUniqueString(rootKinds, "javascript.hapi_proxy_callback")
	}
	if javaScriptIsTypeScriptInterfaceImplementationMethod(node, name, source, evidence.parents) {
		rootKinds = appendUniqueString(rootKinds, "typescript.interface_method_implementation")
	}
	if javaScriptIsTypeScriptModuleContractExport(node, name, source, evidence.parents) {
		rootKinds = appendUniqueString(rootKinds, "typescript.module_contract_export")
	}
	for _, rootKind := range evidence.typeScriptRootKinds[strings.TrimSpace(name)] {
		rootKinds = appendUniqueString(rootKinds, rootKind)
	}
	if javaScriptIsNestJSControllerMethod(node, source, evidence.parents) {
		rootKinds = appendUniqueString(rootKinds, "javascript.nestjs_controller_method")
	}
	slices.Sort(rootKinds)
	return rootKinds
}

func appendRegisteredJavaScriptRootKinds(rootKinds []string, name string, evidence javaScriptDeadCodeEvidence) []string {
	for _, rootKind := range evidence.registeredRootKinds[strings.ToLower(strings.TrimSpace(name))] {
		rootKinds = appendUniqueString(rootKinds, rootKind)
	}
	return rootKinds
}

func javaScriptConstructorClass(node *tree_sitter.Node, name string, source []byte, parents *javaScriptParentLookup) (*tree_sitter.Node, string) {
	if node == nil || node.Kind() != "method_definition" || strings.TrimSpace(name) != "constructor" {
		return nil, ""
	}
	for current := parents.parent(node); current != nil; current = parents.parent(current) {
		switch current.Kind() {
		case "class_declaration", "abstract_class_declaration":
			className := nodeText(current.ChildByFieldName("name"), source)
			if strings.TrimSpace(className) == "" {
				return nil, ""
			}
			return current, className
		case "program":
			return nil, ""
		}
	}
	return nil, ""
}

func javaScriptIsHapiPluginFile(repoRoot string, path string) bool {
	relativePath, ok := relativeSlashPath(repoRoot, path)
	if !ok {
		relativePath = filepath.ToSlash(path)
	}
	return strings.Contains(relativePath, "/server/init/plugins/") ||
		strings.HasPrefix(relativePath, "server/init/plugins/") ||
		strings.Contains(relativePath, "/server/init/hapi-") ||
		strings.HasPrefix(relativePath, "server/init/hapi-")
}

func javaScriptIsHapiControllerFile(repoRoot string, path string) bool {
	relativePath, ok := relativeSlashPath(repoRoot, path)
	if !ok {
		relativePath = filepath.ToSlash(path)
	}
	return strings.Contains(relativePath, "/server/controllers/") || strings.HasPrefix(relativePath, "server/controllers/")
}

func javaScriptIsHapiPluginRegister(node *tree_sitter.Node, name string, source []byte, hapiPluginFile bool, parents *javaScriptParentLookup) bool {
	if strings.ToLower(strings.TrimSpace(name)) != "register" || node == nil {
		return false
	}
	switch node.Kind() {
	case "method_definition":
	case "pair":
		if !isJavaScriptFunctionValue(node.ChildByFieldName("value")) {
			return false
		}
	default:
		return false
	}
	if javaScriptPairInsideCommonJSPluginObject(node, source, parents) {
		return true
	}
	if !hapiPluginFile {
		return false
	}
	for current := parents.parent(node); current != nil; current = parents.parent(current) {
		switch current.Kind() {
		case "object":
			return true
		case "program":
			return false
		}
	}
	return false
}

func javaScriptIsNextJSRouteExport(path string, node *tree_sitter.Node, name string, parents *javaScriptParentLookup) bool {
	if !javaScriptIsNextJSRouteModule(path) {
		return false
	}
	if !javaScriptIsRouteHandlerDeclaration(node) {
		return false
	}
	if _, ok := javaScriptRouteExportNames[strings.ToUpper(strings.TrimSpace(name))]; !ok {
		return false
	}
	return javaScriptIsExported(node, parents)
}

func javaScriptIsRouteHandlerDeclaration(node *tree_sitter.Node) bool {
	if node == nil {
		return false
	}
	switch node.Kind() {
	case "function_declaration", "generator_function_declaration":
		return true
	case "variable_declarator":
		return isJavaScriptFunctionValue(node.ChildByFieldName("value"))
	default:
		return false
	}
}

func javaScriptIsNextJSRouteModule(path string) bool {
	switch filepath.Base(path) {
	case "route.js", "route.jsx", "route.ts", "route.tsx":
		return true
	default:
		return false
	}
}

func javaScriptIsNodeSeedExecute(path string, node *tree_sitter.Node, name string, source []byte, parents *javaScriptParentLookup) bool {
	relativePath := filepath.ToSlash(path)
	if !strings.Contains(relativePath, "/seed/") && !strings.HasPrefix(relativePath, "seed/") &&
		!strings.Contains(relativePath, "/seeds/") && !strings.HasPrefix(relativePath, "seeds/") {
		return false
	}
	return strings.TrimSpace(name) == "execute" && javaScriptIsCommonJSExport(node, name, source, parents)
}

func javaScriptIsHapiAMQPConsumer(path string, node *tree_sitter.Node, name string, source []byte, parents *javaScriptParentLookup) bool {
	relativePath := filepath.ToSlash(path)
	if !strings.Contains(relativePath, "/server/resources/consumers/") &&
		!strings.HasPrefix(relativePath, "server/resources/consumers/") {
		return false
	}
	return strings.TrimSpace(name) == "consume" && javaScriptIsCommonJSExport(node, name, source, parents)
}

func javaScriptIsExported(node *tree_sitter.Node, parents *javaScriptParentLookup) bool {
	for current := node; current != nil; current = parents.parent(current) {
		switch current.Kind() {
		case "export_statement":
			return true
		case "program":
			return false
		}
	}
	return false
}

func javaScriptIsNodeEntrypointFunctionName(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "main", "bootstrap", "start", "run", "handler":
		return true
	default:
		return false
	}
}

func javaScriptIsNodeBinFunctionName(name string) bool {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if javaScriptIsNodeEntrypointFunctionName(normalized) {
		return true
	}
	return strings.HasPrefix(normalized, "run") || strings.HasSuffix(normalized, "cli")
}

func javaScriptIsNodeScriptFunctionName(name string) bool {
	normalized := strings.ToLower(strings.TrimSpace(name))
	switch normalized {
	case "main", "run", "start", "build", "generate", "bootstrap":
		return true
	default:
		return strings.HasPrefix(normalized, "run") || strings.HasPrefix(normalized, "generate")
	}
}

// Express member-expression helpers (javaScriptMemberBaseAndProperty,
// javaScriptMemberExpressionBase, javaScriptIsExpressRouteChain,
// javaScriptExpressHandlerNames, javaScriptIdentifierName) live in
// javascript_member_expression.go to keep this file under the package size
// limit.
