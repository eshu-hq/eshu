import { describe, expect, it, vi } from "vitest";
import type { ServiceTrafficPath } from "./serviceTrafficPath";
import { buildServiceTopology, loadTopologyServices } from "./serviceTopology";
import type { ServiceRow } from "./eshuConsoleLive";
import type { EshuApiClient } from "./client";

const service: ServiceRow = {
  freshness: "fresh",
  id: "svc-api",
  kind: "service",
  name: "svc-catalog",
  repo: "catalog/svc-catalog",
  environments: ["acme-prod"],
  truth: "exact"
};

function trafficPath(overrides: Partial<ServiceTrafficPath> = {}): ServiceTrafficPath {
  return {
    edge: "CloudFront distribution E2EXAMPLE0001",
    environment: "acme-prod",
    evidenceKind: "aws_cloudfront_distribution",
    hostname: "www.example.com",
    origin: "origin-alb-catalog",
    reason: "CloudFront distribution E2EXAMPLE0001",
    runtime: "eks-acme-prod",
    sourceRepo: "catalog/svc-catalog",
    visibility: "public",
    workload: "svc-catalog",
    ...overrides
  };
}

describe("buildServiceTopology", () => {
  it("renders observed traffic evidence without inventing extra ingress nodes", () => {
    const graph = buildServiceTopology({ service, trafficPaths: [trafficPath()] });

    expect(graph.meta.provenance).toBe("live");
    expect(graph.nodes.map((node) => node.label)).toEqual(
      expect.arrayContaining([
        "www.example.com",
        "E2EXAMPLE0001",
        "origin-alb-catalog",
        "eks-acme-prod",
        "svc-catalog",
        "catalog/svc-catalog"
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
    expect(graph.edges.every((edge) => edge.provenance === "live" || edge.provenance === "unavailable")).toBe(true);
  });

  it("renders deployment artifact chains instead of generic delivery placeholders", () => {
    const graph = buildServiceTopology({
      deploymentArtifacts: [
        {
          artifact_family: "kustomize",
          relationship_type: "DEPLOYS_FROM",
          source_repo_id: "repository:iac",
          source_repo_name: "iac-eks-argocd",
          target_repo_id: "repository:service",
          target_repo_name: "svc-catalog"
        },
        {
          artifact_family: "helm",
          path: "charts/svc-catalog/Chart.yaml",
          relationship_type: "DEPLOYS_FROM",
          source_repo_id: "repository:helm",
          source_repo_name: "helm-charts",
          target_repo_id: "repository:service",
          target_repo_name: "svc-catalog"
        }
      ],
      service,
      trafficPaths: [trafficPath()]
    });

    expect(graph.nodes.map((node) => node.label)).toEqual(expect.arrayContaining([
      "iac-eks-argocd",
      "helm-charts",
      "svc-catalog"
    ]));
    expect(graph.nodes.map((node) => node.label)).not.toContain("Delivery evidence");
    expect(graph.edges).toEqual(expect.arrayContaining([
      expect.objectContaining({ s: "repository:iac", t: "repository:helm", verb: "DEPLOYS_HELM", layer: "deploy" }),
      expect.objectContaining({ s: "repository:helm", t: "repository:service", verb: "PACKAGES", layer: "deploy" }),
      expect.objectContaining({ s: "repository:service", t: "workload", verb: "DEPLOYS_FROM", layer: "deploy" })
    ]));
    expect(graph.edges.some((edge) => edge.verb === "BUILDS" || edge.verb === "DEPLOYS")).toBe(false);
  });

  it("marks topology live when deployment artifacts exist without traffic evidence", () => {
    const graph = buildServiceTopology({
      deploymentArtifacts: [{
        artifact_family: "helm",
        path: "charts/svc-catalog/Chart.yaml",
        relationship_type: "DEPLOYS_FROM",
        source_repo_id: "repository:helm",
        source_repo_name: "helm-charts",
        target_repo_id: "repository:service",
        target_repo_name: "svc-catalog"
      }],
      service,
      trafficPaths: []
    });

    expect(graph.meta.provenance).toBe("live");
    expect(graph.nodes.map((node) => node.label)).toEqual(expect.arrayContaining([
      "Entry evidence pending",
      "helm-charts",
      "svc-catalog"
    ]));
    expect(graph.edges).toContainEqual(expect.objectContaining({
      s: "repository:service",
      t: "workload",
      verb: "DEPLOYS_FROM",
      provenance: "live"
    }));
  });

  it("fits long labels into bounded nodes", () => {
    const graph = buildServiceTopology({
      service: {
        ...service,
        name: "svc-salesforce-sync-with-extremely-long-production-name"
      },
      trafficPaths: [
        trafficPath({
          hostname: "svc-salesforce-sync-with-extremely-long-production-name.internal.bg",
          sourceRepo: "enterprise-platform/svc-salesforce-sync-with-extremely-long-production-name"
        })
      ]
    });

    expect(Math.max(...graph.nodes.map((node) => node.w))).toBeLessThanOrEqual(328);
    expect(graph.nodes.every((node) => node.label.length > 0)).toBe(true);
  });
});

describe("loadTopologyServices", () => {
  it("returns services and workloads from the catalog when present", async () => {
    const client = {
      getJson: vi.fn(async () => ({
        services: [
          { id: "svc-1", name: "alpha-service", kind: "service", repo_name: "org/alpha", environments: ["prod"] }
        ],
        workloads: [
          { id: "wl-1", name: "beta-worker", kind: "workload", repo_name: "org/beta", environments: ["prod"] }
        ],
        repositories: []
      }))
    } as unknown as EshuApiClient;

    const rows = await loadTopologyServices(client);

    expect(rows.length).toBe(2);
    expect(rows.map((r) => r.name)).toEqual(expect.arrayContaining(["alpha-service", "beta-worker"]));
    expect(rows[0].kind).toBe("service");
  });

  it("falls back to repositories when services and workloads are absent", async () => {
    const client = {
      getJson: vi.fn(async () => ({
        repositories: [
          { id: "repo-1", name: "gamma-api", repo_slug: "org/gamma-api" },
          { id: "repo-2", name: "delta-lib", repo_slug: "org/delta-lib" }
        ]
      }))
    } as unknown as EshuApiClient;

    const rows = await loadTopologyServices(client);

    expect(rows.length).toBe(2);
    expect(rows.map((r) => r.name)).toEqual(expect.arrayContaining(["gamma-api", "delta-lib"]));
    expect(rows.every((r) => r.kind === "repository")).toBe(true);
  });

  it("returns an empty list and does not throw when the catalog call fails", async () => {
    const client = {
      getJson: vi.fn(async () => { throw new Error("network error"); })
    } as unknown as EshuApiClient;

    const rows = await loadTopologyServices(client);

    expect(rows).toEqual([]);
  });

  it("deduplicates entries that appear in both services and workloads", async () => {
    const client = {
      getJson: vi.fn(async () => ({
        services: [
          { id: "svc-overlap", name: "shared-service", kind: "service", repo_name: "org/shared", environments: [] }
        ],
        workloads: [
          { id: "svc-overlap", name: "shared-service", kind: "workload", repo_name: "org/shared", environments: [] }
        ]
      }))
    } as unknown as EshuApiClient;

    const rows = await loadTopologyServices(client);

    expect(rows.length).toBe(1);
    expect(rows[0].name).toBe("shared-service");
  });

  it("sorts results so named entries precede blank names", async () => {
    const client = {
      getJson: vi.fn(async () => ({
        services: [
          { id: "svc-blank", name: "", kind: "service", environments: [] },
          { id: "svc-named", name: "named-service", kind: "service", environments: [] }
        ]
      }))
    } as unknown as EshuApiClient;

    const rows = await loadTopologyServices(client);

    expect(rows[0].name).toBe("named-service");
  });
});
