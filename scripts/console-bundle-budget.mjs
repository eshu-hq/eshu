// console-bundle-budget.mjs — build-time bundle budget gate for the console.
//
// Run AFTER `npm run console:build`. It reads the emitted JS chunks under
// apps/console/dist/assets, classifies each one to a stable budget key, and
// fails (exit 1) if any chunk is larger than its documented threshold. This
// keeps future chunk growth visible in CI and locally instead of silently
// regressing first-load weight.
//
// Thresholds are RAW (un-gzipped) minified bytes — the same number Vite prints
// in its build summary — with deliberate headroom over the current measured
// sizes so normal churn does not trip the gate but a large regression does.
// Update BUDGETS (and docs/public/reference/console-bundle-budget.md) in the
// same PR whenever a threshold legitimately needs to move, and say why.
//
// Usage:
//   node scripts/console-bundle-budget.mjs            # check apps/console/dist
//   node scripts/console-bundle-budget.mjs <distDir>  # check a custom dist dir
import { readdirSync, readFileSync, statSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join, resolve } from "node:path";

const KIB = 1024;

// BUDGETS maps a stable budget key to its maximum allowed raw minified size in
// bytes. Keys are derived by classifyAsset() from hashed filenames so they stay
// stable across builds. Heavy visualization libraries (mermaid, cytoscape,
// wardley, katex) are intentionally large but are lazy-loaded route/feature
// chunks, NOT part of the initial download, so they get their own generous
// budgets rather than being forced into the main entry budget.
export const BUDGETS = {
  // Main entry chunk: app shell + router + the eagerly imported page set (~40
  // routed pages of business logic). This is intentionally the largest first-
  // load chunk: the console is a dense single-page operator UI and most pages
  // are reachable from the primary nav, so eager loading keeps navigation
  // instant. Heavy *dependencies* have been split out (d3 -> lazy workspace
  // route, react/icons -> vendor chunks, mermaid/cytoscape/wardley/katex ->
  // lazy diagram chunks). Measured ~722.1 KiB after #4746 (guided-questions)
  // and #4024 (semantic-search) each added a routed page whose eager nav entry
  // (icon, message, route, capability mapping) lands here even though both page
  // bodies are lazy-loaded. Measured ~724.8 KiB after #5143 (per-repo
  // freshness verdict): the freshness UI itself (adapter, chip, stage
  // checklist) is fully lazy-loaded off RepositoriesPage.tsx and
  // RepoSourcePage.tsx (mirrors OperationsLiveBoard), and its demo fixture is
  // dynamically imported from demoClient.ts rather than statically, so none
  // of that code lands here -- the ~0.8 KiB growth is only the two new
  // `lazy(() => import(...))` + `Suspense` call sites and the small demo
  // dispatch branch, which cannot shrink further without dropping a required
  // integration point. Budget keeps a small deliberate headroom over that.
  main: 726 * KIB,
  // React/runtime vendor (react, react-dom, react-router-dom). Stable, cached
  // across deploys via its own manualChunk. Measured ~49 KiB.
  "react-vendor": 120 * KIB,
  // d3 (force simulation + scales/shapes) used only by the lazy workspace and
  // relationship views. Split into its own vendor chunk, loaded on demand with
  // the workspace route. Measured ~110 KiB.
  d3: 200 * KIB,
  // lucide-react icon set, split into its own chunk for cache stability.
  // Measured ~20 KiB.
  icons: 80 * KIB,
  // Lazy diagram/graph feature libraries. Large by nature; never on first load.
  mermaid: 900 * KIB,
  cytoscape: 700 * KIB,
  wardley: 900 * KIB,
  katex: 400 * KIB
};

// DEFAULT_ASYNC_BUDGET_BYTES bounds any other lazily-loaded chunk (e.g. a route
// chunk) that does not have an explicit budget above.
export const DEFAULT_ASYNC_BUDGET_BYTES = 700 * KIB;

// classifyAsset maps a built asset filename to its stable budget key, or null
// when the asset is not a JavaScript chunk we budget (CSS, HTML, maps). The
// match is on the leading name segment before the Rollup content hash so it is
// independent of the per-build hash suffix.
export function classifyAsset(name) {
  if (!name.endsWith(".js")) return null;
  if (/^index-[^.]+\.js$/.test(name)) return "main";
  if (name.startsWith("react-vendor")) return "react-vendor";
  if (name.startsWith("d3-vendor") || /^d3-[^.]/.test(name)) return "d3";
  if (name.startsWith("icons-")) return "icons";
  if (name.startsWith("mermaid")) return "mermaid";
  if (name.startsWith("cytoscape")) return "cytoscape";
  if (name.startsWith("wardley")) return "wardley";
  if (name.startsWith("katex")) return "katex";
  return "async-chunk";
}

// evaluateBundleBudget is the pure decision core. Given a list of
// { name, bytes } files, a budget table keyed by classifyAsset() output, and a
// default budget for unlisted async chunks, it returns the checked chunks and
// any budget violations. CSS/HTML assets are ignored.
//
// requireAnchor names a budget key (e.g. "main") that MUST be present among the
// classified chunks. A broken or restructured build that emits no JS chunks, or
// CSS-only assets, or no entry chunk at all, would otherwise classify zero
// chunks and report "0 chunks within budget" — silently green-lighting a build
// that shipped nothing. Per Eshu's no-silent-fallback rule, a missing anchor is
// a hard failure (reported via missingAnchor), not a pass.
export function evaluateBundleBudget({
  files,
  budgets,
  defaultBudgetBytes,
  requireAnchor = null
}) {
  const checked = [];
  const violations = [];
  for (const file of files) {
    const key = classifyAsset(file.name);
    if (key === null) continue;
    const budgetBytes = budgets[key] ?? defaultBudgetBytes;
    const entry = { key, name: file.name, bytes: file.bytes, budgetBytes };
    checked.push(entry);
    if (file.bytes > budgetBytes) violations.push(entry);
  }
  const missingAnchor =
    requireAnchor !== null && !checked.some((entry) => entry.key === requireAnchor)
      ? requireAnchor
      : null;
  return {
    ok: violations.length === 0 && missingAnchor === null,
    checked,
    violations,
    missingAnchor
  };
}

function readAssetFiles(assetsDir) {
  return readdirSync(assetsDir)
    .map((name) => ({ name, path: join(assetsDir, name) }))
    .filter((file) => statSync(file.path).isFile())
    .map((file) => ({ name: file.name, bytes: readFileSync(file.path).byteLength }));
}

function formatKib(bytes) {
  return `${(bytes / KIB).toFixed(1)} KiB`;
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
      `console-bundle-budget: cannot read ${assetsDir}. Run \`npm run console:build\` first.`
    );
    console.error(String(error?.message ?? error));
    process.exit(2);
    return;
  }

  const { ok, checked, violations, missingAnchor } = evaluateBundleBudget({
    files,
    budgets: BUDGETS,
    defaultBudgetBytes: DEFAULT_ASYNC_BUDGET_BYTES,
    requireAnchor: "main"
  });

  const sorted = [...checked].sort((a, b) => b.bytes - a.bytes);
  console.log("console bundle budget (raw minified bytes):");
  for (const chunk of sorted) {
    const status = chunk.bytes > chunk.budgetBytes ? "FAIL" : "ok";
    console.log(
      `  [${status}] ${chunk.name} — ${formatKib(chunk.bytes)} / ${formatKib(chunk.budgetBytes)} (${chunk.key})`
    );
  }

  // A build that emits no main entry chunk produced nothing usable. Fail loudly
  // (exit 2, "broken build") instead of reporting "0 chunks within budget".
  if (missingAnchor !== null) {
    console.error(
      `\nconsole-bundle-budget: no '${missingAnchor}' entry chunk found in ${assetsDir}.`
    );
    console.error(
      `Found ${checked.length} JavaScript chunk(s) but none classified as '${missingAnchor}'.`
    );
    console.error(
      "The console build is broken or the Rollup output layout changed. Re-run"
    );
    console.error(
      "`npm run console:build`; if chunk naming changed, update classifyAsset() in"
    );
    console.error("scripts/console-bundle-budget.mjs.");
    process.exit(2);
    return;
  }

  if (!ok) {
    console.error("\nconsole-bundle-budget: budget exceeded:");
    for (const v of violations) {
      console.error(
        `  ${v.name} (${v.key}) is ${formatKib(v.bytes)}, budget ${formatKib(v.budgetBytes)}`
      );
    }
    console.error(
      "\nReduce the chunk (lazy-load/split) or, if the growth is justified, raise the"
    );
    console.error(
      "threshold in scripts/console-bundle-budget.mjs and document why in"
    );
    console.error("docs/public/reference/console-bundle-budget.md.");
    process.exit(1);
    return;
  }

  console.log(`\nconsole-bundle-budget: ${checked.length} chunks within budget.`);
}

// Only run the CLI when executed directly, so tests can import the pure helpers
// without triggering a build-artifact read.
if (process.argv[1] && fileURLToPath(import.meta.url) === resolve(process.argv[1])) {
  main(process.argv);
}
