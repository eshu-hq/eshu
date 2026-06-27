import { describe, expect, it } from "vitest";

import {
  evaluateMarketingReview,
  type MarketingReviewContract,
  type ViewportObservation,
} from "./marketingReview";

const contract: MarketingReviewContract = {
  anchors: [
    { id: "product", label: "Product" },
    { id: "try-it", label: "Try it" },
  ],
  ctas: [
    { label: "Try it locally", href: "#try-it" },
    { label: "Read the docs", href: "https://example.test/docs" },
  ],
  askEshuPositioning: ["Ask Eshu", "evidence packets"],
  maxDomContentLoadedMs: 4000,
};

function passingObservation(viewport: "desktop" | "mobile"): ViewportObservation {
  return {
    viewport,
    resolvedAnchorIds: ["product", "try-it"],
    ctaLinks: [
      { text: "Try it locally", href: "#try-it" },
      { text: "Read the docs", href: "https://example.test/docs" },
    ],
    bodyText: "Ask Eshu returns evidence packets with provenance.",
    imagesMissingAlt: 0,
    h1Count: 1,
    reachableExternalLinks: ["https://example.test/docs"],
    unreachableExternalLinks: [],
    domContentLoadedMs: 1200,
  };
}

describe("evaluateMarketingReview", () => {
  it("passes when every viewport meets the full contract", () => {
    const result = evaluateMarketingReview(contract, [
      passingObservation("desktop"),
      passingObservation("mobile"),
    ]);

    expect(result.passed).toBe(true);
    expect(result.findings.every((finding) => finding.severity === "pass")).toBe(true);
    // Two viewports, each emitting the same number of checks. The fixed
    // checks per viewport are: img-alt, single-h1, external-reachable, and
    // dom-content-loaded (4 total).
    expect(result.findings).toHaveLength(
      2 * (contract.anchors.length + contract.ctas.length + contract.askEshuPositioning.length + 4),
    );
  });

  it("fails with no observations (nothing was actually reviewed)", () => {
    const result = evaluateMarketingReview(contract, []);
    expect(result.passed).toBe(false);
    expect(result.findings).toHaveLength(0);
  });

  it("flags a missing anchor section", () => {
    const broken: ViewportObservation = {
      ...passingObservation("mobile"),
      resolvedAnchorIds: ["product"],
    };
    const result = evaluateMarketingReview(contract, [broken]);
    expect(result.passed).toBe(false);
    const finding = result.findings.find((item) => item.check === "anchor:#try-it");
    expect(finding?.severity).toBe("fail");
  });

  it("flags a CTA whose href regressed", () => {
    const broken: ViewportObservation = {
      ...passingObservation("desktop"),
      ctaLinks: [
        { text: "Try it locally", href: "#wrong" },
        { text: "Read the docs", href: "https://example.test/docs" },
      ],
    };
    const result = evaluateMarketingReview(contract, [broken]);
    expect(result.passed).toBe(false);
    expect(result.findings.find((item) => item.check === "cta:Try it locally")?.severity).toBe(
      "fail",
    );
  });

  it("flags missing Ask Eshu positioning copy", () => {
    const broken: ViewportObservation = {
      ...passingObservation("desktop"),
      bodyText: "A generic landing page with no positioning.",
    };
    const result = evaluateMarketingReview(contract, [broken]);
    expect(result.passed).toBe(false);
    expect(result.findings.filter((item) => item.severity === "fail").length).toBe(
      contract.askEshuPositioning.length,
    );
  });

  it("flags accessibility regressions: missing alt text and duplicate h1", () => {
    const broken: ViewportObservation = {
      ...passingObservation("mobile"),
      imagesMissingAlt: 2,
      h1Count: 3,
    };
    const result = evaluateMarketingReview(contract, [broken]);
    expect(result.passed).toBe(false);
    expect(result.findings.find((item) => item.check === "a11y:img-alt")?.severity).toBe("fail");
    expect(result.findings.find((item) => item.check === "a11y:single-h1")?.severity).toBe("fail");
  });

  it("flags an unreachable external link", () => {
    const broken: ViewportObservation = {
      ...passingObservation("desktop"),
      reachableExternalLinks: [],
      unreachableExternalLinks: ["https://example.test/docs"],
    };
    const result = evaluateMarketingReview(contract, [broken]);
    expect(result.passed).toBe(false);
    expect(
      result.findings.find((item) => item.check === "links:external-reachable")?.severity,
    ).toBe("fail");
  });

  it("flags a slow load that exceeds the performance budget", () => {
    const broken: ViewportObservation = {
      ...passingObservation("mobile"),
      domContentLoadedMs: contract.maxDomContentLoadedMs + 1,
    };
    const result = evaluateMarketingReview(contract, [broken]);
    expect(result.passed).toBe(false);
    expect(result.findings.find((item) => item.check === "perf:dom-content-loaded")?.severity).toBe(
      "fail",
    );
  });
});
