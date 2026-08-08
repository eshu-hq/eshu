// pages/admin/AdminPanelsClientSwap.test.tsx
// A confirmation is only valid for the source it was opened against (#5187).
//
// The native confirm() these panels used to call blocked the renderer main
// thread, so the operator could not reach the data-source control while a
// prompt was open. The in-app dialog does not block, which opens a window the
// old code did not have: App.activateDemo() (and any private reconnect) calls
// setClient(...) without unmounting the route, so the panel re-renders against
// client B while a dialog opened against client A is still on screen. The
// mutation handler captured client A in its closure, so accepting afterwards
// would send a destructive request to the source the operator is no longer
// looking at.
//
// Each panel therefore cancels a pending confirmation when its client changes.
import { render, screen, waitFor, within, fireEvent } from "@testing-library/react";
import { describe, it, expect, vi, afterEach } from "vitest";

import { AdminInvitationsPanel } from "./AdminInvitationsPanel";
import { AdminTokensPanel } from "./AdminTokensPanel";
import type { EshuApiClient } from "../../api/client";

const NOW = "2026-06-24T10:00:00Z";

afterEach(() => {
  vi.restoreAllMocks();
});

describe("confirmation is scoped to the client it was opened against", () => {
  it("AdminTokensPanel: swapping the client cancels a pending revoke", async () => {
    const revokeA = vi.fn(async () => undefined);
    const tokenRows = {
      tokens: [{ token_id: "t-1", token_class: "personal", status: "active", issued_at: NOW }],
    };
    const clientA = {
      getJson: vi.fn(async () => tokenRows),
      postNoContent: revokeA,
    } as unknown as EshuApiClient;
    const clientB = {
      getJson: vi.fn(async () => tokenRows),
      postNoContent: vi.fn(async () => undefined),
    } as unknown as EshuApiClient;

    const view = render(<AdminTokensPanel client={clientA} />);
    fireEvent.click(await screen.findByRole("button", { name: "Revoke" }));
    expect(await screen.findByRole("alertdialog")).toBeInTheDocument();

    // The operator switches data source while the dialog is open. The panel
    // drops to its loading branch, which does not render the dialog, so the
    // check that matters is what happens once client B's rows land: an
    // unsettled confirmation would render straight back onto the new source.
    view.rerender(<AdminTokensPanel client={clientB} />);
    await screen.findByRole("button", { name: "Revoke" });

    expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();

    // Belt and braces: if a dialog does survive, accepting it must not reach
    // the source the operator navigated away from.
    const stale = screen.queryByRole("alertdialog");
    if (stale !== null) {
      fireEvent.click(within(stale).getByRole("button", { name: "Confirm" }));
    }
    await waitFor(() => expect(revokeA).not.toHaveBeenCalled());
  });

  it("AdminInvitationsPanel: swapping the client cancels a pending revoke", async () => {
    const revokeA = vi.fn(async () => ({ invite_id: "inv-1", revoked: true }));
    const rows = { invitations: [{ invite_id: "inv-1", role_id: "developer", status: "pending" }] };
    const clientA = {
      getJson: vi.fn(async () => rows),
      postJson: revokeA,
    } as unknown as EshuApiClient;
    const clientB = {
      getJson: vi.fn(async () => rows),
      postJson: vi.fn(async () => ({})),
    } as unknown as EshuApiClient;

    const view = render(<AdminInvitationsPanel client={clientA} />);
    fireEvent.click(await screen.findByRole("button", { name: "Revoke" }));
    expect(await screen.findByRole("alertdialog")).toBeInTheDocument();

    view.rerender(<AdminInvitationsPanel client={clientB} />);
    await screen.findByRole("button", { name: "Revoke" });

    expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
    await waitFor(() => expect(revokeA).not.toHaveBeenCalled());
  });
});
