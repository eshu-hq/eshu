import type { Page } from "playwright";
import type { PageTest } from "../types.ts";

export const pageTest: PageTest = {
  path: "/semantic-search?repo=acme%2Fcheckout-service&q=retry+logic",
  label: "Semantic Search",
  area: "ask",
  async assert(page: Page): Promise<void> {
    await page.waitForSelector(".semantic-search-page", { timeout: 10000 });
    const len = await page.evaluate(
      () => document.querySelector(".semantic-search-page")?.textContent?.trim().length ?? 0,
    );
    if (len < 40) throw new Error(`page rendered only ${len} chars`);
  },
};
