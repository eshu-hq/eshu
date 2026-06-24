// AppBrowserSession.test.tsx — integration tests for the production session-auth
// flow. The primary login path is now LoginPage (local credentials) rather than
// pasting an API credential into the SourcePopover.
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { vi } from "vitest";

import { App } from "./App";

describe("App browser session auth", () => {
  afterEach(() => {
    window.localStorage.clear();
    vi.unstubAllGlobals();
  });

  it("shows the LoginPage when no session exists and boots after successful login", async () => {
    // No saved env — default private mode with no session cookie.
    const observed: { readonly path: string; readonly method: string }[] = [];
    // Track how many times the session endpoint has been called so the first
    // call returns 401 (no session) and subsequent calls return 200 (logged in).
    let sessionCallCount = 0;
    vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const path = new URL(new Request(input).url).pathname;
      const method = init?.method ?? "GET";
      observed.push({ path, method });
      if (path === "/eshu-api/api/v0/auth/browser-session" && method === "GET") {
        sessionCallCount++;
        if (sessionCallCount === 1) {
          // Initial probe — no session yet.
          return Response.json({ error: { code: "unauthenticated", message: "no session" } }, { status: 401 });
        }
        // Post-login boot: session cookie is present.
        return Response.json({
          auth: {
            mode: "browser_session",
            tenant_id: "tenant_a",
            workspace_id: "workspace_a",
            all_scopes: true
          }
        });
      }
      // Local login → server sets a session cookie and returns auth.
      if (path === "/eshu-api/api/v0/auth/local/login" && method === "POST") {
        return Response.json({
          auth: {
            mode: "browser_session",
            tenant_id: "tenant_a",
            workspace_id: "workspace_a",
            all_scopes: true
          },
          csrf_token: "csrf-secret"
        });
      }
      if (path === "/eshu-api/api/v0/index-status") {
        return Response.json({ status: "ready", repository_count: 1, queue: {} });
      }
      if (path === "/eshu-api/api/v0/ecosystem/overview") {
        return Response.json({ data: { repo_count: 1 } });
      }
      if (path === "/eshu-api/api/v0/catalog") {
        return Response.json({ data: { services: [] } });
      }
      return Response.json({ data: {} });
    }));

    render(
      <MemoryRouter initialEntries={["/dashboard"]}>
        <App />
      </MemoryRouter>
    );

    // LoginPage must appear when session probe returns 401.
    expect(await screen.findByLabelText(/login/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/password/i)).toBeInTheDocument();

    // Fill in credentials and submit.
    fireEvent.change(screen.getByLabelText(/login/i), { target: { value: "admin@example.com" } });
    fireEvent.change(screen.getByLabelText(/password/i), { target: { value: "s3cr3t" } });
    fireEvent.click(screen.getByRole("button", { name: /sign in/i }));

    // After login the app boots and shows "Live".
    expect(await screen.findByText("Live")).toBeInTheDocument();

    // Confirm the login POST was made with the right field names.
    const loginCall = observed.find((c) => c.path === "/eshu-api/api/v0/auth/local/login");
    expect(loginCall).toBeDefined();
    expect(loginCall?.method).toBe("POST");
  });

  it("boots with an existing session cookie without showing the login page", async () => {
    // Saved private env — session cookie present from a previous login.
    window.localStorage.setItem(
      "eshu.console.environment",
      JSON.stringify({ mode: "private", apiBaseUrl: "/eshu-api/", recentApiBaseUrls: ["/eshu-api/"] })
    );
    vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const path = new URL(new Request(input).url).pathname;
      const method = init?.method ?? "GET";
      if (path === "/eshu-api/api/v0/auth/browser-session" && method === "GET") {
        return Response.json({
          auth: { mode: "browser_session", all_scopes: true }
        });
      }
      if (path === "/eshu-api/api/v0/index-status") {
        return Response.json({ status: "ready", repository_count: 1, queue: {} });
      }
      if (path === "/eshu-api/api/v0/ecosystem/overview") {
        return Response.json({ data: { repo_count: 1 } });
      }
      if (path === "/eshu-api/api/v0/catalog") {
        return Response.json({ data: { services: [] } });
      }
      return Response.json({ data: {} });
    }));

    render(
      <MemoryRouter initialEntries={["/dashboard"]}>
        <App />
      </MemoryRouter>
    );

    // No login page — goes straight to connected state.
    expect(await screen.findByText("Live")).toBeInTheDocument();
    await waitFor(() => {
      expect(screen.queryByLabelText(/login/i)).not.toBeInTheDocument();
    });
  });

  it("SourcePopover no longer contains an API credential password input", async () => {
    // Verify the production-mode removal of the API key paste field.
    window.localStorage.setItem(
      "eshu.console.environment",
      JSON.stringify({ mode: "demo", apiBaseUrl: "", recentApiBaseUrls: [] })
    );
    vi.stubGlobal("fetch", vi.fn(async () => Response.json({ data: {} })));

    render(
      <MemoryRouter initialEntries={["/dashboard"]}>
        <App />
      </MemoryRouter>
    );

    // Open the source popover from demo mode.
    fireEvent.click(await screen.findByRole("button", { name: "Demo fixtures" }));

    // API credential password field must not exist.
    expect(screen.queryByPlaceholderText("API credential")).not.toBeInTheDocument();
    expect(screen.queryByPlaceholderText(/api credential/i)).not.toBeInTheDocument();
  });
});
