// LoginPage.test.tsx — TDD tests for the production login surface.
// Uses @testing-library/react + MemoryRouter. Mocks EshuApiClient injected as prop.
//
// MFA flow: the backend returns HTTP 202 with body {status:"mfa_required"} —
// this is a 2xx so postJson resolves (does not throw). loginLocal maps it to
// LocalLoginResult{status:"mfa_required"}, and LoginPage reveals the recovery-
// code field and re-submits with recovery_code on the next form submit.
//
// OIDC/SAML buttons are hidden in Slice A pending provider discovery (#3682).
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";

import { LoginPage } from "./LoginPage";
import type { EshuApiClient, BrowserSessionResponse } from "../api/client";
import { EshuApiHttpError } from "../api/client";

// mockSession matches the shape loginLocal wraps in {status:"ok", session:...}.
// postJson must return a raw LocalIdentitySessionResponse with status field.
const mockSessionRaw = {
  status: "authenticated",
  auth: {
    mode: "browser_session",
    tenant_id: "tenant_a",
    workspace_id: "ws_a",
    all_scopes: true,
  },
  csrf_token: "csrf-tok",
};

const mockSession: BrowserSessionResponse = {
  auth: {
    mode: "browser_session",
    tenant_id: "tenant_a",
    workspace_id: "ws_a",
    all_scopes: true,
  },
};

function makeClient(overrides: Partial<EshuApiClient> = {}): EshuApiClient {
  return {
    postJson: vi.fn(async () => mockSessionRaw),
    logoutBrowserSession: vi.fn(async () => undefined),
    getBrowserSession: vi.fn(async () => mockSession),
    // getJson is called by listAuthProviders on mount; return empty list so
    // Slice A local-login tests see no SSO buttons and remain unaffected.
    getJson: vi.fn(async () => ({ providers: [] })),
    ...overrides,
  } as unknown as EshuApiClient;
}

function renderLogin(client: EshuApiClient, onSuccess = vi.fn()): void {
  render(
    <MemoryRouter>
      <LoginPage client={client} onSuccess={onSuccess} />
    </MemoryRouter>,
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
    const passwordInput = screen.getByLabelText(/password/i);
    expect((passwordInput as HTMLInputElement).type).toBe("password");
  });

  it("does NOT render SSO buttons when provider discovery returns empty (#3682 zero-provider case)", async () => {
    renderLogin(makeClient());
    expect(screen.queryByText(/continue with oidc/i)).not.toBeInTheDocument();
    expect(screen.queryByText(/continue with saml/i)).not.toBeInTheDocument();
  });

  it("calls postJson with login_id and password on submit", async () => {
    const client = makeClient();
    const onSuccess = vi.fn();
    renderLogin(client, onSuccess);

    fireEvent.change(screen.getByLabelText(/login/i), {
      target: { value: "admin@example.com" },
    });
    fireEvent.change(screen.getByLabelText(/password/i), {
      target: { value: "hunter2" },
    });
    fireEvent.click(screen.getByRole("button", { name: /sign in/i }));

    await waitFor(() =>
      expect(client.postJson).toHaveBeenCalledWith("/api/v0/auth/local/login", {
        login_id: "admin@example.com",
        password: "hunter2",
      }),
    );
    // onSuccess receives the session extracted from LocalLoginResult{status:"ok"}
    expect(onSuccess).toHaveBeenCalledTimes(1);
  });

  it("shows an error message on wrong credentials (401 → status:invalid)", async () => {
    const client = makeClient({
      postJson: vi.fn(async () => {
        throw new EshuApiHttpError(401);
      }),
    });
    renderLogin(client);

    fireEvent.change(screen.getByLabelText(/login/i), { target: { value: "u" } });
    fireEvent.change(screen.getByLabelText(/password/i), { target: { value: "wrong" } });
    fireEvent.click(screen.getByRole("button", { name: /sign in/i }));

    const alert = await screen.findByRole("alert");
    expect(alert).toBeInTheDocument();
    expect(alert.textContent).toMatch(/incorrect login or password/i);
  });

  it("shows 'Account disabled' error on 403 response", async () => {
    const client = makeClient({
      postJson: vi.fn(async () => {
        throw new EshuApiHttpError(403);
      }),
    });
    renderLogin(client);

    fireEvent.change(screen.getByLabelText(/login/i), { target: { value: "u" } });
    fireEvent.change(screen.getByLabelText(/password/i), { target: { value: "p" } });
    fireEvent.click(screen.getByRole("button", { name: /sign in/i }));

    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toMatch(/account disabled/i);
  });

  it("shows 'Account locked' error on 423 response", async () => {
    const client = makeClient({
      postJson: vi.fn(async () => {
        throw new EshuApiHttpError(423);
      }),
    });
    renderLogin(client);

    fireEvent.change(screen.getByLabelText(/login/i), { target: { value: "u" } });
    fireEvent.change(screen.getByLabelText(/password/i), { target: { value: "p" } });
    fireEvent.click(screen.getByRole("button", { name: /sign in/i }));

    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toMatch(/locked/i);
  });

  it("shows MFA field after 202 mfa_required response (not a thrown error)", async () => {
    // Backend returns HTTP 202 with {status:"mfa_required"} — resolves, not throws.
    const client = makeClient({
      // Cast to the generic postJson signature: this mock resolves the raw 202
      // body, but EshuApiClient["postJson"] is generic <TData>.
      postJson: vi.fn(async () => ({
        status: "mfa_required",
      })) as unknown as EshuApiClient["postJson"],
    });
    renderLogin(client);

    fireEvent.change(screen.getByLabelText(/login/i), { target: { value: "u" } });
    fireEvent.change(screen.getByLabelText(/password/i), { target: { value: "p" } });
    fireEvent.click(screen.getByRole("button", { name: /sign in/i }));

    // Recovery-code field must appear (mfa phase).
    expect(await screen.findByLabelText(/recovery code/i)).toBeInTheDocument();
  });

  it("submits with recovery_code when MFA field is filled", async () => {
    // First call: 202 mfa_required (resolves). Second call: 200 authenticated.
    const postJson = vi
      .fn()
      .mockResolvedValueOnce({ status: "mfa_required" })
      .mockResolvedValueOnce(mockSessionRaw);
    const client = makeClient({ postJson });
    const onSuccess = vi.fn();
    renderLogin(client, onSuccess);

    fireEvent.change(screen.getByLabelText(/login/i), { target: { value: "u@x.com" } });
    fireEvent.change(screen.getByLabelText(/password/i), { target: { value: "pass" } });
    fireEvent.click(screen.getByRole("button", { name: /sign in/i }));

    const mfaInput = await screen.findByLabelText(/recovery code/i);
    fireEvent.change(mfaInput, { target: { value: "CODE99" } });
    fireEvent.click(screen.getByRole("button", { name: /sign in/i }));

    await waitFor(() =>
      expect(postJson).toHaveBeenLastCalledWith("/api/v0/auth/local/login", {
        login_id: "u@x.com",
        password: "pass",
        recovery_code: "CODE99",
      }),
    );
    expect(onSuccess).toHaveBeenCalledTimes(1);
  });

  // #4964: the backend's default ESHU_AUTH_COOKIE_SECURE=auto only relaxes
  // the Secure cookie attribute for a plain-HTTP loopback origin. Any other
  // plain-HTTP origin keeps Secure=true, so the browser drops the session
  // cookie there. The login page must show an actionable banner rather than
  // silently failing to keep the user signed in.
  it("shows an insecure-origin banner over plain HTTP on a non-loopback host", () => {
    render(
      <MemoryRouter>
        <LoginPage
          client={makeClient()}
          onSuccess={vi.fn()}
          location={{ protocol: "http:", hostname: "console.internal.example.com" }}
        />
      </MemoryRouter>,
    );
    const banner = screen.getByRole("alert");
    expect(banner.textContent).toMatch(/session will not stay signed in/i);
    expect(banner.textContent).toMatch(/localhost/i);
    expect(banner.textContent).toMatch(/https/i);
  });

  it("does NOT show the insecure-origin banner over plain HTTP on localhost", () => {
    render(
      <MemoryRouter>
        <LoginPage
          client={makeClient()}
          onSuccess={vi.fn()}
          location={{ protocol: "http:", hostname: "localhost" }}
        />
      </MemoryRouter>,
    );
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("does NOT show the insecure-origin banner over https", () => {
    render(
      <MemoryRouter>
        <LoginPage
          client={makeClient()}
          onSuccess={vi.fn()}
          location={{ protocol: "https:", hostname: "console.internal.example.com" }}
        />
      </MemoryRouter>,
    );
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("disables the submit button while a login request is in flight", async () => {
    let resolve!: (v: unknown) => void;
    const client = makeClient({
      postJson: vi.fn(
        () =>
          new Promise((r) => {
            resolve = r;
          }),
      ) as unknown as EshuApiClient["postJson"],
    });
    renderLogin(client);

    fireEvent.change(screen.getByLabelText(/login/i), { target: { value: "u" } });
    fireEvent.change(screen.getByLabelText(/password/i), { target: { value: "p" } });
    fireEvent.click(screen.getByRole("button", { name: /^sign in$/i }));

    const submittingBtn = await screen.findByRole("button", { name: /signing in/i });
    expect(submittingBtn).toBeDisabled();
    resolve(mockSessionRaw);
  });
});
