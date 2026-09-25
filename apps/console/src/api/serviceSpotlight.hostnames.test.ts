import { describe, expect, it } from "vitest";

import { serviceSpotlightFromContext, type ServiceContextResponse } from "./serviceSpotlight";

function hostnames(count: number): NonNullable<ServiceContextResponse["hostnames"]> {
  return Array.from({ length: count }, (_, i) => ({
    environment: "qa",
    hostname: `svc-${String(i).padStart(3, "0")}.qa.example.test`,
    relative_path: "deploy/values-qa.yaml",
  }));
}

describe("serviceSpotlightFromContext hostname count", () => {
  it("reads the true total from result_limits when the server caps hostnames at 50", () => {
    const spotlight = serviceSpotlightFromContext(
      { name: "svc", hostnames: hostnames(50), result_limits: { hostname_count: 671 } },
      "svc",
    );

    expect(spotlight.hostnameCount).toBe(671);
    expect(spotlight.hostnames.length).toBeLessThanOrEqual(12);
  });

  it("counts every returned hostname, not the 12 it displays, when there is no total", () => {
    const spotlight = serviceSpotlightFromContext({ name: "svc", hostnames: hostnames(30) }, "svc");

    expect(spotlight.hostnameCount).toBe(30);
  });
});
