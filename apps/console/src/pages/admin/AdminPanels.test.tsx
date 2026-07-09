// pages/admin/AdminPanels.test.tsx
// Per-panel tests for the admin console (issue #3703). Each panel:
//   - renders rows from mocked loader data
//   - renders "unavailable" on a load error (never fabricated rows)
//   - drives mutations to the right endpoint and refetches the affected list
//   - never renders a secret/hash/invite-code/external-group name
import { render, screen, waitFor, within, fireEvent } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";

import { AdminAssignmentsPanel } from "./AdminAssignmentsPanel";
import { AdminAuditPanel } from "./AdminAuditPanel";
import { AdminIdPGroupMappingsPanel } from "./AdminIdPGroupMappingsPanel";
import { AdminInvitationsPanel } from "./AdminInvitationsPanel";
import { AdminRolesPanel } from "./AdminRolesPanel";
import { AdminTokensPanel } from "./AdminTokensPanel";
import type { EshuApiClient } from "../../api/client";
import { EshuApiHttpError } from "../../api/client";

const NOW = "2026-06-24T10:00:00Z";

beforeEach(() => {
  // confirm-then-call: auto-confirm so mutation tests proceed.
  vi.stubGlobal(
    "confirm",
    vi.fn(() => true),
  );
});
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

// ---------------------------------------------------------------------------
// Stale-client / unmount guard — load resolved after client swap must not commit
// ---------------------------------------------------------------------------

describe("stale-load guard", () => {
  it("AdminInvitationsPanel: load resolved after client change does not commit stale rows", async () => {
    // First client resolves slowly; second client resolves immediately with different data.
    let resolveFirst!: (v: unknown) => void;
    const slowGetJson = vi.fn(
      () =>
        new Promise((res) => {
          resolveFirst = res;
        }),
    );
    const fastGetJson = vi.fn(async () => ({
      invitations: [{ invite_id: "inv-new", role_id: "admin", status: "pending" }],
    }));

    const client1 = { getJson: slowGetJson } as unknown as EshuApiClient;
    const client2 = { getJson: fastGetJson } as unknown as EshuApiClient;

    const { rerender } = render(<AdminInvitationsPanel client={client1} />);
    // Swap to client2 before client1 resolves.
    rerender(<AdminInvitationsPanel client={client2} />);
    // Let client2 settle.
    expect(await screen.findByText("inv-new")).toBeInTheDocument();

    // Now resolve the stale client1 load with old data.
    resolveFirst({ invitations: [{ invite_id: "inv-old", role_id: "viewer", status: "pending" }] });

    // Stale data must never appear.
    await waitFor(() => expect(screen.queryByText("inv-old")).not.toBeInTheDocument());
    expect(screen.getByText("inv-new")).toBeInTheDocument();
  });

  it("AdminTokensPanel: load resolved after unmount does not commit rows", async () => {
    let resolveLoad!: (v: unknown) => void;
    const slowGetJson = vi.fn(
      () =>
        new Promise((res) => {
          resolveLoad = res;
        }),
    );
    const client = { getJson: slowGetJson } as unknown as EshuApiClient;
    const { unmount } = render(<AdminTokensPanel client={client} />);
    unmount();
    // Resolve after unmount — must not throw or commit state.
    resolveLoad({
      tokens: [{ token_id: "t-stale", token_class: "personal", status: "active", issued_at: NOW }],
    });
    // No assertion needed beyond not throwing; but also confirm no stale DOM.
    expect(document.body.innerHTML).not.toContain("t-stale");
  });

  it("AdminAssignmentsPanel: load resolved after client change does not commit stale rows", async () => {
    let resolveFirst!: (v: unknown) => void;
    const slowGetJson = vi.fn(
      () =>
        new Promise((res) => {
          resolveFirst = res;
        }),
    );
    const fastGetJson = vi.fn(async () => ({
      role_assignments: [{ user_id: "u-new", role_id: "admin", status: "active" }],
    }));

    const client1 = { getJson: slowGetJson } as unknown as EshuApiClient;
    const client2 = { getJson: fastGetJson } as unknown as EshuApiClient;

    const { rerender } = render(<AdminAssignmentsPanel client={client1} />);
    rerender(<AdminAssignmentsPanel client={client2} />);
    expect(await screen.findByText("u-new")).toBeInTheDocument();

    resolveFirst({ role_assignments: [{ user_id: "u-old", role_id: "viewer", status: "active" }] });
    await waitFor(() => expect(screen.queryByText("u-old")).not.toBeInTheDocument());
    expect(screen.getByText("u-new")).toBeInTheDocument();
  });
});

// ---------------------------------------------------------------------------
// Invitations
// ---------------------------------------------------------------------------

describe("AdminInvitationsPanel", () => {
  it("renders invitations from mocked data", async () => {
    const client = {
      getJson: async () => ({
        invitations: [
          { invite_id: "inv-1", role_id: "developer", status: "pending", expires_at: NOW },
        ],
      }),
    } as unknown as EshuApiClient;
    render(<AdminInvitationsPanel client={client} />);
    expect(await screen.findByText("inv-1")).toBeInTheDocument();
    expect(screen.getByText("developer")).toBeInTheDocument();
  });

  it("renders unavailable on load error", async () => {
    const client = {
      getJson: async () => {
        throw new Error("503");
      },
    } as unknown as EshuApiClient;
    render(<AdminInvitationsPanel client={client} />);
    expect(
      await screen.findByText("Invitations unavailable from this source."),
    ).toBeInTheDocument();
  });

  it("revoke posts to the revoke endpoint and refetches", async () => {
    const postJson = vi.fn(async () => ({ invite_id: "inv-1", status: "revoked", revoked: true }));
    let call = 0;
    const getJson = vi.fn(async () => {
      call += 1;
      return {
        invitations:
          call === 1
            ? [{ invite_id: "inv-1", role_id: "developer", status: "pending" }]
            : [{ invite_id: "inv-1", role_id: "developer", status: "revoked" }],
      };
    });
    const client = { getJson, postJson } as unknown as EshuApiClient;
    render(<AdminInvitationsPanel client={client} />);
    const btn = await screen.findByRole("button", { name: "Revoke" });
    fireEvent.click(btn);
    await waitFor(() =>
      expect(postJson).toHaveBeenCalledWith("/api/v0/auth/local/invitations/inv-1/revoke", {}),
    );
    // refetch ran (getJson called twice: initial + after mutation).
    await waitFor(() => expect(getJson).toHaveBeenCalledTimes(2));
    expect(await screen.findByText("Invitation inv-1 revoked.")).toBeInTheDocument();
  });
});

// ---------------------------------------------------------------------------
// Role assignments
// ---------------------------------------------------------------------------

describe("AdminAssignmentsPanel", () => {
  it("renders assignments and grants via the form to the right endpoint", async () => {
    const postJson = vi.fn(async () => ({ status: "active", changed: true }));
    const getJson = vi.fn(async () => ({
      role_assignments: [{ user_id: "u-1", role_id: "viewer", status: "active" }],
    }));
    const client = { getJson, postJson } as unknown as EshuApiClient;
    render(<AdminAssignmentsPanel client={client} />);
    expect(await screen.findByText("u-1")).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("User ID"), { target: { value: "u-2" } });
    fireEvent.change(screen.getByLabelText("Role ID"), { target: { value: "admin" } });
    fireEvent.click(screen.getByRole("button", { name: "Grant" }));
    await waitFor(() =>
      expect(postJson).toHaveBeenCalledWith("/api/v0/auth/admin/role-assignments", {
        user_id: "u-2",
        role_id: "admin",
      }),
    );
    await waitFor(() => expect(getJson).toHaveBeenCalledTimes(2));
  });

  it("revoke posts to role-assignments/revoke", async () => {
    const postJson = vi.fn(async () => ({ status: "revoked", changed: true }));
    const getJson = vi.fn(async () => ({
      role_assignments: [{ user_id: "u-1", role_id: "viewer", status: "active" }],
    }));
    const client = { getJson, postJson } as unknown as EshuApiClient;
    render(<AdminAssignmentsPanel client={client} />);
    const btn = await screen.findByRole("button", { name: "Revoke" });
    fireEvent.click(btn);
    await waitFor(() =>
      expect(postJson).toHaveBeenCalledWith("/api/v0/auth/admin/role-assignments/revoke", {
        user_id: "u-1",
        role_id: "viewer",
      }),
    );
  });

  it("renders unavailable on load error", async () => {
    const client = {
      getJson: async () => {
        throw new Error("503");
      },
    } as unknown as EshuApiClient;
    render(<AdminAssignmentsPanel client={client} />);
    expect(
      await screen.findByText("Role assignments unavailable from this source."),
    ).toBeInTheDocument();
  });
});

// ---------------------------------------------------------------------------
// Roles & grants (read-only)
// ---------------------------------------------------------------------------

describe("AdminRolesPanel", () => {
  it("renders roles and grants but no secret hashes", async () => {
    const client = {
      getJson: async () => ({
        roles: [
          {
            role_id: "admin",
            status: "active",
            built_in: true,
            grants: [
              { action: "read", feature: "ask_search", data_class: "public", scope_class: "all" },
            ],
          },
        ],
      }),
    } as unknown as EshuApiClient;
    render(<AdminRolesPanel client={client} />);
    expect(await screen.findByText("admin")).toBeInTheDocument();
    expect(screen.getByText("built-in")).toBeInTheDocument();
    expect(document.body.innerHTML).not.toContain("role_key_hash");
    expect(document.body.innerHTML).not.toContain("policy_revision_hash");
  });

  it("renders unavailable on load error", async () => {
    const client = {
      getJson: async () => {
        throw new Error("503");
      },
    } as unknown as EshuApiClient;
    render(<AdminRolesPanel client={client} />);
    expect(await screen.findByText("Roles unavailable from this source.")).toBeInTheDocument();
  });
});

// ---------------------------------------------------------------------------
// IdP providers — see AdminProvidersPanel.test.tsx (#4967, full CRUD panel
// against the #4966 provider-configs API).
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// IdP group mappings
// ---------------------------------------------------------------------------

describe("AdminIdPGroupMappingsPanel", () => {
  it("renders mapping_ref but never the external group name", async () => {
    const client = {
      getJson: async () => ({
        group_mappings: [
          {
            mapping_ref: "m-ref-1",
            provider_config_id: "p-1",
            role_id: "viewer",
            status: "active",
          },
        ],
      }),
    } as unknown as EshuApiClient;
    render(<AdminIdPGroupMappingsPanel client={client} />);
    expect(await screen.findByText("m-ref-1")).toBeInTheDocument();
    const body = document.body.innerHTML;
    for (const f of ["external_group", "external_group_hash", "group_name"]) {
      expect(body).not.toContain(f);
    }
  });

  it("create posts to the mappings endpoint and clears the external group input", async () => {
    const postJson = vi.fn(async () => ({ mapping_ref: "m-2", status: "active", created: true }));
    const getJson = vi.fn(async () => ({ group_mappings: [] }));
    const client = { getJson, postJson } as unknown as EshuApiClient;
    render(<AdminIdPGroupMappingsPanel client={client} />);
    await screen.findByText("No group mappings found.");

    fireEvent.change(screen.getByLabelText("Provider config ID"), { target: { value: "p-1" } });
    const groupInput = screen.getByLabelText("External group") as HTMLInputElement;
    fireEvent.change(groupInput, { target: { value: "engineers" } });
    fireEvent.change(screen.getByLabelText("Role ID"), { target: { value: "developer" } });
    fireEvent.click(screen.getByRole("button", { name: "Create" }));

    await waitFor(() =>
      expect(postJson).toHaveBeenCalledWith("/api/v0/auth/admin/idp-group-mappings", {
        provider_config_id: "p-1",
        external_group: "engineers",
        role_id: "developer",
      }),
    );
    await waitFor(() => expect(getJson).toHaveBeenCalledTimes(2));
    // The raw external group input is cleared after submit (never retained).
    await waitFor(() => expect(groupInput.value).toBe(""));
  });

  it("delete calls DELETE with the mapping_ref and refetches", async () => {
    const del = vi.fn(async () => undefined);
    const getJson = vi.fn(async () => ({
      group_mappings: [
        { mapping_ref: "m-ref-1", provider_config_id: "p-1", role_id: "v", status: "active" },
      ],
    }));
    const client = { getJson, delete: del } as unknown as EshuApiClient;
    render(<AdminIdPGroupMappingsPanel client={client} />);
    const btn = await screen.findByRole("button", { name: "Delete" });
    fireEvent.click(btn);
    await waitFor(() =>
      expect(del).toHaveBeenCalledWith("/api/v0/auth/admin/idp-group-mappings/m-ref-1"),
    );
    await waitFor(() => expect(getJson).toHaveBeenCalledTimes(2));
  });
});

// ---------------------------------------------------------------------------
// API tokens
// ---------------------------------------------------------------------------

describe("AdminTokensPanel", () => {
  it("renders tokens without token hashes", async () => {
    const client = {
      getJson: async () => ({
        tokens: [
          {
            token_id: "t-1",
            token_class: "personal",
            user_id: "u-1",
            status: "active",
            issued_at: NOW,
          },
        ],
      }),
    } as unknown as EshuApiClient;
    render(<AdminTokensPanel client={client} />);
    expect(await screen.findByText("t-1")).toBeInTheDocument();
    const body = document.body.innerHTML;
    for (const f of ["token_hash", "display_label_hash", "label_hash"]) {
      expect(body).not.toContain(f);
    }
  });

  it("revoke posts (no content) to the token revoke endpoint and refetches", async () => {
    const postNoContent = vi.fn(async () => undefined);
    let call = 0;
    const getJson = vi.fn(async () => {
      call += 1;
      return {
        tokens: [
          {
            token_id: "t-1",
            token_class: "personal",
            status: call === 1 ? "active" : "revoked",
            issued_at: NOW,
            ...(call === 1 ? {} : { revoked_at: NOW }),
          },
        ],
      };
    });
    const client = { getJson, postNoContent } as unknown as EshuApiClient;
    render(<AdminTokensPanel client={client} />);
    const btn = await screen.findByRole("button", { name: "Revoke" });
    fireEvent.click(btn);
    await waitFor(() =>
      expect(postNoContent).toHaveBeenCalledWith("/api/v0/auth/local/api-tokens/t-1/revoke", {}),
    );
    await waitFor(() => expect(getJson).toHaveBeenCalledTimes(2));
  });

  it("renders unavailable on load error", async () => {
    const client = {
      getJson: async () => {
        throw new Error("503");
      },
    } as unknown as EshuApiClient;
    render(<AdminTokensPanel client={client} />);
    expect(await screen.findByText("API tokens unavailable from this source.")).toBeInTheDocument();
  });
});

// ---------------------------------------------------------------------------
// Audit — 403 operator-scope note vs unavailable vs data
// ---------------------------------------------------------------------------

describe("AdminAuditPanel", () => {
  it("renders the operator-scope note (#3717) when audit returns 403", async () => {
    const client = {
      getJson: async () => {
        throw new EshuApiHttpError(403);
      },
    } as unknown as EshuApiClient;
    render(<AdminAuditPanel client={client} />);
    expect(
      await screen.findByText(/Global operator audit — not available for tenant admins \(#3717\)/),
    ).toBeInTheDocument();
    // It must NOT show a generic error.
    expect(screen.queryByText("Audit unavailable from this source.")).not.toBeInTheDocument();
  });

  it("renders unavailable when audit fails with a non-403 error", async () => {
    const client = {
      getJson: async () => {
        throw new EshuApiHttpError(503);
      },
    } as unknown as EshuApiClient;
    render(<AdminAuditPanel client={client} />);
    expect(await screen.findByText("Audit unavailable from this source.")).toBeInTheDocument();
  });

  it("renders audit events and summary when authorized", async () => {
    const client = {
      getJson: async (path: string) => {
        if (path.endsWith("/audit/summary")) {
          return { total: 5, allowed: 4, denied: 1, unavailable: 0, last_occurred_at: NOW };
        }
        return {
          events: [{ event_type: "authz", decision: "allow", reason_code: "ok", occurred_at: NOW }],
        };
      },
    } as unknown as EshuApiClient;
    render(<AdminAuditPanel client={client} />);
    const table = await screen.findByRole("table", { name: "Audit events" });
    expect(within(table).getByText("authz")).toBeInTheDocument();
    expect(screen.getByLabelText("Audit summary")).toBeInTheDocument();
  });
});
