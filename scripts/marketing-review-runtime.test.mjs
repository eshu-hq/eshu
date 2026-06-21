// @vitest-environment node

import { readFile } from "node:fs/promises";
import { describe, expect, it } from "vitest";
import {
  evaluatorFromModule,
  previewBaseUrlFromAddress
} from "./marketing-review-runtime.mjs";

describe("marketing review runtime", () => {
  it("does not rely on Node native TypeScript imports for the evaluator", async () => {
    const script = await readFile(new URL("./marketing-review.mjs", import.meta.url), "utf8");

    expect(script).not.toMatch(/from\s+["']\.\.\/src\/marketingReview\.ts["']/);
  });

  it("derives the browser base URL from the bound preview address", () => {
    const baseUrl = previewBaseUrlFromAddress(
      { address: "127.0.0.1", family: "IPv4", port: 51234 },
      "127.0.0.1"
    );

    expect(baseUrl).toBe("http://127.0.0.1:51234/");
    expect(baseUrl).not.toContain(":4317/");
  });

  it("rejects missing preview server addresses instead of reviewing a fallback port", () => {
    expect(() => previewBaseUrlFromAddress(null, "127.0.0.1")).toThrow(
      /preview server address/
    );
    expect(() => previewBaseUrlFromAddress("pipe", "127.0.0.1")).toThrow(
      /preview server address/
    );
    expect(() =>
      previewBaseUrlFromAddress(
        { address: "127.0.0.1", family: "IPv4", port: 0 },
        "127.0.0.1"
      )
    ).toThrow(/preview server address/);
  });

  it("requires the transformed evaluator module to expose the expected function", () => {
    const evaluateMarketingReview = () => ({ passed: true, findings: [] });

    expect(evaluatorFromModule({ evaluateMarketingReview })).toBe(
      evaluateMarketingReview
    );
    expect(() => evaluatorFromModule({})).toThrow(/evaluateMarketingReview/);
  });
});
