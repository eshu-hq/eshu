import type { Page } from "playwright";
import type { PageTest } from "../types.ts";

export const pageTest: PageTest = {
  path: "/",
  label: "Login Page",
  area: "operations",
  async assert(page: Page): Promise<void> {
    await page.waitForSelector(".login-page, .source-pill", { timeout: 10000 });
    const len = await page.evaluate(
      () => document.querySelector(".login-page, .source-pill")?.textContent?.trim().length ?? 0,
    );
    if (len < 1) throw new Error("login page rendered nothing");
  },
};
