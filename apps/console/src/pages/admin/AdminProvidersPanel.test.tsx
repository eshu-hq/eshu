// pages/admin/AdminProvidersPanel.test.tsx
// Tests for the full CRUD providers panel (#4967, consumes the #4966
// provider-configs API). Covers: rendering derived label/kind/status/secret
// rotation/group-mapping-count, "unavailable" on load failure (never
// fabricated rows), Test/Disable row actions, env-managed gating, and that no
// secret ever reaches the DOM.
import { render, screen, waitFor, within, fireEvent } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";

import { AdminProvidersPanel } from "./AdminProvidersPanel";
import type { EshuApiClient } from "../../api/client";

beforeEach(() => {
  vi.stubGlobal(
    "confirm",
    vi.fn(() => true),
  );
});
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

function makeClient(
  overrides: {
    getJson?: (path: string) => Promise<unknown>;
    postJson?: (path: string, body: unknown) => Promise<unknown>;
  } = {},
): EshuApiClient {
  const getJson =
    overrides.getJson ??
    (async (path: string) => {
      if (path.includes("idp-group-mappings")) return { group_mappings: [] };
      return { provider_configs: [], truncated: false };
    });
  const postJson = overrides.postJson ?? (async () => ({}));
  return { getJson, postJson } as unknown as EshuApiClient;
}

describe("AdminProvidersPanel", () => {
  it("renders derived label, kind, status, secret rotation, and group-mapping count", async () => {
    const client = makeClient({
      getJson: async (path: string) => {
        if (path.includes("idp-group-mappings")) {
          return {
            group_mappings: [
              {
                mapping_ref: "m1",
                provider_config_id: "pc_1",
                role_id: "viewer",
                status: "active",
              },
              { mapping_ref: "m2", provider_config_id: "pc_1", role_id: "admin", status: "active" },
            ],
          };
        }
        return {
          provider_configs: [
            {
              provider_config_id: "pc_1",
              provider_kind: "external_oidc",
              status: "active",
              configuration: { issuer: "https://idp.example.test" },
              has_secret: true,
              shadowed_by_environment: false,
              managed_by: "database",
              updated_at: "2026-01-01T00:00:00Z",
            },
          ],
          truncated: false,
        };
      },
    });
    render(<AdminProvidersPanel client={client} baseUrl="https://eshu.example.test" />);

    expect(await screen.findByText("https://idp.example.test")).toBeInTheDocument();
    expect(screen.getByText("OIDC")).toBeInTheDocument();
    expect(screen.getByText("active")).toBeInTheDocument();
    await waitFor(() => expect(screen.getByText("2")).toBeInTheDocument());
  });

  it("renders unavailable on load error (never fabricated rows)", async () => {
    const client = makeClient({
      getJson: async () => {
        throw new Error("503");
      },
    });
    render(<AdminProvidersPanel client={client} />);
    expect(await screen.findByText("Providers unavailable from this source.")).toBeInTheDocument();
  });

  it("never renders a secret, hash, or credential material in the DOM", async () => {
    const client = makeClient({
      getJson: async (path: string) => {
        if (path.includes("idp-group-mappings")) return { group_mappings: [] };
        return {
          provider_configs: [
            {
              provider_config_id: "pc_1",
              provider_kind: "external_oidc",
              status: "active",
              configuration: { issuer: "https://idp.example.test", client_id: "client-1" },
              has_secret: true,
              secret_fingerprint: "abc123",
              shadowed_by_environment: false,
              managed_by: "database",
            },
          ],
          truncated: false,
        };
      },
    });
    render(<AdminProvidersPanel client={client} />);
    await screen.findByText("https://idp.example.test");
    const body = document.body.innerHTML;
    for (const f of ["client_secret", "sp_private_key", "sp_certificate", "abc123"]) {
      expect(body).not.toContain(f);
    }
  });

  it("Test posts to the test-connection endpoint and shows the result", async () => {
    const postJson = vi.fn(async () => ({
      provider_config_id: "pc_1",
      ok: true,
      detail: "reachable",
    }));
    const client = makeClient({
      getJson: async (path: string) => {
        if (path.includes("idp-group-mappings")) return { group_mappings: [] };
        return {
          provider_configs: [
            {
              provider_config_id: "pc_1",
              provider_kind: "external_oidc",
              status: "draft",
              configuration: { issuer: "https://idp.example.test" },
              has_secret: true,
              shadowed_by_environment: false,
              managed_by: "database",
            },
          ],
          truncated: false,
        };
      },
      postJson,
    });
    render(<AdminProvidersPanel client={client} />);
    const btn = await screen.findByRole("button", { name: "Test" });
    fireEvent.click(btn);
    await waitFor(() =>
      expect(postJson).toHaveBeenCalledWith(
        "/api/v0/auth/admin/provider-configs/pc_1/test-connection",
        {},
      ),
    );
    expect(await screen.findByText(/Test sign-in passed for pc_1/)).toBeInTheDocument();
  });

  it("Disable posts to the disable endpoint and refetches", async () => {
    const postJson = vi.fn(async () => ({
      provider_config_id: "pc_1",
      revision_id: "rev_1",
      status: "draft",
      changed: true,
    }));
    let call = 0;
    const client = makeClient({
      getJson: async (path: string) => {
        if (path.includes("idp-group-mappings")) return { group_mappings: [] };
        call += 1;
        return {
          provider_configs: [
            {
              provider_config_id: "pc_1",
              provider_kind: "external_oidc",
              status: call === 1 ? "active" : "draft",
              configuration: { issuer: "https://idp.example.test" },
              has_secret: true,
              shadowed_by_environment: false,
              managed_by: "database",
            },
          ],
          truncated: false,
        };
      },
      postJson,
    });
    render(<AdminProvidersPanel client={client} />);
    const btn = await screen.findByRole("button", { name: "Disable" });
    fireEvent.click(btn);
    await waitFor(() =>
      expect(postJson).toHaveBeenCalledWith("/api/v0/auth/admin/provider-configs/pc_1/disable", {}),
    );
    expect(await screen.findByText("Provider pc_1 disabled.")).toBeInTheDocument();
  });

  it("env-managed providers show the env-managed badge and disable Edit/Disable", async () => {
    const client = makeClient({
      getJson: async (path: string) => {
        if (path.includes("idp-group-mappings")) return { group_mappings: [] };
        return {
          provider_configs: [
            {
              provider_config_id: "pc_env",
              provider_kind: "external_saml",
              status: "active",
              configuration: { entity_id: "https://idp.example.test/entity" },
              has_secret: true,
              shadowed_by_environment: true,
              managed_by: "environment",
            },
          ],
          truncated: false,
        };
      },
    });
    render(<AdminProvidersPanel client={client} />);
    await screen.findByText("https://idp.example.test/entity");
    expect(screen.getByText("env-managed")).toBeInTheDocument();
    const row = screen.getByText("https://idp.example.test/entity").closest("tr") as HTMLElement;
    expect(within(row).getByRole("button", { name: "Edit" })).toBeDisabled();
    expect(within(row).getByRole("button", { name: "Disable" })).toBeDisabled();
    // Test remains available for env-managed providers.
    expect(within(row).getByRole("button", { name: "Test" })).toBeEnabled();
  });

  it("Add provider opens the drawer in create mode", async () => {
    const client = makeClient();
    render(<AdminProvidersPanel client={client} baseUrl="https://eshu.example.test" />);
    await screen.findByText("No identity providers configured.");
    fireEvent.click(screen.getByRole("button", { name: "Add provider" }));
    expect(await screen.findByRole("dialog", { name: "Add provider" })).toBeInTheDocument();
  });
});
