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

import type { EshuApiClient } from "./api/client";
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
