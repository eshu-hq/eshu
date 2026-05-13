import { readFileSync } from "node:fs";
import { resolve } from "node:path";

describe("console interface design guardrails", () => {
  it("keeps internal console pages out of generic dashboard patterns", () => {
    const sourceRoot = resolve(__dirname);
    const files = [
      "styles.css",
      "dashboard.css",
      "tables.css",
      "pages/HomePage.tsx",
      "pages/WorkspacePage.tsx",
      "pages/DashboardPage.tsx",
      "pages/CatalogPage.tsx",
      "grid/EvidenceGrid.tsx",
      "visualization/DeploymentGraphView.tsx"
    ];
    const source = files
      .map((file) => readFileSync(resolve(sourceRoot, file), "utf8"))
      .join("\n");

    expect(source).not.toMatch(/eyebrow|workspace-hero|metric-card|metric-grid/);
    expect(source).not.toMatch(/catalog-graph|runtime-bar|linear-gradient/);
    expect(source).not.toMatch(/border-radius:\s*999px|text-transform:\s*uppercase/);
    expect(source).not.toMatch(/Segoe UI|Inter|Roboto|Trebuchet MS|Arial/);
  });
});
