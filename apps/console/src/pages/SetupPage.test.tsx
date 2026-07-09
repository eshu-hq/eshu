// SetupPage.test.tsx — TDD tests for the first-run setup wizard UI (#4965).
// Mocks EshuApiClient.postJson, routing on the request path so the test
// exercises the real setupSession.ts helpers end to end, mirroring
// LoginPage.test.tsx's approach.
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";

import { SetupPage } from "./SetupPage";
import type { BrowserSessionResponse, EshuApiClient } from "../api/client";
import { EshuApiHttpError } from "../api/client";

function makeClient(
  postJsonImpl: (path: string, body: unknown) => Promise<unknown>,
): EshuApiClient {
  return {
    postJson: vi.fn(postJsonImpl),
  } as unknown as EshuApiClient;
}

function renderSetup(
  client: EshuApiClient,
  onSuccess: (session: BrowserSessionResponse) => void = vi.fn(),
): void {
  render(
    <MemoryRouter>
      <SetupPage client={client} onSuccess={onSuccess} />
    </MemoryRouter>,
  );
}

describe("SetupPage — step 1 (claim)", () => {
  it("renders the one-time credential form", () => {
    renderSetup(makeClient(async () => ({})));
    expect(screen.getByLabelText(/username/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/one-time password/i)).toBeInTheDocument();
  });

  it("advances to step 2 on a valid claim", async () => {
    const client = makeClient(async (path) => {
      if (path === "/api/v0/auth/setup/claim") return { status: "claimed" };
      throw new Error(`unexpected path ${path}`);
    });
    renderSetup(client);

    fireEvent.change(screen.getByLabelText(/username/i), { target: { value: "admin" } });
    fireEvent.change(screen.getByLabelText(/one-time password/i), {
      target: { value: "generated-pw" },
    });
    fireEvent.click(screen.getByRole("button", { name: /continue/i }));

    await waitFor(() => expect(screen.getByLabelText(/new password/i)).toBeInTheDocument());
  });

  it("shows the CLI recovery pointer on a wrong credential and stays on step 1", async () => {
    const client = makeClient(async () => {
      throw new EshuApiHttpError(401);
    });
    renderSetup(client);

    fireEvent.change(screen.getByLabelText(/username/i), { target: { value: "admin" } });
    fireEvent.change(screen.getByLabelText(/one-time password/i), { target: { value: "wrong" } });
    fireEvent.click(screen.getByRole("button", { name: /continue/i }));

    await waitFor(() => expect(screen.getByRole("alert")).toHaveTextContent(/initial-credential/i));
    expect(screen.getByLabelText(/username/i)).toBeInTheDocument();
  });
});

describe("SetupPage — step 2 (create administrator)", () => {
  async function advanceToStep2(client: EshuApiClient): Promise<void> {
    renderSetup(client);
    fireEvent.change(screen.getByLabelText(/username/i), { target: { value: "admin" } });
    fireEvent.change(screen.getByLabelText(/one-time password/i), {
      target: { value: "generated-pw" },
    });
    fireEvent.click(screen.getByRole("button", { name: /continue/i }));
    await waitFor(() => expect(screen.getByLabelText(/new password/i)).toBeInTheDocument());
  }

  it("rejects a mismatched confirmation without calling the API", async () => {
    const postJson = vi.fn(async (path: string) =>
      path === "/api/v0/auth/setup/claim" ? { status: "claimed" } : {},
    );
    const client = makeClient(postJson);
    await advanceToStep2(client);
    postJson.mockClear();

    fireEvent.change(screen.getByLabelText(/^new password/i), {
      target: { value: "a-strong-password" },
    });
    fireEvent.change(screen.getByLabelText(/confirm password/i), {
      target: { value: "different" },
    });
    fireEvent.click(screen.getByRole("button", { name: /continue/i }));

    await waitFor(() => expect(screen.getByRole("alert")).toHaveTextContent(/match/i));
    expect(postJson).not.toHaveBeenCalled();
  });

  it("advances to step 3 and auto-requests recovery codes once the administrator password is set", async () => {
    const client = makeClient(async (path) => {
      if (path === "/api/v0/auth/setup/claim") return { status: "claimed" };
      if (path === "/api/v0/auth/setup/admin") {
        return { status: "admin_created", tenant_id: "default", workspace_id: "default" };
      }
      if (path === "/api/v0/auth/setup/mfa") {
        return {
          status: "completed",
          recovery_codes: ["code-one", "code-two"],
          auth: {
            mode: "browser_session",
            tenant_id: "default",
            workspace_id: "default",
            all_scopes: true,
          },
          csrf_token: "csrf-tok",
        };
      }
      throw new Error(`unexpected path ${path}`);
    });
    await advanceToStep2(client);

    fireEvent.change(screen.getByLabelText(/^new password/i), {
      target: { value: "a-strong-password" },
    });
    fireEvent.change(screen.getByLabelText(/confirm password/i), {
      target: { value: "a-strong-password" },
    });
    fireEvent.click(screen.getByRole("button", { name: /continue/i }));

    // Step 3 fires the recovery-code generation call automatically on entry
    // (no separate "Generate" click) — the codes appear once it resolves.
    await waitFor(() => expect(screen.getByText("code-one")).toBeInTheDocument());
  });
});

describe("SetupPage — step 3 (MFA) and completion", () => {
  async function advanceToStep3WithCodes(
    client: EshuApiClient,
    onSuccess: (session: BrowserSessionResponse) => void,
  ): Promise<void> {
    renderSetup(client, onSuccess);
    fireEvent.change(screen.getByLabelText(/username/i), { target: { value: "admin" } });
    fireEvent.change(screen.getByLabelText(/one-time password/i), {
      target: { value: "generated-pw" },
    });
    fireEvent.click(screen.getByRole("button", { name: /continue/i }));
    await waitFor(() => expect(screen.getByLabelText(/new password/i)).toBeInTheDocument());
    fireEvent.change(screen.getByLabelText(/^new password/i), {
      target: { value: "a-strong-password" },
    });
    fireEvent.change(screen.getByLabelText(/confirm password/i), {
      target: { value: "a-strong-password" },
    });
    fireEvent.click(screen.getByRole("button", { name: /continue/i }));
    await waitFor(() => expect(screen.getByText("code-one")).toBeInTheDocument());
  }

  it("gates Finish behind the saved-confirmation checkbox and never renders TOTP UI", async () => {
    const onSuccess = vi.fn<(session: BrowserSessionResponse) => void>();
    const client = makeClient(async (path) => {
      if (path === "/api/v0/auth/setup/claim") return { status: "claimed" };
      if (path === "/api/v0/auth/setup/admin") {
        return { status: "admin_created", tenant_id: "default", workspace_id: "default" };
      }
      if (path === "/api/v0/auth/setup/mfa") {
        return {
          status: "completed",
          recovery_codes: ["code-one", "code-two"],
          auth: {
            mode: "browser_session",
            tenant_id: "default",
            workspace_id: "default",
            all_scopes: true,
          },
          csrf_token: "csrf-tok",
        };
      }
      throw new Error(`unexpected path ${path}`);
    });
    await advanceToStep3WithCodes(client, onSuccess);

    expect(screen.getByText("code-two")).toBeInTheDocument();
    // TOTP is not shipped as a working control (#4986) — no QR, secret, or
    // one-time-code input may render anywhere in the wizard.
    expect(screen.queryByRole("img", { name: /qr/i })).not.toBeInTheDocument();
    expect(screen.queryByLabelText(/authenticator|totp|6-digit/i)).not.toBeInTheDocument();

    const finishButton = screen.getByRole("button", { name: /finish setup/i });
    expect(finishButton).toBeDisabled();
    expect(onSuccess).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("checkbox", { name: /saved/i }));
    expect(finishButton).not.toBeDisabled();
    fireEvent.click(finishButton);

    expect(onSuccess).toHaveBeenCalledTimes(1);
    const [session] = onSuccess.mock.calls[0];
    expect(session.auth.tenant_id).toBe("default");
  });

  it("shows the CLI recovery pointer when the mfa call rejects the reproved credential", async () => {
    const client = makeClient(async (path) => {
      if (path === "/api/v0/auth/setup/claim") return { status: "claimed" };
      if (path === "/api/v0/auth/setup/admin") {
        return { status: "admin_created", tenant_id: "default", workspace_id: "default" };
      }
      if (path === "/api/v0/auth/setup/mfa") throw new EshuApiHttpError(401);
      throw new Error(`unexpected path ${path}`);
    });
    renderSetup(client);
    fireEvent.change(screen.getByLabelText(/username/i), { target: { value: "admin" } });
    fireEvent.change(screen.getByLabelText(/one-time password/i), {
      target: { value: "generated-pw" },
    });
    fireEvent.click(screen.getByRole("button", { name: /continue/i }));
    await waitFor(() => expect(screen.getByLabelText(/new password/i)).toBeInTheDocument());
    fireEvent.change(screen.getByLabelText(/^new password/i), {
      target: { value: "a-strong-password" },
    });
    fireEvent.change(screen.getByLabelText(/confirm password/i), {
      target: { value: "a-strong-password" },
    });
    fireEvent.click(screen.getByRole("button", { name: /continue/i }));

    await waitFor(() => expect(screen.getByRole("alert")).toHaveTextContent(/initial-credential/i));
  });
});
