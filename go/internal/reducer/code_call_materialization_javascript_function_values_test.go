package reducer

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser"
)

func TestExtractCodeCallRowsResolvesTopLevelImportedFunctionValueReferences(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "server", "handlers", "_status.ts")
	firstCalleePath := filepath.Join(repoRoot, "server", "resources", "datasources", "primary-repository.ts")
	secondCalleePath := filepath.Join(repoRoot, "server", "resources", "datasources", "secondary-repository.ts")
	writeReducerTestFile(t, callerPath, `import * as primaryRepository from '../resources/datasources/primary-repository';
import * as secondaryRepository from '../resources/datasources/secondary-repository';

const statusFactory = (checkStatus) => async () => {
  return Promise.all(checkStatus.map((check) => check()));
};

export const get = statusFactory([
  primaryRepository.checkStatus,
  secondaryRepository.checkStatus,
]);
`)
	writeReducerTestFile(t, firstCalleePath, `export const checkStatus = async () => true;
`)
	writeReducerTestFile(t, secondCalleePath, `export const checkStatus = async () => true;
`)

	rows := parsedJavaScriptFunctionValueRows(t, repoRoot, callerPath, firstCalleePath, secondCalleePath)
	assertReducerRowsContainCallee(t, rows, "content-entity:primary-check-status")
	assertReducerRowsContainCallee(t, rows, "content-entity:secondary-check-status")
}

func TestExtractCodeCallRowsResolvesReturnedImportedFunctionValues(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "server", "resources", "block-lists.ts")
	calleePath := filepath.Join(repoRoot, "server", "resources", "datasources", "records-repository.ts")
	writeReducerTestFile(t, callerPath, `import { deleteListItems } from './datasources/records-repository';

const getDeleteMethod = async (listId: string) => {
  if (listId === 'known') {
    return deleteListItems;
  }
  return undefined;
};

export const deleteItemsFromBlockList = async (listId: string) => {
  const deleteMethod = await getDeleteMethod(listId);
  if (deleteMethod) {
    await deleteMethod();
  }
};
`)
	writeReducerTestFile(t, calleePath, `export const deleteListItems = async () => {};
`)

	rows := parsedJavaScriptFunctionValueRows(t, repoRoot, callerPath, calleePath)
	assertReducerRowsContainCallee(t, rows, "content-entity:delete-list-items")
}

func TestExtractCodeCallRowsResolvesDynamicImportMemberCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "service-entry.ts")
	calleePath := filepath.Join(repoRoot, "server", "resources", "next.adapter.ts")
	writeReducerTestFile(t, callerPath, `export const startServer = async () => {
  return await (await import('./server/resources/next.adapter.js')).createNextHandler();
};
`)
	writeReducerTestFile(t, calleePath, `export const createNextHandler = async () => {
  return async () => Symbol.for('h.abandon');
};
`)

	rows := parsedJavaScriptFunctionValueRows(t, repoRoot, callerPath, calleePath)
	assertReducerRowsContainCallee(t, rows, "content-entity:create-next-handler")
}

func TestExtractCodeCallRowsResolvesImportedTypeReferences(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "server", "resources", "constants", "auth-cookie-options.ts")
	calleePath := filepath.Join(repoRoot, "server", "resources", "types", "auth.ts")
	writeReducerTestFile(t, callerPath, `import { AuthCookieOptions } from '../types/auth';

export const AUTH_COOKIE_OPTS: AuthCookieOptions = {
  ttl: 3600000,
};
`)
	writeReducerTestFile(t, calleePath, `export interface AuthCookieOptions {
  ttl: number;
}
`)

	rows := parsedJavaScriptFunctionValueRows(t, repoRoot, callerPath, calleePath)
	assertReducerRowsContainCallee(t, rows, "content-entity:auth-cookie-options")
}

func TestExtractCodeCallRowsResolvesFastifyRouteObjectHandlerReference(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "server", "routes.ts")
	calleePath := filepath.Join(repoRoot, "server", "handlers", "health.ts")
	writeReducerTestFile(t, callerPath, `import fastify from "fastify";
import { healthHandler } from "./handlers/health";

const app = fastify();

export const registerRoutes = () => {
  app.route({
    method: "GET",
    url: "/health",
    handler: healthHandler,
  });
};
`)
	writeReducerTestFile(t, calleePath, `export const healthHandler = async () => ({ ok: true });
`)

	rows := parsedJavaScriptFunctionValueRows(t, repoRoot, callerPath, calleePath)
	assertReducerRowsContainCallee(t, rows, "content-entity:health-handler")
}

func TestExtractCodeCallRowsResolvesConstructorFunctionValueReference(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "workers", "queue.ts")
	calleePath := filepath.Join(repoRoot, "workers", "process-job.ts")
	writeReducerTestFile(t, callerPath, `import { Worker } from "bullmq";
import { processJob } from "./process-job";

export const startWorker = () => {
  return new Worker("emails", processJob);
};
`)
	writeReducerTestFile(t, calleePath, `export async function processJob(job) {
  return job.id;
}
`)

	rows := parsedJavaScriptFunctionValueRows(t, repoRoot, callerPath, calleePath)
	assertReducerRowsContainCallee(t, rows, "content-entity:process-job")
}

func parsedJavaScriptFunctionValueRows(t *testing.T, repoRoot string, paths ...string) []map[string]any {
	t.Helper()

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	importsMap, err := engine.PreScanRepositoryPaths(repoRoot, paths)
	if err != nil {
		t.Fatalf("PreScanRepositoryPaths() error = %v, want nil", err)
	}

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id":     "repo-ts",
				"imports_map": importsMap,
			},
		},
	}
	for _, path := range paths {
		payload, err := engine.ParsePath(repoRoot, path, false, parser.Options{})
		if err != nil {
			t.Fatalf("ParsePath(%s) error = %v, want nil", path, err)
		}
		assignJavaScriptFunctionValueUIDs(t, path, payload)
		envelopes = append(envelopes, facts.Envelope{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          "repo-ts",
				"relative_path":    reducerTestRelativePath(t, repoRoot, path),
				"parsed_file_data": payload,
			},
		})
	}

	_, rows := ExtractCodeCallRows(envelopes)
	return rows
}

func assignJavaScriptFunctionValueUIDs(t *testing.T, path string, payload map[string]any) {
	t.Helper()

	switch filepath.Base(path) {
	case "_status.ts":
		assignReducerTestFunctionUID(t, payload, "statusFactory", "content-entity:status-factory")
	case "primary-repository.ts":
		assignReducerTestFunctionUID(t, payload, "checkStatus", "content-entity:primary-check-status")
	case "secondary-repository.ts":
		assignReducerTestFunctionUID(t, payload, "checkStatus", "content-entity:secondary-check-status")
	case "block-lists.ts":
		assignReducerTestFunctionUID(t, payload, "getDeleteMethod", "content-entity:get-delete-method")
		assignReducerTestFunctionUID(
			t,
			payload,
			"deleteItemsFromBlockList",
			"content-entity:delete-items-from-block-list",
		)
	case "records-repository.ts":
		assignReducerTestFunctionUID(t, payload, "deleteListItems", "content-entity:delete-list-items")
	case "service-entry.ts":
		assignReducerTestFunctionUID(t, payload, "startServer", "content-entity:start-server")
	case "next.adapter.ts":
		assignReducerTestFunctionUID(t, payload, "createNextHandler", "content-entity:create-next-handler")
	case "auth.ts":
		assignReducerTestInterfaceUID(t, payload, "AuthCookieOptions", "content-entity:auth-cookie-options")
	case "routes.ts":
		assignReducerTestFunctionUID(t, payload, "registerRoutes", "content-entity:register-routes")
	case "health.ts":
		assignReducerTestFunctionUID(t, payload, "healthHandler", "content-entity:health-handler")
	case "queue.ts":
		assignReducerTestFunctionUID(t, payload, "startWorker", "content-entity:start-worker")
	case "process-job.ts":
		assignReducerTestFunctionUID(t, payload, "processJob", "content-entity:process-job")
	}
}
