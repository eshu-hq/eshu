import { spawn, type ChildProcessByStdio } from "node:child_process";
import { resolve } from "node:path";
import type { Readable } from "node:stream";

export interface LiveE2EDevServer {
  readonly process: ChildProcessByStdio<null, Readable, Readable>;
  readonly baseUrl: string;
}

interface LiveE2EDevServerOptions {
  readonly repoRoot: string;
  readonly apiBase: string;
  readonly apiKey: string;
  readonly useBearerAuth: boolean;
  readonly port: number;
}

const devServerReadyTimeoutMs = 120_000;
const defaultConsolePort = 5180;

export function parseLiveE2EConsolePort(value: string | undefined): number {
  const normalized = value?.trim() ?? "";
  if (normalized === "") return defaultConsolePort;
  if (!/^\d+$/.test(normalized)) {
    throw new Error("ESHU_E2E_CONSOLE_PORT must be an integer from 1 through 65535");
  }
  const port = Number(normalized);
  if (port < 1 || port > 65_535) {
    throw new Error("ESHU_E2E_CONSOLE_PORT must be an integer from 1 through 65535");
  }
  return port;
}

export function liveE2EDevServerArgs(port: number): readonly string[] {
  return [
    "--config",
    "apps/console/vite.config.ts",
    "--host",
    "127.0.0.1",
    "--strictPort",
    "--port",
    String(port),
  ];
}

// stopLiveE2EDevServer terminates Vite and waits until its port is free.
export async function stopLiveE2EDevServer(
  server: LiveE2EDevServer,
  forceKillAfterMs = 5_000,
  forcedExitTimeoutMs = 5_000,
): Promise<void> {
  if (server.process.exitCode !== null || server.process.signalCode !== null) {
    return;
  }
  await new Promise<void>((resolvePromise, rejectPromise) => {
    let settled = false;
    let killTimer: ReturnType<typeof setTimeout> | undefined;
    let exitTimer: ReturnType<typeof setTimeout> | undefined;
    const done = (error?: Error): void => {
      if (settled) return;
      settled = true;
      if (killTimer) clearTimeout(killTimer);
      if (exitTimer) clearTimeout(exitTimer);
      if (error) rejectPromise(error);
      else resolvePromise();
    };
    server.process.once("exit", () => done());
    server.process.kill("SIGTERM");
    killTimer = setTimeout(() => {
      server.process.kill("SIGKILL");
      exitTimer = setTimeout(
        () => done(new Error("dev server did not exit after SIGKILL")),
        forcedExitTimeoutMs,
      );
    }, forceKillAfterMs);
  });
}

export async function startDevServerWithSpawner(
  spawnProcess: () => LiveE2EDevServer["process"],
  readyTimeoutMs = devServerReadyTimeoutMs,
): Promise<LiveE2EDevServer> {
  const child = spawnProcess();
  try {
    const baseUrl = await new Promise<string>((resolvePromise, rejectPromise) => {
      const timer = setTimeout(() => {
        rejectPromise(new Error("dev server did not become ready in time"));
      }, readyTimeoutMs);

      child.stdout.on("data", (chunk: Buffer) => {
        const output = chunk.toString();
        process.stdout.write(`[vite] ${output}`);
        const match = output.match(/Local:\s+(http:\/\/127\.0\.0\.1:\d+)\/?/);
        if (match) {
          clearTimeout(timer);
          resolvePromise(match[1]);
        }
      });
      child.stderr.on("data", (chunk: Buffer) =>
        process.stderr.write(`[vite-err] ${chunk.toString()}`),
      );
      child.on("exit", (code) => {
        clearTimeout(timer);
        rejectPromise(new Error(`dev server exited early with code ${String(code)}`));
      });
    });

    return { process: child, baseUrl };
  } catch (error) {
    await stopLiveE2EDevServer({ process: child, baseUrl: "" });
    throw error;
  }
}

// startLiveE2EDevServer launches the real console dev server and API proxy.
export function startLiveE2EDevServer(options: LiveE2EDevServerOptions): Promise<LiveE2EDevServer> {
  const viteBin = resolve(options.repoRoot, "node_modules", ".bin", "vite");
  return startDevServerWithSpawner(() =>
    spawn(viteBin, liveE2EDevServerArgs(options.port), {
      cwd: options.repoRoot,
      env: {
        ...process.env,
        VITE_ESHU_API_KEY: options.useBearerAuth ? options.apiKey : "",
        ESHU_DEV_PROXY_TARGET: options.apiBase,
      },
      stdio: ["ignore", "pipe", "pipe"],
    }),
  );
}
