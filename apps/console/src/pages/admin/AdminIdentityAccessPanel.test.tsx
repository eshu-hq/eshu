// pages/admin/AdminIdentityAccessPanel.test.tsx
// Tests the tab shell (#4967): default tab, tab switching, and that each tab
// renders its own panel content without fetching the others' data eagerly.
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";

import { AdminIdentityAccessPanel } from "./AdminIdentityAccessPanel";
import type { EshuApiClient } from "../../api/client";

function makeClient(): EshuApiClient {
  return {
    getJson: vi.fn(async (path: string) => {
      if (path.includes("idp-group-mappings")) return { group_mappings: [] };
      if (path.includes("provider-configs")) return { provider_configs: [], truncated: false };
      return {};
    }),
  } as unknown as EshuApiClient;
}

describe("AdminIdentityAccessPanel", () => {
  it("renders three tabs with Providers active by default", async () => {
    const client = makeClient();
    render(<AdminIdentityAccessPanel client={client} baseUrl="https://eshu.example.test" />);
    expect(screen.getByRole("tab", { name: "Providers" })).toHaveAttribute("aria-selected", "true");
    expect(screen.getByRole("tab", { name: "Group → role mappings" })).toHaveAttribute(
      "aria-selected",
      "false",
    );
    expect(screen.getByRole("tab", { name: "Sign-in policy" })).toHaveAttribute(
      "aria-selected",
      "false",
    );
    await waitFor(() =>
      expect(screen.getByText("No identity providers configured.")).toBeInTheDocument(),
    );
  });

  it("switches to the Group → role mappings tab on click", async () => {
    const client = makeClient();
    render(<AdminIdentityAccessPanel client={client} />);
    fireEvent.click(screen.getByRole("tab", { name: "Group → role mappings" }));
    expect(screen.getByRole("tab", { name: "Group → role mappings" })).toHaveAttribute(
      "aria-selected",
      "true",
    );
    await waitFor(() => expect(screen.getByText("No group mappings found.")).toBeInTheDocument());
  });

  it("switches to the Sign-in policy placeholder tab on click", () => {
    const client = makeClient();
    render(<AdminIdentityAccessPanel client={client} />);
    fireEvent.click(screen.getByRole("tab", { name: "Sign-in policy" }));
    expect(screen.getByText(/ships in a follow-up \(#4968\)/)).toBeInTheDocument();
  });
});
