// setup.ts — Shared Playwright/Vite setup for per-page e2e tests.
//
// Starts the Vite dev server once, launches Chromium once, and installs mock
// API handlers. All page test files share this browser + server context.

import { spawn, type ChildProcessByStdio } from "node:child_process";
import type { Readable } from "node:stream";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { chromium, type Browser, type Page } from "playwright";
import { installMockApi } from "./mockApi.ts";

let _server: DevServer | undefined;
let _browser: Browser | undefined;

export interface DevServer {
  process: ChildProcessByStdio<null, Readable, Readable>;
  baseUrl: string;
}

const consoleDir = resolve(fileURLToPath(import.meta.url), "..", "..");
const repoRoot = resolve(consoleDir, "..", "..");
const devServerPort = 5190;
const navTimeoutMs = 30000;
const devServerReadyTimeoutMs = 120000;

async function startDevServer(): Promise<DevServer> {
  const viteEntry = resolve(repoRoot, "node_modules", "vite", "bin", "vite.js");
  const child = spawn(
    process.execPath,
    [
      viteEntry,
      "--config",
      "apps/console/vite.config.ts",
      "--host",
      "127.0.0.1",
      "--strictPort",
      "--port",
      String(devServerPort),
    ],
    { cwd: repoRoot, env: { ...process.env }, stdio: ["ignore", "pipe", "pipe"], shell: true },
  );

  // Pipe stderr so Vite errors are visible in CI logs.
  child.stderr.on("data", (chunk: Buffer) => {
    process.stderr.write(`[vite-err] ${chunk.toString()}`);
  });

  const baseUrl = await new Promise<string>((resolvePromise, rejectPromise) => {
    const timer = setTimeout(
      () => rejectPromise(new Error("dev server did not become ready")),
      devServerReadyTimeoutMs,
    );
    const onData = (chunk: Buffer): void => {
      const text = chunk.toString();
      const m = text.match(/Local:\s+(http:\/\/127\.0\.0\.1:\d+)\/?/);
      if (m) {
        clearTimeout(timer);
        resolvePromise(m[1]);
      }
    };
    child.stdout.on("data", onData);
    child.on("exit", (code) => {
      clearTimeout(timer);
      rejectPromise(new Error(`dev server exited with code ${String(code)}`));
    });
  });
  return { process: child, baseUrl };
}

async function stopDevServer(server: DevServer): Promise<void> {
  if (server.process.exitCode !== null || server.process.signalCode !== null) return;
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

export async function getPage(): Promise<{ page: Page; baseUrl: string }> {
  if (!_server) {
    _server = await startDevServer();
  }
  if (!_browser) {
    _browser = await chromium.launch();
  }
  const context = await _browser.newContext();

  await context.addInitScript(() => {
    window.localStorage.setItem(
      "eshu.console.environment",
      JSON.stringify({
        mode: "private",
        apiBaseUrl: "/eshu-api/",
        recentApiBaseUrls: ["/eshu-api/"],
      }),
    );
  });

  const page = await context.newPage();
  await installMockApi(page);

  // Navigate to base to trigger the boot sequence; wait for connected pill.
  await page.goto(_server.baseUrl, { waitUntil: "domcontentloaded", timeout: navTimeoutMs });
  await page.waitForSelector(".source-pill.src-connected", { timeout: navTimeoutMs });

  return { page, baseUrl: _server.baseUrl };
}

export function getBaseUrl(): string {
  if (!_server) throw new Error("Dev server not started");
  return _server.baseUrl;
}

export async function cleanup(): Promise<void> {
  if (_browser) {
    await _browser.close().catch(() => undefined);
    _browser = undefined;
  }
  if (_server) {
    await stopDevServer(_server);
    _server = undefined;
  }
}

export { navTimeoutMs };
