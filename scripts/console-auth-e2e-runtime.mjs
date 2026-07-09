// Bootstrap for the console browser-auth E2E runner (issue #4971 phase 2).
//
// Loads apps/console/e2e/runAuthE2E.ts through Vite's SSR transformer,
// mirroring scripts/console-live-e2e-runtime.mjs exactly (see that file's
// header comment for why: native Node TypeScript stripping is Node >= 23.6
// only, and this keeps the gate runnable across the repo's supported Node
// range without an extra `tsx`-style dependency).
//
// The runner exports `runAuthE2E()`, which returns a process exit code and
// never calls process.exit itself, so this bootstrap owns the exit code and
// guarantees the Vite loader server is closed on every path.

import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { createServer } from "vite";

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(here, "..");

/** Extract and validate the runner function from a transformed module. */
export function runnerFromModule(moduleNamespace) {
  const runner = moduleNamespace?.runAuthE2E;
  if (typeof runner !== "function") {
    throw new Error("auth E2E runner did not export runAuthE2E");
  }
  return runner;
}

/** Load the TypeScript runner through Vite rather than native Node TS support. */
export async function loadAuthE2ERunner(root) {
  const server = await createServer({
    root,
    // No project vite config: this loader only needs Vite's TS->ESM transform
    // for the runner module, not the root Cloudflare Pages dev config.
    configFile: false,
    logLevel: "silent",
    server: { middlewareMode: true },
    appType: "custom"
  });
  try {
    const moduleNamespace = await server.ssrLoadModule(
      "/apps/console/e2e/runAuthE2E.ts"
    );
    return runnerFromModule(moduleNamespace);
  } finally {
    await server.close();
  }
}

const invokedDirectly = process.argv[1] === fileURLToPath(import.meta.url);

if (invokedDirectly) {
  loadAuthE2ERunner(repoRoot)
    .then((runner) => runner())
    .then((code) => {
      process.exitCode = code;
    })
    .catch((error) => {
      process.stderr.write(
        `auth-e2e: ${error instanceof Error ? (error.stack ?? error.message) : String(error)}\n`
      );
      process.exitCode = 1;
    });
}
