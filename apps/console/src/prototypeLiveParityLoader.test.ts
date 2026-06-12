import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import vm from "node:vm";
import { describe, expect, it } from "vitest";

interface PrototypeClient {
  readonly paths: string[];
  get(path: string): Promise<unknown>;
}

interface PrototypeModel {
  readonly prov: Record<string, string>;
  readonly advisoryCatalog?: readonly { readonly id: string; readonly severity: string; readonly cvss: number; readonly kev: boolean }[];
  readonly langInventory?: readonly { readonly label: string; readonly value: number }[];
  readonly obsCoverage?: Record<string, Record<string, { readonly state: string; readonly ref: string; readonly freshness: string }>>;
}

interface PrototypeWindow {
  ESHU: {
    loadLive(client: PrototypeClient): Promise<PrototypeModel>;
  };
}

const loaderPath = resolve(process.cwd(), "apps/console/prototype/eshu-console/console/live-parity-loader.js");

function loadPrototypeWindow(): PrototypeWindow {
  const win: PrototypeWindow = {
    ESHU: {
      async loadLive(client: PrototypeClient) {
        await client.get("/api/v0/repositories/by-language?limit=100&offset=0");
        await client.get("/api/v0/observability/coverage/correlations?limit=200");
        return { prov: {} };
      }
    }
  };
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
              target_service_ref: "api-node-platform",
              coverage_status: "covered",
              freshness_state: "fresh"
            }]
          }
        };
      }
      if (path.includes("/supply-chain/sbom-attestations/attachments/count")) {
        return { data: { total_attachments: 0 } };
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
      return { data: { images: [], resources: [], buckets: [], dependencies: [] } };
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
    expect(client.paths).toEqual(expect.arrayContaining([
      "/api/v0/observability/coverage/correlations?provider=grafana&limit=200",
      "/api/v0/observability/coverage/correlations?provider=prometheus&limit=200",
      "/api/v0/observability/coverage/correlations?provider=loki&limit=200",
      "/api/v0/observability/coverage/correlations?provider=tempo&limit=200"
    ]));
    expect(model.langInventory).toEqual([{ label: "typescript", value: 3 }]);
    expect(model.obsCoverage?.["api-node-platform"]?.logs).toMatchObject({
      state: "covered",
      ref: "grafana:logs",
      freshness: "fresh"
    });
    expect(model.prov.langInventory).toBe("live");
    expect(model.prov.obsCoverage).toBe("live");
    expect(client.paths).toContain("/api/v0/supply-chain/advisories?limit=50");
    expect(model.advisoryCatalog?.[0]).toMatchObject({
      id: "CVE-2026-0001",
      severity: "critical",
      cvss: 9.8,
      kev: true
    });
    expect(model.prov.advisoryCatalog).toBe("live");
  });
});
