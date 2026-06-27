// Tests for the console bundle composition report evaluator.
//
// The report shares chunk classification with the bundle budget gate, then turns
// emitted Vite assets into a stable markdown table for CI logs and PR review.
import { describe, expect, it } from "vitest";

import {
  evaluateBundleReport,
  formatBundleReportMarkdown,
} from "../../../scripts/console-bundle-report.mjs";

describe("evaluateBundleReport", () => {
  it("renders built assets as a dependency-attributed markdown table", () => {
    const report = evaluateBundleReport([
      { name: "WorkspacePage-Bb22.js", bytes: 40 * 1024 },
      { name: "react-vendor-Aa11.js", bytes: 52 * 1024 },
      { name: "index-Cc33.js", bytes: 640 * 1024 },
      { name: "d3-Dd44.js", bytes: 111 * 1024 },
      { name: "icons-Ee55.js", bytes: 20 * 1024 },
      { name: "mermaid.core-Ff66.js", bytes: 300 * 1024 },
      { name: "index-Gg77.css", bytes: 18 * 1024 },
    ]);

    expect(report.ok).toBe(true);
    expect(formatBundleReportMarkdown(report)).toBe(
      [
        "| chunk | dependency | KB | first-load? |",
        "| --- | --- | ---: | :---: |",
        "| index-Cc33.js | app/main | 640.0 | yes |",
        "| react-vendor-Aa11.js | react/react-dom/react-router-dom | 52.0 | yes |",
        "| icons-Ee55.js | lucide-react | 20.0 | yes |",
        "| mermaid.core-Ff66.js | mermaid | 300.0 | no |",
        "| d3-Dd44.js | d3 | 111.0 | no |",
        "| WorkspacePage-Bb22.js | app/async | 40.0 | no |",
      ].join("\n"),
    );
  });

  it("fails when no usable main JavaScript chunk is present", () => {
    const report = evaluateBundleReport([
      { name: "react-vendor-Aa11.js", bytes: 52 * 1024 },
      { name: "index-Bb22.css", bytes: 18 * 1024 },
    ]);

    expect(report.ok).toBe(false);
    expect(report.missingAnchor).toBe("main");
    expect(report.rows.map((row) => row.chunk)).toEqual(["react-vendor-Aa11.js"]);
  });
});
