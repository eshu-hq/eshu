import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import vm from "node:vm";
import { describe, expect, it } from "vitest";

interface PrototypeClient {
  readonly paths: string[];
  get(path: string): Promise<unknown>;
  post(path: string, body: unknown): Promise<unknown>;
}

interface PrototypeModel {
  readonly deadCode?: readonly {
    readonly entityId: string;
    readonly file: string;
    readonly repo: string;
    readonly repoId: string;
    readonly symbol: string;
  }[];
  readonly prov: Record<string, string>;
  readonly advisoryCatalog?: readonly { readonly id: string; readonly severity: string; readonly cvss: number; readonly kev: boolean }[];
  readonly cloudAccounts?: readonly { readonly id: string; readonly provider: string; readonly account: string }[];
  readonly cloudInventory?: {
    readonly count: number;
    readonly rows: readonly { readonly uid: string; readonly provider: string; readonly resourceType: string; readonly scope: string }[];
  };
  readonly cloudResources?: readonly {
    readonly uid: string;
    readonly provider: string;
    readonly account: string;
    readonly type: string;
    readonly family: string;
    readonly name: string;
    readonly tf: boolean;
  }[];
  readonly imageInventory?: readonly {
    readonly id: string;
    readonly registry: string;
    readonly repository: string;
    readonly tag: string;
    readonly sizeBytes: number | null;
  }[];
  readonly dependencyInventory?: readonly {
    readonly anchorPackage: string;
    readonly relatedPackage: string;
    readonly declaringVersion: string;
    readonly range: string;
    readonly dependencyType: string;
  }[];
  readonly iacParityRows?: readonly {
    readonly category: string;
    readonly lineNumber: number | null;
    readonly relativePath: string;
    readonly repoId: string;
    readonly resourceName: string;
  }[];
  readonly sbomInventory?: { readonly buckets: readonly { readonly value: string; readonly dimension: string; readonly count: number }[] };
  readonly langInventory?: readonly { readonly label: string; readonly value: number }[];
  readonly metrics?: {
    readonly ingestRate?: readonly number[];
    readonly queueDepth?: readonly number[];
    readonly deadLetters?: readonly number[];
    readonly graphNodes?: readonly number[];
    readonly graphEdges?: readonly number[];
    readonly queryP50?: readonly number[];
    readonly queryP95?: readonly number[];
    readonly queryP99?: readonly number[];
  };
  readonly obsCoverage?: Record<string, Record<string, { readonly state: string; readonly ref: string; readonly freshness: string }>>;
}

interface PrototypeWindow {
  ESHU: {
    loadLive(client: PrototypeClient): Promise<PrototypeModel>;
  };
}

function repoRoot(): string {
  return process.cwd().endsWith("apps/console") ? resolve(process.cwd(), "../..") : process.cwd();
}

const loaderPath = resolve(repoRoot(), "apps/console/prototype/eshu-console/console/live-parity-loader.js");
const deadCodeLoaderPath = resolve(repoRoot(), "apps/console/prototype/eshu-console/console/live-dead-code-loader.js");

function loadPrototypeWindow(): PrototypeWindow {
  const win: PrototypeWindow = {
    ESHU: {
      async loadLive(client: PrototypeClient) {
        await client.get("/api/v0/repositories/by-language?limit=100&offset=0");
        await client.get("/api/v0/observability/coverage/correlations?limit=200");
        await client.post("/api/v0/code/imports/investigate", { repo_id: "repository:r1", query_type: "module_dependencies", limit: 80 });
        await client.post("/api/v0/code/call-graph/metrics", { repo_id: "repository:r1", metric: "hub_functions", limit: 8 });
        return { prov: {} };
      }
    }
  };
  vm.runInNewContext(readFileSync(deadCodeLoaderPath, "utf8"), { window: win, Number, Boolean });
  vm.runInNewContext(readFileSync(loaderPath, "utf8"), { window: win, Number, Boolean });
  return win;
}

function liveClient(): PrototypeClient {
  return {
    paths: [],
    async get(path: string): Promise<unknown> {
      this.paths.push(path);
      if (path.includes("/repositories/language-inventory")) {
        return { data: { languages: [{ language: "typescript", repository_count: 3 }] } };
      }
      if (path.includes("/repositories/by-language")) {
        throw new Error("old by-language endpoint should not be used for inventory");
      }
      if (path.includes("/observability/coverage/correlations")) {
        if (!path.includes("provider=")) throw new Error("coverage correlations require a provider anchor");
        const provider = new URL("http://local" + path).searchParams.get("provider") ?? "unknown";
        return {
          data: {
            correlations: [{
              correlation_id: `${provider}:logs:svc`,
              provider,
              coverage_signal: "log_signal",
              observability_object_ref: `${provider}:logs`,
              target_service_ref: "svc-platform",
              coverage_status: "covered",
              freshness_state: "fresh"
            }]
          }
        };
      }
      if (path.includes("/metrics/timeseries")) {
        const metric = new URL("http://local" + path).searchParams.get("metric");
        return {
          data: {
            points: [{ t: "2026-06-01T00:00:00Z", v: metric === "ingest_rate" ? 12 : 4 }]
          }
        };
      }
      if (path.includes("/cloud/inventory")) {
        return {
          data: {
            count: 1,
            limit: 50,
            truncated: false,
            resources: [{
              cloud_resource_uid: "gcp:project-synthetic:compute.googleapis.com/Instance:vm-1",
              provider: "gcp",
              resource_type: "compute.googleapis.com/Instance",
              management_origin: "declared",
              scope_id: "cloud-scope:gcp:project-synthetic",
              source_state: "exact",
              evidence: { declared: true, applied: true, observed: false }
            }]
          },
          error: null,
          truth: {
            profile: "production",
            level: "exact",
            capability: "cloud_inventory.readback.list",
            freshness: { state: "fresh" }
          }
        };
      }
      if (path.includes("/images")) {
        return {
          data: {
            images: [{
              id: "oci-image://reg/team/api@sha256:aaa",
              digest: "sha256:aaa",
              registry: "reg",
              repository: "team/api",
              tag: "1.2.3",
              size_bytes: 1234,
              media_type: "application/vnd.oci.image.manifest.v1+json",
              source_system: "oci_registry"
            }]
          },
          error: null,
          truth: { level: "exact", freshness: { state: "fresh" } }
        };
      }
      if (path.includes("/supply-chain/sbom-attestations/attachments/inventory")) {
        return {
          data: {
            group_by: "subject_digest",
            truncated: false,
            buckets: [{ dimension: "subject_digest", value: "sha256:aaa", count: 3 }]
          }
        };
      }
      if (path.includes("/supply-chain/sbom-attestations/attachments/count")) {
        return {
          data: {
            total_attachments: 3,
            by_attachment_status: { attached_verified: 2 },
            by_artifact_kind: { sbom: 2, attestation: 1 }
          }
        };
      }
      if (path.includes("/iac/resources")) {
        return {
          data: {
            resources: [{ id: "iac-resource:aws_s3_bucket.logs", kind: "resource", name: "aws_s3_bucket.logs", resource_name: "logs", type: "aws_s3_bucket", provider: "aws", resource_service: "svc-platform", resource_category: "storage", module: "modules/logs", repo_id: "repository:iac", relative_path: "terraform/logs.tf", line_number: 42 }]
          },
          truth: { level: "exact", freshness: { state: "fresh" } }
        };
      }
      if (path.includes("/supply-chain/advisories")) {
        return {
          data: {
            advisories: [{
              advisory_key: "CVE-2026-0001",
              cve_id: "CVE-2026-0001",
              severity_label: "critical",
              cvss_score: 9.8,
              kev: true,
              ecosystems: ["npm"],
              package_ids: ["pkg:npm/express"]
            }]
          }
        };
      }
      if (path.includes("/dependencies")) {
        return {
          data: {
            dependencies: [{
              direction: "forward",
              anchor_package: "@eshu/core",
              anchor_package_id: "pkg:npm/%40eshu/core",
              declaring_version: "1.0.0",
              related_package: "left-pad",
              related_package_id: "pkg:npm/left-pad",
              related_ecosystem: "npm",
              dependency_range: "^1.3.0",
              dependency_type: "runtime",
              optional: false,
              edge_id: "dep-edge-1"
            }],
            direction: "forward",
            truncated: false
          },
          error: null,
          truth: { level: "exact", freshness: { state: "fresh" } }
        };
      }
      return { data: { images: [], resources: [], buckets: [], dependencies: [] } };
    },
    async post(path: string): Promise<unknown> {
      this.paths.push(path);
      if (path.includes("/code/imports/investigate") || path.includes("/code/call-graph/metrics")) {
        throw new Error("legacy prototype code graph endpoints should be shielded in live mode");
      }
      return { data: {} };
    }
  };
}

describe("prototype live parity loader", () => {
  it("hydrates language inventory and observability with current live API contracts", async () => {
    const win = loadPrototypeWindow();
    const client = liveClient();

    const model = await win.ESHU.loadLive(client);

    expect(client.paths).toContain("/api/v0/repositories/language-inventory?limit=100&offset=0");
    expect(client.paths.some((path) => path.includes("/repositories/by-language"))).toBe(false);
    expect(client.paths.some((path) => path.includes("/code/imports/investigate"))).toBe(false);
    expect(client.paths.some((path) => path.includes("/code/call-graph/metrics"))).toBe(false);
    expect(client.paths).toEqual(expect.arrayContaining([
      "/api/v0/observability/coverage/correlations?provider=grafana&limit=200",
      "/api/v0/observability/coverage/correlations?provider=prometheus&limit=200",
      "/api/v0/observability/coverage/correlations?provider=loki&limit=200",
      "/api/v0/observability/coverage/correlations?provider=tempo&limit=200"
    ]));
    expect(model.langInventory).toEqual([{ label: "typescript", value: 3 }]);
    expect(model.obsCoverage?.["svc-platform"]?.logs).toMatchObject({
      state: "covered",
      ref: "grafana:logs",
      freshness: "fresh"
    });
    expect(model.prov.langInventory).toBe("live");
    expect(model.prov.obsCoverage).toBe("live");
    expect(client.paths).toContain("/api/v0/supply-chain/advisories?limit=50");
    expect(client.paths).toContain("/api/v0/metrics/timeseries?metric=ingest_rate&window=24h&step=30m");
    expect(client.paths).toContain("/api/v0/metrics/timeseries?metric=queue_depth&window=24h&step=30m");
    expect(client.paths).toContain("/api/v0/metrics/timeseries?metric=dead_letters&window=24h&step=30m");
    expect(client.paths).toContain("/api/v0/metrics/timeseries?metric=graph_nodes&window=24h&step=30m");
    expect(client.paths).toContain("/api/v0/metrics/timeseries?metric=graph_edges&window=24h&step=30m");
    expect(client.paths).toContain("/api/v0/metrics/timeseries?metric=query_p50&window=24h&step=30m");
    expect(client.paths).toContain("/api/v0/metrics/timeseries?metric=query_p95&window=24h&step=30m");
    expect(client.paths).toContain("/api/v0/metrics/timeseries?metric=query_p99&window=24h&step=30m");
    expect(model.metrics?.ingestRate).toEqual([12]);
    expect(model.metrics?.queueDepth).toEqual([4]);
    expect(model.metrics?.deadLetters).toEqual([4]);
    expect(model.metrics?.queryP50).toEqual([4]);
    expect(model.metrics?.queryP95).toEqual([4]);
    expect(model.metrics?.queryP99).toEqual([4]);
    expect(model.prov.metrics).toBe("live");
    expect(client.paths).toContain("/api/v0/cloud/inventory?limit=50");
    expect(model.cloudInventory?.rows[0]).toMatchObject({
      uid: "gcp:project-synthetic:compute.googleapis.com/Instance:vm-1",
      provider: "gcp",
      resourceType: "compute.googleapis.com/Instance",
      scope: "cloud-scope:gcp:project-synthetic"
    });
    expect(model.cloudResources?.[0]).toMatchObject({
      uid: "gcp:project-synthetic:compute.googleapis.com/Instance:vm-1",
      provider: "gcp",
      account: "cloud-scope:gcp:project-synthetic",
      type: "compute.googleapis.com/Instance",
      family: "compute",
      name: "vm-1",
      tf: true
    });
    expect(client.paths).toContain("/api/v0/images?limit=50&offset=0");
    expect(model.imageInventory?.[0]).toMatchObject({
      id: "oci-image://reg/team/api@sha256:aaa",
      registry: "reg",
      repository: "team/api",
      tag: "1.2.3",
      sizeBytes: 1234
    });
    expect(model.imageInventory?.[0]).not.toHaveProperty("service");
    expect(model.imageInventory?.[0]).not.toHaveProperty("vulnCount");
    expect(model.cloudAccounts?.[0]).toMatchObject({
      id: "cloud-scope:gcp:project-synthetic",
      provider: "gcp",
      account: "cloud-scope:gcp:project-synthetic"
    });
    expect(model.iacParityRows?.[0]).toMatchObject({
      category: "storage",
      lineNumber: 42,
      relativePath: "terraform/logs.tf",
      repoId: "repository:iac",
      resourceName: "logs"
    });
    expect(model.prov.cloudInventory).toBe("live");
    expect(model.advisoryCatalog?.[0]).toMatchObject({
      id: "CVE-2026-0001",
      severity: "critical",
      cvss: 9.8,
      kev: true
    });
    expect(model.prov.advisoryCatalog).toBe("live");
    expect(client.paths).toContain("/api/v0/dependencies?direction=forward&limit=50");
    expect(model.dependencyInventory?.[0]).toMatchObject({
      anchorPackage: "@eshu/core",
      relatedPackage: "left-pad",
      declaringVersion: "1.0.0",
      range: "^1.3.0",
      dependencyType: "runtime"
    });
    expect(model.sbomInventory?.buckets[0]).toMatchObject({
      value: "sha256:aaa",
      dimension: "subject_digest",
      count: 3
    });
    expect(model.sbomInventory?.buckets[0]).not.toHaveProperty("advisory");
    expect(model.sbomInventory?.buckets[0]).not.toHaveProperty("services");
  });

  it("keeps live dead-code candidates even when source metadata is partial", async () => {
    const win = loadPrototypeWindow();
    const client: PrototypeClient = {
      paths: [],
      async get(path: string): Promise<unknown> {
        this.paths.push(path);
        if (path.includes("/repositories?limit=500&offset=0")) {
          return { data: { repositories: [{ id: "repository:r1", name: "svc-platform" }] } };
        }
        if (path.includes("/observability/coverage/correlations")) return { data: { correlations: [] } };
        if (path.includes("/metrics/timeseries")) return { data: { points: [] } };
        if (path.includes("/cloud/inventory")) return { data: { resources: [] } };
        if (path.includes("/supply-chain/sbom-attestations/attachments/count")) return { data: { total_attachments: 0 } };
        return { data: { advisories: [], buckets: [], dependencies: [], images: [], languages: [], resources: [] } };
      },
      async post(path: string): Promise<unknown> {
        this.paths.push(path);
        if (path.includes("/code/dead-code")) {
          return {
            data: {
              results: [
                {
                  classification: "unused",
                  entity_id: "content-entity:e1",
                  file_path: "server/routes.ts",
                  labels: ["Function"],
                  name: "unusedRoute",
                  repo_id: "repository:r1",
                  start_line: 10
                },
                {
                  classification: "ambiguous",
                  labels: ["Function"],
                  name: "missingSourceCandidate",
                  repo_id: "repository:r1"
                }
              ]
            },
            truth: { level: "derived" }
          };
        }
        return { data: {} };
      }
    };

    const model = await win.ESHU.loadLive(client);

    expect(model.deadCode?.map((row) => row.symbol)).toEqual(["unusedRoute", "missingSourceCandidate"]);
    expect(model.deadCode?.[1]).toMatchObject({
      entityId: "",
      file: "",
      repo: "svc-platform",
      repoId: "repository:r1"
    });
    expect(model.prov.deadCode).toBe("live");
  });

  it("marks prototype live sections unavailable when APIs return error envelopes", async () => {
    const win = loadPrototypeWindow();
    const client: PrototypeClient = {
      paths: [],
      async get(path: string): Promise<unknown> {
        this.paths.push(path);
        if (path.includes("/cloud/inventory")) {
          return {
            data: {
              resources: [{
                cloud_resource_uid: "aws:123:instance:i-1",
                provider: "aws",
                resource_type: "ec2_instance",
                scope_id: "cloud-scope:aws:123"
              }]
            },
            error: { message: "cloud inventory query failed" }
          };
        }
        if (path.includes("/observability/coverage/correlations")) return { data: { correlations: [] } };
        if (path.includes("/metrics/timeseries")) return { data: { points: [] } };
        if (path.includes("/repositories?limit=500&offset=0")) return { data: { repositories: [] } };
        if (path.includes("/supply-chain/sbom-attestations/attachments/count")) return { data: { total_attachments: 0 } };
        return { data: { advisories: [], buckets: [], dependencies: [], images: [], languages: [], resources: [] } };
      },
      async post(path: string): Promise<unknown> {
        this.paths.push(path);
        return { data: { results: [] } };
      }
    };

    const model = await win.ESHU.loadLive(client);

    expect(model.cloudInventory).toBeUndefined();
    expect(model.prov.cloudInventory).toBe("error:cloud inventory query failed");
  });

  it("marks prototype dead-code unavailable when the dead-code API returns an error envelope", async () => {
    const win = loadPrototypeWindow();
    const client: PrototypeClient = {
      paths: [],
      async get(path: string): Promise<unknown> {
        this.paths.push(path);
        if (path.includes("/repositories?limit=500&offset=0")) return { data: { repositories: [] } };
        if (path.includes("/observability/coverage/correlations")) return { data: { correlations: [] } };
        if (path.includes("/metrics/timeseries")) return { data: { points: [] } };
        if (path.includes("/supply-chain/sbom-attestations/attachments/count")) return { data: { total_attachments: 0 } };
        return { data: { advisories: [], buckets: [], dependencies: [], images: [], languages: [], resources: [] } };
      },
      async post(path: string): Promise<unknown> {
        this.paths.push(path);
        if (path.includes("/code/dead-code")) {
          return {
            data: {
              results: [{
                classification: "unused",
                entity_id: "content-entity:e1",
                file_path: "server/routes.ts",
                labels: ["Function"],
                name: "unusedRoute",
                repo_id: "repository:r1"
              }]
            },
            error: { message: "dead-code query failed" }
          };
        }
        return { data: {} };
      }
    };

    const model = await win.ESHU.loadLive(client);

    expect(model.deadCode).toBeUndefined();
    expect(model.prov.deadCode).toBe("error:dead-code query failed");
  });
});
