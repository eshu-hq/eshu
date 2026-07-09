// setupSession.test.ts — TDD tests for the first-run setup wizard API helpers
// (#4965). Mirrors authSession.test.ts's conventions: mock EshuApiClient,
// assert exact backend field names, and assert the discriminated result
// unions map HTTP status codes correctly.
import { describe, expect, it, vi } from "vitest";

import type { EshuApiClient } from "./client";
import { EshuApiHttpError } from "./client";
import { claimSetup, completeSetupMFA, createSetupAdmin, getSetupState } from "./setupSession";

function makeClient(overrides: Partial<EshuApiClient> = {}): EshuApiClient {
  return {
    getJson: vi.fn(),
    postJson: vi.fn(),
    ...overrides,
  } as unknown as EshuApiClient;
}

describe("getSetupState", () => {
  it("returns the raw needs_setup/bootstrap_mode fields", async () => {
    const client = makeClient({
      getJson: vi.fn(async () => ({
        needs_setup: true,
        bootstrap_mode: "generated",
      })) as unknown as EshuApiClient["getJson"],
    });
    const state = await getSetupState(client);
    expect(state).toEqual({ needs_setup: true, bootstrap_mode: "generated" });
    expect(client.getJson).toHaveBeenCalledWith("/api/v0/auth/setup-state");
  });
});

describe("claimSetup", () => {
  it("posts username/password and returns claimed on success", async () => {
    const client = makeClient({
      postJson: vi.fn(async () => ({ status: "claimed" })) as unknown as EshuApiClient["postJson"],
    });
    const result = await claimSetup(client, { username: "admin", password: "generated-pw" });
    expect(result).toEqual({ status: "claimed" });
    expect(client.postJson).toHaveBeenCalledWith("/api/v0/auth/setup/claim", {
      username: "admin",
      password: "generated-pw",
    });
  });

  it("maps 401 to invalid without throwing", async () => {
    const client = makeClient({
      postJson: vi.fn(async () => {
        throw new EshuApiHttpError(401);
      }),
    });
    const result = await claimSetup(client, { username: "admin", password: "wrong" });
    expect(result).toEqual({ status: "invalid" });
  });

  it("maps 410 to gone without throwing", async () => {
    const client = makeClient({
      postJson: vi.fn(async () => {
        throw new EshuApiHttpError(410);
      }),
    });
    const result = await claimSetup(client, { username: "admin", password: "x" });
    expect(result).toEqual({ status: "gone" });
  });

  it("rethrows other HTTP errors", async () => {
    const client = makeClient({
      postJson: vi.fn(async () => {
        throw new EshuApiHttpError(500);
      }),
    });
    await expect(claimSetup(client, { username: "admin", password: "x" })).rejects.toBeInstanceOf(
      EshuApiHttpError,
    );
  });
});

describe("createSetupAdmin", () => {
  it("posts the new password and returns the resolved tenant/workspace", async () => {
    const client = makeClient({
      postJson: vi.fn(async () => ({
        status: "admin_created",
        tenant_id: "default",
        workspace_id: "default",
      })) as unknown as EshuApiClient["postJson"],
    });
    const result = await createSetupAdmin(client, {
      username: "admin",
      password: "generated-pw",
      newPassword: "operator-chosen-password",
    });
    expect(result).toEqual({
      status: "admin_created",
      tenantId: "default",
      workspaceId: "default",
    });
    expect(client.postJson).toHaveBeenCalledWith("/api/v0/auth/setup/admin", {
      username: "admin",
      password: "generated-pw",
      new_password: "operator-chosen-password",
    });
  });

  it("maps 401 to invalid", async () => {
    const client = makeClient({
      postJson: vi.fn(async () => {
        throw new EshuApiHttpError(401);
      }),
    });
    const result = await createSetupAdmin(client, {
      username: "admin",
      password: "wrong",
      newPassword: "x",
    });
    expect(result).toEqual({ status: "invalid" });
  });
});

describe("completeSetupMFA", () => {
  it("returns recovery codes and a BrowserSessionResponse-shaped session", async () => {
    const client = makeClient({
      postJson: vi.fn(async () => ({
        status: "completed",
        recovery_codes: ["code-a", "code-b"],
        auth: {
          mode: "browser_session",
          tenant_id: "default",
          workspace_id: "default",
          all_scopes: true,
        },
        csrf_token: "csrf-tok",
      })) as unknown as EshuApiClient["postJson"],
    });
    const result = await completeSetupMFA(client, {
      username: "admin",
      password: "operator-chosen-password",
    });
    expect(result.status).toBe("completed");
    if (result.status === "completed") {
      expect(result.recoveryCodes).toEqual(["code-a", "code-b"]);
      expect(result.session.auth.tenant_id).toBe("default");
    }
  });

  it("maps 401 to invalid", async () => {
    const client = makeClient({
      postJson: vi.fn(async () => {
        throw new EshuApiHttpError(401);
      }),
    });
    const result = await completeSetupMFA(client, { username: "admin", password: "wrong" });
    expect(result).toEqual({ status: "invalid" });
  });
});
