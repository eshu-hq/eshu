// pages/ProfilePage.test.tsx
// Verifies ProfilePage:
//   - renders loading state initially
//   - renders identity / context / sessions / tokens from mocked data
//   - renders unavailable state on error for each section
//   - NEVER renders session_hash, token_hash, csrf, or credential handles
import { render, screen, waitFor, within } from "@testing-library/react";
import { describe, it, expect } from "vitest";

import { ProfilePage } from "./ProfilePage";
import type { EshuApiClient } from "../api/client";

// ---------------------------------------------------------------------------
// Test fixtures
// ---------------------------------------------------------------------------

const NOW = "2026-06-24T10:00:00Z";

const profileFixture = {
  external_provider_config_id: "oidc-config-xyz",
  active_tenant_id: "tenant_a",
  active_workspace_id: "workspace_a",
  role_ids: ["developer"],
  allowed_permission_features: ["ask_search"],
  permission_catalog_enforced: true,
  mfa: { has_active_mfa: true, factor_kind: "totp" },
  memberships: [{ tenant_id: "tenant_a", workspace_id: "workspace_a" }],
};

const sessionsFixture = {
  sessions: [
    {
      issued_at: NOW,
      last_seen_at: NOW,
      idle_expires_at: NOW,
      absolute_expires_at: NOW,
      tenant_id: "tenant_a",
      workspace_id: "workspace_a",
      current: true,
    },
  ],
};

const tokensFixture = {
  tokens: [
    {
      token_id: "tok-001",
      token_class: "personal",
      issued_at: NOW,
      expires_at: NOW,
    },
  ],
};

function makeClient(overrides: {
  profile?: unknown;
  sessions?: unknown;
  tokens?: unknown;
  throwAll?: boolean;
}): EshuApiClient {
  return {
    getJson: async (path: string) => {
      if (overrides.throwAll) throw new Error("HTTP 503");
      if (path === "/api/v0/auth/profile") {
        if (overrides.profile === undefined) return profileFixture;
        throw new Error("forced error");
      }
      if (path === "/api/v0/auth/sessions") {
        if (overrides.sessions === undefined) return sessionsFixture;
        throw new Error("forced error");
      }
      if (path === "/api/v0/auth/local/api-tokens") {
        if (overrides.tokens === undefined) return tokensFixture;
        throw new Error("forced error");
      }
      return {};
    },
  } as unknown as EshuApiClient;
}

function happyClient(): EshuApiClient {
  return makeClient({});
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe("ProfilePage", () => {
  it("shows loading state initially", () => {
    const client = {
      getJson: () => new Promise(() => {}),
    } as unknown as EshuApiClient;
    render(<ProfilePage client={client} />);
    expect(screen.getByText("Loading profile…")).toBeInTheDocument();
  });

  it("renders identity provider from profile", async () => {
    render(<ProfilePage client={happyClient()} />);
    await waitFor(() => expect(screen.getByText("oidc-config-xyz")).toBeInTheDocument());
  });

  it("renders 'Local' when external_provider_config_id is absent", async () => {
    const client = {
      getJson: async (path: string) => {
        if (path === "/api/v0/auth/profile")
          return {
            active_tenant_id: "t",
            active_workspace_id: "w",
            permission_catalog_enforced: false,
            mfa: { has_active_mfa: false },
            memberships: [],
          };
        if (path === "/api/v0/auth/sessions") return { sessions: [] };
        return { tokens: [] };
      },
    } as unknown as EshuApiClient;
    render(<ProfilePage client={client} />);
    await waitFor(() => expect(screen.getByText("Local")).toBeInTheDocument());
  });

  it("renders active tenant and workspace", async () => {
    const { container } = render(<ProfilePage client={happyClient()} />);
    // tenant_a/workspace_a legitimately appear in the active-context list AND in
    // the memberships/sessions tables, so scope the assertion to the active
    // context definition list to avoid ambiguous multi-match.
    await waitFor(() =>
      expect(container.querySelector('[aria-label="Active context details"]')).not.toBeNull(),
    );
    const activeContext = container.querySelector(
      '[aria-label="Active context details"]',
    ) as HTMLElement;
    expect(within(activeContext).getByText("tenant_a")).toBeInTheDocument();
    expect(within(activeContext).getByText("workspace_a")).toBeInTheDocument();
  });

  it("renders sessions table with current marker", async () => {
    render(<ProfilePage client={happyClient()} />);
    await waitFor(() =>
      expect(screen.getByRole("table", { name: "Browser sessions" })).toBeInTheDocument(),
    );
    expect(screen.getByText("current")).toBeInTheDocument();
  });

  it("renders tokens table with token_id but no Label column (hash-as-label removed)", async () => {
    render(<ProfilePage client={happyClient()} />);
    await waitFor(() =>
      expect(screen.getByRole("table", { name: "API tokens" })).toBeInTheDocument(),
    );
    expect(screen.getByText("tok-001")).toBeInTheDocument();
    // "Label" column was removed: SHA-256(display_label) must not be presented
    // as a human label. Real-label persistence tracked in #3708.
    expect(screen.queryByText("Label")).not.toBeInTheDocument();
  });

  it("labels an expired (not revoked) token 'expired', not 'active'", async () => {
    const client = {
      getJson: async (path: string) => {
        if (path === "/api/v0/auth/profile") return profileFixture;
        if (path === "/api/v0/auth/sessions") return { sessions: [] };
        return {
          tokens: [
            {
              token_id: "tok-expired",
              token_class: "personal",
              issued_at: "2020-01-01T00:00:00Z",
              expires_at: "2020-02-01T00:00:00Z",
            },
          ],
        };
      },
    } as unknown as EshuApiClient;
    render(<ProfilePage client={client} />);
    const table = await screen.findByRole("table", { name: "API tokens" });
    expect(within(table).getByText("expired")).toBeInTheDocument();
    expect(within(table).queryByText("active")).not.toBeInTheDocument();
  });

  it("renders unavailable state for all sections when all endpoints fail", async () => {
    const client = makeClient({ throwAll: true });
    render(<ProfilePage client={client} />);
    await waitFor(() =>
      expect(screen.getAllByText(/unavailable from this source/).length).toBeGreaterThan(0),
    );
  });

  it("renders sessions unavailable when sessions endpoint fails", async () => {
    const client = makeClient({ sessions: "throw" });
    render(<ProfilePage client={client} />);
    await waitFor(() =>
      expect(screen.getByText("Sessions unavailable from this source.")).toBeInTheDocument(),
    );
  });

  it("renders tokens unavailable when tokens endpoint fails", async () => {
    const client = makeClient({ tokens: "throw" });
    render(<ProfilePage client={client} />);
    await waitFor(() =>
      expect(screen.getByText("Tokens unavailable from this source.")).toBeInTheDocument(),
    );
  });

  it("never renders session_hash, token_hash, csrf, or credential handles", async () => {
    render(<ProfilePage client={happyClient()} />);
    await waitFor(() => expect(screen.queryByText("Loading profile…")).not.toBeInTheDocument());
    const body = document.body.innerHTML;
    const forbidden = [
      "session_hash",
      "csrf_token",
      "csrf_token_hash",
      "token_hash",
      "credential_handle",
      "password_hash",
      "recovery_code_hash",
      "mfa_hash",
    ];
    for (const f of forbidden) {
      expect(body).not.toContain(f);
    }
  });

  it("renders MFA enabled badge when mfa.has_active_mfa is true", async () => {
    render(<ProfilePage client={happyClient()} />);
    await waitFor(() => expect(screen.getByText("enabled")).toBeInTheDocument());
  });

  it("renders effective permissions (roles + granted families) when the catalog is enforced (#4969)", async () => {
    render(<ProfilePage client={happyClient()} />);
    await waitFor(() =>
      expect(screen.getByRole("heading", { name: "Effective permissions" })).toBeInTheDocument(),
    );
    const section = screen
      .getByRole("heading", { name: "Effective permissions" })
      .closest("section") as HTMLElement;
    // profileFixture: role_ids ["developer"], allowed_permission_features ["ask_search"].
    expect(within(section).getByText("developer")).toBeInTheDocument();
    expect(within(section).getByText("ask_search")).toBeInTheDocument();
  });

  it("shows the not-enforced note in effective permissions when the catalog is off (#4969)", async () => {
    const client = {
      getJson: async (path: string) => {
        if (path === "/api/v0/auth/profile")
          return {
            role_ids: ["developer"],
            allowed_permission_features: [],
            permission_catalog_enforced: false,
            mfa: { has_active_mfa: false },
            memberships: [],
          };
        if (path === "/api/v0/auth/sessions") return { sessions: [] };
        return { tokens: [] };
      },
    } as unknown as EshuApiClient;
    render(<ProfilePage client={client} />);
    await waitFor(() =>
      expect(screen.getByRole("heading", { name: "Effective permissions" })).toBeInTheDocument(),
    );
    expect(screen.getByText(/Catalog not enforced/)).toBeInTheDocument();
  });

  it("renders MFA none badge when mfa.has_active_mfa is false", async () => {
    const client = {
      getJson: async (path: string) => {
        if (path === "/api/v0/auth/profile")
          return {
            permission_catalog_enforced: false,
            mfa: { has_active_mfa: false },
            memberships: [],
          };
        if (path === "/api/v0/auth/sessions") return { sessions: [] };
        return { tokens: [] };
      },
    } as unknown as EshuApiClient;
    render(<ProfilePage client={client} />);
    await waitFor(() => expect(screen.getByText("none")).toBeInTheDocument());
  });
});
