import { readFileSync } from "node:fs";
import { resolve } from "node:path";

import { describe, expect, it } from "vitest";

function repoRoot(): string {
  return process.cwd().endsWith("apps/console") ? resolve(process.cwd(), "../..") : process.cwd();
}

function repoFile(path: string): string {
  return readFileSync(resolve(repoRoot(), path), "utf8");
}

describe("prototype code page parity", () => {
  it("groups dead-code rows by canonical repository key instead of display label", () => {
    const page = repoFile("apps/console/prototype/eshu-console/console/pages-code.jsx");

    expect(page).toContain("const key = deadCodeRepoKey(d);");
    expect(page).toContain("groupedLabels[key] = deadCodeRepoLabel(d);");
    expect(page).not.toContain("grouped[d.repo]");
  });
});
