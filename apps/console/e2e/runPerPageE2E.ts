// runPerPageE2E.ts — Runner for per-page Playwright e2e tests (84 pages).
//
// Starts the Vite dev server + Chromium once, seeds localStorage for private
// mode, installs mock API handlers, then iterates through all registered page
// tests. Each page test navigates to its route and runs assertions.
//
// Run via: npx tsx apps/console/e2e/runPerPageE2E.ts

import type { Page } from "playwright";
import { getPage, getBaseUrl, cleanup } from "./setup.js";
import type { PageTest, PageTestResult } from "./types.js";
import { allTests } from "./allPageTests.js";

const navTimeoutMs = 30000;
const settleMs = 1500;

async function runOneTest(page: Page, test: PageTest): Promise<PageTestResult> {
  const start = Date.now();
  const consoleErrors: string[] = [];

  const onConsole = (msg: { type: () => string; text: () => string }): void => {
    if (msg.type() === "error" && !msg.text().includes("Download the React DevTools")) {
      consoleErrors.push(msg.text());
    }
  };
  page.on("console", onConsole);

  try {
    await page.goto(`${getBaseUrl()}${test.path}`, { waitUntil: "domcontentloaded", timeout: navTimeoutMs });
    await page.waitForTimeout(settleMs);
    await test.assert(page);

    if (consoleErrors.length > 0) {
      return {
        path: test.path,
        label: test.label,
        passed: false,
        error: `console errors: ${consoleErrors.join("; ")}`,
        durationMs: Date.now() - start,
      };
    }
    return { path: test.path, label: test.label, passed: true, durationMs: Date.now() - start };
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    const errDetail =
      consoleErrors.length > 0 ? `${msg} (+ console errors: ${consoleErrors.join("; ")})` : msg;
    return {
      path: test.path,
      label: test.label,
      passed: false,
      error: errDetail,
      durationMs: Date.now() - start,
    };
  } finally {
    page.off("console", onConsole);
  }
}

async function main(): Promise<void> {
  process.stdout.write(`console-e2e-mock: starting (${allTests.length} pages)\n`);
  const { page } = await getPage();

  const results: PageTestResult[] = [];
  for (const test of allTests) {
    const result = await runOneTest(page, test);
    results.push(result);
    process.stdout.write(
      `  ${result.passed ? "PASS" : "FAIL"} ${test.path} (${test.label}) [${result.durationMs}ms]\n`,
    );
    if (result.error) {
      process.stdout.write(`        ${result.error}\n`);
    }
  }

  const passed = results.filter((r) => r.passed).length;
  const failed = results.filter((r) => !r.passed).length;
  process.stdout.write(`console-e2e-mock: ${passed}/${results.length} passed, ${failed} failed\n`);

  await cleanup();
  if (failed > 0) process.exit(1);
}

void main();
