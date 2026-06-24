// @vitest-environment node
//
// This suite reads project files off disk with node:fs / node:path. The global
// vitest environment is jsdom (see vite.config.ts), which is the wrong runtime
// for filesystem assertions and was observed to leave node:path helpers such as
// `join` unresolved on drifted installs (#3104). Pin this file to the node
// environment so it always runs against real Node globals.
import { readFileSync } from "node:fs";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

describe("Cloudflare Pages configuration", () => {
  it("pins the Pages build Node version to the Vite 7 runtime floor", () => {
    const nodeVersion = readFileSync(
      join(process.cwd(), ".nvmrc"),
      "utf8"
    ).trim();
    const pagesRunbook = readFileSync(
      join(process.cwd(), "CLOUDFLARE_PAGES.md"),
      "utf8"
    );

    expect(nodeVersion).toBe("22.12.0");
    expect(pagesRunbook).toContain(
      "Node version | `22.12.0` (pinned via `.nvmrc`)"
    );
    expect(pagesRunbook).toContain("If a project sets `NODE_VERSION`");
  });

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
