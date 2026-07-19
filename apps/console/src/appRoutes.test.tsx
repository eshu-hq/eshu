// appRoutes.test.tsx
// Regression coverage for the /admin route wiring (#4967 review follow-up):
// AdminPage needs baseUrl to render the read-only OIDC redirect URI / SAML
// SP entity id + ACS URL an operator copies into their IdP. Proves the value
// actually reaches ProviderConfigDrawer through AppRoutes -> AdminPage ->
// AdminIdentityAccessPanel -> AdminProvidersPanel, not just that the prop
// exists on AdminPage's signature.
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, it, expect, vi } from "vitest";

import type { BrowserSessionAuth, EshuApiClient } from "./api/client";
import { AppRoutes } from "./appRoutes";
import { demoModel } from "./console/demoModel";

function makeClient(): EshuApiClient {
  return {
    getJson: vi.fn(async (path: string) => {
      if (path.includes("provider-configs")) return { provider_configs: [], truncated: false };
      if (path.includes("idp-group-mappings")) return { group_mappings: [] };
      return {};
    }),
  } as unknown as EshuApiClient;
}

describe("AppRoutes /admin baseUrl wiring", () => {
  it("threads source.base into AdminPage so the Add-provider drawer renders the real redirect URI", async () => {
    const client = makeClient();
    render(
      <MemoryRouter initialEntries={["/admin"]}>
        <AppRoutes
          model={demoModel}
          client={client}
          source={{
            base: "https://custom-eshu.example.test",
            key: "",
            mode: "private",
            status: "connected",
            msg: "",
          }}
          repositories={[]}
          onOpenService={vi.fn()}
        />
      </MemoryRouter>,
    );

    // AdminIdentityAccessPanel is lazy-loaded, so wait for it to resolve.
    await waitFor(() => expect(screen.getByText("Identity & Access")).toBeInTheDocument());
    fireEvent.click(await screen.findByRole("button", { name: "Add provider" }));

    // ProviderConfigDrawer is itself lazy-loaded; wait for the redirect URI
    // field to render with the base URL AppRoutes was given, not a stale or
    // empty default.
    const redirectInput = (await screen.findByLabelText("OIDC redirect URI")) as HTMLInputElement;
    expect(redirectInput.value).toBe("https://custom-eshu.example.test/api/v0/auth/oidc/callback");
  });
});

describe("AppRoutes /admin capability gating (#4969)", () => {
  function makeAuth(overrides: Partial<BrowserSessionAuth> = {}): BrowserSessionAuth {
    return {
      mode: "browser_session",
      all_scopes: false,
      permission_catalog_enforced: true,
      ...overrides,
    };
  }

  function renderAdmin(auth: BrowserSessionAuth | null | undefined): void {
    const client = {
      getJson: vi.fn(async () => ({})),
    } as unknown as EshuApiClient;
    render(
      <MemoryRouter initialEntries={["/admin"]}>
        <AppRoutes
          model={demoModel}
          client={client}
          source={{ base: "", key: "", mode: "private", status: "connected", msg: "" }}
          repositories={[]}
          onOpenService={vi.fn()}
          auth={auth}
        />
      </MemoryRouter>,
    );
  }

  it("shows the 403 access screen for a non-admin session under an enforced catalog", () => {
    const warnSpy = vi.spyOn(console, "warn").mockImplementation(() => {});
    renderAdmin(makeAuth({ allowed_permission_features: ["ask_search"] }));
    expect(
      screen.getByRole("heading", { name: "You don't have access to this area" }),
    ).toBeInTheDocument();
    expect(screen.queryByRole("heading", { level: 1, name: "Admin" })).not.toBeInTheDocument();
    warnSpy.mockRestore();
  });

  it("renders only the Tokens panel for a partial `tokens`-only grant", async () => {
    renderAdmin(makeAuth({ allowed_permission_features: ["tokens"] }));
    expect(screen.getByRole("heading", { level: 1, name: "Admin" })).toBeInTheDocument();
    await waitFor(() => expect(screen.getByText("API tokens")).toBeInTheDocument());
    expect(screen.queryByText("Invitations")).not.toBeInTheDocument();
    expect(screen.queryByText("Audit")).not.toBeInTheDocument();
  });

  it("shows the full Admin area unchanged for an all_scopes session", async () => {
    renderAdmin(makeAuth({ all_scopes: true }));
    expect(screen.getByRole("heading", { level: 1, name: "Admin" })).toBeInTheDocument();
    await waitFor(() => expect(screen.getByText("Invitations")).toBeInTheDocument());
    expect(screen.getByText("API tokens")).toBeInTheDocument();
    expect(screen.getByText("Audit")).toBeInTheDocument();
  });

  it("fails open (renders Admin) when the server does not report catalog enforcement", async () => {
    renderAdmin(makeAuth({ permission_catalog_enforced: false, allowed_permission_features: [] }));
    expect(screen.getByRole("heading", { level: 1, name: "Admin" })).toBeInTheDocument();
    await waitFor(() => expect(screen.getByText("Invitations")).toBeInTheDocument());
  });
});

describe("AppRoutes /dead-code performance boundary", () => {
  it("renders the route shell synchronously without a route-level lazy hop", () => {
    render(
      <MemoryRouter initialEntries={["/dead-code"]}>
        <AppRoutes
          client={undefined}
          model={demoModel}
          source={{ base: "", key: "", mode: "private", status: "connected", msg: "" }}
          repositories={[]}
          onOpenService={vi.fn()}
        />
      </MemoryRouter>,
    );

    expect(screen.getByRole("heading", { level: 2, name: "Dead code" })).toBeInTheDocument();
  });
});

describe("AppRoutes /impact performance boundary", () => {
  it("keeps the Impact implementation behind a route-level lazy loading state", async () => {
    render(
      <MemoryRouter initialEntries={["/impact"]}>
        <AppRoutes
          client={undefined}
          model={demoModel}
          source={{ base: "", key: "", mode: "private", status: "connected", msg: "" }}
          repositories={[]}
          onOpenService={vi.fn()}
        />
      </MemoryRouter>,
    );

    expect(screen.getByRole("heading", { level: 1, name: "Loading impact" })).toBeInTheDocument();
    expect(await screen.findByRole("heading", { level: 2, name: "Impact" })).toBeInTheDocument();
  });
});
