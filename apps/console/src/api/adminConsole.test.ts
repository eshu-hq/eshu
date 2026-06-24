// api/adminConsole.test.ts
// Verifies the admin-console loaders and mutators added in PR-3 of issue #3703:
//   - each loader hits the real backend path
//   - any error yields provenance "unavailable" with an EMPTY result set
//     (never a fabricated row)
//   - the audit loaders treat a 403 as "forbidden" (a scope signal, not a
//     failure), distinct from a real "unavailable" error
//   - each mutator posts/deletes to the real endpoint with the right body
import { describe, it, expect, vi } from "vitest";
import { EshuApiHttpError } from "./client";
import type { EshuApiClient } from "./client";
import {
  loadInvitations,
  loadRoleAssignments,
  loadRoles,
  loadIdPProviders,
  loadIdPGroupMappings,
  loadApiTokens,
  loadAuditEvents,
  loadAuditSummary,
  revokeInvitation,
  grantRoleAssignment,
  revokeRoleAssignment,
  createIdPGroupMapping,
  deleteIdPGroupMapping,
  revokeApiToken
} from "./adminConsole";

const NOW = "2026-06-24T10:00:00Z";

// ---------------------------------------------------------------------------
// Loaders — happy path uses real backend paths
// ---------------------------------------------------------------------------

describe("adminConsole loaders — endpoint paths and shapes", () => {
  it("loadInvitations calls GET /api/v0/auth/local/invitations", async () => {
    const getJson = vi.fn(async () => ({
      invitations: [{ invite_id: "inv-1", role_id: "developer", status: "pending" }]
    }));
    const client = { getJson } as unknown as EshuApiClient;
    const result = await loadInvitations(client);
    expect(getJson).toHaveBeenCalledWith("/api/v0/auth/local/invitations");
    expect(result.provenance).toBe("live");
    expect(result.invitations).toHaveLength(1);
    expect(result.invitations[0].invite_id).toBe("inv-1");
  });

  it("loadRoleAssignments calls GET /api/v0/auth/admin/role-assignments (no filter)", async () => {
    const getJson = vi.fn(async () => ({ role_assignments: [] }));
    const client = { getJson } as unknown as EshuApiClient;
    const result = await loadRoleAssignments(client);
    expect(getJson).toHaveBeenCalledWith("/api/v0/auth/admin/role-assignments");
    expect(result.provenance).toBe("live");
  });

  it("loadRoleAssignments appends ?user_id when filtered", async () => {
    const getJson = vi.fn(async () => ({ role_assignments: [] }));
    const client = { getJson } as unknown as EshuApiClient;
    await loadRoleAssignments(client, "user-42");
    expect(getJson).toHaveBeenCalledWith(
      "/api/v0/auth/admin/role-assignments?user_id=user-42"
    );
  });

  it("loadRoles calls GET /api/v0/auth/admin/roles", async () => {
    const getJson = vi.fn(async () => ({
      roles: [{ role_id: "admin", status: "active", built_in: true, grants: [] }]
    }));
    const client = { getJson } as unknown as EshuApiClient;
    const result = await loadRoles(client);
    expect(getJson).toHaveBeenCalledWith("/api/v0/auth/admin/roles");
    expect(result.roles[0].role_id).toBe("admin");
  });

  it("loadIdPProviders calls GET /api/v0/auth/admin/idp-providers", async () => {
    const getJson = vi.fn(async () => ({
      providers: [{ provider_config_id: "p-1", provider_kind: "oidc", status: "active" }]
    }));
    const client = { getJson } as unknown as EshuApiClient;
    const result = await loadIdPProviders(client);
    expect(getJson).toHaveBeenCalledWith("/api/v0/auth/admin/idp-providers");
    expect(result.providers[0].provider_config_id).toBe("p-1");
  });

  it("loadIdPGroupMappings calls GET /api/v0/auth/admin/idp-group-mappings", async () => {
    const getJson = vi.fn(async () => ({
      group_mappings: [
        { mapping_ref: "m-1", provider_config_id: "p-1", role_id: "viewer", status: "active" }
      ]
    }));
    const client = { getJson } as unknown as EshuApiClient;
    const result = await loadIdPGroupMappings(client);
    expect(getJson).toHaveBeenCalledWith("/api/v0/auth/admin/idp-group-mappings");
    expect(result.mappings[0].mapping_ref).toBe("m-1");
  });

  it("loadApiTokens calls GET /api/v0/auth/admin/api-tokens", async () => {
    const getJson = vi.fn(async () => ({
      tokens: [{ token_id: "t-1", token_class: "personal", status: "active", issued_at: NOW }]
    }));
    const client = { getJson } as unknown as EshuApiClient;
    const result = await loadApiTokens(client);
    expect(getJson).toHaveBeenCalledWith("/api/v0/auth/admin/api-tokens");
    expect(result.tokens[0].token_id).toBe("t-1");
  });

  it("loadAuditEvents calls GET /api/v0/auth/admin/audit/events", async () => {
    const getJson = vi.fn(async () => ({
      events: [{ event_type: "authz", decision: "allow", occurred_at: NOW }]
    }));
    const client = { getJson } as unknown as EshuApiClient;
    const result = await loadAuditEvents(client);
    expect(getJson).toHaveBeenCalledWith("/api/v0/auth/admin/audit/events");
    expect(result.provenance).toBe("live");
    expect(result.events[0].event_type).toBe("authz");
  });

  it("loadAuditSummary calls GET /api/v0/auth/admin/audit/summary", async () => {
    const getJson = vi.fn(async () => ({ total: 3, allowed: 2, denied: 1, unavailable: 0 }));
    const client = { getJson } as unknown as EshuApiClient;
    const result = await loadAuditSummary(client);
    expect(getJson).toHaveBeenCalledWith("/api/v0/auth/admin/audit/summary");
    expect(result.summary?.total).toBe(3);
  });
});

// ---------------------------------------------------------------------------
// Loaders — error path: unavailable + EMPTY (no fabrication)
// ---------------------------------------------------------------------------

describe("adminConsole loaders — unavailable on error, never fabricated", () => {
  function throwingClient(): EshuApiClient {
    return { getJson: async () => { throw new Error("HTTP 503"); } } as unknown as EshuApiClient;
  }

  it("loadInvitations → unavailable + empty on error", async () => {
    const r = await loadInvitations(throwingClient());
    expect(r.provenance).toBe("unavailable");
    expect(r.invitations).toEqual([]);
  });

  it("loadRoleAssignments → unavailable + empty on error", async () => {
    const r = await loadRoleAssignments(throwingClient());
    expect(r.provenance).toBe("unavailable");
    expect(r.assignments).toEqual([]);
  });

  it("loadRoles → unavailable + empty on error", async () => {
    const r = await loadRoles(throwingClient());
    expect(r.provenance).toBe("unavailable");
    expect(r.roles).toEqual([]);
  });

  it("loadIdPProviders → unavailable + empty on error", async () => {
    const r = await loadIdPProviders(throwingClient());
    expect(r.provenance).toBe("unavailable");
    expect(r.providers).toEqual([]);
  });

  it("loadIdPGroupMappings → unavailable + empty on error", async () => {
    const r = await loadIdPGroupMappings(throwingClient());
    expect(r.provenance).toBe("unavailable");
    expect(r.mappings).toEqual([]);
  });

  it("loadApiTokens → unavailable + empty on error", async () => {
    const r = await loadApiTokens(throwingClient());
    expect(r.provenance).toBe("unavailable");
    expect(r.tokens).toEqual([]);
  });
});

// ---------------------------------------------------------------------------
// Audit loaders — distinguish 403 (forbidden / scope) from real failure
// ---------------------------------------------------------------------------

describe("adminConsole audit loaders — 403 vs failure", () => {
  function client403(): EshuApiClient {
    return {
      getJson: async () => { throw new EshuApiHttpError(403); }
    } as unknown as EshuApiClient;
  }
  function client503(): EshuApiClient {
    return {
      getJson: async () => { throw new EshuApiHttpError(503); }
    } as unknown as EshuApiClient;
  }

  it("loadAuditEvents maps a 403 to provenance 'forbidden' (not 'unavailable')", async () => {
    const r = await loadAuditEvents(client403());
    expect(r.provenance).toBe("forbidden");
    expect(r.events).toEqual([]);
  });

  it("loadAuditEvents maps a 503 to provenance 'unavailable'", async () => {
    const r = await loadAuditEvents(client503());
    expect(r.provenance).toBe("unavailable");
    expect(r.events).toEqual([]);
  });

  it("loadAuditSummary maps a 403 to provenance 'forbidden'", async () => {
    const r = await loadAuditSummary(client403());
    expect(r.provenance).toBe("forbidden");
    expect(r.summary).toBeNull();
  });

  it("loadAuditSummary maps a non-403 error to provenance 'unavailable'", async () => {
    const r = await loadAuditSummary(client503());
    expect(r.provenance).toBe("unavailable");
    expect(r.summary).toBeNull();
  });
});

// ---------------------------------------------------------------------------
// Mutators — real endpoints + bodies
// ---------------------------------------------------------------------------

describe("adminConsole mutators — endpoint + body", () => {
  it("revokeInvitation posts to /api/v0/auth/local/invitations/{id}/revoke", async () => {
    const postJson = vi.fn(async () => ({ invite_id: "inv-1", status: "revoked", revoked: true }));
    const client = { postJson } as unknown as EshuApiClient;
    const ok = await revokeInvitation(client, "inv-1");
    expect(postJson).toHaveBeenCalledWith("/api/v0/auth/local/invitations/inv-1/revoke", {});
    expect(ok).toBe(true);
  });

  it("revokeInvitation encodes the invite id into the path", async () => {
    const postJson = vi.fn(async () => ({ revoked: true }));
    const client = { postJson } as unknown as EshuApiClient;
    await revokeInvitation(client, "inv/with space");
    expect(postJson).toHaveBeenCalledWith(
      "/api/v0/auth/local/invitations/inv%2Fwith%20space/revoke",
      {}
    );
  });

  it("revokeInvitation returns false on error (never throws to the panel)", async () => {
    const client = { postJson: async () => { throw new Error("boom"); } } as unknown as EshuApiClient;
    expect(await revokeInvitation(client, "inv-1")).toBe(false);
  });

  it("grantRoleAssignment posts to /api/v0/auth/admin/role-assignments with user/role/workspace", async () => {
    const postJson = vi.fn(async () => ({ user_id: "u", role_id: "r", status: "active", changed: true }));
    const client = { postJson } as unknown as EshuApiClient;
    const ok = await grantRoleAssignment(client, { user_id: "u", role_id: "r", workspace_id: "w" });
    expect(postJson).toHaveBeenCalledWith("/api/v0/auth/admin/role-assignments", {
      user_id: "u",
      role_id: "r",
      workspace_id: "w"
    });
    expect(ok).toBe(true);
  });

  it("grantRoleAssignment omits an empty workspace_id from the body", async () => {
    const postJson = vi.fn(async () => ({ changed: true }));
    const client = { postJson } as unknown as EshuApiClient;
    await grantRoleAssignment(client, { user_id: "u", role_id: "r", workspace_id: "" });
    expect(postJson).toHaveBeenCalledWith("/api/v0/auth/admin/role-assignments", {
      user_id: "u",
      role_id: "r"
    });
  });

  it("revokeRoleAssignment posts to /api/v0/auth/admin/role-assignments/revoke", async () => {
    const postJson = vi.fn(async () => ({ status: "revoked", changed: true }));
    const client = { postJson } as unknown as EshuApiClient;
    const ok = await revokeRoleAssignment(client, { user_id: "u", role_id: "r" });
    expect(postJson).toHaveBeenCalledWith("/api/v0/auth/admin/role-assignments/revoke", {
      user_id: "u",
      role_id: "r"
    });
    expect(ok).toBe(true);
  });

  it("createIdPGroupMapping posts to /api/v0/auth/admin/idp-group-mappings", async () => {
    const postJson = vi.fn(async () => ({ mapping_ref: "m-1", status: "active", created: true }));
    const client = { postJson } as unknown as EshuApiClient;
    const ok = await createIdPGroupMapping(client, {
      provider_config_id: "p-1",
      external_group: "engineers",
      role_id: "developer"
    });
    expect(postJson).toHaveBeenCalledWith("/api/v0/auth/admin/idp-group-mappings", {
      provider_config_id: "p-1",
      external_group: "engineers",
      role_id: "developer"
    });
    expect(ok).toBe(true);
  });

  it("deleteIdPGroupMapping deletes /api/v0/auth/admin/idp-group-mappings/{ref}", async () => {
    const del = vi.fn(async () => undefined);
    const client = { delete: del } as unknown as EshuApiClient;
    const ok = await deleteIdPGroupMapping(client, "m-1");
    expect(del).toHaveBeenCalledWith("/api/v0/auth/admin/idp-group-mappings/m-1");
    expect(ok).toBe(true);
  });

  it("deleteIdPGroupMapping URL-encodes the mapping_ref", async () => {
    const del = vi.fn(async () => undefined);
    const client = { delete: del } as unknown as EshuApiClient;
    await deleteIdPGroupMapping(client, "m/1 ref");
    expect(del).toHaveBeenCalledWith("/api/v0/auth/admin/idp-group-mappings/m%2F1%20ref");
  });

  it("revokeApiToken posts (no content) to /api/v0/auth/local/api-tokens/{id}/revoke", async () => {
    const postNoContent = vi.fn(async () => undefined);
    const client = { postNoContent } as unknown as EshuApiClient;
    const ok = await revokeApiToken(client, "t-1");
    expect(postNoContent).toHaveBeenCalledWith("/api/v0/auth/local/api-tokens/t-1/revoke", {});
    expect(ok).toBe(true);
  });

  it("revokeApiToken returns false on error", async () => {
    const client = { postNoContent: async () => { throw new Error("nope"); } } as unknown as EshuApiClient;
    expect(await revokeApiToken(client, "t-1")).toBe(false);
  });
});
