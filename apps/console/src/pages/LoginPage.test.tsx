// LoginPage.test.tsx — TDD tests for the production login surface.
// Uses @testing-library/react + MemoryRouter. Mocks EshuApiClient injected as prop.
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import type { EshuApiClient, BrowserSessionResponse } from "../api/client";
import { EshuApiHttpError } from "../api/client";
import { LoginPage } from "./LoginPage";

const mockSession: BrowserSessionResponse = {
  auth: {
    mode: "browser_session",
    tenant_id: "tenant_a",
    workspace_id: "ws_a",
    all_scopes: true
  }
};

function makeClient(overrides: Partial<EshuApiClient> = {}): EshuApiClient {
  return {
    postJson: vi.fn(async () => mockSession),
    logoutBrowserSession: vi.fn(async () => undefined),
    getBrowserSession: vi.fn(async () => mockSession),
    ...overrides
  } as unknown as EshuApiClient;
}

function renderLogin(
  client: EshuApiClient,
  onSuccess = vi.fn()
): void {
  render(
    <MemoryRouter>
      <LoginPage client={client} onSuccess={onSuccess} />
    </MemoryRouter>
  );
}

describe("LoginPage", () => {
  it("renders the login form with login and password fields", () => {
    renderLogin(makeClient());
    expect(screen.getByLabelText(/login/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/password/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /sign in/i })).toBeInTheDocument();
  });

  it("does NOT render an API key input", () => {
    renderLogin(makeClient());
    expect(screen.queryByPlaceholderText(/api credential/i)).not.toBeInTheDocument();
    expect(screen.queryByPlaceholderText(/api key/i)).not.toBeInTheDocument();
    // password field should be for user password, not an API key
    const passwordInput = screen.getByLabelText(/password/i);
    expect((passwordInput as HTMLInputElement).type).toBe("password");
  });

  it("renders OIDC sign-in option", () => {
    renderLogin(makeClient());
    expect(screen.getByText(/continue with oidc/i)).toBeInTheDocument();
  });

  it("calls postJson with login_id and password on submit", async () => {
    const client = makeClient();
    const onSuccess = vi.fn();
    renderLogin(client, onSuccess);

    fireEvent.change(screen.getByLabelText(/login/i), {
      target: { value: "admin@example.com" }
    });
    fireEvent.change(screen.getByLabelText(/password/i), {
      target: { value: "hunter2" }
    });
    fireEvent.click(screen.getByRole("button", { name: /sign in/i }));

    await waitFor(() => expect(client.postJson).toHaveBeenCalledWith(
      "/api/v0/auth/local/login",
      { login_id: "admin@example.com", password: "hunter2" }
    ));
    expect(onSuccess).toHaveBeenCalledWith(mockSession);
  });

  it("shows an error message on wrong credentials (401)", async () => {
    const client = makeClient({
      postJson: vi.fn(async () => { throw new EshuApiHttpError(401); })
    });
    renderLogin(client);

    fireEvent.change(screen.getByLabelText(/login/i), { target: { value: "u" } });
    fireEvent.change(screen.getByLabelText(/password/i), { target: { value: "wrong" } });
    fireEvent.click(screen.getByRole("button", { name: /sign in/i }));

    expect(await screen.findByRole("alert")).toBeInTheDocument();
  });

  it("shows MFA field after initial submit when MFA error is indicated", async () => {
    // Backend returns 401 with a code that hints MFA is required
    const client = makeClient({
      postJson: vi.fn(async () => {
        throw new EshuApiHttpError(401, {
          code: "mfa_required",
          message: "MFA code required"
        });
      })
    });
    renderLogin(client);

    fireEvent.change(screen.getByLabelText(/login/i), { target: { value: "u" } });
    fireEvent.change(screen.getByLabelText(/password/i), { target: { value: "p" } });
    fireEvent.click(screen.getByRole("button", { name: /sign in/i }));

    // MFA / recovery-code field should appear
    expect(await screen.findByLabelText(/recovery code|mfa/i)).toBeInTheDocument();
  });

  it("submits with recovery_code when MFA field is filled", async () => {
    // First call demands MFA, second succeeds
    const postJson = vi.fn()
      .mockRejectedValueOnce(new EshuApiHttpError(401, { code: "mfa_required", message: "MFA required" }))
      .mockResolvedValueOnce(mockSession);
    const client = makeClient({ postJson });
    const onSuccess = vi.fn();
    renderLogin(client, onSuccess);

    fireEvent.change(screen.getByLabelText(/login/i), { target: { value: "u@x.com" } });
    fireEvent.change(screen.getByLabelText(/password/i), { target: { value: "pass" } });
    fireEvent.click(screen.getByRole("button", { name: /sign in/i }));

    const mfaInput = await screen.findByLabelText(/recovery code|mfa/i);
    fireEvent.change(mfaInput, { target: { value: "CODE99" } });
    fireEvent.click(screen.getByRole("button", { name: /sign in/i }));

    await waitFor(() => expect(postJson).toHaveBeenLastCalledWith(
      "/api/v0/auth/local/login",
      { login_id: "u@x.com", password: "pass", recovery_code: "CODE99" }
    ));
    expect(onSuccess).toHaveBeenCalledWith(mockSession);
  });

  it("disables the submit button while a login request is in flight", async () => {
    let resolve!: (v: BrowserSessionResponse) => void;
    const client = makeClient({
      postJson: vi.fn(() => new Promise<BrowserSessionResponse>((r) => { resolve = r; }))
    });
    renderLogin(client);

    fireEvent.change(screen.getByLabelText(/login/i), { target: { value: "u" } });
    fireEvent.change(screen.getByLabelText(/password/i), { target: { value: "p" } });
    fireEvent.click(screen.getByRole("button", { name: /^sign in$/i }));

    // While in-flight the button shows "Signing in…" and must be disabled.
    const submittingBtn = await screen.findByRole("button", { name: /signing in/i });
    expect(submittingBtn).toBeDisabled();
    resolve(mockSession);
  });
});
