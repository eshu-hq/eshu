// pages/AdminPage.test.tsx
// Verifies the AdminPage shell composes every panel and that, with all loaders
// failing, no fabricated data appears and every panel degrades to "unavailable"
// (the audit panel's 403 case is covered in admin/AdminPanels.test.tsx).
import { render, screen, waitFor } from "@testing-library/react";
import { describe, it, expect } from "vitest";

import { AdminPage } from "./AdminPage";
import type { BrowserSessionAuth, EshuApiClient } from "../api/client";

function makeAuth(overrides: Partial<BrowserSessionAuth> = {}): BrowserSessionAuth {
  return {
    mode: "browser_session",
    all_scopes: false,
    permission_catalog_enforced: true,
    ...overrides,
  };
}

describe("AdminPage", () => {
  it("renders the page heading and every panel title", async () => {
    const client = {
      getJson: async () => ({}), // empty-but-ok payloads → empty panels
    } as unknown as EshuApiClient;
    render(<AdminPage client={client} />);
    expect(screen.getByRole("heading", { level: 1, name: "Admin" })).toBeInTheDocument();
    await waitFor(() => expect(screen.getByText("Invitations")).toBeInTheDocument());
    expect(screen.getByText("Role assignments")).toBeInTheDocument();
    expect(screen.getByText("Roles & grants")).toBeInTheDocument();
    // AdminIdentityAccessPanel is lazy-loaded (bundle-budget gate), so its
    // content arrives after an extra microtask/chunk-resolution tick.
    expect(await screen.findByText("Identity & Access")).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "Providers" })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "Group → role mappings" })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "Sign-in policy" })).toBeInTheDocument();
    expect(screen.getByText("API tokens")).toBeInTheDocument();
    expect(screen.getByText("Audit")).toBeInTheDocument();
  });

  it("degrades every list panel to 'unavailable' when loaders fail (no fabrication)", async () => {
    const client = {
      getJson: async () => {
        throw new Error("503");
      },
    } as unknown as EshuApiClient;
    render(<AdminPage client={client} />);
    await waitFor(() =>
      expect(screen.getAllByText(/unavailable from this source/).length).toBeGreaterThan(0),
    );
  });

  it("renders unavailable when no client is provided", async () => {
    render(<AdminPage client={undefined} />);
    await waitFor(() =>
      expect(screen.getAllByText(/unavailable from this source/).length).toBeGreaterThan(0),
    );
  });

  it("renders only the Tokens panel for a `tokens`-only grant (#4969 partial grant)", async () => {
    const client = {
      getJson: async () => ({}),
    } as unknown as EshuApiClient;
    render(
      <AdminPage client={client} auth={makeAuth({ allowed_permission_features: ["tokens"] })} />,
    );
    await waitFor(() => expect(screen.getByText("API tokens")).toBeInTheDocument());
    expect(screen.queryByText("Invitations")).not.toBeInTheDocument();
    expect(screen.queryByText("Role assignments")).not.toBeInTheDocument();
    expect(screen.queryByText("Roles & grants")).not.toBeInTheDocument();
    expect(screen.queryByText("Identity & Access")).not.toBeInTheDocument();
    expect(screen.queryByText("Audit")).not.toBeInTheDocument();
  });

  it("renders every panel for a full admin session (all_scopes)", async () => {
    const client = {
      getJson: async () => ({}),
    } as unknown as EshuApiClient;
    render(<AdminPage client={client} auth={makeAuth({ all_scopes: true })} />);
    await waitFor(() => expect(screen.getByText("Invitations")).toBeInTheDocument());
    expect(screen.getByText("Role assignments")).toBeInTheDocument();
    expect(screen.getByText("Roles & grants")).toBeInTheDocument();
    expect(await screen.findByText("Identity & Access")).toBeInTheDocument();
    expect(screen.getByText("API tokens")).toBeInTheDocument();
    expect(screen.getByText("Audit")).toBeInTheDocument();
  });

  it("renders no panels when the catalog is enforced and no admin family is granted", () => {
    const client = {
      getJson: async () => ({}),
    } as unknown as EshuApiClient;
    render(<AdminPage client={client} auth={makeAuth({ allowed_permission_features: [] })} />);
    expect(screen.queryByText("Invitations")).not.toBeInTheDocument();
    expect(screen.queryByText("API tokens")).not.toBeInTheDocument();
    expect(screen.queryByText("Audit")).not.toBeInTheDocument();
  });
});
