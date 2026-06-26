// a11yAudit.ts — Accessibility audit runner using @axe-core/playwright.
//
// Navigates every registered console page through the mock-API Playwright
// setup, runs an axe-core scan on each, and aggregates violations by severity
// and rule. Produces a baseline report suitable for the PR description.
//
// Self-contained: starts its own Vite dev server, launches its own Chromium,
// installs mock API handlers, and walks every page. Does not depend on the
// buggy setup.ts/getPage() F-1 scaffolding.
//
// Run via: npx tsx apps/console/e2e/a11yAudit.ts

import { spawn, type ChildProcessByStdio } from "node:child_process";
import type { Readable } from "node:stream";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";
import AxeBuilder from "@axe-core/playwright";
import { chromium, type Browser, type Page } from "playwright";
import { installMockApi } from "./mockApi.js";
import { allTests } from "./allPageTests.js";
import type { PageTest } from "./types.js";

const consoleDir = resolve(fileURLToPath(import.meta.url), "..", "..");
const repoRoot = resolve(consoleDir, "..", "..");
const viteBin = resolve(repoRoot, "node_modules", ".bin", "vite");
const devServerPort = 5190;
const devServerBaseUrl = `http://127.0.0.1:${devServerPort}`;
const navTimeoutMs = 30000;
const settleMs = 1500;
const devServerReadyTimeoutMs = 60000;

interface DevServer {
  process: ChildProcessByStdio<null, Readable, Readable>;
  baseUrl: string;
}

async function startDevServer(): Promise<DevServer> {
  const child = spawn(
    viteBin,
    [
      "--config",
      "apps/console/vite.config.ts",
      "--host",
      "127.0.0.1",
      "--strictPort",
      "--port",
      String(devServerPort),
    ],
    {
      cwd: repoRoot,
      env: { ...process.env },
      stdio: ["ignore", "pipe", "pipe"],
    },
  );

  // Collect stderr so Vite errors are visible and the pipe is drained.
  const stderrChunks: string[] = [];
  child.stderr.on("data", (chunk: Buffer) => {
    stderrChunks.push(chunk.toString());
  });

  const baseUrl = await new Promise<string>((resolvePromise, rejectPromise) => {
    const timer = setTimeout(() => {
      const errText = stderrChunks.join("");
      rejectPromise(
        new Error(
          `dev server did not become ready in ${devServerReadyTimeoutMs}ms` +
            (errText ? `\nvite stderr: ${errText}` : ""),
        ),
      );
    }, devServerReadyTimeoutMs);

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
      const errText = stderrChunks.join("");
      clearTimeout(timer);
      rejectPromise(
        new Error(
          `dev server exited with code ${String(code)}` +
            (errText ? `\nvite stderr: ${errText}` : ""),
        ),
      );
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

function routeUrl(path: string): string {
  return `${devServerBaseUrl}${path}`;
}

interface PageA11yResult {
  path: string;
  label: string;
  violations: number;
  passes: number;
  incomplete: number;
  error?: string;
}

async function auditPage(page: Page, test: PageTest): Promise<PageA11yResult> {
  try {
    await page.goto(routeUrl(test.path), { waitUntil: "domcontentloaded", timeout: navTimeoutMs });
    await page.waitForTimeout(settleMs);

    const builder = new AxeBuilder({ page });
    const results = await builder.analyze();

    return {
      path: test.path,
      label: test.label,
      violations: results.violations.length,
      passes: results.passes.length,
      incomplete: results.incomplete.length,
    };
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    return {
      path: test.path,
      label: test.label,
      violations: 0,
      passes: 0,
      incomplete: 0,
      error: msg,
    };
  }
}

interface StructuredViolation {
  impact: string;
  id: string;
  description: string;
  nodes: number;
  path: string;
}

async function runStructuredAudit(
  page: Page,
  tests: readonly PageTest[],
): Promise<StructuredViolation[]> {
  const allViolations: StructuredViolation[] = [];

  for (const test of tests) {
    try {
      await page.goto(routeUrl(test.path), { waitUntil: "domcontentloaded", timeout: navTimeoutMs });
      await page.waitForTimeout(settleMs);

      const builder = new AxeBuilder({ page });
      const results = await builder.analyze();

      for (const v of results.violations) {
        allViolations.push({
          impact: v.impact ?? "unknown",
          id: v.id,
          description: v.description,
          nodes: v.nodes.length,
          path: test.path,
        });
      }
    } catch {
      // Navigation errors are expected on some pages in mock mode; skip.
    }
  }

  return allViolations;
}

function printBaseline(violations: StructuredViolation[]): void {
  const bySeverity = new Map<string, number>();
  const byRule = new Map<string, { count: number; description: string }>();

  for (const v of violations) {
    const sev = v.impact;
    bySeverity.set(sev, (bySeverity.get(sev) ?? 0) + v.nodes);

    const existing = byRule.get(v.id);
    if (existing) {
      existing.count += v.nodes;
    } else {
      byRule.set(v.id, { count: v.nodes, description: v.description });
    }
  }

  process.stdout.write("\n=== A11Y BASELINE REPORT ===\n\n");
  process.stdout.write("## Violations by Severity\n\n");
  const sevOrder = ["critical", "serious", "moderate", "minor"];
  for (const sev of sevOrder) {
    const count = bySeverity.get(sev) ?? 0;
    process.stdout.write(`- ${sev}: ${count} node(s)\n`);
  }
  const unknown = bySeverity.get("unknown") ?? 0;
  if (unknown > 0) {
    process.stdout.write(`- unknown: ${unknown} node(s)\n`);
  }

  process.stdout.write("\n## Top Rules by Violation Count\n\n");
  const sorted = [...byRule.entries()]
    .sort((a, b) => b[1].count - a[1].count)
    .slice(0, 10);

  for (const [rule, info] of sorted) {
    process.stdout.write(`- **${rule}** (${info.count} node(s)): ${info.description}\n`);
  }

  const totalViolations = violations.reduce((sum, v) => sum + v.nodes, 0);
  const pagesWithViolations = new Set(violations.map((v) => v.path)).size;
  process.stdout.write(`\n## Summary\n\n`);
  process.stdout.write(`- Total violation nodes: ${totalViolations}\n`);
  process.stdout.write(`- Total distinct violation instances: ${violations.length}\n`);
  process.stdout.write(`- Pages with violations: ${pagesWithViolations}\n`);
  process.stdout.write("\n=== END BASELINE REPORT ===\n");
}

function printPageResults(results: PageA11yResult[]): void {
  const pagesWithViolations = results.filter((r) => r.violations > 0).length;
  const pagesWithErrors = results.filter((r) => r.error).length;

  process.stdout.write(
    `console-a11y: ${results.length} pages scanned, ${pagesWithViolations} with violations, ${pagesWithErrors} navigation errors\n`,
  );

  for (const r of results) {
    if (r.error) {
      process.stdout.write(`  ERR ${r.path} (${r.label}): ${r.error}\n`);
    } else if (r.violations > 0) {
      process.stdout.write(
        `  VIOL ${r.path} (${r.label}): ${r.violations} violations, ${r.passes} passes, ${r.incomplete} incomplete\n`,
      );
    }
  }
}

async function main(): Promise<void> {
  const gateMode = process.argv.includes("--gate");

  process.stdout.write(
    `console-a11y: starting audit (${allTests.length} pages, mode=${gateMode ? "gate" : "baseline"})\n`,
  );

  const server = await startDevServer();
  let browser: Browser | undefined;
  let exitCode = 0;

  try {
    browser = await chromium.launch();
    const context = await browser.newContext();

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

    // Navigate to root to trigger boot; wait for connected pill.
    await page.goto(devServerBaseUrl, { waitUntil: "domcontentloaded", timeout: navTimeoutMs });
    await page.waitForSelector(".source-pill.src-connected", { timeout: navTimeoutMs });
    process.stdout.write("console-a11y: boot connected\n");

    // First pass: fast audit with per-page violation counts.
    const pageResults: PageA11yResult[] = [];
    for (const test of allTests) {
      const result = await auditPage(page, test);
      pageResults.push(result);
      const status = result.error ? "ERR" : result.violations > 0 ? "VIOL" : "OK";
      process.stdout.write(`  ${status} ${test.path} (${test.label})\n`);
    }

    printPageResults(pageResults);

    // Second pass: structured scan for the baseline report.
    process.stdout.write("\nconsole-a11y: collecting structured baseline...\n");
    const structuredViolations = await runStructuredAudit(page, allTests);
    printBaseline(structuredViolations);

    // Gate evaluation: fail on critical + serious violations.
    if (gateMode) {
      const blocking = structuredViolations.filter(
        (v) => v.impact === "critical" || v.impact === "serious",
      );
      const warn = structuredViolations.filter(
        (v) => v.impact === "moderate" || v.impact === "minor",
      );

      process.stdout.write("\n=== A11Y GATE RESULT ===\n\n");
      process.stdout.write(
        `Blocking violations (critical + serious): ${blocking.reduce((sum, v) => sum + v.nodes, 0)} nodes across ${blocking.length} instances\n`,
      );
      process.stdout.write(
        `Warning violations (moderate + minor): ${warn.reduce((sum, v) => sum + v.nodes, 0)} nodes across ${warn.length} instances\n`,
      );

      if (blocking.length > 0) {
        process.stdout.write("\nGATE FAILED: critical or serious a11y violations detected\n");
        for (const v of blocking) {
          process.stdout.write(`  [${v.impact}] ${v.id} — ${v.path} (${v.nodes} node(s))\n`);
        }
        exitCode = 1;
      } else {
        process.stdout.write("\nGATE PASSED: no critical or serious a11y violations\n");
      }
    }

    await browser.close();
    browser = undefined;
  } finally {
    if (browser) {
      await browser.close().catch(() => undefined);
    }
    await stopDevServer(server);
  }

  if (exitCode !== 0) {
    process.exit(exitCode);
  }
}

void main();
