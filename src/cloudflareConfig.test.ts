import { readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

describe("Cloudflare Pages configuration", () => {
  it("declares the static build output directory for Pages", () => {
    const wranglerConfig = readFileSync(
      join(process.cwd(), "wrangler.jsonc"),
      "utf8"
    );

    expect(wranglerConfig).toContain('"pages_build_output_dir": "./site-dist"');
    expect(wranglerConfig).not.toMatch(/^\s*"main"\s*:/m);
  });

  it("documents that Workers Builds are not part of the release gate", () => {
    const pagesRunbook = readFileSync(
      join(process.cwd(), "CLOUDFLARE_PAGES.md"),
      "utf8"
    );

    expect(pagesRunbook).toContain("Workers Builds are not a release gate");
  });
});
