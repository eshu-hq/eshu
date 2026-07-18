import type { Page } from "playwright";
import type { PageTest } from "../types.ts";

export const pageTest: PageTest = {
  path: "/catalog",
  label: "Catalog",
  area: "service",
  async assert(page: Page): Promise<void> {
    await page.waitForSelector(".page-shell", { timeout: 10000 });
    const len = await page.evaluate(
      () => document.querySelector(".page-shell")?.textContent?.trim().length ?? 0,
    );
    if (len < 40) throw new Error(`page rendered only ${len} chars`);

    const initialViewport = page.viewportSize();
    const initialZoom = await page.evaluate(() => document.documentElement.style.zoom);
    try {
      const initialViewport = page.viewportSize();
      const initialZoom = await page.evaluate(() => document.documentElement.style.zoom);
      try {
        const initialViewport = page.viewportSize();
        const initialZoom = await page.evaluate(() => document.documentElement.style.zoom);
        try {
          await installLongValueFixture(page);

          for (const width of [1024, 1280, 1767]) {
            await page.setViewportSize({ width, height: 900 });
            await assertCatalogOverflowOwnership(page, `${width}px`, width !== 1767);
          }

          await page.setViewportSize({ width: 1280, height: 900 });
          await page.evaluate(() => {
            document.documentElement.style.zoom = "125%";
          });
          await assertCatalogOverflowOwnership(page, "1280px at 125% zoom", true);
        } finally {
          await page.evaluate((zoom) => {
            document.documentElement.style.zoom = zoom;
          }, initialZoom);
          if (initialViewport !== null) {
            await page.setViewportSize(initialViewport);
          }
        }
      } finally {
        await page.evaluate((zoom) => {
          document.documentElement.style.zoom = zoom;
        }, initialZoom);
        if (initialViewport !== null) {
          await page.setViewportSize(initialViewport);
        }
      }
    } finally {
      await page.evaluate((zoom) => {
        document.documentElement.style.zoom = zoom;
      }, initialZoom);
      if (initialViewport !== null) {
        await page.setViewportSize(initialViewport);
      }
    }
  },
};

async function installLongValueFixture(page: Page): Promise<void> {
  await page.evaluate(() => {
    const row = document.querySelector<HTMLTableRowElement>(".catalog-table tbody tr");
    const nameCell = row?.cells.item(0);
    const repositoryCell = row?.cells.item(6);
    if (
      nameCell === null ||
      nameCell === undefined ||
      repositoryCell === null ||
      repositoryCell === undefined
    ) {
      throw new Error("catalog row does not expose Name and Repository cells");
    }
    const name = "customer-account-reconciliation-and-regulatory-reporting-orchestrator";
    const repository =
      "platform/customer-account-reconciliation-and-regulatory-reporting-orchestrator";
    const nameValue = nameCell.querySelector<HTMLElement>(".catalog-cell-value") ?? nameCell;
    const repositoryValue =
      repositoryCell.querySelector<HTMLElement>(".catalog-cell-value") ?? repositoryCell;
    nameValue.textContent = name;
    nameValue.title = name;
    repositoryValue.textContent = repository;
    repositoryValue.title = repository;
  });
}

interface CatalogLayout {
  readonly documentClientWidth: number;
  readonly documentScrollWidth: number;
  readonly panelRight: number;
  readonly regionClientWidth: number;
  readonly regionRight: number;
  readonly regionScrollWidth: number;
}

async function assertCatalogOverflowOwnership(
  page: Page,
  viewport: string,
  expectInternalOverflow: boolean,
): Promise<void> {
  const layout = await page.evaluate<CatalogLayout>(() => {
    const panel = document.querySelector<HTMLElement>(".catalog-panel");
    const region = document.querySelector<HTMLElement>(".catalog-table-scroll");
    if (panel === null || region === null) {
      throw new Error("catalog panel or scroll region is missing");
    }
    return {
      documentClientWidth: document.documentElement.clientWidth,
      documentScrollWidth: document.documentElement.scrollWidth,
      panelRight: panel.getBoundingClientRect().right,
      regionClientWidth: region.clientWidth,
      regionRight: region.getBoundingClientRect().right,
      regionScrollWidth: region.scrollWidth,
    };
  });

  if (layout.documentScrollWidth > layout.documentClientWidth) {
    throw new Error(
      `${viewport}: document overflowed ${layout.documentScrollWidth}px > ${layout.documentClientWidth}px`,
    );
  }
  if (layout.regionRight > layout.panelRight + 1) {
    throw new Error(
      `${viewport}: scroll region escaped panel (${layout.regionRight}px > ${layout.panelRight}px)`,
    );
  }
  if (layout.regionScrollWidth < layout.regionClientWidth) {
    throw new Error(
      `${viewport}: scroll region geometry is invalid (${layout.regionScrollWidth}px < ${layout.regionClientWidth}px)`,
    );
  }
  if (expectInternalOverflow && layout.regionScrollWidth <= layout.regionClientWidth) {
    throw new Error(
      `${viewport}: long fixture did not exercise internal overflow (${layout.regionScrollWidth}px <= ${layout.regionClientWidth}px)`,
    );
  }
}
