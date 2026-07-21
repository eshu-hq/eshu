import type { Page } from "playwright";

import type { RouteInputState } from "./routeResponseEvidenceCache.ts";

export interface RouteDOMState {
  readonly connected: boolean;
  readonly demoBannerPresent: boolean;
  readonly input: RouteInputState;
  readonly mainContentChars: number;
  readonly sourceMode: string;
}

/** Reads verdict state and exact visible route inputs without persisting values. */
export async function readRouteDOMState(page: Page): Promise<RouteDOMState> {
  return page.evaluate(() => {
    const pill = document.querySelector(".source-pill");
    const main = document.querySelector(".main");
    const demoBanner = Array.from(document.querySelectorAll(".prov-banner")).some(
      (node) =>
        (node.textContent ?? "").includes("Prospect demo") ||
        (node.textContent ?? "").toLowerCase().includes("demo fixtures"),
    );
    const pillText = (pill?.textContent ?? "").trim();
    const connected = pill?.className.includes("src-connected") ?? false;
    const sourceMode = connected
      ? pillText.toLowerCase().includes("demo")
        ? "demo"
        : "live"
      : pillText.toLowerCase().replace(/[^a-z]+/g, "-") || "unknown";
    const controls = Array.from(
      document.querySelectorAll<HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement>(
        ".main input, .main select, .main textarea",
      ),
    ).map((control, index) => {
      const ariaLabel = control.getAttribute("aria-label")?.trim() ?? "";
      const baseIdentity =
        control.name.trim().length > 0
          ? `name:${control.name}`
          : ariaLabel.length > 0
            ? `aria-label:${ariaLabel}`
            : control.id.trim().length > 0
              ? `id:${control.id}`
              : `index:${index}`;
      const kind =
        control instanceof HTMLInputElement ? control.type : control.tagName.toLowerCase();
      let value = control.value;
      if (control instanceof HTMLInputElement && ["checkbox", "radio"].includes(control.type)) {
        value = `${control.checked ? "checked" : "unchecked"}:${control.value}`;
      } else if (control instanceof HTMLSelectElement && control.multiple) {
        value = Array.from(control.selectedOptions, (option) => option.value).join("\u0000");
      }
      return { identity: `${baseIdentity}#${index}`, kind, value };
    });
    return {
      connected,
      sourceMode,
      demoBannerPresent: demoBanner,
      input: {
        controls,
        pathname: window.location.pathname,
        search: window.location.search,
      },
      mainContentChars: (main?.textContent ?? "").trim().length,
    };
  });
}
