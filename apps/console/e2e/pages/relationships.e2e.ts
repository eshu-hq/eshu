import type { Page } from "playwright";
import type { PageTest } from "../types.ts";

export const pageTest: PageTest = {
  path: "/relationships",
  label: "Relationships",
  area: "graph",
  async assert(page: Page): Promise<void> {
    await page.waitForSelector(".page-shell", { timeout: 10000 });
    const len = await page.evaluate(
      () => document.querySelector(".page-shell")?.textContent?.trim().length ?? 0,
    );
    if (len < 40) throw new Error(`page rendered only ${len} chars`);
    // Selecting a verb issues POST /relationships/edges — the path that hit the
    // mock handler's `postDataJSON().catch()` crash (postDataJSON is synchronous
    // in Playwright, so `.catch` threw a TypeError). Exercise it so a regression
    // fails here instead of staying latent: the first verb tile must load its
    // concrete edge slice.
    await page.click(".rel-verb-row");
    await page.waitForSelector(".rel-edge-row", { timeout: 10000 });
  },
};
