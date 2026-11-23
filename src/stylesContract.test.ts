// @vitest-environment node
import { readFileSync } from "node:fs";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

describe("global styles contract", () => {
  const styles = readFileSync(join(process.cwd(), "src/styles.css"), "utf8");
  const heroGraph = readFileSync(join(process.cwd(), "src/heroGraph.css"), "utf8");
  const workbench = readFileSync(join(process.cwd(), "src/workbenchFooter.css"), "utf8");

  it("disables smooth scrolling for reduced-motion users", () => {
    expect(styles).toContain("@media (prefers-reduced-motion: reduce)");
    expect(styles).toContain("scroll-behavior: auto");
  });

  it("defines both dark obsidian and warm light theme tokens with full-bleed bands", () => {
    expect(styles).toContain(':root[data-theme="dark"]');
    expect(styles).toContain(':root[data-theme="light"]');
    expect(styles).toContain(".band--elevated");
  });

  it("styles the 2D SVG hero knowledge graph and balanced 12-column Bento surfaces grid", () => {
    expect(heroGraph).toContain(".graph-svg");
    expect(heroGraph).toContain(".svg-node.is-active");
    expect(heroGraph).toContain(".edge-line.is-active");
    expect(workbench).toContain(".surface-bento");
    expect(workbench).toContain(".surface-card--mcp");
    expect(workbench).toContain(".surface-card--console");
  });
});
