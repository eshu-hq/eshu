import type { Page } from "playwright";
import type { PageTest } from "../types.ts";

export const pageTest: PageTest = {
  path: "/admin",
  label: "Admin - API Tokens",
  area: "operations",
  async assert(page: Page): Promise<void> {
    await page.waitForSelector(".panel-grid", { timeout: 10000 });
    const len = await page.evaluate(
      () => document.querySelector(".panel-grid")?.textContent?.trim().length ?? 0,
    );
    if (len < 40) throw new Error(`admin panel rendered only ${len} chars`);
  },
};
