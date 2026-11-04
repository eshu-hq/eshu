import { readFileSync } from "node:fs";
import { resolve } from "node:path";

import { describe, expect, it } from "vitest";

// The console sends profile=comprehensive for possibly_affected reads and
// expects detection_profile echoed on both impact routes. Pin that the Go
// OpenAPI still declares those names so a rename fails here, not silently in
// the browser.
function repoFile(path: string): string {
  const root = process.cwd().endsWith("apps/console")
    ? resolve(process.cwd(), "../..")
    : process.cwd();
  return readFileSync(resolve(root, path), "utf8");
}

describe("impact profile OpenAPI contract", () => {
  it("declares profile, possibly_affected, and the detection_profile echo on the findings route", () => {
    const findings = repoFile("go/internal/query/openapi/paths/supply/chain/impact_findings.go");
    expect(findings).toContain('{"name": "profile", "in": "query"');
    expect(findings).toContain('"enum": ["precise", "comprehensive"]');
    expect(findings).toContain("possibly_affected");
    expect(findings).toContain('"detection_profile"');
  });
});
