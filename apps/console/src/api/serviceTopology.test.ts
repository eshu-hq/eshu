import { describe, expect, it } from "vitest";
import type { ServiceTrafficPath } from "./serviceTrafficPath";
import { buildServiceTopology } from "./serviceTopology";
import type { ServiceRow } from "./eshuConsoleLive";

const service: ServiceRow = {
  freshness: "fresh",
  id: "svc-api",
  kind: "service",
  name: "api-node-boats",
  repo: "boats/api-node-boats",
  environments: ["bg-prod"],
  truth: "exact"
};

function trafficPath(overrides: Partial<ServiceTrafficPath> = {}): ServiceTrafficPath {
  return {
    edge: "CloudFront distribution E2BGBOATS",
    environment: "bg-prod",
    evidenceKind: "aws_cloudfront_distribution",
    hostname: "www.boats.com",
    origin: "origin-alb-boats",
    reason: "CloudFront distribution E2BGBOATS",
    runtime: "eks-bg-prod",
    sourceRepo: "boats/api-node-boats",
    visibility: "public",
    workload: "api-node-boats",
    ...overrides
  };
}

describe("buildServiceTopology", () => {
  it("renders observed traffic evidence without inventing extra ingress nodes", () => {
    const graph = buildServiceTopology({ service, trafficPaths: [trafficPath()] });

    expect(graph.meta.provenance).toBe("live");
    expect(graph.nodes.map((node) => node.label)).toEqual(
      expect.arrayContaining([
        "www.boats.com",
        "E2BGBOATS",
        "origin-alb-boats",
        "eks-bg-prod",
        "api-node-boats",
        "boats/api-node-boats"
      ])
    );
    expect(graph.nodes.map((node) => node.label)).not.toContain("WAF web ACL");
    expect(graph.edges.map((edge) => edge.verb)).toEqual(
      expect.arrayContaining(["ROUTES_TO", "ORIGINATES_AT", "RUNS_ON", "DEPLOYS"])
    );
  });

  it("shows an explicit pending state when no traffic path is available", () => {
    const graph = buildServiceTopology({ service, trafficPaths: [] });

    expect(graph.meta.provenance).toBe("unavailable");
    expect(graph.nodes.map((node) => node.label)).toContain("Entry evidence pending");
    expect(graph.nodes.map((node) => node.label)).not.toContain("CloudFront");
    expect(graph.edges.every((edge) => edge.provenance !== "fabricated")).toBe(true);
  });

  it("fits long labels into bounded nodes", () => {
    const graph = buildServiceTopology({
      service: {
        ...service,
        name: "api-node-salesforce-sync-with-extremely-long-production-name"
      },
      trafficPaths: [
        trafficPath({
          hostname: "api-node-salesforce-sync-with-extremely-long-production-name.internal.bg",
          sourceRepo: "enterprise-platform/api-node-salesforce-sync-with-extremely-long-production-name"
        })
      ]
    });

    expect(Math.max(...graph.nodes.map((node) => node.w))).toBeLessThanOrEqual(328);
    expect(graph.nodes.every((node) => node.label.length > 0)).toBe(true);
  });
});
