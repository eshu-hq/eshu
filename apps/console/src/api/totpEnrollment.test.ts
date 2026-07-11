// api/totpEnrollment.test.ts — issue #4986.
// Verifies beginTOTPEnrollment / confirmTOTPEnrollment:
//   - hit the correct endpoint paths with the correct request shape
//   - confirm uses postNoContent (the confirm route returns HTTP 204, no body)
//   - confirm classifies a 400 as invalid_code and other errors as "error"
import { describe, it, expect, vi } from "vitest";

import type { EshuApiClient } from "./client";
import { EshuApiHttpError } from "./client";
import { beginTOTPEnrollment, confirmTOTPEnrollment } from "./totpEnrollment";

function makeClient(overrides: Partial<EshuApiClient> = {}): EshuApiClient {
  return {
    postJson: vi.fn(async () => ({
      factor_id: "factor-1",
      otpauth_uri: "otpauth://totp/Eshu:account?secret=ABC",
      secret: "ABC",
      issuer: "Eshu",
      digits: 6,
      period_seconds: 30,
    })),
    postNoContent: vi.fn(async () => undefined),
    ...overrides,
  } as unknown as EshuApiClient;
}

describe("beginTOTPEnrollment", () => {
  it("posts to the begin route with no body when accountLabel is omitted", async () => {
    const client = makeClient();
    await beginTOTPEnrollment(client);
    expect(client.postJson).toHaveBeenCalledWith("/api/v0/auth/local/mfa/totp/begin", {});
  });

  it("posts account_label when provided", async () => {
    const client = makeClient();
    await beginTOTPEnrollment(client, "owner@example.test");
    expect(client.postJson).toHaveBeenCalledWith("/api/v0/auth/local/mfa/totp/begin", {
      account_label: "owner@example.test",
    });
  });

  it("returns the begin response verbatim", async () => {
    const client = makeClient();
    const result = await beginTOTPEnrollment(client);
    expect(result.factor_id).toBe("factor-1");
    expect(result.secret).toBe("ABC");
  });
});

describe("confirmTOTPEnrollment", () => {
  it("uses postNoContent (the confirm route returns HTTP 204)", async () => {
    const client = makeClient();
    const result = await confirmTOTPEnrollment(client, "factor-1", "123456");
    expect(client.postNoContent).toHaveBeenCalledWith("/api/v0/auth/local/mfa/totp/confirm", {
      factor_id: "factor-1",
      code: "123456",
    });
    expect(result).toEqual({ status: "activated" });
  });

  it("trims the submitted code before sending it", async () => {
    const client = makeClient();
    await confirmTOTPEnrollment(client, "factor-1", "  123456  ");
    expect(client.postNoContent).toHaveBeenCalledWith("/api/v0/auth/local/mfa/totp/confirm", {
      factor_id: "factor-1",
      code: "123456",
    });
  });

  it("classifies a 400 response as invalid_code", async () => {
    const client = makeClient({
      postNoContent: vi.fn(async () => {
        throw new EshuApiHttpError(400);
      }),
    });
    const result = await confirmTOTPEnrollment(client, "factor-1", "000000");
    expect(result).toEqual({ status: "invalid_code" });
  });

  it("classifies a non-400 HTTP error as a generic error with a message", async () => {
    const client = makeClient({
      postNoContent: vi.fn(async () => {
        throw new EshuApiHttpError(503);
      }),
    });
    const result = await confirmTOTPEnrollment(client, "factor-1", "123456");
    expect(result.status).toBe("error");
  });

  it("classifies a network error as a generic error", async () => {
    const client = makeClient({
      postNoContent: vi.fn(async () => {
        throw new Error("network down");
      }),
    });
    const result = await confirmTOTPEnrollment(client, "factor-1", "123456");
    expect(result).toEqual({ status: "error", message: "network down" });
  });
});
