package parser

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
}

func javaScriptDeadCodeRootEvidence(
	repoRoot string,
	path string,
	root *tree_sitter.Node,
	source []byte,
) javaScriptDeadCodeEvidence {
	hapiHandlerFile := javaScriptIsHapiHandlerFile(repoRoot, path)
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
		typeScriptRootKinds: javaScriptTypeScriptSurfaceRootKinds(repoRoot, path, root, source),
		fileRootKinds:       javaScriptPackageFileRootKinds(repoRoot, path),
		hapiControllerFile:  javaScriptIsHapiControllerFile(repoRoot, path),
		hapiHandlerFile:     hapiHandlerFile,
		hapiPluginFile:      javaScriptIsHapiPluginFile(repoRoot, path),
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
	if express, ok := detectExpressSemantics(string(source)); ok {
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
	if classNode, className := javaScriptConstructorClass(node, name, source); classNode != nil {
		rootKinds = appendRegisteredJavaScriptRootKinds(rootKinds, className, evidence)
		for _, rootKind := range evidence.fileRootKinds {
			if rootKind == "javascript.node_package_export" &&
				(javaScriptIsExported(classNode) || javaScriptIsCommonJSExport(classNode, className, source)) {
				rootKinds = appendUniqueString(rootKinds, rootKind)
			}
		}
	}
	if javaScriptIsNextJSRouteExport(path, node, name) {
		rootKinds = appendUniqueString(rootKinds, "javascript.nextjs_route_export")
	}
	if javaScriptIsNextJSAppExport(path, node) {
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
			if javaScriptIsExported(node) || javaScriptIsCommonJSExport(node, name, source) {
				rootKinds = appendUniqueString(rootKinds, rootKind)
			}
		}
	}
	if evidence.hapiHandlerFile && (javaScriptIsExported(node) || javaScriptIsCommonJSExport(node, name, source)) {
		rootKinds = appendUniqueString(rootKinds, "javascript.hapi_handler_export")
	}
	if javaScriptIsCommonJSMixinExport(node, name, source) {
		rootKinds = appendUniqueString(rootKinds, "javascript.commonjs_mixin_export")
	}
	if javaScriptIsHapiPluginRegister(node, name, source, evidence.hapiPluginFile) {
		rootKinds = appendUniqueString(rootKinds, "javascript.hapi_plugin_register")
	}
	if javaScriptIsHapiRouteConfigHandler(node, name, source) {
		rootKinds = appendUniqueString(rootKinds, "javascript.hapi_route_config_handler")
	}
	if javaScriptIsNodeSeedExecute(path, node, name, source) {
		rootKinds = appendUniqueString(rootKinds, "javascript.node_seed_execute")
	}
	if javaScriptIsNodeMigrationExport(path, node, name) {
		rootKinds = appendUniqueString(rootKinds, "javascript.node_migration_export")
	}
	if javaScriptIsHapiAMQPConsumer(path, node, name, source) {
		rootKinds = appendUniqueString(rootKinds, "javascript.hapi_amqp_consumer")
	}
	if javaScriptIsHapiProxyCallback(node, name, source) {
		rootKinds = appendUniqueString(rootKinds, "javascript.hapi_proxy_callback")
	}
	if javaScriptIsTypeScriptInterfaceImplementationMethod(node, name, source) {
		rootKinds = appendUniqueString(rootKinds, "typescript.interface_method_implementation")
	}
	if javaScriptIsTypeScriptModuleContractExport(node, name, source) {
		rootKinds = appendUniqueString(rootKinds, "typescript.module_contract_export")
	}
	for _, rootKind := range evidence.typeScriptRootKinds[strings.TrimSpace(name)] {
		rootKinds = appendUniqueString(rootKinds, rootKind)
	}
	if javaScriptIsNestJSControllerMethod(node, source) {
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

func javaScriptConstructorClass(node *tree_sitter.Node, name string, source []byte) (*tree_sitter.Node, string) {
	if node == nil || node.Kind() != "method_definition" || strings.TrimSpace(name) != "constructor" {
		return nil, ""
	}
	for current := node.Parent(); current != nil; current = current.Parent() {
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

func javaScriptIsHapiPluginRegister(node *tree_sitter.Node, name string, source []byte, hapiPluginFile bool) bool {
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
	if javaScriptPairInsideCommonJSPluginObject(node, source) {
		return true
	}
	if !hapiPluginFile {
		return false
	}
	for current := node.Parent(); current != nil; current = current.Parent() {
		switch current.Kind() {
		case "object":
			return true
		case "program":
			return false
		}
	}
	return false
}

func javaScriptIsNextJSRouteExport(path string, node *tree_sitter.Node, name string) bool {
	if !javaScriptIsNextJSRouteModule(path) {
		return false
	}
	if !javaScriptIsRouteHandlerDeclaration(node) {
		return false
	}
	if _, ok := javaScriptRouteExportNames[strings.ToUpper(strings.TrimSpace(name))]; !ok {
		return false
	}
	return javaScriptIsExported(node)
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

func javaScriptIsNodeSeedExecute(path string, node *tree_sitter.Node, name string, source []byte) bool {
	relativePath := filepath.ToSlash(path)
	if !strings.Contains(relativePath, "/seed/") && !strings.HasPrefix(relativePath, "seed/") &&
		!strings.Contains(relativePath, "/seeds/") && !strings.HasPrefix(relativePath, "seeds/") {
		return false
	}
	return strings.TrimSpace(name) == "execute" && javaScriptIsCommonJSExport(node, name, source)
}

func javaScriptIsHapiAMQPConsumer(path string, node *tree_sitter.Node, name string, source []byte) bool {
	relativePath := filepath.ToSlash(path)
	if !strings.Contains(relativePath, "/server/resources/consumers/") &&
		!strings.HasPrefix(relativePath, "server/resources/consumers/") {
		return false
	}
	return strings.TrimSpace(name) == "consume" && javaScriptIsCommonJSExport(node, name, source)
}

func javaScriptIsExported(node *tree_sitter.Node) bool {
	for current := node; current != nil; current = current.Parent() {
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

func javaScriptMemberBaseAndProperty(node *tree_sitter.Node, source []byte) (string, string, bool) {
	if node == nil || node.Kind() != "member_expression" {
		return "", "", false
	}
	objectNode := node.ChildByFieldName("object")
	propertyNode := node.ChildByFieldName("property")
	base := javaScriptMemberExpressionBase(objectNode, source)
	property := javaScriptIdentifierName(propertyNode, source)
	if base == "" || property == "" {
		return "", "", false
	}
	return base, property, true
}

func javaScriptMemberExpressionBase(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	if base := javaScriptIdentifierName(node, source); base != "" {
		return base
	}
	switch node.Kind() {
	case "call_expression":
		functionNode := node.ChildByFieldName("function")
		if functionNode == nil || functionNode.Kind() != "member_expression" {
			return ""
		}
		routeBase, routeProperty, ok := javaScriptMemberBaseAndProperty(functionNode, source)
		if !ok || strings.ToLower(routeProperty) != "route" {
			return ""
		}
		return routeBase
	default:
		return ""
	}
}

func javaScriptIsExpressRouteChain(node *tree_sitter.Node, source []byte) bool {
	if node == nil || node.Kind() != "member_expression" {
		return false
	}
	objectNode := node.ChildByFieldName("object")
	if objectNode == nil || objectNode.Kind() != "call_expression" {
		return false
	}
	functionNode := objectNode.ChildByFieldName("function")
	_, property, ok := javaScriptMemberBaseAndProperty(functionNode, source)
	return ok && strings.ToLower(property) == "route"
}

// javaScriptExpressHandlerNames returns named route callbacks from an Express
// route argument, including handler arrays. Anonymous inline callbacks are not
// roots because the parser has no stable symbol to annotate.
func javaScriptExpressHandlerNames(node *tree_sitter.Node, source []byte) []string {
	if node == nil {
		return nil
	}
	if name := javaScriptIdentifierName(node, source); name != "" {
		return []string{name}
	}
	switch node.Kind() {
	case "array", "parenthesized_expression":
	default:
		return nil
	}

	names := []string{}
	cursor := node.Walk()
	children := node.NamedChildren(cursor)
	cursor.Close()
	for i := range children {
		for _, name := range javaScriptExpressHandlerNames(&children[i], source) {
			names = appendUniqueString(names, name)
		}
	}
	return names
}

func javaScriptIdentifierName(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	switch node.Kind() {
	case "identifier", "property_identifier":
		return strings.TrimSpace(nodeText(node, source))
	default:
		return ""
	}
}
