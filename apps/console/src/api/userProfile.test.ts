// api/userProfile.test.ts
// Verifies loadProfile / loadSessions / loadTokens:
//   - hit the correct endpoint paths
//   - return "unavailable" on error, never fabricating rows
//   - never include session_hash, token_hash, or other secrets in view models
// Also verifies the self-service token mutators added in issue #5164:
// createPersonalApiToken, rotatePersonalApiToken, revokeApiToken.
import { describe, it, expect, vi } from "vitest";

import type { EshuApiClient } from "./client";
import { EshuApiHttpError } from "./client";
import {
  loadProfile,
  loadSessions,
  loadTokens,
  createPersonalApiToken,
  rotatePersonalApiToken,
  revokeApiToken,
} from "./userProfile";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function makeClient(handler: (path: string) => unknown): EshuApiClient {
  return {
    getJson: async (path: string) => handler(path),
  } as unknown as EshuApiClient;
}

function throwingClient(path?: string): EshuApiClient {
  return {
    getJson: async (p: string) => {
      if (path === undefined || p === path) throw new Error("HTTP 503");
      return {};
    },
  } as unknown as EshuApiClient;
}

// ---------------------------------------------------------------------------
// loadProfile
// ---------------------------------------------------------------------------

describe("loadProfile", () => {
  it("hits /api/v0/auth/profile", async () => {
    let calledPath = "";
    const client = makeClient((path) => {
      calledPath = path;
      return {
        active_tenant_id: "tenant_a",
        active_workspace_id: "workspace_a",
        role_ids: ["developer"],
        allowed_permission_features: ["ask_search"],
        permission_catalog_enforced: true,
        mfa: { has_active_mfa: false },
        memberships: [{ tenant_id: "tenant_a", workspace_id: "workspace_a" }],
      };
    });
    const result = await loadProfile(client);
    expect(calledPath).toBe("/api/v0/auth/profile");
    expect(result.provenance).toBe("live");
    expect(result.data).not.toBeNull();
  });

  it("returns unavailable on error, never fabricating data", async () => {
    const result = await loadProfile(throwingClient("/api/v0/auth/profile"));
    expect(result.provenance).toBe("unavailable");
    expect(result.data).toBeNull();
  });

  it("surfaces external_provider_config_id when present", async () => {
    const client = makeClient(() => ({
      external_provider_config_id: "oidc-config-xyz",
      active_tenant_id: "tenant_a",
      active_workspace_id: "workspace_a",
      permission_catalog_enforced: false,
      mfa: { has_active_mfa: false },
      memberships: [],
    }));
    const result = await loadProfile(client);
    expect(result.data?.external_provider_config_id).toBe("oidc-config-xyz");
  });

  it("never fabricates memberships on error", async () => {
    const result = await loadProfile(throwingClient());
    expect(result.data).toBeNull();
    // provenance must be "unavailable", not inventing a fake profile
    expect(result.provenance).toBe("unavailable");
  });
});

// ---------------------------------------------------------------------------
// loadSessions
// ---------------------------------------------------------------------------

describe("loadSessions", () => {
  it("hits /api/v0/auth/sessions", async () => {
    let calledPath = "";
    const client = makeClient((path) => {
      calledPath = path;
      return { sessions: [] };
    });
    await loadSessions(client);
    expect(calledPath).toBe("/api/v0/auth/sessions");
  });

  it("returns sessions array on success", async () => {
    const now = new Date().toISOString();
    const client = makeClient(() => ({
      sessions: [
        {
          issued_at: now,
          last_seen_at: now,
          idle_expires_at: now,
          absolute_expires_at: now,
          tenant_id: "tenant_a",
          workspace_id: "workspace_a",
          current: true,
        },
      ],
    }));
    const result = await loadSessions(client);
    expect(result.provenance).toBe("live");
    expect(result.sessions).toHaveLength(1);
    expect(result.sessions[0].current).toBe(true);
  });

  it("returns unavailable with empty array on error, never fabricating rows", async () => {
    const result = await loadSessions(throwingClient("/api/v0/auth/sessions"));
    expect(result.provenance).toBe("unavailable");
    expect(result.sessions).toHaveLength(0);
  });

  it("view model never contains session_hash or csrf fields", async () => {
    const client = makeClient(() => ({
      sessions: [
        {
          issued_at: new Date().toISOString(),
          last_seen_at: new Date().toISOString(),
          idle_expires_at: new Date().toISOString(),
          absolute_expires_at: new Date().toISOString(),
          current: false,
        },
      ],
    }));
    const result = await loadSessions(client);
    const serialised = JSON.stringify(result);
    expect(serialised).not.toContain("session_hash");
    expect(serialised).not.toContain("csrf_token");
    expect(serialised).not.toContain("token_hash");
  });
});

// ---------------------------------------------------------------------------
// loadTokens
// ---------------------------------------------------------------------------

describe("loadTokens", () => {
  it("hits /api/v0/auth/local/api-tokens", async () => {
    let calledPath = "";
    const client = makeClient((path) => {
      calledPath = path;
      return { tokens: [] };
    });
    await loadTokens(client);
    expect(calledPath).toBe("/api/v0/auth/local/api-tokens");
  });

  it("returns tokens array on success, display_label absent when the token has none", async () => {
    const now = new Date().toISOString();
    const client = makeClient(() => ({
      tokens: [
        {
          token_id: "tok-001",
          token_class: "personal",
          issued_at: now,
          expires_at: now,
        },
      ],
    }));
    const result = await loadTokens(client);
    expect(result.provenance).toBe("live");
    expect(result.tokens).toHaveLength(1);
    expect(result.tokens[0].token_id).toBe("tok-001");
    expect(result.tokens[0].display_label).toBeUndefined();
  });

  it("passes through the real display_label (issue #3708)", async () => {
    const now = new Date().toISOString();
    const client = makeClient(() => ({
      tokens: [
        {
          token_id: "tok-001",
          token_class: "personal",
          display_label: "owner laptop",
          issued_at: now,
        },
      ],
    }));
    const result = await loadTokens(client);
    expect(result.tokens[0].display_label).toBe("owner laptop");
  });

  it("returns unavailable with empty array on error, never fabricating rows", async () => {
    const result = await loadTokens(throwingClient("/api/v0/auth/local/api-tokens"));
    expect(result.provenance).toBe("unavailable");
    expect(result.tokens).toHaveLength(0);
  });

  it("view model never contains token_hash", async () => {
    const client = makeClient(() => ({
      tokens: [
        {
          token_id: "tok-002",
          token_class: "personal",
          issued_at: new Date().toISOString(),
        },
      ],
    }));
    const result = await loadTokens(client);
    const serialised = JSON.stringify(result);
    expect(serialised).not.toContain("token_hash");
    expect(serialised).not.toContain("session_hash");
  });
});

// ---------------------------------------------------------------------------
// createPersonalApiToken / rotatePersonalApiToken / revokeApiToken (#5164)
// ---------------------------------------------------------------------------

describe("createPersonalApiToken", () => {
  it("posts token_class=personal with the trimmed label, omitting user_id (server resolves it)", async () => {
    const postJson = vi.fn(async () => ({
      token_id: "tok-new",
      api_token: "raw-token",
      issued_at: "2026-06-24T10:00:00Z",
    }));
    const client = { postJson } as unknown as EshuApiClient;
    const result = await createPersonalApiToken(client, { displayLabel: "  laptop  " });
    expect(postJson).toHaveBeenCalledWith("/api/v0/auth/local/api-tokens", {
      token_class: "personal",
      display_label: "laptop",
    });
    expect(result).toEqual({
      status: "created",
      token: { token_id: "tok-new", api_token: "raw-token", issued_at: "2026-06-24T10:00:00Z" },
    });
  });

  it("includes expires_at only when provided", async () => {
    const postJson = vi.fn(async () => ({
      token_id: "tok-new",
      api_token: "raw-token",
      issued_at: "2026-06-24T10:00:00Z",
    }));
    const client = { postJson } as unknown as EshuApiClient;
    await createPersonalApiToken(client, {
      displayLabel: "laptop",
      expiresAt: "2026-07-01T00:00:00.000Z",
    });
    expect(postJson).toHaveBeenCalledWith("/api/v0/auth/local/api-tokens", {
      token_class: "personal",
      display_label: "laptop",
      expires_at: "2026-07-01T00:00:00.000Z",
    });
  });

  it("returns a 'forbidden' result on a 403, not a thrown error", async () => {
    const client = {
      postJson: async () => {
        throw new EshuApiHttpError(403);
      },
    } as unknown as EshuApiClient;
    const result = await createPersonalApiToken(client, { displayLabel: "laptop" });
    expect(result).toEqual({ status: "forbidden" });
  });

  it("returns an 'error' result with a message on any other failure", async () => {
    const client = {
      postJson: async () => {
        throw new Error("network down");
      },
    } as unknown as EshuApiClient;
    const result = await createPersonalApiToken(client, { displayLabel: "laptop" });
    expect(result.status).toBe("error");
  });
});

describe("rotatePersonalApiToken", () => {
  it("posts to the rotate endpoint and returns the replacement token", async () => {
    const postJson = vi.fn(async () => ({
      token_id: "tok-rotated",
      api_token: "raw-rotated-token",
      issued_at: "2026-06-24T10:00:00Z",
    }));
    const client = { postJson } as unknown as EshuApiClient;
    const result = await rotatePersonalApiToken(client, "tok-old");
    expect(postJson).toHaveBeenCalledWith("/api/v0/auth/local/api-tokens/tok-old/rotate", {});
    expect(result.status).toBe("created");
  });

  it("returns 'forbidden' on a 403", async () => {
    const client = {
      postJson: async () => {
        throw new EshuApiHttpError(403);
      },
    } as unknown as EshuApiClient;
    const result = await rotatePersonalApiToken(client, "tok-old");
    expect(result).toEqual({ status: "forbidden" });
  });
});

describe("revokeApiToken (self-service)", () => {
  it("posts (no content) to the revoke endpoint", async () => {
    const postNoContent = vi.fn(async () => undefined);
    const client = { postNoContent } as unknown as EshuApiClient;
    const ok = await revokeApiToken(client, "tok-1");
    expect(postNoContent).toHaveBeenCalledWith("/api/v0/auth/local/api-tokens/tok-1/revoke", {});
    expect(ok).toBe(true);
  });

  it("returns false on error", async () => {
    const client = {
      postNoContent: async () => {
        throw new Error("nope");
      },
    } as unknown as EshuApiClient;
    expect(await revokeApiToken(client, "tok-1")).toBe(false);
  });
});
