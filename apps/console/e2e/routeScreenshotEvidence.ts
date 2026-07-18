import { resolve } from "node:path";
import type { Page } from "playwright";

import type { ConsoleRoute } from "../src/e2e/routeAssertions";

// captureLandingScreenshotIfRequired preserves the route-level truth state
// before a workflow follows a retained detail link and changes the page.
export async function captureLandingScreenshotIfRequired(
  page: Page,
  route: ConsoleRoute,
  screenshotsDir: string,
  safeName: string,
): Promise<void> {
  const workflow = route.workflow;
  if (workflow?.kind !== "tabs" || workflow.proveVulnerabilityLandingTruth !== true) {
    return;
  }

  await page.screenshot({
    path: resolve(screenshotsDir, `${safeName}_landing.png`),
    fullPage: true,
  });
}
