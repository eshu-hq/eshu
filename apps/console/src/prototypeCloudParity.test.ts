import { readFileSync } from "node:fs";
import { resolve } from "node:path";

import { describe, expect, it } from "vitest";

function repoRoot(): string {
  return process.cwd().endsWith("apps/console") ? resolve(process.cwd(), "../..") : process.cwd();
}

function repoFile(path: string): string {
  return readFileSync(resolve(repoRoot(), path), "utf8");
}

describe("prototype cloud parity", () => {
  it("keeps the standalone live cloud overlay on canonical inventory readback", () => {
    const overlay = repoFile("apps/console/prototype/eshu-console/console/pages-cloud-parity.jsx");

    expect(overlay).toContain("/api/v0/cloud/inventory");
    expect(overlay).toContain("Canonical inventory");
    expect(overlay).toContain("declared");
    expect(overlay).toContain("applied");
    expect(overlay).toContain("observed");
    expect(overlay).toContain("Canonical inventory unavailable");
  });
});
