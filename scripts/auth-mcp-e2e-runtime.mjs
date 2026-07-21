// Bootstrap for the MCP-identity E2E runner (F-9, issue #5170).
//
// Clone of scripts/console-auth-e2e-runtime.mjs -- see that file's header
// comment for why the runner module is loaded through Vite's SSR transformer
// rather than relying on native Node TypeScript stripping.
//
// The runner exports `runAuthMcpE2E()`, which returns a process exit code and
// never calls process.exit itself, so this bootstrap owns the exit code and
// guarantees the Vite loader server is closed on every path.

import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { createServer } from "vite";

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(here, "..");

/** Extract and validate the runner function from a transformed module. */
export function runnerFromModule(moduleNamespace) {
  const runner = moduleNamespace?.runAuthMcpE2E;
  if (typeof runner !== "function") {
    throw new Error("auth MCP E2E runner did not export runAuthMcpE2E");
  }
  return runner;
}

/** Load the TypeScript runner through Vite rather than native Node TS support. */
export async function loadAuthMcpE2ERunner(root) {
  const server = await createServer({
    root,
    configFile: false,
    logLevel: "silent",
    server: { middlewareMode: true },
    appType: "custom",
  });
  try {
    const moduleNamespace = await server.ssrLoadModule(
      "/apps/console/e2e/runAuthMcpE2E.ts",
    );
    return runnerFromModule(moduleNamespace);
  } finally {
    await server.close();
  }
}

const invokedDirectly = process.argv[1] === fileURLToPath(import.meta.url);

if (invokedDirectly) {
  loadAuthMcpE2ERunner(repoRoot)
    .then((runner) => runner())
    .then((code) => {
      process.exitCode = code;
    })
    .catch((error) => {
      process.stderr.write(
        `auth-mcp-e2e: ${error instanceof Error ? (error.stack ?? error.message) : String(error)}\n`,
      );
      process.exitCode = 1;
    });
}
