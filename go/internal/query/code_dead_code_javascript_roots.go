package query

import (
	"slices"
	"strings"
)

func deadCodeIsJavaScriptFrameworkRoot(result map[string]any, entity *EntityContent, stats *deadCodePolicyStats) bool {
	switch strings.ToLower(deadCodeEntityLanguage(result, entity)) {
	case "javascript", "jsx", "typescript", "tsx":
	default:
		return false
	}
	rootKinds := deadCodeRootKinds(result, entity)
	if len(rootKinds) == 0 {
		return false
	}
	if slices.Contains(rootKinds, "javascript.node_package_export") {
		stats.ParserMetadataFrameworkRoots++
		return true
	}
	if slices.Contains(rootKinds, "javascript.commonjs_default_export") {
		stats.ParserMetadataFrameworkRoots++
		return true
	}
	if primaryEntityLabel(result) != "Function" {
		return false
	}
	if slices.Contains(rootKinds, "javascript.nextjs_route_export") ||
		slices.Contains(rootKinds, "javascript.nextjs_app_export") ||
		slices.Contains(rootKinds, "javascript.express_route_registration") ||
		slices.Contains(rootKinds, "javascript.express_middleware_registration") ||
		slices.Contains(rootKinds, "javascript.koa_middleware_registration") ||
		slices.Contains(rootKinds, "javascript.koa_route_registration") ||
		slices.Contains(rootKinds, "javascript.fastify_hook_registration") ||
		slices.Contains(rootKinds, "javascript.fastify_route_registration") ||
		slices.Contains(rootKinds, "javascript.fastify_plugin_registration") ||
		slices.Contains(rootKinds, "javascript.nestjs_controller_method") ||
		slices.Contains(rootKinds, "javascript.node_package_entrypoint") ||
		slices.Contains(rootKinds, "javascript.node_package_bin") ||
		slices.Contains(rootKinds, "javascript.node_package_script") ||
		slices.Contains(rootKinds, "javascript.node_seed_execute") ||
		slices.Contains(rootKinds, "javascript.node_migration_export") ||
		slices.Contains(rootKinds, "javascript.commonjs_mixin_export") ||
		slices.Contains(rootKinds, "javascript.hapi_amqp_consumer") ||
		slices.Contains(rootKinds, "javascript.hapi_handler_export") ||
		slices.Contains(rootKinds, "javascript.hapi_plugin_register") ||
		slices.Contains(rootKinds, "javascript.hapi_route_config_handler") ||
		slices.Contains(rootKinds, "javascript.hapi_proxy_callback") ||
		slices.Contains(rootKinds, "typescript.interface_method_implementation") ||
		slices.Contains(rootKinds, "typescript.module_contract_export") ||
		slices.Contains(rootKinds, "typescript.static_registry_member") {
		stats.ParserMetadataFrameworkRoots++
		return true
	}
	return false
}
