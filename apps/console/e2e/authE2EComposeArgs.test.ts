import { describe, expect, it } from "vitest";

import { authE2EComposeArgs } from "./authE2EComposeArgs";

describe("authE2EComposeArgs", () => {
  it("uses the same final CI overlay for exec and logs", () => {
    expect(authE2EComposeArgs("proof", "/tmp/overlay.json")).toEqual([
      "compose", "-p", "proof", "-f", "docker-compose.e2e.yaml", "-f", "/tmp/overlay.json",
    ]);
  });

  it("keeps the product Compose command unchanged outside CI", () => {
    expect(authE2EComposeArgs("proof", "")).toEqual([
      "compose", "-p", "proof", "-f", "docker-compose.e2e.yaml",
    ]);
  });
});
