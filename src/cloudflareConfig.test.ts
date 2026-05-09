import { readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

describe("Cloudflare configuration", () => {
  it("declares the static build output directory for Pages and Workers", () => {
    const wranglerConfig = readFileSync(
      join(process.cwd(), "wrangler.jsonc"),
      "utf8"
    );

    expect(wranglerConfig).toContain('"pages_build_output_dir": "./site-dist"');
    expect(wranglerConfig).toContain('"directory": "./site-dist"');
  });
});
