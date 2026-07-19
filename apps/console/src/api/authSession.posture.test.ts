// authSession.posture.test.ts — TDD tests for listAuthPosture (issue #5165,
// F-4). Replaces the console's two separate pre-auth fetches
// (listAuthProviders + loadPublicRequireSSO) with one call to the extended
// GET /api/v0/auth/providers response: { providers, local_login_offered,
// self_service_tokens_offered }. Matches
// go/internal/query/auth_posture.go's AuthPosture shape verbatim.
import { describe, expect, it, vi } from "vitest";

import { listAuthPosture } from "./authSession";
import type { EshuApiClient } from "./client";

function makeClient(getJson: EshuApiClient["getJson"]): EshuApiClient {
  return { getJson } as unknown as EshuApiClient;
}

describe("listAuthPosture", () => {
  it("fetches GET /api/v0/auth/providers with no tenant_id when tenantId is absent", async () => {
    const getJson = vi.fn(async () => ({
      providers: [],
      local_login_offered: true,
      self_service_tokens_offered: true,
    })) as unknown as EshuApiClient["getJson"];
    await listAuthPosture(makeClient(getJson), undefined);
    expect(getJson).toHaveBeenCalledWith("/api/v0/auth/providers");
  });

  it("includes tenant_id in the query string when provided", async () => {
    const getJson = vi.fn(async () => ({
      providers: [],
      local_login_offered: true,
      self_service_tokens_offered: true,
    })) as unknown as EshuApiClient["getJson"];
    await listAuthPosture(makeClient(getJson), "tenant_a");
    expect(getJson).toHaveBeenCalledWith(
      expect.stringContaining("/api/v0/auth/providers?tenant_id=tenant_a"),
    );
  });

  it("returns the posture fields verbatim from a successful response", async () => {
    const getJson = vi.fn(async () => ({
      providers: [
        {
          provider_config_id: "pc_1",
          display_label: "Okta",
          provider_kind: "oidc",
          icon_hint: "oidc",
        },
      ],
      local_login_offered: false,
      self_service_tokens_offered: true,
    })) as unknown as EshuApiClient["getJson"];
    const posture = await listAuthPosture(makeClient(getJson), "tenant_a");
    expect(posture).toEqual({
      providers: [
        {
          provider_config_id: "pc_1",
          display_label: "Okta",
          provider_kind: "oidc",
          icon_hint: "oidc",
        },
      ],
      local_login_offered: false,
      self_service_tokens_offered: true,
    });
  });

  it("fails open to the local-only default on a network/HTTP error", async () => {
    const getJson = vi.fn(async () => {
      throw new Error("network failure");
    }) as unknown as EshuApiClient["getJson"];
    const posture = await listAuthPosture(makeClient(getJson), "tenant_a");
    expect(posture).toEqual({
      providers: [],
      local_login_offered: true,
      self_service_tokens_offered: true,
    });
  });

  it("defaults missing boolean fields to true (forward-compatible with an older backend)", async () => {
    const getJson = vi.fn(async () => ({
      providers: [],
    })) as unknown as EshuApiClient["getJson"];
    const posture = await listAuthPosture(makeClient(getJson), "tenant_a");
    expect(posture.local_login_offered).toBe(true);
    expect(posture.self_service_tokens_offered).toBe(true);
  });
});
