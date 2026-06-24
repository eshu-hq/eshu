// authSession.test.ts — TDD tests for authSession helpers.
// loginLocal returns a discriminated LocalLoginResult union. The backend always
// returns a JSON body (go/internal/query/local_identity_handler_helpers.go):
//   200 → {status:"authenticated", auth:{...}}  → LocalLoginResult{status:"ok"}
//   202 → {status:"mfa_required"}               → LocalLoginResult{status:"mfa_required"}  (resolves, NOT throws)
//   423 → EshuApiHttpError(423)                  → LocalLoginResult{status:"locked"}
//   403 → EshuApiHttpError(403)                  → LocalLoginResult{status:"disabled"}
//   401 → EshuApiHttpError(401)                  → LocalLoginResult{status:"invalid"}
//   5xx → EshuApiHttpError(5xx) re-thrown
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { LocalLoginResult } from "./authSession";
import {
  loadCurrentSession,
  loginLocal,
  beginOidcLogin,
  beginSamlLogin,
  logout
} from "./authSession";
import type { EshuApiClient, BrowserSessionResponse } from "./client";
import { EshuApiHttpError } from "./client";

const mockSession: BrowserSessionResponse = {
  auth: {
    mode: "browser_session",
    tenant_id: "tenant_a",
    workspace_id: "ws_a",
    all_scopes: true
  }
};

// Raw LocalIdentitySessionResponse returned by the backend on 200.
const mockSessionRaw = {
  status: "authenticated",
  auth: {
    mode: "browser_session",
    tenant_id: "tenant_a",
    workspace_id: "ws_a",
    all_scopes: true
  },
  csrf_token: "csrf-tok"
};

function makeClient(overrides: Partial<EshuApiClient> = {}): EshuApiClient {
  return {
    getBrowserSession: vi.fn(async () => mockSession),
    postJson: vi.fn(async () => mockSessionRaw),
    logoutBrowserSession: vi.fn(async () => undefined),
    createBrowserSession: vi.fn(async () => mockSession),
    ...overrides
  } as unknown as EshuApiClient;
}

describe("loadCurrentSession", () => {
  beforeEach(() => { vi.clearAllMocks(); });

  it("returns the session when getBrowserSession succeeds", async () => {
    const client = makeClient();
    const result = await loadCurrentSession(client);
    expect(result).toEqual(mockSession);
    expect(client.getBrowserSession).toHaveBeenCalledTimes(1);
  });

  it("returns null on 401", async () => {
    const client = makeClient({
      getBrowserSession: vi.fn(async () => { throw new EshuApiHttpError(401); })
    });
    const result = await loadCurrentSession(client);
    expect(result).toBeNull();
  });

  it("returns null on 403", async () => {
    const client = makeClient({
      getBrowserSession: vi.fn(async () => { throw new EshuApiHttpError(403); })
    });
    const result = await loadCurrentSession(client);
    expect(result).toBeNull();
  });

  it("returns null on 404", async () => {
    const client = makeClient({
      getBrowserSession: vi.fn(async () => { throw new EshuApiHttpError(404); })
    });
    const result = await loadCurrentSession(client);
    expect(result).toBeNull();
  });

  it("rethrows non-auth errors (e.g. 500)", async () => {
    const client = makeClient({
      getBrowserSession: vi.fn(async () => { throw new EshuApiHttpError(500); })
    });
    await expect(loadCurrentSession(client)).rejects.toThrow(EshuApiHttpError);
  });

  it("rethrows network errors (non-http)", async () => {
    const client = makeClient({
      getBrowserSession: vi.fn(async () => { throw new Error("network failure"); })
    });
    await expect(loadCurrentSession(client)).rejects.toThrow("network failure");
  });
});

describe("loginLocal", () => {
  beforeEach(() => { vi.clearAllMocks(); });

  it("POSTs login_id and password to /api/v0/auth/local/login", async () => {
    const client = makeClient();
    await loginLocal(client, { login: "user@example.com", password: "s3cr3t" });
    expect(client.postJson).toHaveBeenCalledWith(
      "/api/v0/auth/local/login",
      { login_id: "user@example.com", password: "s3cr3t" }
    );
  });

  it("returns {status:'ok', session} on 200 authenticated response", async () => {
    const client = makeClient();
    const result = await loginLocal(client, { login: "u", password: "p" });
    expect(result.status).toBe("ok");
    if (result.status === "ok") {
      // The session wraps the raw response which has auth nested inside
      expect(result.session).toBeDefined();
    }
  });

  it("returns {status:'mfa_required'} when backend sends 202 body (resolves, not throws)", async () => {
    const client = makeClient({
      // Cast to the generic postJson signature: this mock resolves the raw 202
      // body, but EshuApiClient["postJson"] is generic <TData>.
      postJson: vi.fn(async () => ({ status: "mfa_required" })) as unknown as EshuApiClient["postJson"]
    });
    const result = await loginLocal(client, { login: "u", password: "p" });
    expect(result).toEqual<LocalLoginResult>({ status: "mfa_required" });
  });

  it("returns {status:'invalid'} on 401", async () => {
    const client = makeClient({
      postJson: vi.fn(async () => { throw new EshuApiHttpError(401); })
    });
    const result = await loginLocal(client, { login: "u", password: "p" });
    expect(result).toEqual<LocalLoginResult>({ status: "invalid" });
  });

  it("returns {status:'disabled'} on 403", async () => {
    const client = makeClient({
      postJson: vi.fn(async () => { throw new EshuApiHttpError(403); })
    });
    const result = await loginLocal(client, { login: "u", password: "p" });
    expect(result).toEqual<LocalLoginResult>({ status: "disabled" });
  });

  it("returns {status:'locked'} on 423", async () => {
    const client = makeClient({
      postJson: vi.fn(async () => { throw new EshuApiHttpError(423); })
    });
    const result = await loginLocal(client, { login: "u", password: "p" });
    expect(result).toEqual<LocalLoginResult>({ status: "locked" });
  });

  it("rethrows 5xx errors — never swallows non-auth HTTP errors", async () => {
    const client = makeClient({
      postJson: vi.fn(async () => { throw new EshuApiHttpError(500); })
    });
    await expect(loginLocal(client, { login: "u", password: "p" })).rejects.toThrow(EshuApiHttpError);
  });

  it("rethrows network/timeout errors", async () => {
    const client = makeClient({
      postJson: vi.fn(async () => { throw new Error("fetch failed"); })
    });
    await expect(loginLocal(client, { login: "u", password: "p" })).rejects.toThrow("fetch failed");
  });

  it("includes recovery_code when mfaCode is provided", async () => {
    const client = makeClient();
    await loginLocal(client, { login: "user@example.com", password: "s3cr3t", mfaCode: "ABC123" });
    expect(client.postJson).toHaveBeenCalledWith(
      "/api/v0/auth/local/login",
      { login_id: "user@example.com", password: "s3cr3t", recovery_code: "ABC123" }
    );
  });

  it("omits recovery_code when mfaCode is not provided", async () => {
    const client = makeClient();
    await loginLocal(client, { login: "user@example.com", password: "s3cr3t" });
    const body = (client.postJson as ReturnType<typeof vi.fn>).mock.calls[0][1] as Record<string, unknown>;
    expect(body).not.toHaveProperty("recovery_code");
  });
});

describe("beginOidcLogin", () => {
  it("returns a URL pointing to /api/v0/auth/oidc/login with provider_config_id", () => {
    const url = beginOidcLogin("/eshu-api/", {
      providerConfigId: "google",
      returnTo: "/dashboard"
    });
    expect(url).toContain("/api/v0/auth/oidc/login");
    expect(url).toContain("provider_config_id=google");
    expect(url).toContain("return_to=");
  });

  it("includes tenant_id and workspace_id when provided", () => {
    const url = beginOidcLogin("/eshu-api/", {
      providerConfigId: "google",
      tenantId: "t1",
      workspaceId: "w1",
      returnTo: "/"
    });
    expect(url).toContain("tenant_id=t1");
    expect(url).toContain("workspace_id=w1");
  });

  it("omits tenant_id and workspace_id when not provided", () => {
    const url = beginOidcLogin("/eshu-api/", {
      providerConfigId: "google",
      returnTo: "/"
    });
    expect(url).not.toContain("tenant_id");
    expect(url).not.toContain("workspace_id");
  });

  it("uses optional redirect fn when provided", () => {
    const redirectFn = vi.fn();
    beginOidcLogin("/eshu-api/", { providerConfigId: "google", returnTo: "/" }, redirectFn);
    expect(redirectFn).toHaveBeenCalledTimes(1);
    expect(redirectFn).toHaveBeenCalledWith(expect.stringContaining("/api/v0/auth/oidc/login"));
  });
});

describe("beginSamlLogin", () => {
  it("returns a URL pointing to the SAML provider login path", () => {
    const url = beginSamlLogin("/eshu-api/", { providerId: "okta", returnTo: "/dashboard" });
    expect(url).toContain("/api/v0/auth/saml/providers/okta/login");
  });

  it("includes return_to in the URL", () => {
    const url = beginSamlLogin("/eshu-api/", { providerId: "okta", returnTo: "/dashboard" });
    expect(url).toContain("return_to=");
  });

  it("uses optional redirect fn when provided", () => {
    const redirectFn = vi.fn();
    beginSamlLogin("/eshu-api/", { providerId: "okta", returnTo: "/" }, redirectFn);
    expect(redirectFn).toHaveBeenCalledTimes(1);
    expect(redirectFn).toHaveBeenCalledWith(expect.stringContaining("/api/v0/auth/saml/providers/okta/login"));
  });
});

describe("logout", () => {
  it("calls logoutBrowserSession on the client", async () => {
    const client = makeClient();
    await logout(client);
    expect(client.logoutBrowserSession).toHaveBeenCalledTimes(1);
  });
});
