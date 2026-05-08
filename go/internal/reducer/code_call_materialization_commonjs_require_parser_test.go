package reducer

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser"
)

func TestExtractCodeCallRowsResolvesParsedCommonJSNamespaceRequire(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "server", "handlers", "remoteid.js")
	calleePath := filepath.Join(repoRoot, "server", "resources", "jwt.js")
	writeReducerTestFile(t, callerPath, `const jwt = require('../resources/jwt');

module.exports.post = async req => {
  return jwt.encode(req.payload);
};
`)
	writeReducerTestFile(t, calleePath, `module.exports.encode = async data => {
  return String(data);
};
`)

	rows := parsedJavaScriptCodeCallRows(t, repoRoot, callerPath, "post", calleePath, "encode")
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1; rows=%#v", len(rows), rows)
	}
	if got, want := rows[0]["callee_entity_id"], "content-entity:encode"; got != want {
		t.Fatalf("callee_entity_id = %#v, want %#v; rows=%#v", got, want, rows)
	}
}

func TestExtractCodeCallRowsResolvesParsedCommonJSDestructuredRequireAlias(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "server", "handlers", "remoteid.js")
	calleePath := filepath.Join(repoRoot, "server", "resources", "jwt.js")
	writeReducerTestFile(t, callerPath, `const { encode: sign } = require('../resources/jwt');

module.exports.post = async req => {
  return sign(req.payload);
};
`)
	writeReducerTestFile(t, calleePath, `const encode = async data => {
  return String(data);
};

module.exports = { encode };
`)

	rows := parsedJavaScriptCodeCallRows(t, repoRoot, callerPath, "post", calleePath, "encode")
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1; rows=%#v", len(rows), rows)
	}
	if got, want := rows[0]["callee_entity_id"], "content-entity:encode"; got != want {
		t.Fatalf("callee_entity_id = %#v, want %#v; rows=%#v", got, want, rows)
	}
}

func TestExtractCodeCallRowsResolvesParsedHapiRouteHandlerReference(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "server", "controllers", "alerts.js")
	calleePath := filepath.Join(repoRoot, "server", "resources", "alerts.js")
	writeReducerTestFile(t, callerPath, `const alerts = require('../resources/alerts');

module.exports = {
  adsNotLive: {
    handler: alerts.adsNotLive,
  },
};
`)
	writeReducerTestFile(t, calleePath, `module.exports.adsNotLive = (request, reply) => {
  return reply.response({ ok: true });
};
`)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	importsMap, err := engine.PreScanRepositoryPaths(repoRoot, []string{callerPath, calleePath})
	if err != nil {
		t.Fatalf("PreScanRepositoryPaths() error = %v, want nil", err)
	}
	callerPayload, err := engine.ParsePath(repoRoot, callerPath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(caller) error = %v, want nil", err)
	}
	calleePayload, err := engine.ParsePath(repoRoot, calleePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(callee) error = %v, want nil", err)
	}
	assignReducerTestFunctionUID(t, calleePayload, "adsNotLive", "content-entity:ads-not-live")
	callerRelativePath := reducerTestRelativePath(t, repoRoot, callerPath)

	_, rows := ExtractCodeCallRows([]facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id":     "repo-js",
				"imports_map": importsMap,
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          "repo-js",
				"relative_path":    callerRelativePath,
				"parsed_file_data": callerPayload,
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          "repo-js",
				"relative_path":    reducerTestRelativePath(t, repoRoot, calleePath),
				"parsed_file_data": calleePayload,
			},
		},
	})

	assertReducerCodeCallRow(t, rows, "repo-js:"+callerRelativePath, "content-entity:ads-not-live")
	for _, row := range rows {
		if row["caller_entity_id"] == "repo-js:"+callerRelativePath && row["callee_entity_id"] == "content-entity:ads-not-live" {
			if got, want := row["relationship_type"], "REFERENCES"; got != want {
				t.Fatalf("relationship_type = %#v, want %#v in %#v", got, want, rows)
			}
			return
		}
	}
}

func TestExtractCodeCallRowsResolvesParsedHapiRouteHandlerRequirePropertyAlias(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "server", "controllers", "listings.js")
	calleePath := filepath.Join(repoRoot, "server", "resources", "listings", "findByPartyAndListing.js")
	writeReducerTestFile(t, callerPath, `const findByPartyAndListing = require('../resources/listings/findByPartyAndListing').byPartyAndListingId;

module.exports = {
  byPartyAndListingId: {
    handler: findByPartyAndListing,
  },
};
`)
	writeReducerTestFile(t, calleePath, `module.exports.byPartyAndListingId = async (request, reply) => {
  return reply.response({ ok: true });
};
`)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	importsMap, err := engine.PreScanRepositoryPaths(repoRoot, []string{callerPath, calleePath})
	if err != nil {
		t.Fatalf("PreScanRepositoryPaths() error = %v, want nil", err)
	}
	callerPayload, err := engine.ParsePath(repoRoot, callerPath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(caller) error = %v, want nil", err)
	}
	calleePayload, err := engine.ParsePath(repoRoot, calleePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(callee) error = %v, want nil", err)
	}
	assignReducerTestFunctionUID(t, calleePayload, "byPartyAndListingId", "content-entity:by-party-and-listing")
	callerRelativePath := reducerTestRelativePath(t, repoRoot, callerPath)

	_, rows := ExtractCodeCallRows([]facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id":     "repo-js",
				"imports_map": importsMap,
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          "repo-js",
				"relative_path":    callerRelativePath,
				"parsed_file_data": callerPayload,
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          "repo-js",
				"relative_path":    reducerTestRelativePath(t, repoRoot, calleePath),
				"parsed_file_data": calleePayload,
			},
		},
	})

	assertReducerCodeCallRow(t, rows, "repo-js:"+callerRelativePath, "content-entity:by-party-and-listing")
}

func TestExtractCodeCallRowsResolvesParsedCommonJSModuleExportsAlias(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "server", "resources", "salesforce", "core.js")
	writeReducerTestFile(t, filePath, `const async = require('async');

var sfCore = module.exports;

module.exports.createOrder = function (order, callback) {
  callback(null, order);
};

module.exports.createAccount = function (order, callback) {
  callback(null, order);
};

module.exports.upsertOrder = function (order, callback) {
  sfCore.createOrder(order, callback);
};

module.exports.upsertAccount = function (order, callback) {
  async.waterfall([
    async.apply(sfCore.createAccount, order),
  ], callback);
};
`)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	payload, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}
	assignReducerTestFunctionUID(t, payload, "createOrder", "content-entity:create-order")
	assignReducerTestFunctionUID(t, payload, "createAccount", "content-entity:create-account")
	assignReducerTestFunctionUID(t, payload, "upsertOrder", "content-entity:upsert-order")
	assignReducerTestFunctionUID(t, payload, "upsertAccount", "content-entity:upsert-account")
	relativePath := reducerTestRelativePath(t, repoRoot, filePath)

	_, rows := ExtractCodeCallRows([]facts.Envelope{
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          "repo-js",
				"relative_path":    relativePath,
				"parsed_file_data": payload,
			},
		},
	})

	assertReducerCodeCallRow(t, rows, "content-entity:upsert-order", "content-entity:create-order")
	assertReducerCodeCallRow(t, rows, "content-entity:upsert-account", "content-entity:create-account")
}

func TestExtractCodeCallRowsResolvesParsedJavaScriptFunctionCallReceiver(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "server", "resources", "salesforce", "order-wrapper-mixin-listing.js")
	writeReducerTestFile(t, filePath, `var _enhanceValue = function(key, value) {
  return value;
};

module.exports.mixin = {};
module.exports.mixin.getFromListing = function(key, defaultValue) {
  return _enhanceValue.call(this, key, defaultValue);
};
`)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	payload, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}
	assignReducerTestFunctionUID(t, payload, "_enhanceValue", "content-entity:enhance-value")
	assignReducerTestFunctionUID(t, payload, "getFromListing", "content-entity:get-from-listing")
	relativePath := reducerTestRelativePath(t, repoRoot, filePath)

	_, rows := ExtractCodeCallRows([]facts.Envelope{
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          "repo-js",
				"relative_path":    relativePath,
				"parsed_file_data": payload,
			},
		},
	})

	assertReducerCodeCallRow(t, rows, "content-entity:get-from-listing", "content-entity:enhance-value")
}

func parsedJavaScriptCodeCallRows(
	t *testing.T,
	repoRoot string,
	callerPath string,
	callerName string,
	calleePath string,
	calleeName string,
) []map[string]any {
	t.Helper()

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	importsMap, err := engine.PreScanRepositoryPaths(repoRoot, []string{callerPath, calleePath})
	if err != nil {
		t.Fatalf("PreScanRepositoryPaths() error = %v, want nil", err)
	}
	callerPayload, err := engine.ParsePath(repoRoot, callerPath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(caller) error = %v, want nil", err)
	}
	calleePayload, err := engine.ParsePath(repoRoot, calleePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(callee) error = %v, want nil", err)
	}
	assignReducerTestFunctionUID(t, callerPayload, callerName, "content-entity:"+callerName)
	assignReducerTestFunctionUID(t, calleePayload, calleeName, "content-entity:"+calleeName)

	_, rows := ExtractCodeCallRows([]facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id":     "repo-js",
				"imports_map": importsMap,
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          "repo-js",
				"relative_path":    reducerTestRelativePath(t, repoRoot, callerPath),
				"parsed_file_data": callerPayload,
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          "repo-js",
				"relative_path":    reducerTestRelativePath(t, repoRoot, calleePath),
				"parsed_file_data": calleePayload,
			},
		},
	})
	return rows
}
