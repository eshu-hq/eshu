import { describe, expect, it, vi } from "vitest";

import {
  awaitRetainedWizardSessionQuiet,
  establishRetainedWizardSession,
  parseLiveE2EAuthMode,
  type RetainedWizardSessionDependencies,
} from "./liveE2EBrowserSession";

describe("retained live E2E browser session", () => {
  it("defaults the retained gate to the supported browser-session path", () => {
    expect(parseLiveE2EAuthMode(undefined)).toBe("browser_session");
    expect(parseLiveE2EAuthMode("")).toBe("browser_session");
    expect(parseLiveE2EAuthMode("browser_session")).toBe("browser_session");
    expect(parseLiveE2EAuthMode("bearer")).toBe("bearer");
    expect(() => parseLiveE2EAuthMode("unknown")).toThrow(/ESHU_E2E_AUTH_MODE/);
  });

  it("claims the retained-data proof identity through the wizard without returning secrets", async () => {
    const page = {} as never;
    const credential = {
      username: "proof-admin",
      password: "one-time-password",
      recoveryCode: "one-time-recovery",
    };
    const dependencies: RetainedWizardSessionDependencies = {
      retrieveCredential: vi.fn().mockResolvedValue({
        status: "available",
        credential,
        failureReason: null,
        rawStderr: "",
      }),
      showSetupWizard: vi.fn().mockResolvedValue(undefined),
      driveWizard: vi.fn().mockResolvedValue("generated-recovery-code"),
      assertSetupGone: vi.fn().mockResolvedValue(undefined),
    };

    await expect(
      establishRetainedWizardSession(page, {
        consoleBaseUrl: "http://127.0.0.1:5180",
        postgresDSN: "postgresql://proof.invalid/eshu",
        authSecretEncKey: "test-only-key",
        newPassword: "replacement-password",
        timeoutMs: 30_000,
        dependencies,
      }),
    ).resolves.toBe("wizard owner session established for proof-admin");

    expect(dependencies.retrieveCredential).toHaveBeenCalledWith(
      expect.stringMatching(/\/go$/),
      "postgresql://proof.invalid/eshu",
      "test-only-key",
    );
    expect(dependencies.retrieveCredential).not.toHaveBeenCalledWith(
      expect.stringMatching(/\/apps\/go$/),
      expect.anything(),
      expect.anything(),
    );
    expect(dependencies.showSetupWizard).toHaveBeenCalledWith(
      page,
      "http://127.0.0.1:5180",
      30_000,
    );
    expect(dependencies.driveWizard).toHaveBeenCalledWith(
      page,
      credential.username,
      credential.password,
      "replacement-password",
      30_000,
    );
    expect(dependencies.assertSetupGone).toHaveBeenCalledWith("http://127.0.0.1:5180/eshu-api");
  });

  it("fails closed when no retrievable first-run credential exists", async () => {
    const dependencies: RetainedWizardSessionDependencies = {
      retrieveCredential: vi.fn().mockResolvedValue({
        status: "unavailable",
        credential: null,
        failureReason: "credential_unavailable",
        rawStderr: "consumed",
      }),
      showSetupWizard: vi.fn(),
      driveWizard: vi.fn(),
      assertSetupGone: vi.fn(),
    };

    await expect(
      establishRetainedWizardSession({} as never, {
        consoleBaseUrl: "http://127.0.0.1:5180",
        postgresDSN: "postgresql://proof.invalid/eshu",
        authSecretEncKey: "test-only-key",
        newPassword: "replacement-password",
        timeoutMs: 30_000,
        dependencies,
      }),
    ).rejects.toThrow(/fresh retained-proof identity/);
  });

  it("reports credential infrastructure failure without exposing raw diagnostics", async () => {
    const dependencies: RetainedWizardSessionDependencies = {
      retrieveCredential: vi.fn().mockResolvedValue({
        status: "error",
        credential: null,
        failureReason: "postgres_unavailable",
        rawStderr: "postgresql://operator:secret@private-host/eshu connection refused",
      }),
      showSetupWizard: vi.fn(),
      driveWizard: vi.fn(),
      assertSetupGone: vi.fn(),
    };

    await expect(
      establishRetainedWizardSession({} as never, {
        consoleBaseUrl: "http://127.0.0.1:5180",
        postgresDSN: "postgresql://proof.invalid/eshu",
        authSecretEncKey: "test-only-key",
        newPassword: "replacement-password",
        timeoutMs: 30_000,
        dependencies,
      }),
    ).rejects.toThrow("credential retrieval failed (postgres_unavailable)");
    await expect(
      establishRetainedWizardSession({} as never, {
        consoleBaseUrl: "http://127.0.0.1:5180",
        postgresDSN: "postgresql://proof.invalid/eshu",
        authSecretEncKey: "test-only-key",
        newPassword: "replacement-password",
        timeoutMs: 30_000,
        dependencies,
      }),
    ).rejects.not.toThrow(/private-host|operator:secret/);
  });

  it("refuses to start retained route proof while wizard boot requests are still owned", async () => {
    const waitForQuiet = vi
      .fn()
      .mockResolvedValue({ settled: false, inFlight: 2, waitedMs: 36_000 });

    await expect(
      awaitRetainedWizardSessionQuiet(
        {} as never,
        { inFlight: () => 2, lastChangeAt: () => Date.now() },
        waitForQuiet,
      ),
    ).rejects.toThrow(/2 wizard-session API request/);
  });
});
