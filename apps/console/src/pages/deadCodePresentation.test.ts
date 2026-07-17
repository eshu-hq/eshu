import { describe, expect, it } from "vitest";

import { deadCodeScanLabel } from "./deadCodePresentation";
import type { DeadCodePage } from "../api/deadCode";

function page(
  overrides: Partial<
    Pick<DeadCodePage, "candidateScanTruncated" | "displayTruncated" | "truncated">
  >,
): DeadCodePage {
  return {
    analysis: {},
    candidateScanTruncated: false,
    displayTruncated: false,
    limit: 100,
    rows: [],
    truncated: false,
    truth: { freshness: "fresh", level: "derived", profile: "production" },
    ...overrides,
  };
}

describe("dead-code presentation", () => {
  it("keeps display truncation distinct from candidate-scan truncation", () => {
    const displayOnly = deadCodeScanLabel(page({ displayTruncated: true, truncated: true }), true);
    const scanOnly = deadCodeScanLabel(
      page({ candidateScanTruncated: true, truncated: true }),
      true,
    );

    expect(displayOnly).toContain("display limited to 100 candidates");
    expect(displayOnly).not.toContain("candidate scan window incomplete");
    expect(scanOnly).toContain("candidate scan window incomplete");
    expect(scanOnly).not.toContain("display limited");
  });

  it("labels an untruncated response as complete only for its current scope", () => {
    expect(deadCodeScanLabel(page({}), true)).toContain("complete for current scope");
  });
});
