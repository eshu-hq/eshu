import type { Page } from "playwright";
import type { PageTest } from "../types.ts";

export const pageTest: PageTest = {
  path: "/repositories/repository:payments-api/source",
  label: "Repo Source (payments-api)",
  area: "repositories",
  async assert(page: Page): Promise<void> {
    await page.waitForSelector(".page-shell", { timeout: 10000 });
    const len = await page.evaluate(
      () => document.querySelector(".page-shell")?.textContent?.trim().length ?? 0,
    );
    if (len < 40) throw new Error(`page rendered only ${len} chars`);
  },
};
