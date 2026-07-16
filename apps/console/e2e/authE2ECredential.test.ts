import { describe, expect, it } from "vitest";

import { classifyCredentialFailure } from "./authE2ECredential.ts";

describe("initial credential failure classification", () => {
  it("distinguishes an unavailable one-time credential from infrastructure failures", () => {
    expect(
      classifyCredentialFailure(
        "Error: no retrievable bootstrap credential: it was already consumed by a login",
      ),
    ).toEqual({ status: "unavailable", reason: "credential_unavailable" });
    expect(
      classifyCredentialFailure(
        "cannot decrypt the sealed bootstrap credential: configured key differs",
      ),
    ).toEqual({ status: "error", reason: "encryption_key_mismatch" });
    expect(classifyCredentialFailure("Command timed out after 60000 milliseconds")).toEqual({
      status: "error",
      reason: "credential_command_timeout",
    });
    expect(classifyCredentialFailure("ping postgres connection: connection refused")).toEqual({
      status: "error",
      reason: "postgres_unavailable",
    });
  });

  it("uses a bounded safe code for unknown failures", () => {
    expect(classifyCredentialFailure("unexpected low-level text containing local details")).toEqual(
      { status: "error", reason: "credential_command_failed" },
    );
  });
});
