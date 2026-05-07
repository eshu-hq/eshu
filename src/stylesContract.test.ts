import { readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

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
