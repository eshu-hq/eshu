import { describe, expect, it } from "vitest";

import type { EshuApiClient } from "./client";
import { loadConsoleSnapshot } from "./eshuConsoleLive";
import { fakeClient } from "./eshuConsoleLive.testSupport";
import { dependenciesPath, loadDependencies } from "./eshuDependencies";

// The console adapter must match the real API response shapes:
// - ecosystem/overview is enveloped and uses repo_count (not repository_count)
// - index-status and status/ingesters return RAW JSON (no envelope), so they
//   must be read with getJson, not get
// - the language overview comes from repositories/language-inventory, not
//   repositories/by-language (which requires a ?language= and 400s without it)
describe("eshuConsoleLive", () => {
  it("maps runtime counts and status from enveloped + raw endpoints", async () => {
    const snap = await loadConsoleSnapshot(fakeClient());
    expect(snap.runtime.repositories).toBe(33);
    expect(snap.runtime.workloads).toBe(21);
    expect(snap.runtime.platforms).toBe(7);
    expect(snap.runtime.instances).toBe(92);
    expect(snap.runtime.indexStatus).toBe("healthy");
    expect(snap.runtime.queueOutstanding).toBe(2);
    expect(snap.runtime.succeeded).toBe(333);
    expect(snap.runtime.profile).toBe("production");
  });

  it("reads the language overview from language-inventory (repository_count)", async () => {
    const snap = await loadConsoleSnapshot(fakeClient());
    expect(snap.languages).toEqual([
      { language: "yaml", count: 32 },
      { language: "go", count: 5 },
    ]);
  });

  it("merges the repository ingester with coordinator collector instances", async () => {
    const snap = await loadConsoleSnapshot(fakeClient());
    // 1 ingester (status/ingesters) + 3 collectors (index-status.coordinator)
    expect(snap.ingesters).toHaveLength(4);
    expect(snap.ingesters[0]).toMatchObject({
      id: "repository",
      state: "healthy",
      kind: "ingester",
    });
    const grafana = snap.ingesters.find((r) => r.id === "remote-e2e-grafana");
    expect(grafana).toMatchObject({
      kind: "grafana",
      state: "active",
      facts: null,
      freshness: "fresh",
    });
    const loki = snap.ingesters.find((r) => r.id === "remote-e2e-loki");
    // disabled + deactivated collector renders as a stale, non-active row
    expect(loki).toMatchObject({
      kind: "loki",
      state: "deactivated",
      facts: null,
      freshness: "stale",
    });
  });

  it("dedups the catalog by id across services and workloads", async () => {
    const snap = await loadConsoleSnapshot(fakeClient());
    const ids = snap.services.map((s) => s.id);
    expect(ids).toEqual(["workload:api", "workload:lib-config"]);
    expect(new Set(ids).size).toBe(ids.length);
  });

  it("maps per-service environments from the catalog response", async () => {
    const snap = await loadConsoleSnapshot(fakeClient());
    const api = snap.services.find((s) => s.id === "workload:api");
    expect(api?.environments).toEqual(["qa", "prod"]);
    // A service with no environment evidence resolves to an empty array, never
    // a fabricated value.
    const lib = snap.services.find((s) => s.id === "workload:lib-config");
    expect(lib?.environments).toEqual([]);
  });

  it("loads the SBOM/attestation count rollup and captures its truth", async () => {
    const snap = await loadConsoleSnapshot(fakeClient());
    expect(snap.sbom).toEqual({ total: 148, verified: 100, sbomCount: 120, attestationCount: 28 });
    expect(snap.provenance.sbom).toBe("live");
    expect(snap.truth.sbom?.capability).toBe("supply_chain.sbom_attestation_attachments.aggregate");
  });

  it("maps the IaC resource inventory and captures its truth envelope", async () => {
    const snap = await loadConsoleSnapshot(fakeClient());
    expect(snap.iacResources).toHaveLength(2);
    expect(snap.iacResources[0]).toMatchObject({
      id: "tf1",
      type: "aws_iam_role",
      provider: "aws",
      service: "aws.iam",
      module: "api",
      repoId: "r_1",
      relativePath: "main.tf",
    });
    // tfstate-only row keeps optional attribution empty rather than fabricated.
    expect(snap.iacResources[1]).toMatchObject({
      id: "tf2",
      type: "aws_s3_bucket",
      provider: "",
      module: "",
    });
    expect(snap.provenance.iacResources).toBe("live");
    expect(snap.truth.iacResources?.capability).toBe("iac_inventory.resources.list");
  });

  it("marks the SBOM section empty when the count endpoint reports zero attachments", async () => {
    const client = {
      get: async (path: string) => {
        if (path.includes("/sbom-attestations/attachments/count")) {
          return {
            data: { total_attachments: 0, by_attachment_status: {}, by_artifact_kind: {} },
            error: null,
            truth: null,
          };
        }
        return { data: {}, error: null, truth: null };
      },
      getJson: async () => ({ status: "healthy", queue: {} }),
      post: async () => ({ data: {}, error: null, truth: null }),
    } as unknown as EshuApiClient;
    const snap = await loadConsoleSnapshot(client);
    expect(snap.sbom).toBeNull();
    expect(snap.provenance.sbom).toBe("empty");
  });

  it("loads the default forward dependency browse and captures its truth", async () => {
    const snap = await loadConsoleSnapshot(fakeClient());
    expect(snap.dependencies).toHaveLength(1);
    expect(snap.dependencies[0]).toMatchObject({
      direction: "forward",
      anchorPackage: "@eshu/core",
      relatedPackage: "left-pad",
      ecosystem: "npm",
      range: "^1.3.0",
      optional: false,
    });
    expect(snap.truth.dependencies?.capability).toBe("dependencies.list");
    expect(snap.provenance.dependencies).toBe("live");
  });

  it("builds a reverse dependency path with the package anchor and limit", () => {
    const path = dependenciesPath({ direction: "reverse", pkg: "tslib", limit: 25 });
    expect(path).toContain("direction=reverse");
    expect(path).toContain("package=tslib");
    expect(path).toContain("limit=25");
  });

  it("returns a typed reverse dependency page with paging cursor and falls back to id for missing names", async () => {
    const client = {
      get: async (path: string) => {
        expect(path).toContain("direction=reverse");
        expect(path).toContain("package=tslib");
        return {
          data: {
            dependencies: [
              {
                direction: "reverse",
                anchor_package: "tslib",
                anchor_package_id: "npm://r/tslib",
                related_package_id: "npm://r/@eshu/web",
                related_ecosystem: "npm",
                dependency_range: "^2.5.0",
                edge_id: "e1",
              },
            ],
            direction: "reverse",
            truncated: true,
            next_cursor: { after_name: "npm://r/@eshu/web", after_edge: "e1" },
          },
          error: null,
          truth: {
            profile: "production",
            level: "exact",
            capability: "dependencies.list",
            freshness: { state: "fresh" },
          },
        };
      },
    } as unknown as EshuApiClient;

    const page = await loadDependencies(client, { direction: "reverse", pkg: "tslib" });
    expect(page.direction).toBe("reverse");
    expect(page.truncated).toBe(true);
    expect(page.nextCursor).toEqual({ afterName: "npm://r/@eshu/web", afterEdge: "e1" });
    // related_package was absent, so the row falls back to the related package id.
    expect(page.rows[0].relatedPackage).toBe("npm://r/@eshu/web");
  });

  it("loads the container image inventory head page into the snapshot", async () => {
    const snap = await loadConsoleSnapshot(fakeClient());
    expect(snap.images).toHaveLength(1);
    expect(snap.images[0]).toMatchObject({
      id: "oci-image://reg/team/api@sha256:aaa",
      registry: "reg",
      repository: "team/api",
      tag: "1.2.3",
      sizeBytes: 1234567,
    });
    expect(snap.provenance.images).toBe("live");
    expect(snap.truth.images?.capability).toBe("platform_impact.container_image_list");
  });

  it("reports the IaC section unavailable when the endpoint is not served", async () => {
    const client = {
      get: async (path: string) => {
        if (path.includes("/iac/resources")) throw new Error("501 unsupported_capability");
        if (path.includes("/ecosystem/overview")) {
          return { data: { repo_count: 1 }, error: null, truth: null };
        }
        return { data: {}, error: null, truth: null };
      },
      getJson: async () => ({ status: "healthy", queue: {} }),
      post: async () => ({ data: {}, error: null, truth: null }),
    } as unknown as EshuApiClient;
    const snap = await loadConsoleSnapshot(client);
    expect(snap.iacResources).toEqual([]);
    expect(snap.provenance.iacResources).toBe("unavailable");
  });

  it("still resolves a degraded snapshot when one endpoint aborts (timeout)", async () => {
    // Simulates issue #1680/#1678: index-status hangs and the client aborts it.
    // The affected section must degrade gracefully (runtime falls back to its
    // unknown-status baseline) while the rest of the snapshot still resolves,
    // instead of leaving the app stuck "Connecting…".
    const abort = (): never => {
      throw new DOMException("The operation was aborted due to timeout", "TimeoutError");
    };
    const client = {
      get: async (path: string) => {
        if (path.includes("/ecosystem/overview")) abort();
        if (path.includes("/catalog")) {
          return {
            data: {
              services: [{ id: "workload:api", name: "api", kind: "deployment", repo_name: "api" }],
            },
            error: null,
            truth: {
              profile: "production",
              level: "exact",
              capability: "x",
              freshness: { state: "fresh" },
            },
          };
        }
        return { data: {}, error: null, truth: null };
      },
      getJson: async (path: string) => {
        // index-status is the hung endpoint that aborts under timeout.
        if (path.includes("/index-status")) abort();
        return {};
      },
      post: async () => ({ data: {}, error: null, truth: null }),
    } as unknown as EshuApiClient;

    const snap = await loadConsoleSnapshot(client);

    // The snapshot resolved (no hang). The runtime section swallows its own
    // optional sub-fetch failures and degrades to the unknown-status baseline
    // rather than throwing, so the dashboard still renders.
    expect(snap.runtime.indexStatus).toBe("unknown");
    expect(snap.runtime.repositories).toBe(0);
    // A healthy section still rendered live alongside the degraded one.
    expect(snap.provenance.services).toBe("live");
    expect(snap.services).toHaveLength(1);
  });

  it("loads dashboard trend series from the metrics time-series endpoint", async () => {
    const requested: string[] = [];
    const client = {
      get: async (path: string) => {
        requested.push(path);
        if (path.includes("/metrics/timeseries")) {
          const metric = new URL(path, "http://console.test").searchParams.get("metric");
          return {
            data: { points: [{ t: "2026-06-01T00:00:00Z", v: metric === "ingest_rate" ? 12 : 4 }] },
            error: null,
            truth: {
              profile: "production",
              level: "derived",
              capability: "platform_metrics.timeseries",
              freshness: { state: "fresh" },
            },
          };
        }
        if (path.includes("/ecosystem/overview")) {
          return {
            data: { repo_count: 1 },
            error: null,
            truth: {
              profile: "production",
              level: "exact",
              capability: "x",
              freshness: { state: "fresh" },
            },
          };
        }
        return { data: {}, error: null, truth: null };
      },
      getJson: async () => ({ status: "healthy", queue: { outstanding: 4 } }),
      post: async () => ({ data: {}, error: null, truth: null }),
    } as unknown as EshuApiClient;

    const snap = await loadConsoleSnapshot(client);
    expect(snap.series.ingestRate).toEqual([12]);
    expect(snap.series.queueDepth).toEqual([4]);
    expect(snap.series.deadLetters).toEqual([4]);
    expect(snap.series.queryP50).toEqual([4]);
    expect(snap.series.queryP95).toEqual([4]);
    expect(snap.series.queryP99).toEqual([4]);
    expect(requested).toContain(
      "/api/v0/metrics/timeseries?metric=ingest_rate&window=24h&step=30m",
    );
    expect(requested).toContain(
      "/api/v0/metrics/timeseries?metric=queue_depth&window=24h&step=30m",
    );
    expect(requested).toContain(
      "/api/v0/metrics/timeseries?metric=dead_letters&window=24h&step=30m",
    );
    expect(requested).toContain("/api/v0/metrics/timeseries?metric=query_p50&window=24h&step=30m");
    expect(requested).toContain("/api/v0/metrics/timeseries?metric=query_p95&window=24h&step=30m");
    expect(requested).toContain("/api/v0/metrics/timeseries?metric=query_p99&window=24h&step=30m");
  });

  it("uses repositories API total over index-status repository_count for sidebar count", async () => {
    // Regression for issue #3392: loadRuntime must read the repositories total
    // field (true graph COUNT independent of page) rather than the
    // index-status repository_count, so the sidebar, Dashboard tile, and
    // status/index all display the same number.
    const client = {
      get: async (path: string) => {
        if (path.includes("/ecosystem/overview")) {
          return {
            data: { repo_count: 500, workload_count: 0, platform_count: 0, instance_count: 0 },
            error: null,
            truth: null,
          };
        }
        return { data: {}, error: null, truth: null };
      },
      getJson: async (path: string) => {
        if (path.includes("/index-status")) {
          return { status: "healthy", repository_count: 906, queue: {} };
        }
        if (path.includes("/repositories")) {
          // The repositories list probe returns total=951 (true graph count).
          return { count: 1, total: 951, limit: 1, offset: 0, truncated: true };
        }
        return {};
      },
      post: async () => ({ data: {}, error: null, truth: null }),
    } as unknown as EshuApiClient;

    const snap = await loadConsoleSnapshot(client);
    // total from repositories API wins over index-status repository_count (906)
    // and ecosystem repo_count (500).
    expect(snap.runtime.repositories).toBe(951);
  });
});
