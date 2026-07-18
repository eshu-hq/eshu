import type { Page } from "playwright";
import { describe, expect, it, vi } from "vitest";

import { captureLandingScreenshotIfRequired } from "./routeScreenshotEvidence";

describe("retained route screenshot evidence", () => {
  it("captures the vulnerability landing state before the detail workflow leaves it", async () => {
    const screenshot = vi.fn().mockResolvedValue(undefined);
    const page = { screenshot } as unknown as Page;

    await captureLandingScreenshotIfRequired(
      page,
      {
        path: "/vulnerabilities",
        label: "Vulnerabilities",
        area: "security",
        workflow: {
          id: "vulnerabilities-live-tabs",
          kind: "tabs",
          proveVulnerabilityLandingTruth: true,
          tabs: [],
        },
      },
      "/tmp/eshu-live-e2e-test-screenshots",
      "vulnerabilities",
    );

    expect(screenshot).toHaveBeenCalledOnce();
    expect(screenshot).toHaveBeenCalledWith({
      path: "/tmp/eshu-live-e2e-test-screenshots/vulnerabilities_landing.png",
      fullPage: true,
    });
  });

  it("does not add landing screenshots to unrelated routes", async () => {
    const screenshot = vi.fn().mockResolvedValue(undefined);
    const page = { screenshot } as unknown as Page;

    await captureLandingScreenshotIfRequired(
      page,
      {
        path: "/catalog",
        label: "Catalog",
        area: "service",
      },
      "/tmp/eshu-live-e2e-test-screenshots",
      "catalog",
    );

    expect(screenshot).not.toHaveBeenCalled();
  });
});
