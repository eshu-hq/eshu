// LoginPage.sso.test.tsx — TDD tests for the provider-selection SSO UX (#3682).
// These tests validate the provider-discovery fetch, button rendering, and
// begin*Login redirect behavior. They are separate from LoginPage.test.tsx so
// the Slice-A local-login tests remain focused and stable.
//
// Provider API shape: GET /api/v0/auth/providers → { providers: Array<{ provider_config_id, display_label, provider_kind }> }
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";

import { LoginPage } from "./LoginPage";
import type { EshuApiClient, BrowserSessionResponse } from "../api/client";

const mockSession: BrowserSessionResponse = {
  auth: { mode: "browser_session", tenant_id: "t", workspace_id: "w", all_scopes: true },
};

function makeClient(overrides: Partial<EshuApiClient> = {}): EshuApiClient {
  return {
    postJson: vi.fn(async () => ({
      status: "authenticated",
      auth: mockSession.auth,
      csrf_token: "tok",
    })),
    logoutBrowserSession: vi.fn(async () => undefined),
    getBrowserSession: vi.fn(async () => mockSession),
    getJson: vi.fn(async () => ({ providers: [] })),
    ...overrides,
  } as unknown as EshuApiClient;
}

function renderLogin(client: EshuApiClient, onSuccess = vi.fn()): void {
  render(
    <MemoryRouter>
      <LoginPage client={client} onSuccess={onSuccess} baseUrl="http://localhost:8080" />
    </MemoryRouter>,
  );
}

describe("LoginPage SSO provider discovery (#3682)", () => {
  it("renders an SSO button for each discovered OIDC provider", async () => {
    const client = makeClient({
      getJson: vi.fn(async () => ({
        providers: [
          {
            provider_config_id: "okta-dev",
            display_label: "Single sign-on (OIDC)",
            provider_kind: "oidc",
          },
        ],
      })) as unknown as EshuApiClient["getJson"],
    });
    renderLogin(client);

    const btn = await screen.findByRole("button", {
      name: /Continue with Single sign-on \(OIDC\)/i,
    });
    expect(btn).toBeInTheDocument();
  });

  it("renders an SSO button for each discovered SAML provider", async () => {
    const client = makeClient({
      getJson: vi.fn(async () => ({
        providers: [
          {
            provider_config_id: "saml-corp",
            display_label: "Single sign-on (SAML)",
            provider_kind: "saml",
          },
        ],
      })) as unknown as EshuApiClient["getJson"],
    });
    renderLogin(client);

    const btn = await screen.findByRole("button", {
      name: /Continue with Single sign-on \(SAML\)/i,
    });
    expect(btn).toBeInTheDocument();
  });

  it("renders no SSO buttons when providers list is empty", async () => {
    const client = makeClient({
      getJson: vi.fn(async () => ({ providers: [] })) as unknown as EshuApiClient["getJson"],
    });
    renderLogin(client);

    // Wait for fetch to settle, then verify no SSO buttons.
    await waitFor(() => {
      expect(screen.queryByText(/Continue with/i)).not.toBeInTheDocument();
    });
  });

  it("local login form is always present (default surface)", async () => {
    const client = makeClient({
      getJson: vi.fn(async () => ({
        providers: [
          {
            provider_config_id: "okta-dev",
            display_label: "Single sign-on (OIDC)",
            provider_kind: "oidc",
          },
        ],
      })) as unknown as EshuApiClient["getJson"],
    });
    renderLogin(client);

    // Local form fields must always be visible even when SSO buttons appear.
    expect(screen.getByLabelText(/login/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/password/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /sign in/i })).toBeInTheDocument();
  });

  it("clicking OIDC provider button triggers OIDC redirect URL construction", async () => {
    const redirectFn = vi.fn();
    const client = makeClient({
      getJson: vi.fn(async () => ({
        providers: [
          {
            provider_config_id: "okta-dev",
            display_label: "Single sign-on (OIDC)",
            provider_kind: "oidc",
          },
        ],
      })) as unknown as EshuApiClient["getJson"],
    });
    render(
      <MemoryRouter>
        <LoginPage
          client={client}
          onSuccess={vi.fn()}
          baseUrl="http://localhost:8080"
          redirectFn={redirectFn}
        />
      </MemoryRouter>,
    );

    const btn = await screen.findByRole("button", {
      name: /Continue with Single sign-on \(OIDC\)/i,
    });
    fireEvent.click(btn);

    // Should redirect to the OIDC login endpoint with provider_config_id param.
    expect(redirectFn).toHaveBeenCalledTimes(1);
    const redirectUrl = redirectFn.mock.calls[0][0] as string;
    expect(redirectUrl).toContain("/api/v0/auth/oidc/login");
    expect(redirectUrl).toContain("provider_config_id=okta-dev");
  });

  it("clicking SAML provider button triggers SAML redirect URL construction", async () => {
    const redirectFn = vi.fn();
    const client = makeClient({
      getJson: vi.fn(async () => ({
        providers: [
          {
            provider_config_id: "saml-corp",
            display_label: "Single sign-on (SAML)",
            provider_kind: "saml",
          },
        ],
      })) as unknown as EshuApiClient["getJson"],
    });
    render(
      <MemoryRouter>
        <LoginPage
          client={client}
          onSuccess={vi.fn()}
          baseUrl="http://localhost:8080"
          redirectFn={redirectFn}
        />
      </MemoryRouter>,
    );

    const btn = await screen.findByRole("button", {
      name: /Continue with Single sign-on \(SAML\)/i,
    });
    fireEvent.click(btn);

    expect(redirectFn).toHaveBeenCalledTimes(1);
    const redirectUrl = redirectFn.mock.calls[0][0] as string;
    expect(redirectUrl).toContain("/api/v0/auth/saml/providers/saml-corp/login");
  });

  it("fetches providers from GET /api/v0/auth/providers on mount (no tenant_id in URL)", async () => {
    // jsdom default: window.location.search = "" → no tenant_id → path has no param.
    const getJson = vi.fn(async () => ({ providers: [] })) as unknown as EshuApiClient["getJson"];
    const client = makeClient({ getJson });
    renderLogin(client);

    await waitFor(() => {
      expect(getJson).toHaveBeenCalledWith("/api/v0/auth/providers");
    });
  });

  it("passes tenant_id from URL search params when fetching providers", async () => {
    // Simulate ?tenant_id=my-org in the login page URL.
    const original = globalThis.location;
    Object.defineProperty(globalThis, "location", {
      configurable: true,
      value: { ...original, search: "?tenant_id=my-org" },
    });

    const getJson = vi.fn(async () => ({ providers: [] })) as unknown as EshuApiClient["getJson"];
    const client = makeClient({ getJson });
    renderLogin(client);

    await waitFor(() => {
      expect(getJson).toHaveBeenCalledWith(expect.stringContaining("tenant_id=my-org"));
    });

    Object.defineProperty(globalThis, "location", { configurable: true, value: original });
  });

  it("renders multiple SSO buttons when multiple providers are returned", async () => {
    const client = makeClient({
      getJson: vi.fn(async () => ({
        providers: [
          {
            provider_config_id: "oidc-1",
            display_label: "Single sign-on (OIDC)",
            provider_kind: "oidc",
          },
          {
            provider_config_id: "saml-1",
            display_label: "Single sign-on (SAML)",
            provider_kind: "saml",
          },
        ],
      })) as unknown as EshuApiClient["getJson"],
    });
    renderLogin(client);

    expect(
      await screen.findByRole("button", { name: /Continue with Single sign-on \(OIDC\)/i }),
    ).toBeInTheDocument();
    expect(
      await screen.findByRole("button", { name: /Continue with Single sign-on \(SAML\)/i }),
    ).toBeInTheDocument();
  });
});
