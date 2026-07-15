import { describe, expect, it, vi } from "vitest";
import type { Page, Response } from "playwright";

import {
  assertAdminSessionSurface,
  assertProfileSessionSurface,
  assertWholeDashboardSessionAccess,
  type DashboardApiQuietWait,
} from "./authE2ESurfaceProof";
import { runSessionSurfaceProofSteps } from "./authE2ESurfaceSteps";

interface FakeResponse {
  readonly url: () => string;
  readonly status: () => number;
  readonly request: () => { readonly method: () => string };
}

function response(path: string, status = 200): FakeResponse {
  return {
    url: () => `http://127.0.0.1:5185/eshu-api${path}`,
    status: () => status,
    request: () => ({ method: () => "GET" }),
  };
}

describe("browser-session surface proof", () => {
  const apiQuiet: DashboardApiQuietWait = vi.fn().mockResolvedValue({
    inFlight: 0,
    settled: true,
    waitedMs: 1,
  });

  it("records Profile and Admin as separate browser-session proof steps", async () => {
    const ids: string[] = [];
    const step = vi.fn(async (id: string, fn: () => Promise<string>) => {
      ids.push(id);
      await fn();
    });
    const proofs = {
      profile: vi.fn().mockResolvedValue("profile pass"),
      admin: vi.fn().mockResolvedValue("admin pass"),
      wholeDashboard: vi.fn().mockResolvedValue("dashboard pass"),
    };

    await runSessionSurfaceProofSteps(step, {} as Page, 1_000, proofs);

    expect(ids).toEqual([
      "item2_profile_session_surface",
      "item2_admin_session_surface",
      "item2_whole_dashboard_session_access",
    ]);
    expect(proofs.profile).toHaveBeenCalledOnce();
    expect(proofs.admin).toHaveBeenCalledOnce();
    expect(proofs.wholeDashboard).toHaveBeenCalledOnce();
  });

  it("proves ordinary dashboard routes without a browser-session authorization denial", async () => {
    let onResponse: ((value: FakeResponse) => void) | undefined;
    const page = {
      on: vi.fn((_event: string, handler: (value: FakeResponse) => void) => {
        onResponse = handler;
      }),
      off: vi.fn(),
      goto: vi.fn(async () => {
        onResponse?.(response("/api/v0/repositories"));
      }),
      waitForSelector: vi.fn().mockResolvedValue(undefined),
      waitForTimeout: vi.fn().mockResolvedValue(undefined),
      locator: vi.fn(() => ({ count: vi.fn().mockResolvedValue(0) })),
      url: vi.fn(() => "http://127.0.0.1:5185/repositories"),
    };

    await expect(
      assertWholeDashboardSessionAccess(
        page as unknown as Page,
        [{ path: "/repositories", label: "Repositories", area: "repositories" }],
        1_000,
        apiQuiet,
      ),
    ).resolves.toContain("1 ordinary dashboard route");
  });

  it("rejects an ordinary dashboard route when its browser session receives 403", async () => {
    let onResponse: ((value: FakeResponse) => void) | undefined;
    const page = {
      on: vi.fn((_event: string, handler: (value: FakeResponse) => void) => {
        onResponse = handler;
      }),
      off: vi.fn(),
      goto: vi.fn(async () => {
        onResponse?.(response("/api/v0/repositories", 403));
      }),
      waitForSelector: vi.fn().mockResolvedValue(undefined),
      waitForTimeout: vi.fn().mockResolvedValue(undefined),
      locator: vi.fn(() => ({ count: vi.fn().mockResolvedValue(0) })),
      url: vi.fn(() => "http://127.0.0.1:5185/repositories"),
    };

    await expect(
      assertWholeDashboardSessionAccess(
        page as unknown as Page,
        [{ path: "/repositories", label: "Repositories", area: "repositories" }],
        1_000,
        apiQuiet,
      ),
    ).rejects.toThrow("/repositories received browser-session authorization denial");
  });

  it("keeps denial capture active until the route API becomes quiet", async () => {
    let onResponse: ((value: FakeResponse) => void) | undefined;
    const page = {
      on: vi.fn((event: string, handler: (value: FakeResponse) => void) => {
        if (event === "response") onResponse = handler;
      }),
      off: vi.fn(),
      goto: vi.fn().mockResolvedValue(undefined),
      waitForSelector: vi.fn().mockResolvedValue(undefined),
      locator: vi.fn(() => ({ count: vi.fn().mockResolvedValue(0) })),
      url: vi.fn(() => "http://127.0.0.1:5185/repositories"),
    };
    const delayedDenial: DashboardApiQuietWait = vi.fn(async () => {
      onResponse?.(response("/api/v0/repositories", 403));
      return { inFlight: 0, settled: true, waitedMs: 1_001 };
    });

    await expect(
      assertWholeDashboardSessionAccess(
        page as unknown as Page,
        [{ path: "/repositories", label: "Repositories", area: "repositories" }],
        1_000,
        delayedDenial,
      ),
    ).rejects.toThrow("/repositories received browser-session authorization denial");
  });

  it("proves all three caller-bound profile reads and the current-session row", async () => {
    const page = {
      waitForResponse: vi
        .fn()
        .mockResolvedValueOnce(response("/api/v0/auth/profile"))
        .mockResolvedValueOnce(response("/api/v0/auth/sessions"))
        .mockResolvedValueOnce(response("/api/v0/auth/local/api-tokens")),
      goto: vi.fn().mockResolvedValue(undefined),
      waitForSelector: vi.fn().mockResolvedValue(undefined),
      locator: vi.fn(() => ({ count: vi.fn().mockResolvedValue(0) })),
      getByText: vi.fn(() => ({ count: vi.fn().mockResolvedValue(0) })),
    };

    await expect(assertProfileSessionSurface(page as unknown as Page, 1_000)).resolves.toContain(
      "3 caller-bound reads returned 200",
    );
    expect(page.goto).toHaveBeenCalledWith("/profile", expect.any(Object));
    expect(page.waitForSelector).toHaveBeenCalledWith(
      'tr[aria-current="true"]',
      expect.any(Object),
    );
  });

  it("fails profile proof when a caller-bound read is not successful", async () => {
    const page = {
      waitForResponse: vi
        .fn()
        .mockResolvedValueOnce(response("/api/v0/auth/profile"))
        .mockResolvedValueOnce(response("/api/v0/auth/sessions", 401))
        .mockResolvedValueOnce(response("/api/v0/auth/local/api-tokens")),
      goto: vi.fn().mockResolvedValue(undefined),
      waitForSelector: vi.fn().mockResolvedValue(undefined),
      locator: vi.fn(() => ({ count: vi.fn().mockResolvedValue(0) })),
    };

    await expect(assertProfileSessionSurface(page as unknown as Page, 1_000)).rejects.toThrow(
      "/api/v0/auth/sessions returned 401",
    );
  });

  it("ignores a neighboring profile response before the exact proxied endpoint", async () => {
    const exactResponses = [
      response("/api/v0/auth/profile"),
      response("/api/v0/auth/sessions"),
      response("/api/v0/auth/local/api-tokens"),
    ];
    let call = 0;
    const page = {
      waitForResponse: vi.fn(
        async (predicate: (candidate: Response) => boolean): Promise<FakeResponse> => {
          const exact = exactResponses[call++]!;
          const expectedPath = new URL(exact.url()).pathname.slice("/eshu-api".length);
          const neighbor = response(`/neighbor${expectedPath}`, 500);
          if (predicate(neighbor as unknown as Response)) return neighbor;
          if (predicate(exact as unknown as Response)) return exact;
          throw new Error(`no response matched ${expectedPath}`);
        },
      ),
      goto: vi.fn().mockResolvedValue(undefined),
      waitForSelector: vi.fn().mockResolvedValue(undefined),
      getByText: vi.fn(() => ({ count: vi.fn().mockResolvedValue(0) })),
    };

    await expect(assertProfileSessionSurface(page as unknown as Page, 1_000)).resolves.toContain(
      "3 caller-bound reads returned 200",
    );
  });

  it("accepts the valid catalog-not-enforced unavailable-note copy", async () => {
    const page = {
      waitForResponse: vi
        .fn()
        .mockResolvedValueOnce(response("/api/v0/auth/profile"))
        .mockResolvedValueOnce(response("/api/v0/auth/sessions"))
        .mockResolvedValueOnce(response("/api/v0/auth/local/api-tokens")),
      goto: vi.fn().mockResolvedValue(undefined),
      waitForSelector: vi.fn().mockResolvedValue(undefined),
      locator: vi.fn(() => ({ count: vi.fn().mockResolvedValue(1) })),
      getByText: vi.fn(() => ({ count: vi.fn().mockResolvedValue(0) })),
    };

    await expect(assertProfileSessionSurface(page as unknown as Page, 1_000)).resolves.toContain(
      "3 caller-bound reads returned 200",
    );
  });

  it("rejects an actual unavailable profile section", async () => {
    const page = {
      waitForResponse: vi
        .fn()
        .mockResolvedValueOnce(response("/api/v0/auth/profile"))
        .mockResolvedValueOnce(response("/api/v0/auth/sessions"))
        .mockResolvedValueOnce(response("/api/v0/auth/local/api-tokens")),
      goto: vi.fn().mockResolvedValue(undefined),
      waitForSelector: vi.fn().mockResolvedValue(undefined),
      getByText: vi.fn((text: string) => ({
        count: vi.fn().mockResolvedValue(text === "Sessions unavailable from this source." ? 1 : 0),
      })),
    };

    await expect(assertProfileSessionSurface(page as unknown as Page, 1_000)).rejects.toThrow(
      "profile rendered failure state: Sessions unavailable from this source.",
    );
  });

  it("proves the admin shell and live sign-in policy read", async () => {
    const click = vi.fn().mockResolvedValue(undefined);
    const page = {
      goto: vi.fn().mockResolvedValue(undefined),
      waitForSelector: vi.fn().mockResolvedValue(undefined),
      waitForResponse: vi.fn().mockResolvedValue(response("/api/v0/auth/admin/sign-in-policy")),
      getByRole: vi.fn(() => ({
        click,
        count: vi.fn().mockResolvedValue(1),
        waitFor: vi.fn().mockResolvedValue(undefined),
      })),
      locator: vi.fn(() => ({ count: vi.fn().mockResolvedValue(0) })),
    };

    await expect(assertAdminSessionSurface(page as unknown as Page, 1_000)).resolves.toContain(
      "sign-in policy returned 200",
    );
    expect(page.goto).toHaveBeenCalledWith("/admin", expect.any(Object));
    expect(click).toHaveBeenCalledOnce();
  });

  it("waits for the lazy-loaded Sign-in policy tab before checking uniqueness", async () => {
    let tabMounted = false;
    const policyTab = {
      waitFor: vi.fn(async () => {
        tabMounted = true;
      }),
      count: vi.fn(async () => (tabMounted ? 1 : 0)),
      click: vi.fn().mockResolvedValue(undefined),
    };
    const page = {
      goto: vi.fn().mockResolvedValue(undefined),
      waitForSelector: vi.fn().mockResolvedValue(undefined),
      waitForResponse: vi.fn().mockResolvedValue(response("/api/v0/auth/admin/sign-in-policy")),
      getByRole: vi.fn(() => policyTab),
      locator: vi.fn(() => ({ count: vi.fn().mockResolvedValue(0) })),
    };

    await expect(assertAdminSessionSurface(page as unknown as Page, 1_000)).resolves.toContain(
      "sign-in policy returned 200",
    );
    expect(policyTab.waitFor).toHaveBeenCalledWith({ state: "visible", timeout: 1_000 });
    expect(policyTab.click).toHaveBeenCalledOnce();
  });

  it("ignores a neighboring admin policy response before the exact proxied endpoint", async () => {
    const policyTab = {
      waitFor: vi.fn().mockResolvedValue(undefined),
      count: vi.fn().mockResolvedValue(1),
      click: vi.fn().mockResolvedValue(undefined),
      getAttribute: vi.fn().mockResolvedValue("true"),
    };
    const page = {
      goto: vi.fn().mockResolvedValue(undefined),
      waitForSelector: vi.fn().mockResolvedValue(undefined),
      waitForResponse: vi.fn(
        async (predicate: (candidate: Response) => boolean): Promise<FakeResponse> => {
          const neighbor = response("/neighbor/api/v0/auth/admin/sign-in-policy", 500);
          if (predicate(neighbor as unknown as Response)) return neighbor;
          const exact = response("/api/v0/auth/admin/sign-in-policy");
          if (predicate(exact as unknown as Response)) return exact;
          throw new Error("no exact policy response matched");
        },
      ),
      getByRole: vi.fn(() => policyTab),
      locator: vi.fn(() => ({ count: vi.fn().mockResolvedValue(0) })),
    };

    await expect(assertAdminSessionSurface(page as unknown as Page, 1_000)).resolves.toContain(
      "sign-in policy returned 200",
    );
  });

  it("rejects a successful admin policy response when its loaded control never renders", async () => {
    const policyTab = {
      waitFor: vi.fn().mockResolvedValue(undefined),
      count: vi.fn().mockResolvedValue(1),
      click: vi.fn().mockResolvedValue(undefined),
      getAttribute: vi.fn().mockResolvedValue("true"),
    };
    const page = {
      goto: vi.fn().mockResolvedValue(undefined),
      waitForSelector: vi.fn(async (selector: string) => {
        if (selector === "#policy-require-sso") {
          throw new Error("policy control did not render");
        }
      }),
      waitForResponse: vi.fn().mockResolvedValue(response("/api/v0/auth/admin/sign-in-policy")),
      getByRole: vi.fn(() => policyTab),
      locator: vi.fn(() => ({ count: vi.fn().mockResolvedValue(0) })),
    };

    await expect(assertAdminSessionSurface(page as unknown as Page, 1_000)).rejects.toThrow(
      "policy control did not render",
    );
  });

  it("rejects the admin policy proof when the clicked tab is not selected", async () => {
    const policyTab = {
      waitFor: vi.fn().mockResolvedValue(undefined),
      count: vi.fn().mockResolvedValue(1),
      click: vi.fn().mockResolvedValue(undefined),
      getAttribute: vi.fn().mockResolvedValue("false"),
    };
    const page = {
      goto: vi.fn().mockResolvedValue(undefined),
      waitForSelector: vi.fn(async (selector: string) => {
        if (selector === '#identity-access-tab-sign-in-policy[aria-selected="true"]') {
          throw new Error("admin Sign-in policy tab was not selected");
        }
      }),
      waitForResponse: vi.fn().mockResolvedValue(response("/api/v0/auth/admin/sign-in-policy")),
      getByRole: vi.fn(() => policyTab),
      locator: vi.fn(() => ({ count: vi.fn().mockResolvedValue(0) })),
    };

    await expect(assertAdminSessionSurface(page as unknown as Page, 1_000)).rejects.toThrow(
      "admin Sign-in policy tab was not selected",
    );
  });
});
