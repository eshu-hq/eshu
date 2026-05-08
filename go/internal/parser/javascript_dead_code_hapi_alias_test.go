package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathJavaScriptHapiHandlerCommonJSAliasRoots(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeTestFile(
		t,
		filepath.Join(repoRoot, "package.json"),
		`{"name":"service-hapi"}`,
	)
	writeTestFile(
		t,
		filepath.Join(repoRoot, "server", "init", "plugins", "specs.ts"),
		`import path from 'path';

export const options = {
  openapi: {
    handlers: path.resolve(__dirname, '../../handlers'),
  },
};
`,
	)
	handlerPath := filepath.Join(repoRoot, "server", "handlers", "{dynamicPath}.js")
	writeTestFile(
		t,
		handlerPath,
		`const handleRequest = async (request) => request.payload;

module.exports.get = handleRequest;
module.exports.post = handleRequest;
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, handlerPath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(handler) error = %v, want nil", err)
	}

	assertParserStringSliceContains(
		t,
		assertFunctionByName(t, got, "handleRequest"),
		"dead_code_root_kinds",
		"javascript.hapi_handler_export",
	)
}

func TestDefaultEngineParsePathJavaScriptHapiProxyCallbackRoots(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "server", "resources", "proxy.js")
	writeTestFile(
		t,
		filePath,
		`module.exports.prepareProxy = (h) => {
  return h.proxy({
    mapUri: (request) => ({ uri: request.path }),
    onResponse: (error, response) => response,
  });
};

const plainObject = {
  plainCallback: () => "not a hapi proxy callback",
};
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(proxy resource) error = %v, want nil", err)
	}

	assertParserStringSliceContains(
		t,
		assertFunctionByName(t, got, "mapUri"),
		"dead_code_root_kinds",
		"javascript.hapi_proxy_callback",
	)
	assertParserStringSliceContains(
		t,
		assertFunctionByName(t, got, "onResponse"),
		"dead_code_root_kinds",
		"javascript.hapi_proxy_callback",
	)
}

func TestDefaultEngineParsePathJavaScriptHapiControllerRouteConfigHandlerRoots(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "server", "controllers", "orders.js")
	writeTestFile(
		t,
		filePath,
		`module.exports = {
  list: {
    handler: async (request) => request.params.id,
    description: "list orders",
  },
};

const helper = () => "helper";
const plainObject = {
  handler: () => "not exported route config",
};
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertParserStringSliceContains(
		t,
		assertFunctionByName(t, got, "handler"),
		"dead_code_root_kinds",
		"javascript.hapi_route_config_handler",
	)
	if _, ok := assertFunctionByName(t, got, "helper")["dead_code_root_kinds"]; ok {
		t.Fatalf("helper dead_code_root_kinds present, want absent for local helper")
	}
}

func TestDefaultEngineParsePathJavaScriptFlatHapiInitPluginRegisterRoots(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "server", "init", "orders-init.js")
	writeTestFile(
		t,
		filePath,
		`const register = async (server) => {
  await server.register({});
};

module.exports.plugin = {
  register,
  name: "orders",
};

module.exports.inlinePlugin = {
  name: "orders",
  register: async (server) => {
    await server.register({});
  },
};

const localRegister = async () => {};
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertParserStringSliceContains(
		t,
		assertFunctionByName(t, got, "register"),
		"dead_code_root_kinds",
		"javascript.hapi_plugin_register",
	)
	if _, ok := assertFunctionByName(t, got, "localRegister")["dead_code_root_kinds"]; ok {
		t.Fatalf("localRegister dead_code_root_kinds present, want absent for local helper")
	}
}

func TestDefaultEngineParsePathJavaScriptHapiRouteHandlerReferenceCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "server", "controllers", "alerts.js")
	writeTestFile(
		t,
		filePath,
		`const alerts = require('../resources/alerts');

module.exports = {
  adsNotLive: {
    handler: alerts.adsNotLive,
  },
};

const plainObject = {
  handler: alerts.notARoute,
};
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	call := assertBucketItemByFieldValue(t, got, "function_calls", "full_name", "alerts.adsNotLive")
	assertStringFieldValue(t, call, "call_kind", "javascript.hapi_route_handler_reference")
	if item := bucketItemByFieldValue(got, "function_calls", "full_name", "alerts.notARoute"); item != nil {
		t.Fatalf("unexpected non-exported route handler reference call = %#v", item)
	}
}

func TestDefaultEngineParsePathJavaScriptCommonJSHapiRouteArrayHandlerReferences(t *testing.T) {
	t.Parallel()

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	repoRoot := nodeTypeScriptFixtureRoot()
	filePath := filepath.Join(repoRoot, "server", "routes", "search.js")
	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	insert := assertBucketItemByFieldValue(t, got, "function_calls", "full_name", "searchController.insertApiKey")
	assertStringFieldValue(t, insert, "call_kind", "javascript.hapi_route_handler_reference")
	getKeys := assertBucketItemByFieldValue(t, got, "function_calls", "full_name", "searchController.getKeys")
	assertStringFieldValue(t, getKeys, "call_kind", "javascript.hapi_route_handler_reference")
	update := assertBucketItemByFieldValue(t, got, "function_calls", "full_name", "searchController.updateKey")
	assertStringFieldValue(t, update, "call_kind", "javascript.hapi_route_handler_reference")
	if item := bucketItemByFieldValue(got, "function_calls", "full_name", "searchController.notMounted"); item != nil {
		t.Fatalf("unexpected non-exported route handler reference call = %#v", item)
	}
}

func TestDefaultEngineParsePathJavaScriptHapiServerRouteHandlerRootsAndReferences(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "server", "init", "routes.js")
	writeTestFile(
		t,
		filePath,
		`const alerts = require('../resources/alerts');

module.exports = function registerRoutes(server) {
  server.route([
    { method: "GET", path: "/health", handler: async () => ({ ok: true }) },
    { method: "GET", path: "/alerts", handler: alerts.adsNotLive },
    {
      method: "POST",
      path: "/orders",
      config: {
        handler: async (request) => request.payload,
      },
    },
  ]);
};
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertParserStringSliceContains(
		t,
		assertFunctionByName(t, got, "handler"),
		"dead_code_root_kinds",
		"javascript.hapi_route_config_handler",
	)
	call := assertBucketItemByFieldValue(t, got, "function_calls", "full_name", "alerts.adsNotLive")
	assertStringFieldValue(t, call, "call_kind", "javascript.hapi_route_handler_reference")
}

func TestDefaultEngineParsePathJavaScriptCommonJSLifecycleRoots(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	routesPath := filepath.Join(repoRoot, "server", "init", "routes.js")
	seedPath := filepath.Join(repoRoot, "seed", "20260508_add_records.js")
	consumerPath := filepath.Join(repoRoot, "server", "resources", "consumers", "order-updated.js")
	writeTestFile(
		t,
		routesPath,
		`const registerRoutes = function (server) {
  server.route([]);
};

module.exports = registerRoutes;
`,
	)
	writeTestFile(
		t,
		seedPath,
		`module.exports.execute = async (dbClient) => {
  return dbClient;
};
`,
	)
	writeTestFile(
		t,
		consumerPath,
		`module.exports.queue = "order.updated";
module.exports.consume = (channel) => {
  return channel;
};
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	routesPayload, err := engine.ParsePath(repoRoot, routesPath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(routes) error = %v, want nil", err)
	}
	seedPayload, err := engine.ParsePath(repoRoot, seedPath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(seed) error = %v, want nil", err)
	}
	consumerPayload, err := engine.ParsePath(repoRoot, consumerPath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(consumer) error = %v, want nil", err)
	}

	assertParserStringSliceContains(
		t,
		assertFunctionByName(t, routesPayload, "registerRoutes"),
		"dead_code_root_kinds",
		"javascript.commonjs_default_export",
	)
	assertParserStringSliceContains(
		t,
		assertFunctionByName(t, seedPayload, "execute"),
		"dead_code_root_kinds",
		"javascript.node_seed_execute",
	)
	assertParserStringSliceContains(
		t,
		assertFunctionByName(t, consumerPayload, "consume"),
		"dead_code_root_kinds",
		"javascript.hapi_amqp_consumer",
	)
}

func TestDefaultEngineParsePathJavaScriptFunctionValueReferenceCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "server", "resources", "orders.js")
	writeTestFile(
		t,
		filePath,
		`const isEnabled = (item) => item.enabled;
module.exports.updateOrder = (order) => order;

module.exports.list = (items, async) => {
  const enabled = items.filter(isEnabled);
  async.apply(module.exports.updateOrder, enabled);
  return enabled;
};
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	isEnabled := assertBucketItemByFieldValue(t, got, "function_calls", "full_name", "isEnabled")
	assertStringFieldValue(t, isEnabled, "call_kind", "javascript.function_value_reference")
	updateOrder := assertBucketItemByFieldValue(t, got, "function_calls", "full_name", "module.exports.updateOrder")
	assertStringFieldValue(t, updateOrder, "call_kind", "javascript.function_value_reference")
}

func bucketItemByFieldValue(payload map[string]any, bucket string, field string, want string) map[string]any {
	items, ok := payload[bucket].([]map[string]any)
	if !ok {
		return nil
	}
	for _, item := range items {
		value, _ := item[field].(string)
		if value == want {
			return item
		}
	}
	return nil
}
