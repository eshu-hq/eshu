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
  });
});
