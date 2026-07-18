import type { Page } from "playwright";
import type { PageTest } from "../types.ts";

export const pageTest: PageTest = {
  path: "/vulnerabilities",
  label: "Vulnerabilities - Catalog View",
  area: "security",
  async assert(page: Page): Promise<void> {
    await page.getByRole("tab", { name: "Known intelligence (catalog)" }).click();
    await page.getByRole("textbox", { name: "Search advisories" }).waitFor({
      state: "visible",
      timeout: 10000,
    });

    const unavailable = page.getByText("The vulnerability-intelligence catalog is unavailable", {
      exact: false,
    });
    if ((await unavailable.count()) > 0) {
      throw new Error("catalog tab rendered unavailable against the advisory mock");
    }
  },
};
