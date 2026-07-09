// authE2EDevServer.ts — Vite dev-server lifecycle for the browser-auth E2E
// runner (issue #4971 phase 2).
//
// Console-reachability decision: this runner drives the console through the
// HOST Vite dev server, not a containerized console build, exactly like the
// existing live-stack gate (e2e/runConsoleLiveE2E.ts). The dev server owns
// the `/eshu-api` proxy (apps/console/vite.config.ts), so pointing
// ESHU_DEV_PROXY_TARGET at the e2e stack's host-mapped API port
// (docker-compose.e2e.yaml's `eshu` service, default 127.0.0.1:28080) is
// sufficient for both the browser's XHR/fetch traffic AND this runner's own
// out-of-band HTTP assertions to reach the same API. Containerizing the
// console was rejected for this phase: it would need its own Dockerfile
// stage, its own compose service, and its own build step, for no behavioral
// difference over the proxy path every other console E2E gate already uses.
// A containerized console only becomes worth it if a future phase needs the
// console reachable from something that cannot resolve the host loopback
// (not the case for a local Playwright-driven Chromium).
import { spawn, type ChildProcessByStdio } from "node:child_process";
import type { Readable } from "node:stream";
import { resolve } from "node:path";

export interface AuthE2EDevServer {
  readonly process: ChildProcessByStdio<null, Readable, Readable>;
  readonly baseUrl: string;
}

const devServerReadyTimeoutMs = 120000;

// startAuthE2EDevServer launches Vite for the console, proxying /eshu-api to
// apiBase, and resolves once it prints its local URL. Unlike
// runConsoleLiveE2E's dev server, no VITE_ESHU_API_KEY is set: this gate
// authenticates through the real browser-session cookie flow (setup wizard
// and local login), never an API key.
export async function startAuthE2EDevServer(
  repoRoot: string,
  apiBase: string,
  port: number,
): Promise<AuthE2EDevServer> {
  const viteBin = resolve(repoRoot, "node_modules", ".bin", "vite");
  const child = spawn(
    viteBin,
    [
      "--config",
      "apps/console/vite.config.ts",
      "--host",
      "127.0.0.1",
      "--strictPort",
      "--port",
      String(port),
    ],
    {
      cwd: repoRoot,
      env: {
        ...process.env,
        ESHU_DEV_PROXY_TARGET: apiBase,
      },
      stdio: ["ignore", "pipe", "pipe"],
    },
  );

  const baseUrl = await new Promise<string>((resolvePromise, rejectPromise) => {
    const timer = setTimeout(() => {
      rejectPromise(new Error("dev server did not become ready in time"));
    }, devServerReadyTimeoutMs);

    const onData = (chunk: Buffer): void => {
      const text = chunk.toString();
      process.stdout.write(`[vite] ${text}`);
      const match = text.match(/Local:\s+(http:\/\/127\.0\.0\.1:\d+)\/?/);
      if (match) {
        clearTimeout(timer);
        resolvePromise(match[1]);
      }
    };
    child.stdout.on("data", onData);
    child.stderr.on("data", (chunk: Buffer) =>
      process.stderr.write(`[vite-err] ${chunk.toString()}`),
    );
    child.on("exit", (code) => {
      clearTimeout(timer);
      rejectPromise(new Error(`dev server exited early with code ${String(code)}`));
    });
  });

  return { process: child, baseUrl };
}

// stopAuthE2EDevServer terminates the Vite process and resolves once it has
// exited so the listening port is freed before the runner returns.
export async function stopAuthE2EDevServer(server: AuthE2EDevServer): Promise<void> {
  if (server.process.exitCode !== null || server.process.signalCode !== null) {
    return;
  }
  await new Promise<void>((resolvePromise) => {
    const done = (): void => resolvePromise();
    server.process.once("exit", done);
    server.process.kill("SIGTERM");
    setTimeout(() => {
      server.process.kill("SIGKILL");
      resolvePromise();
    }, 5000);
  });
}
