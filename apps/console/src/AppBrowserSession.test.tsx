import { fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { vi } from "vitest";
import { App } from "./App";

describe("App browser session auth", () => {
  afterEach(() => {
    window.localStorage.clear();
    vi.unstubAllGlobals();
  });

  it("exchanges an entered credential for a browser session before loading live data", async () => {
    window.localStorage.setItem(
      "eshu.console.environment",
      JSON.stringify({ mode: "demo", apiBaseUrl: "", recentApiBaseUrls: [] })
    );
    const observed: { readonly path: string; readonly method: string; readonly authorization: string | null }[] = [];
    vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const path = new URL(new Request(input).url).pathname;
      const headers = new Headers(init?.headers);
      const method = init?.method ?? "GET";
      observed.push({ path, method, authorization: headers.get("Authorization") });
      if (path === "/eshu-api/api/v0/auth/browser-session") {
        return Response.json({
          auth: {
            mode: "browser_session",
            tenant_id: "tenant_a",
            workspace_id: "workspace_a",
            all_scopes: false
          },
          csrf_token: "csrf-secret"
        }, { status: 201 });
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

    fireEvent.click(await screen.findByRole("button", { name: "Demo fixtures" }));
    fireEvent.change(screen.getByPlaceholderText("API credential"), {
      target: { value: "scoped-login-token" }
    });
    fireEvent.click(screen.getByRole("button", { name: "Connect" }));

    expect(await screen.findByText("Live")).toBeInTheDocument();
    const sessionCall = observed.find((call) => call.path === "/eshu-api/api/v0/auth/browser-session");
    expect(sessionCall).toMatchObject({
      method: "POST",
      authorization: "Bearer scoped-login-token"
    });
    const liveReadCalls = observed.filter((call) =>
      call.path !== "/eshu-api/api/v0/auth/browser-session"
    );
    expect(liveReadCalls.length).toBeGreaterThan(0);
    expect(liveReadCalls.every((call) => call.authorization === null)).toBe(true);
  });

  it("falls back to bearer live reads when a shared key cannot create a browser session", async () => {
    window.localStorage.setItem(
      "eshu.console.environment",
      JSON.stringify({ mode: "demo", apiBaseUrl: "", recentApiBaseUrls: [] })
    );
    const observed: { readonly path: string; readonly method: string; readonly authorization: string | null }[] = [];
    vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const path = new URL(new Request(input).url).pathname;
      const headers = new Headers(init?.headers);
      const method = init?.method ?? "GET";
      const authorization = headers.get("Authorization");
      observed.push({ path, method, authorization });
      if (path === "/eshu-api/api/v0/auth/browser-session") {
        return Response.json({
          error: {
            code: "invalid_request",
            message: "tenant_id and workspace_id are required to create a browser session"
          }
        }, { status: 400 });
      }
      if (authorization !== "Bearer shared-api-key") {
        return Response.json({
          error: {
            code: "unauthenticated",
            message: "authentication is required"
          }
        }, { status: 401 });
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

    fireEvent.click(await screen.findByRole("button", { name: "Demo fixtures" }));
    fireEvent.change(screen.getByPlaceholderText("API credential"), {
      target: { value: "shared-api-key" }
    });
    fireEvent.click(screen.getByRole("button", { name: "Connect" }));

    expect(await screen.findByText("Live")).toBeInTheDocument();
    const sessionCall = observed.find((call) => call.path === "/eshu-api/api/v0/auth/browser-session");
    expect(sessionCall).toMatchObject({
      method: "POST",
      authorization: "Bearer shared-api-key"
    });
    const liveReadCalls = observed.filter((call) =>
      call.path !== "/eshu-api/api/v0/auth/browser-session"
    );
    expect(liveReadCalls.length).toBeGreaterThan(0);
    expect(liveReadCalls.every((call) => call.authorization === "Bearer shared-api-key")).toBe(true);
  });
});
