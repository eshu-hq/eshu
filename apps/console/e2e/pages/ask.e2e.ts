import type { Page } from "playwright";
import type { PageTest } from "../types.ts";

export const pageTest: PageTest = {
  path: "/ask",
  label: "Ask Eshu",
  area: "ask",
  async assert(page: Page): Promise<void> {
    await page.waitForSelector(".chat-panel", { timeout: 10000 });
    const len = await page.evaluate(
      () => document.querySelector(".chat-panel")?.textContent?.trim().length ?? 0,
    );
    if (len < 40) throw new Error(`page rendered only ${len} chars`);
  },
};
