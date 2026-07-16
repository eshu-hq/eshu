import type { Page, Response } from "playwright";

import type { ConsoleRoute } from "../src/e2e/consoleRouteCatalog.ts";
import {
  awaitApiQuiet,
  installApiQuietTracker,
  type ApiQuietResult,
  type ApiQuietTracker,
} from "./liveE2EPolicy.ts";

const profileReadPaths = [
  "/api/v0/auth/profile",
  "/api/v0/auth/sessions",
  "/api/v0/auth/local/api-tokens",
] as const;

const profileUnavailableTexts = [
  "Profile unavailable from this source.",
  "Permissions unavailable from this source.",
  "Sessions unavailable from this source.",
  "Tokens unavailable from this source.",
] as const;

function isGetResponseFor(response: Response, path: string): boolean {
  try {
    const pathname = new URL(response.url()).pathname;
    const normalizedPath = pathname.startsWith("/eshu-api/")
      ? pathname.slice("/eshu-api".length)
      : pathname;
    return response.request().method() === "GET" && normalizedPath === path;
  } catch {
    return false;
  }
}

function assertSuccessfulResponse(response: Response, path: string): void {
  if (response.status() !== 200) {
    throw new Error(`${path} returned ${response.status()}, expected 200`);
  }
}

// assertProfileSessionSurface proves the caller-bound reads behind /profile
// through the real browser-session cookie. It reads only safe profile metadata
// and never inspects cookies, tokens, local storage, or credential fields.
export async function assertProfileSessionSurface(page: Page, timeoutMs: number): Promise<string> {
  const responsePromises = profileReadPaths.map((path) =>
    page.waitForResponse((response) => isGetResponseFor(response, path), { timeout: timeoutMs }),
  );
  await page.goto("/profile", { waitUntil: "domcontentloaded", timeout: timeoutMs });
  const responses = await Promise.all(responsePromises);
  responses.forEach((response, index) =>
    assertSuccessfulResponse(response, profileReadPaths[index]!),
  );
  await page.waitForSelector('tr[aria-current="true"]', { timeout: timeoutMs });
  for (const text of profileUnavailableTexts) {
    if ((await page.getByText(text, { exact: true }).count()) > 0) {
      throw new Error(`profile rendered failure state: ${text}`);
    }
  }
  return "Profile rendered its current-session row; 3 caller-bound reads returned 200";
}

// assertAdminSessionSurface proves one read-only admin workflow. It does not
// toggle policy fields or submit a mutation; the existing auth E2E owns those
// separate mutation contracts.
export async function assertAdminSessionSurface(page: Page, timeoutMs: number): Promise<string> {
  await page.goto("/admin", { waitUntil: "domcontentloaded", timeout: timeoutMs });
  await page.waitForSelector(".panel-grid", { timeout: timeoutMs });
  await page.waitForSelector(".identity-access-panel", { timeout: timeoutMs });
  if ((await page.locator(".access-denied-panel").count()) > 0) {
    throw new Error("admin browser session rendered AccessDeniedPage");
  }

  const policyResponse = page.waitForResponse(
    (response) => isGetResponseFor(response, "/api/v0/auth/admin/sign-in-policy"),
    { timeout: timeoutMs },
  );
  const policyTab = page.getByRole("tab", { name: "Sign-in policy", exact: true });
  await policyTab.waitFor({ state: "visible", timeout: timeoutMs });
  if ((await policyTab.count()) !== 1) {
    throw new Error("admin Sign-in policy tab was not uniquely available");
  }
  await policyTab.click();
  assertSuccessfulResponse(await policyResponse, "/api/v0/auth/admin/sign-in-policy");
  await page.waitForSelector('#identity-access-tab-sign-in-policy[aria-selected="true"]', {
    timeout: timeoutMs,
  });
  await page.waitForSelector("#identity-access-panel-sign-in-policy", { timeout: timeoutMs });
  await page.waitForSelector("#policy-require-sso", { timeout: timeoutMs });
  if ((await page.locator("#identity-access-panel-sign-in-policy .unavailable-note").count()) > 0) {
    throw new Error("admin sign-in policy rendered unavailable after a successful read");
  }
  return "Admin rendered its authorized shell; sign-in policy returned 200";
}

// assertWholeDashboardSessionAccess proves that the tenant-bound owner session
// created by the normal setup wizard can enter every ordinary console route.
// Data accuracy and route workflows stay in the retained-corpus bearer gate;
// this fresh-stack sweep isolates authorization and rejects any API 401/403,
// login/setup redirect, or access-denied screen.
export async function assertWholeDashboardSessionAccess(
  page: Page,
  routes: readonly ConsoleRoute[],
  timeoutMs: number,
  waitForQuiet: DashboardApiQuietWait = defaultDashboardApiQuietWait,
): Promise<string> {
  const tracker = installApiQuietTracker(page);
  const ordinaryRoutes = routes.filter((route) => route.authMode !== "browser_session");
  for (const route of ordinaryRoutes) {
    const denials: string[] = [];
    const onResponse = (response: Response): void => {
      const url = new URL(response.url());
      if (!url.pathname.includes("/eshu-api/api/") || ![401, 403].includes(response.status())) {
        return;
      }
      denials.push(`${response.request().method()} ${url.pathname} -> ${response.status()}`);
    };
    page.on("response", onResponse);
    try {
      await page.goto(route.path, { waitUntil: "domcontentloaded", timeout: timeoutMs });
      await page.waitForSelector(".source-pill.src-connected", { timeout: timeoutMs });
      const quiet = await waitForQuiet(page, tracker);
      if (!quiet.settled) {
        throw new Error(
          `${route.path} left ${quiet.inFlight} API request(s) active after ${quiet.waitedMs}ms`,
        );
      }
      const renderedPath = new URL(page.url()).pathname;
      if (renderedPath === "/login" || renderedPath === "/setup") {
        throw new Error(`${route.path} redirected the browser session to ${renderedPath}`);
      }
      if ((await page.locator(".access-denied-panel").count()) > 0 || denials.length > 0) {
        throw new Error(
          `${route.path} received browser-session authorization denial: ${denials.join(", ") || "access-denied screen"}`,
        );
      }
    } finally {
      page.off("response", onResponse);
    }
  }
  return `${ordinaryRoutes.length} ordinary dashboard routes accepted the tenant-bound browser session without 401/403`;
}

export type DashboardApiQuietWait = (
  page: Page,
  tracker: ApiQuietTracker,
) => Promise<ApiQuietResult>;

const defaultDashboardApiQuietWait: DashboardApiQuietWait = (page, tracker) =>
  awaitApiQuiet(tracker, (duration) => page.waitForTimeout(duration));
