// api/userProfile.test.ts
// Verifies loadProfile / loadSessions / loadTokens:
//   - hit the correct endpoint paths
//   - return "unavailable" on error, never fabricating rows
//   - never include session_hash, token_hash, or other secrets in view models
import { describe, it, expect } from "vitest";
import type { EshuApiClient } from "./client";
import { loadProfile, loadSessions, loadTokens } from "./userProfile";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function makeClient(handler: (path: string) => unknown): EshuApiClient {
  return {
    getJson: async (path: string) => handler(path)
  } as unknown as EshuApiClient;
}

function throwingClient(path?: string): EshuApiClient {
  return {
    getJson: async (p: string) => {
      if (path === undefined || p === path) throw new Error("HTTP 503");
      return {};
    }
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
        memberships: [{ tenant_id: "tenant_a", workspace_id: "workspace_a" }]
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
      memberships: []
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
          current: true
        }
      ]
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
          current: false
        }
      ]
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

  it("returns tokens array on success without display_label (hash removed, see #3708)", async () => {
    const now = new Date().toISOString();
    const client = makeClient(() => ({
      tokens: [
        {
          token_id: "tok-001",
          token_class: "personal",
          issued_at: now,
          expires_at: now
        }
      ]
    }));
    const result = await loadTokens(client);
    expect(result.provenance).toBe("live");
    expect(result.tokens).toHaveLength(1);
    expect(result.tokens[0].token_id).toBe("tok-001");
    // display_label is intentionally absent: SHA-256(display_label) is a hash,
    // not a human label. Issue #3708 tracks persisting a real non-secret label.
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
          issued_at: new Date().toISOString()
        }
      ]
    }));
    const result = await loadTokens(client);
    const serialised = JSON.stringify(result);
    expect(serialised).not.toContain("token_hash");
    expect(serialised).not.toContain("session_hash");
  });
});
