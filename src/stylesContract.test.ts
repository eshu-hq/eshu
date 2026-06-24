import { readFileSync } from "node:fs";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

import { siteContent } from "./siteContent";

describe("global styles contract", () => {
  const styles = readFileSync(join(process.cwd(), "src/styles.css"), "utf8");

  it("disables smooth scrolling for reduced-motion users", () => {
    expect(styles).toContain("@media (prefers-reduced-motion: reduce)");
    expect(styles).toContain("scroll-behavior: auto");
  });

  it("does not accidentally group hero h1 styles with global h2 styles", () => {
    expect(styles).not.toContain(".hero-copy h1,\nh2");
  });
});

describe("source-to-runtime graph node positioning", () => {
  const heroGraph = readFileSync(join(process.cwd(), "src/heroGraph.css"), "utf8");
  const responsive = readFileSync(join(process.cwd(), "src/responsive.css"), "utf8");
  const nodeIds = siteContent.demoTrace.nodes.map((node) => node.id);
  const declaredNodeClasses = (css: string): readonly string[] =>
    [...css.matchAll(/\.node-([a-z-]+)\s*\{/g)].map((match) => match[1]);

  it("absolutely positions every demo-trace node id rendered by App", () => {
    // App renders each node as `node-${node.id}`; without a matching positioned
    // rule the node collapses to the canvas origin and the nodes pile up.
    for (const id of nodeIds) {
      const positionedRule = new RegExp(`\\.node-${id}\\s*\\{[^}]*\\bleft\\s*:`);
      expect(heroGraph, `heroGraph.css must position .node-${id}`).toMatch(
        positionedRule
      );
    }
  });

  it("does not define base node classes for ids absent from the demo data", () => {
    for (const cls of declaredNodeClasses(heroGraph)) {
      expect(
        nodeIds,
        `heroGraph.css .node-${cls} has no matching demo-trace node`
      ).toContain(cls);
    }
  });

  it("keeps responsive node overrides aligned to real node ids", () => {
    for (const cls of declaredNodeClasses(responsive)) {
      expect(
        nodeIds,
        `responsive.css .node-${cls} has no matching demo-trace node`
      ).toContain(cls);
    }
  });
});
