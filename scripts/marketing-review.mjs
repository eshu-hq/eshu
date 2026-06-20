#!/usr/bin/env node
// Marketing-site browser review gate (issue #3330).
//
// This is a LOCAL, repeatable browser-level review of the ROOT Vite marketing
// site (the site that builds to `site-dist` and is published by Cloudflare
// Pages). It is intentionally SEPARATE from any console private-data proof.
//
// What it does:
//   1. Builds the root site (`npm run build` -> `site-dist`) unless --no-build.
//   2. Serves the static build with `vite preview` (the same static path
//      Cloudflare Pages serves — no Workers-only behavior is exercised).
//   3. Loads the site in Chromium at a desktop AND a mobile viewport.
//   4. Verifies primary routes (anchor sections), nav links, CTAs, external
//      links, and the Ask Eshu / first-run positioning do not regress.
//   5. Runs basic accessibility (alt text, single <h1>) and performance
//      (DOMContentLoaded budget) checks.
//   6. Captures desktop + mobile screenshots into a gitignored artifacts dir.
//   7. Evaluates everything through the unit-tested pure evaluator in
//      `src/marketingReview.ts` and exits non-zero on any failed check.
//
// Usage:
//   npm run site:review                 # build, serve, review
//   npm run site:review -- --no-build   # reuse an existing site-dist
//   npm run site:review -- --keep-build # do not delete site-dist afterwards
//
// Browsers: if Chromium is missing, run `npx playwright install chromium`.

import { spawn } from "node:child_process";
import { mkdir, rm, writeFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright";
import { evaluateMarketingReview } from "../src/marketingReview.ts";

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = join(here, "..");
const artifactsDir = join(repoRoot, "site-review-artifacts");
const previewPort = 4317;
const previewHost = "127.0.0.1";
const baseUrl = `http://${previewHost}:${previewPort}/`;

const args = new Set(process.argv.slice(2));
const skipBuild = args.has("--no-build");
const keepBuild = args.has("--keep-build");

// Review contract. The anchor ids, CTA hrefs, and positioning phrases mirror
// the shipped `src/siteContent.ts`; the matching vitest suites
// (`src/siteContent.test.ts`) keep that source of truth honest, and this gate
// proves the rendered page actually exposes them.
const contract = {
  anchors: [
    { id: "product", label: "Product" },
    { id: "whats-new", label: "What's new" },
    { id: "how-it-works", label: "How it works" },
    { id: "personas", label: "Personas" },
    { id: "try-it", label: "Try it" },
    { id: "use-cases", label: "Use cases" }
  ],
  ctas: [
    { label: "Try it locally", href: "#try-it" },
    {
      label: "Read the docs",
      href: "https://github.com/eshu-hq/eshu/tree/main/docs/public"
    }
  ],
  askEshuPositioning: ["Ask Eshu", "evidence packets", "One Graph"],
  maxDomContentLoadedMs: 4000
};

const viewports = [
  { viewport: "desktop", width: 1440, height: 900 },
  { viewport: "mobile", width: 390, height: 844 }
];

/** Spawn a process in the repo root, optionally detached. */
function spawnProcess(command, commandArgs, { detached = false } = {}) {
  return spawn(command, commandArgs, {
    cwd: repoRoot,
    stdio: detached ? ["ignore", "pipe", "pipe"] : "inherit",
    detached
  });
}

function awaitExit(child, label) {
  return new Promise((resolve, reject) => {
    child.on("exit", (code) => {
      if (code === 0) {
        resolve();
      } else {
        reject(new Error(`${label} exited with code ${code}`));
      }
    });
    child.on("error", reject);
  });
}

/** Poll the preview server until it answers or the deadline passes. */
async function waitForServer(url, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      const response = await fetch(url, { redirect: "manual" });
      if (response.ok || response.status === 304) {
        return;
      }
    } catch {
      // Server not up yet; retry after a short delay.
    }
    await new Promise((resolve) => setTimeout(resolve, 250));
  }
  throw new Error(`preview server did not become ready at ${url}`);
}

/** Collect the observed facts for one viewport. */
async function observeViewport(browser, spec) {
  const context = await browser.newContext({
    viewport: { width: spec.width, height: spec.height },
    deviceScaleFactor: spec.viewport === "mobile" ? 2 : 1,
    isMobile: spec.viewport === "mobile",
    hasTouch: spec.viewport === "mobile"
  });
  const page = await context.newPage();
  await page.goto(baseUrl, { waitUntil: "networkidle" });

  const resolvedAnchorIds = [];
  for (const anchor of contract.anchors) {
    const locator = page.locator(`#${anchor.id}`);
    if ((await locator.count()) > 0 && (await locator.first().isVisible())) {
      resolvedAnchorIds.push(anchor.id);
    }
  }

  const ctaLinks = await page.$$eval("a", (anchorEls) =>
    anchorEls.map((el) => ({
      text: (el.textContent ?? "").replace(/\s+/g, " ").trim(),
      href: el.getAttribute("href") ?? ""
    }))
  );

  // Use textContent (not innerText) for positioning checks: it reflects the
  // copy present in the DOM regardless of layout/visibility quirks in headless
  // Chromium, which is the right signal for "marketing copy did not regress".
  const bodyText = await page.evaluate(() => document.body.textContent ?? "");

  // A decorative image marked aria-hidden="true" or role="presentation" with
  // an empty alt is the correct accessibility pattern and is NOT a violation.
  // Only count images that expose neither real alt text nor a decorative
  // marker.
  const imagesMissingAlt = await page.$$eval("img", (imgs) =>
    imgs.filter((img) => {
      const alt = (img.getAttribute("alt") ?? "").trim();
      if (alt.length > 0) {
        return false;
      }
      const decorative =
        img.getAttribute("aria-hidden") === "true" ||
        img.getAttribute("role") === "presentation";
      return !decorative;
    }).length
  );

  const h1Count = await page.locator("h1").count();

  // A missing navigation timing entry is a broken measurement, not a fast
  // load. Return Infinity so it deterministically FAILS the perf budget rather
  // than silently passing as 0ms.
  const domContentLoadedMs = await page.evaluate(() => {
    const [nav] = performance.getEntriesByType("navigation");
    if (!nav) {
      return Number.POSITIVE_INFINITY;
    }
    return Math.round(nav.domContentLoadedEventEnd);
  });

  // External link reachability. Cap concurrency by doing a small set; the
  // marketing site uses a handful of stable github.com targets.
  const externalHrefs = [
    ...new Set(
      ctaLinks
        .map((link) => link.href)
        .filter((href) => href.startsWith("http://") || href.startsWith("https://"))
    )
  ];
  const reachableExternalLinks = [];
  const unreachableExternalLinks = [];
  for (const href of externalHrefs) {
    try {
      const response = await fetch(href, {
        method: "HEAD",
        redirect: "follow",
        // A hung connection must not block the gate forever; a timeout aborts
        // the fetch and lands deterministically in the unreachable branch.
        signal: AbortSignal.timeout(10_000)
      });
      if (response.ok || response.status === 405) {
        reachableExternalLinks.push(href);
      } else {
        unreachableExternalLinks.push(`${href} (${response.status})`);
      }
    } catch (error) {
      unreachableExternalLinks.push(`${href} (${error.message})`);
    }
  }

  await page.screenshot({
    path: join(artifactsDir, `marketing-${spec.viewport}.png`),
    fullPage: true
  });

  await context.close();

  return {
    viewport: spec.viewport,
    resolvedAnchorIds,
    ctaLinks,
    bodyText,
    imagesMissingAlt,
    h1Count,
    reachableExternalLinks,
    unreachableExternalLinks,
    domContentLoadedMs
  };
}

function printReport(result) {
  const byViewport = new Map();
  for (const finding of result.findings) {
    const list = byViewport.get(finding.viewport) ?? [];
    list.push(finding);
    byViewport.set(finding.viewport, list);
  }
  for (const [viewport, findings] of byViewport) {
    console.log(`\n[${viewport}]`);
    for (const finding of findings) {
      const mark = finding.severity === "pass" ? "PASS" : "FAIL";
      console.log(`  ${mark}  ${finding.check} — ${finding.detail}`);
    }
  }
}

async function main() {
  let preview;
  let browser;
  try {
    await rm(artifactsDir, { recursive: true, force: true });
    await mkdir(artifactsDir, { recursive: true });

    if (!skipBuild) {
      console.log("> building root marketing site (vite -> site-dist)");
      await awaitExit(spawnProcess("npm", ["run", "build"]), "npm run build");
    }

    console.log(`> serving site-dist via vite preview on ${baseUrl}`);
    // Invoke `vite preview` directly with a single --host. Going through the
    // `preview` npm script would layer this on top of its built-in
    // `--host 0.0.0.0`, passing vite two conflicting --host flags.
    preview = spawnProcess(
      "npx",
      ["vite", "preview", "--port", String(previewPort), "--host", previewHost],
      { detached: true }
    );
    preview.stdout?.on("data", () => {});
    preview.stderr?.on("data", (chunk) => process.stderr.write(chunk));
    await waitForServer(baseUrl, 30_000);

    console.log("> launching Chromium for desktop + mobile review");
    browser = await chromium.launch();

    const observations = [];
    for (const spec of viewports) {
      observations.push(await observeViewport(browser, spec));
    }

    const result = evaluateMarketingReview(contract, observations);
    printReport(result);

    const summary = {
      generatedAt: new Date().toISOString(),
      baseUrl,
      passed: result.passed,
      findings: result.findings
    };
    await writeFile(
      join(artifactsDir, "marketing-review.json"),
      `${JSON.stringify(summary, null, 2)}\n`,
      "utf8"
    );

    console.log(
      `\n> artifacts written to site-review-artifacts/ (screenshots + marketing-review.json)`
    );

    if (!result.passed) {
      console.error("\nMarketing review FAILED — see findings above.");
      process.exitCode = 1;
    } else {
      console.log("\nMarketing review PASSED for desktop and mobile.");
    }
  } finally {
    if (browser) {
      await browser.close();
    }
    if (preview && preview.pid) {
      // vite preview is detached; kill the whole process group.
      try {
        process.kill(-preview.pid, "SIGTERM");
      } catch {
        preview.kill("SIGTERM");
      }
    }
    if (!keepBuild && !skipBuild) {
      await rm(join(repoRoot, "site-dist"), { recursive: true, force: true });
    }
  }
}

main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
