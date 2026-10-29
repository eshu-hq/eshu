import { afterEach, describe, expect, it, vi } from "vitest";

import { authE2EComposeArgs } from "./authE2EComposeArgs";

describe("authE2EComposeArgs", () => {
  afterEach(() => {
    vi.unstubAllEnvs();
  });

  it("uses the same final CI overlay for exec and logs", () => {
    vi.stubEnv("ESHU_DEPLOY_CHECKOUT", "/tmp/eshu-deploy");
    expect(authE2EComposeArgs("proof", "/tmp/overlay.json")).toEqual([
      "compose",
      "-p",
      "proof",
      "-f",
      "/tmp/eshu-deploy/compose/docker-compose.e2e.yaml",
      "-f",
      "/tmp/overlay.json",
    ]);
  });

  it("resolves the deploy-checkout stack when the overlay is empty", () => {
    vi.stubEnv("ESHU_DEPLOY_CHECKOUT", "/tmp/eshu-deploy");
    expect(authE2EComposeArgs("proof", "")).toEqual([
      "compose",
      "-p",
      "proof",
      "-f",
      "/tmp/eshu-deploy/compose/docker-compose.e2e.yaml",
    ]);
  });

  it("resolves the stack inside the ESHU_DEPLOY_CHECKOUT checkout", () => {
    vi.stubEnv("ESHU_DEPLOY_CHECKOUT", "/checkouts/eshu-deploy");
    expect(authE2EComposeArgs("proof", undefined)).toEqual([
      "compose",
      "-p",
      "proof",
      "-f",
      "/checkouts/eshu-deploy/compose/docker-compose.e2e.yaml",
    ]);
  });

  it("fails closed when ESHU_DEPLOY_CHECKOUT is unset", () => {
    vi.stubEnv("ESHU_DEPLOY_CHECKOUT", "");
    expect(() => authE2EComposeArgs("proof", "")).toThrow(/ESHU_DEPLOY_CHECKOUT/);
  });
});
