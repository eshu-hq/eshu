// Bootstrap for the console live E2E gate (issue #3326).
//
// Loads the TypeScript runner (apps/console/e2e/runConsoleLiveE2E.ts) through
// Vite's SSR transformer instead of relying on native Node TypeScript stripping
// (default only on Node >= 23.6) or an extra dependency such as tsx. This keeps
// the gate runnable across the repo's supported Node range, including Node 22
// LTS. It mirrors the in-repo marketing-review runtime pattern.
//
// The runner exports `runConsoleLiveE2E()`, which returns a process exit code
// and never calls process.exit itself, so this bootstrap owns the exit code and
// guarantees the Vite loader server is closed on every path.

import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { createServer } from "vite";

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(here, "..");

/** Extract and validate the runner function from a transformed module. */
export function runnerFromModule(moduleNamespace) {
  const runner = moduleNamespace?.runConsoleLiveE2E;
  if (typeof runner !== "function") {
    throw new Error("console live E2E runner did not export runConsoleLiveE2E");
  }
  return runner;
}

/** Load the TypeScript runner through Vite rather than native Node TS support. */
export async function loadConsoleLiveE2ERunner(root) {
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
      "/apps/console/e2e/runConsoleLiveE2E.ts"
    );
    return runnerFromModule(moduleNamespace);
  } finally {
    await server.close();
  }
}

const invokedDirectly = process.argv[1] === fileURLToPath(import.meta.url);

if (invokedDirectly) {
  loadConsoleLiveE2ERunner(repoRoot)
    .then((runner) => runner())
    .then((code) => {
      process.exitCode = code;
    })
    .catch((error) => {
      process.stderr.write(
        `console-live-e2e: ${error instanceof Error ? (error.stack ?? error.message) : String(error)}\n`
      );
      process.exitCode = 1;
    });
}
