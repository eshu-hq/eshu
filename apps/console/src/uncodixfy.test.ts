import { readFileSync } from "node:fs";
import { resolve } from "node:path";

describe("console interface design guardrails", () => {
  // The Console v2 redesign is a bespoke, hand-built dark theme. It intentionally
  // uses gradients, pill radii, uppercase micro-labels, and the Inter type family,
  // so the original anti-styling rules no longer apply. What still matters is that
  // the console does not regress into the generic, templated "dashboard kit"
  // component patterns the rewrite removed.
  it("keeps internal console pages out of generic dashboard component patterns", () => {
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
    expect(source).not.toMatch(/catalog-graph|runtime-bar/);
  });
});
