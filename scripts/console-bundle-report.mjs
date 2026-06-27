// console-bundle-report.mjs — build-time bundle composition report for the console.
//
// Run AFTER `npm run console:build`. It reads the emitted JS chunks under
// apps/console/dist/assets, reuses the bundle-budget chunk classifier, and prints
// a stable markdown table for CI logs and PR review.
//
// Usage:
//   node scripts/console-bundle-report.mjs            # report apps/console/dist/assets
//   node scripts/console-bundle-report.mjs <distDir>  # report <distDir>/assets
import { readdirSync, readFileSync, statSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { classifyAsset } from "./console-bundle-budget.mjs";

const KIB = 1024;

const CHUNK_SEMANTICS = {
  main: { dependency: "app/main", firstLoad: true },
  "react-vendor": {
    dependency: "react/react-dom/react-router-dom",
    firstLoad: true
  },
  icons: { dependency: "lucide-react", firstLoad: true },
  d3: { dependency: "d3", firstLoad: false },
  mermaid: { dependency: "mermaid", firstLoad: false },
  cytoscape: { dependency: "cytoscape", firstLoad: false },
  wardley: { dependency: "wardley", firstLoad: false },
  katex: { dependency: "katex", firstLoad: false },
  "async-chunk": { dependency: "app/async", firstLoad: false }
};

function semanticsFor(key) {
  return CHUNK_SEMANTICS[key] ?? CHUNK_SEMANTICS["async-chunk"];
}

function readAssetFiles(assetsDir) {
  return readdirSync(assetsDir)
    .map((name) => ({ name, path: join(assetsDir, name) }))
    .filter((file) => statSync(file.path).isFile())
    .map((file) => ({ name: file.name, bytes: readFileSync(file.path).byteLength }));
}

function compareReportRows(a, b) {
  if (a.firstLoad !== b.firstLoad) return a.firstLoad ? -1 : 1;
  if (a.bytes !== b.bytes) return b.bytes - a.bytes;
  return a.chunk.localeCompare(b.chunk);
}

function formatKb(bytes) {
  return (bytes / KIB).toFixed(1);
}

export function evaluateBundleReport(files) {
  const rows = [];
  for (const file of files) {
    const key = classifyAsset(file.name);
    if (key === null) continue;
    const semantics = semanticsFor(key);
    rows.push({
      chunk: file.name,
      dependency: semantics.dependency,
      bytes: file.bytes,
      firstLoad: semantics.firstLoad,
      key
    });
  }
  rows.sort(compareReportRows);
  const missingAnchor = rows.some((row) => row.key === "main") ? null : "main";
  return {
    ok: missingAnchor === null,
    rows,
    missingAnchor
  };
}

export function formatBundleReportMarkdown(report) {
  const lines = [
    "| chunk | dependency | KB | first-load? |",
    "| --- | --- | ---: | :---: |"
  ];
  for (const row of report.rows) {
    lines.push(
      `| ${row.chunk} | ${row.dependency} | ${formatKb(row.bytes)} | ${row.firstLoad ? "yes" : "no"} |`
    );
  }
  return lines.join("\n");
}

function main(argv) {
  const here = dirname(fileURLToPath(import.meta.url));
  const repoRoot = resolve(here, "..");
  const distArg = argv[2];
  const distDir = distArg
    ? resolve(process.cwd(), distArg)
    : join(repoRoot, "apps", "console", "dist");
  const assetsDir = join(distDir, "assets");

  let files;
  try {
    files = readAssetFiles(assetsDir);
  } catch (error) {
    console.error(
      `console-bundle-report: cannot read ${assetsDir}. Run \`npm run console:build\` first.`
    );
    console.error(String(error?.message ?? error));
    process.exit(2);
    return;
  }

  const report = evaluateBundleReport(files);
  console.log(formatBundleReportMarkdown(report));

  if (!report.ok) {
    console.error(
      `\nconsole-bundle-report: no '${report.missingAnchor}' entry chunk found in ${assetsDir}.`
    );
    console.error(
      "The console build is broken or the Rollup output layout changed. Re-run"
    );
    console.error(
      "`npm run console:build`; if chunk naming changed, update classifyAsset() in"
    );
    console.error("scripts/console-bundle-budget.mjs.");
    process.exit(2);
  }
}

if (process.argv[1] && fileURLToPath(import.meta.url) === resolve(process.argv[1])) {
  main(process.argv);
}
