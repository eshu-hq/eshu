// Tests for the console bundle budget evaluator.
//
// The evaluator is the pure core of scripts/console-bundle-budget.mjs. It takes
// a list of built asset files with sizes plus a budget table and decides which
// chunks exceeded their documented threshold. Keeping the decision logic pure
// lets us assert behavior without running a real Vite build.
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

import { describe, expect, it } from "vitest";

import {
  classifyAsset,
  evaluateBundleBudget
} from "../../../scripts/console-bundle-budget.mjs";

type PackageJson = {
  scripts?: Record<string, string>;
};

function readRootPackageJson(): PackageJson {
  const packageJsonPath = resolve(process.cwd(), "package.json");
  const parsed: unknown = JSON.parse(readFileSync(packageJsonPath, "utf8"));
  if (typeof parsed !== "object" || parsed === null) {
    throw new Error("root package.json must contain an object");
  }
  return parsed;
}

describe("classifyAsset", () => {
  it("maps the hashed main entry chunk to the 'main' budget key", () => {
    expect(classifyAsset("index-ASmi5FIg.js")).toBe("main");
  });

  it("maps known lazy vendor chunks to their stable budget keys", () => {
    expect(classifyAsset("mermaid.core-CxHanD_0.js")).toBe("mermaid");
    expect(classifyAsset("cytoscape.esm-CUqq0XTU.js")).toBe("cytoscape");
    expect(classifyAsset("wardley-L42UT6IY-Dk4Hwjct.js")).toBe("wardley");
    expect(classifyAsset("katex-C5jXJg4s.js")).toBe("katex");
  });

  it("treats any other javascript chunk as a generic async chunk", () => {
    expect(classifyAsset("WorkspacePage-Ab12Cd34.js")).toBe("async-chunk");
    expect(classifyAsset("react-vendor-9Z8Y7X.js")).toBe("react-vendor");
  });

  it("ignores non-javascript assets", () => {
    expect(classifyAsset("index-CQPdM6O3.css")).toBeNull();
    expect(classifyAsset("index.html")).toBeNull();
  });
});

describe("console build script", () => {
  it("runs the bundle budget gate after the Vite console build", () => {
    const scripts = readRootPackageJson().scripts;

    expect(scripts?.["console:build"]).toBe(
      "vite build --config apps/console/vite.config.ts && npm run console:bundle-budget"
    );
  });
});

describe("evaluateBundleBudget", () => {
  const budgets = {
    main: 600_000,
    "react-vendor": 200_000,
    mermaid: 900_000,
    "async-chunk": 700_000
  };
  const defaultBudgetBytes = 700_000;

  it("passes when every classified chunk is within its budget", () => {
    const result = evaluateBundleBudget({
      files: [
        { name: "index-aaaa.js", bytes: 480_000 },
        { name: "react-vendor-bbbb.js", bytes: 150_000 },
        { name: "mermaid.core-cccc.js", bytes: 580_000 },
        { name: "index-dddd.css", bytes: 90_000 }
      ],
      budgets,
      defaultBudgetBytes
    });
    expect(result.ok).toBe(true);
    expect(result.violations).toEqual([]);
    expect(result.checked).toHaveLength(3);
  });

  it("flags a chunk that exceeds its specific budget", () => {
    const result = evaluateBundleBudget({
      files: [{ name: "index-aaaa.js", bytes: 900_000 }],
      budgets,
      defaultBudgetBytes
    });
    expect(result.ok).toBe(false);
    expect(result.violations).toHaveLength(1);
    expect(result.violations[0]).toMatchObject({
      key: "main",
      name: "index-aaaa.js",
      bytes: 900_000,
      budgetBytes: 600_000
    });
  });

  it("applies the default budget to unbudgeted async chunks", () => {
    const result = evaluateBundleBudget({
      files: [{ name: "SomeOtherChunk-eeee.js", bytes: 720_000 }],
      budgets: { main: 600_000 },
      defaultBudgetBytes
    });
    expect(result.ok).toBe(false);
    expect(result.violations[0]).toMatchObject({
      key: "async-chunk",
      budgetBytes: defaultBudgetBytes
    });
  });

  it("treats a chunk exactly at its budget as passing", () => {
    const result = evaluateBundleBudget({
      files: [{ name: "index-aaaa.js", bytes: 600_000 }],
      budgets,
      defaultBudgetBytes
    });
    expect(result.ok).toBe(true);
  });

  it("ignores css and html assets entirely", () => {
    const result = evaluateBundleBudget({
      files: [
        { name: "index-aaaa.css", bytes: 5_000_000 },
        { name: "index.html", bytes: 5_000_000 }
      ],
      budgets,
      defaultBudgetBytes
    });
    expect(result.ok).toBe(true);
    expect(result.checked).toEqual([]);
  });

  it("fails when a required anchor chunk is absent from an empty assets dir", () => {
    const result = evaluateBundleBudget({
      files: [],
      budgets,
      defaultBudgetBytes,
      requireAnchor: "main"
    });
    expect(result.ok).toBe(false);
    expect(result.missingAnchor).toBe("main");
  });

  it("fails on a css-only build that emits no main entry chunk", () => {
    const result = evaluateBundleBudget({
      files: [
        { name: "index-aaaa.css", bytes: 90_000 },
        { name: "index.html", bytes: 400 }
      ],
      budgets,
      defaultBudgetBytes,
      requireAnchor: "main"
    });
    // A build that ships only CSS/HTML must not green-light: the anchor is
    // missing even though there are zero budget violations.
    expect(result.violations).toEqual([]);
    expect(result.ok).toBe(false);
    expect(result.missingAnchor).toBe("main");
  });

  it("fails when only async chunks are emitted but the main anchor is missing", () => {
    const result = evaluateBundleBudget({
      files: [{ name: "SomeChunk-eeee.js", bytes: 10_000 }],
      budgets,
      defaultBudgetBytes,
      requireAnchor: "main"
    });
    expect(result.ok).toBe(false);
    expect(result.missingAnchor).toBe("main");
  });

  it("passes (no anchor requirement) when requireAnchor is omitted", () => {
    const result = evaluateBundleBudget({
      files: [{ name: "SomeChunk-eeee.js", bytes: 10_000 }],
      budgets,
      defaultBudgetBytes
    });
    expect(result.ok).toBe(true);
    expect(result.missingAnchor).toBeNull();
  });
});
