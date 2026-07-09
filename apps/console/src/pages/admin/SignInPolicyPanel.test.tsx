// pages/admin/SignInPolicyPanel.test.tsx
// Tests for the sign-in policy panel (#4968, epic #4962). Covers: rendering
// current policy state, "unavailable" on load failure (never fabricated
// data), toggling each policy field, a guardrail rejection surfacing the
// server's exact message (never a client-side re-derivation of the
// guardrail), and the idle/absolute timeout minutes<->seconds conversion.
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";

import { SignInPolicyPanel } from "./SignInPolicyPanel";
import type { EshuApiClient } from "../../api/client";

const basePolicy = {
  tenant_id: "tenant_a",
  require_sso: false,
  allow_local_user_creation: true,
  require_mfa_for_all_users: false,
  idle_timeout_seconds: 0,
  absolute_timeout_seconds: 0,
  policy_revision_hash: "sha256:rev1",
  updated_at: "2026-06-01T00:00:00Z",
};

function makeClient(
  overrides: {
    getJson?: (path: string) => Promise<unknown>;
    patchJson?: (path: string, body: unknown) => Promise<unknown>;
  } = {},
): EshuApiClient {
  const getJson = overrides.getJson ?? (async () => basePolicy);
  const patchJson = overrides.patchJson ?? (async () => basePolicy);
  return { getJson, patchJson } as unknown as EshuApiClient;
}

describe("SignInPolicyPanel", () => {
  it("renders the current policy toggle and timeout state", async () => {
    const client = makeClient({
      getJson: async () => ({
        ...basePolicy,
        require_sso: true,
        allow_local_user_creation: false,
        require_mfa_for_all_users: true,
        idle_timeout_seconds: 900,
        absolute_timeout_seconds: 7200,
        sso_admin_verified_at: "2026-05-01T00:00:00Z",
        sso_admin_verified_provider_config_id: "pc_abc",
      }),
    });
    render(<SignInPolicyPanel client={client} />);

    await waitFor(() => expect(screen.getByLabelText("Require SSO for sign-in")).toBeChecked());
    expect(screen.getByLabelText("Allow local user creation")).not.toBeChecked();
    expect(screen.getByLabelText("Require MFA for all users")).toBeChecked();
    expect(
      (screen.getByLabelText("Idle session timeout (minutes)") as HTMLInputElement).value,
    ).toBe("15");
    expect(
      (screen.getByLabelText("Absolute session timeout (minutes)") as HTMLInputElement).value,
    ).toBe("120");
    expect(screen.getByText(/SSO admin proof via pc_abc/)).toBeInTheDocument();
  });

  it("shows unavailable rather than a fabricated policy on load failure", async () => {
    const client = makeClient({
      getJson: async () => {
        throw new Error("503");
      },
    });
    render(<SignInPolicyPanel client={client} />);
    await waitFor(() =>
      expect(screen.getByText("Sign-in policy unavailable from this source.")).toBeInTheDocument(),
    );
  });

  it("shows a no-SSO-proof-yet hint when sso_admin_verified_at is absent", async () => {
    render(<SignInPolicyPanel client={makeClient()} />);
    await waitFor(() =>
      expect(screen.getByText(/No admin has signed in via SSO yet/)).toBeInTheDocument(),
    );
  });

  it("toggling Allow local user creation calls patchJson with the field and updates state", async () => {
    const patchJson = vi.fn(async () => ({ ...basePolicy, allow_local_user_creation: false }));
    const client = makeClient({ patchJson });
    render(<SignInPolicyPanel client={client} />);

    const checkbox = await screen.findByLabelText("Allow local user creation");
    fireEvent.click(checkbox);

    await waitFor(() =>
      expect(patchJson).toHaveBeenCalledWith("/api/v0/auth/admin/sign-in-policy", {
        allow_local_user_creation: false,
      }),
    );
    await waitFor(() =>
      expect(screen.getByLabelText("Allow local user creation")).not.toBeChecked(),
    );
    expect(screen.getByText("Sign-in policy updated.")).toBeInTheDocument();
  });

  it("surfaces the server's exact guardrail rejection message, not a client-derived one", async () => {
    const patchJson = vi.fn(async () => {
      throw new Error("require_sso cannot be enabled: no admin has signed in via SSO yet");
    });
    const client = makeClient({ patchJson });
    render(<SignInPolicyPanel client={client} />);

    const checkbox = await screen.findByLabelText("Require SSO for sign-in");
    fireEvent.click(checkbox);

    expect(
      await screen.findByText("require_sso cannot be enabled: no admin has signed in via SSO yet"),
    ).toBeInTheDocument();
    // The checkbox must reflect the server's rejection (still unchecked) —
    // never optimistically show require_sso as on when the server refused it.
    expect(screen.getByLabelText("Require SSO for sign-in")).not.toBeChecked();
  });

  it("converts idle timeout minutes to seconds on blur", async () => {
    const patchJson = vi.fn(async () => ({ ...basePolicy, idle_timeout_seconds: 600 }));
    const client = makeClient({ patchJson });
    render(<SignInPolicyPanel client={client} />);

    const input = await screen.findByLabelText("Idle session timeout (minutes)");
    fireEvent.change(input, { target: { value: "10" } });
    fireEvent.blur(input);

    await waitFor(() =>
      expect(patchJson).toHaveBeenCalledWith("/api/v0/auth/admin/sign-in-policy", {
        idle_timeout_seconds: 600,
      }),
    );
  });

  it("renders nothing to configure state when no client is provided", () => {
    render(<SignInPolicyPanel />);
    expect(screen.getByText("Sign-in policy unavailable from this source.")).toBeInTheDocument();
  });
});
