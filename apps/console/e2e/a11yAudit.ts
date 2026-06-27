// a11yAudit.ts — Accessibility audit runner using @axe-core/playwright.
//
// Navigates every registered console page through the mock-API Playwright
// setup, runs an axe-core scan on each, and aggregates violations by severity
// and rule. Produces a baseline report suitable for the PR description.
//
// Reuses the proven setup.ts/getPage() infrastructure (shared with the mock
// E2E runner) instead of managing its own Vite dev server, so the dev-server
// startup path is identical to what passes in CI.
//
// Run via: npx tsx apps/console/e2e/a11yAudit.ts

import AxeBuilder from "@axe-core/playwright";
import type { Page } from "playwright";
import { getPage, getBaseUrl, cleanup } from "./setup.js";
import { allTests } from "./allPageTests.js";
import type { PageTest } from "./types.js";

const navTimeoutMs = 30000;
const settleMs = 1500;

interface PageA11yResult {
  path: string;
  label: string;
  violations: number;
  passes: number;
  incomplete: number;
  /** Non-empty when navigation or axe-core scan failed for this page. */
  error?: string;
}

interface StructuredViolation {
  impact: string;
  id: string;
  description: string;
  nodes: number;
  path: string;
}

interface AuditResult {
  pageResults: PageA11yResult[];
  structuredViolations: StructuredViolation[];
  skippedPages: number;
}

function routeUrl(path: string): string {
  return `${getBaseUrl()}${path}`;
}

// auditAllPages navigates every page, runs axe-core once per page, and collects
// both per-page counts and structured violation data in a single pass. This
// avoids a false-green gate when the dev server dies between two separate passes.
async function auditAllPages(page: Page, tests: readonly PageTest[]): Promise<AuditResult> {
  const pageResults: PageA11yResult[] = [];
  const structuredViolations: StructuredViolation[] = [];
  let skippedPages = 0;

  for (const test of tests) {
    try {
      await page.goto(routeUrl(test.path), {
        waitUntil: "domcontentloaded",
        timeout: navTimeoutMs,
      });
      await page.waitForTimeout(settleMs);

      const builder = new AxeBuilder({ page });
      const results = await builder.analyze();

      pageResults.push({
        path: test.path,
        label: test.label,
        violations: results.violations.length,
        passes: results.passes.length,
        incomplete: results.incomplete.length,
      });

      for (const v of results.violations) {
        structuredViolations.push({
          impact: v.impact ?? "unknown",
          id: v.id,
          description: v.description,
          nodes: v.nodes.length,
          path: test.path,
        });
      }

      const status = results.violations.length > 0 ? "VIOL" : "OK";
      process.stdout.write(`  ${status} ${test.path} (${test.label})\n`);
    } catch (err) {
      skippedPages += 1;
      const msg = err instanceof Error ? err.message : String(err);
      pageResults.push({
        path: test.path,
        label: test.label,
        violations: 0,
        passes: 0,
        incomplete: 0,
        error: msg,
      });
      process.stdout.write(`  ERR ${test.path} (${test.label}): ${msg}\n`);
    }
  }

  return { pageResults, structuredViolations, skippedPages };
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
  const sorted = [...byRule.entries()].sort((a, b) => b[1].count - a[1].count).slice(0, 10);

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

  let gateFailed = false;

  try {
    // Start the shared dev-server + browser (same path as the mock E2E runner,
    // proven in CI) INSIDE the try so cleanup() still runs if getPage() rejects
    // after spawning Vite / launching Chromium (e.g. the boot selector never
    // appears) — otherwise a failed boot would orphan the dev server on port
    // 5190 and poison the next local/CI retry.
    const { page } = await getPage();

    // Single atomic pass: navigate each page once, collect both per-page
    // counts and structured violations. This prevents a false-green gate
    // when the dev server or browser dies between separate passes.
    const { pageResults, structuredViolations, skippedPages } = await auditAllPages(page, allTests);

    printPageResults(pageResults);
    printBaseline(structuredViolations);

    // Gate evaluation: fail on critical + serious violations, or on excessive
    // skipped pages (degraded audit coverage that could mask violations).
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
        gateFailed = true;
      }

      // If more than 10% of pages (ceil to nearest >= 9) were skipped, the
      // audit coverage is too degraded to trust the result. Fail the gate
      // so operators notice rather than silently accepting an incomplete scan.
      const maxAllowedSkipped = Math.max(9, Math.ceil(allTests.length * 0.1));
      if (skippedPages > maxAllowedSkipped) {
        process.stdout.write(
          `\nGATE FAILED: ${skippedPages} pages skipped (max allowed: ${maxAllowedSkipped}) — audit coverage degraded\n`,
        );
        gateFailed = true;
      }

      if (!gateFailed) {
        process.stdout.write("\nGATE PASSED: no critical or serious a11y violations\n");
      }
    }
  } finally {
    await cleanup();
  }

  // Exit only AFTER cleanup() has shut down Vite + Chromium. Calling
  // process.exit(1) from inside the try would skip the finally and orphan the
  // dev server on port 5190, breaking subsequent reruns.
  if (gateFailed) {
    process.exit(1);
  }
}

void main();
