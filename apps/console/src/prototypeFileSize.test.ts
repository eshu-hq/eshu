import { existsSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

const MAX_REVIEWABLE_LINES = 500;
const cwdPrototypeDir = join(process.cwd(), "prototype", "eshu-console", "console");
const rootPrototypeDir = join(process.cwd(), "apps", "console", "prototype", "eshu-console", "console");
const PROTOTYPE_CONSOLE_DIR = existsSync(cwdPrototypeDir) ? cwdPrototypeDir : rootPrototypeDir;
const REVIEWED_PROTOTYPE_FILES = [
  "data.js",
  "live-base-loader.js",
  "pages-admin.jsx",
  "pages-data.jsx",
  "pages-repositories.jsx",
  "pages-repository-model.jsx"
] as const;

describe("prototype console file size", () => {
  it("keeps source files under the reviewable line limit", () => {
    const oversized = REVIEWED_PROTOTYPE_FILES
      .map((name) => join(PROTOTYPE_CONSOLE_DIR, name))
      .map((path) => ({ path, lines: lineCount(path) }))
      .filter((file) => file.lines > MAX_REVIEWABLE_LINES);

    expect(oversized).toEqual([]);
  });
});

function lineCount(path: string): number {
  if (!statSync(path).isFile()) return 0;
  return readFileSync(path, "utf8").split("\n").length;
}
