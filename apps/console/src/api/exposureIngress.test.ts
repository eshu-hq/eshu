import { describe, expect, it } from "vitest";
import type { EshuApiClient } from "./client";
import { loadExposureIngress } from "./exposureIngress";

describe("loadExposureIngress", () => {
  it("builds an internet origin chain for an observed-public entrypoint", async () => {
    const client = {
      get: async (path: string) => {
        expect(path).toBe("/api/v0/services/checkout/context");
        return {
          data: {
            name: "checkout",
            entrypoints: [
              { type: "hostname", target: "checkout.example.test", visibility: "public" }
            ],
            network_paths: [
              {
                path_type: "hostname_to_runtime",
                from_type: "hostname",
                from: "checkout.example.test",
                to_type: "runtime_platform",
                to: "checkout-eks",
                platform_kind: "eks",
                environment: "production",
                visibility: "public",
                reason: "ingress host maps to the eks runtime"
              }
            ],
            ingress_posture: {
              waf_coverage: "protected",
              tls_termination: "terminated",
              edge_count: 1,
              waf_protected: 1,
              tls_terminated: 1,
              reason: "observed across 1 internet-facing edge resource"
            }
          },
          error: null,
          truth: {
            capability: "platform_impact.context_overview",
            level: "derived",
            profile: "production",
            freshness: { state: "fresh" }
          }
        };
      }
    } as unknown as EshuApiClient;

    const ingress = await loadExposureIngress(client, "checkout");
    expect(ingress.provenance).toBe("live");
    expect(ingress.publicEntrypoints).toBe(1);
    expect(ingress.chains).toHaveLength(1);
    const chain = ingress.chains[0];
    // Internet origin -> entrypoint -> runtime.
    expect(chain.hops[0].kind).toBe("internet");
    expect(chain.hops[0].label).toBe("Internet");
    expect(chain.hops[1].detail).toBe("checkout.example.test");
    expect(chain.hops[2].label).toBe("EKS");
    expect(ingress.posture.wafCoverage).toBe("protected");
    expect(ingress.posture.tlsTermination).toBe("terminated");
  });

  it("draws a network-boundary origin for an internal entrypoint, never internet", async () => {
    const client = {
      get: async () => ({
        data: {
          name: "internal-api",
          entrypoints: [
            { type: "docs_route", target: "/internal/health", visibility: "internal" }
          ],
          network_paths: [
            {
              path_type: "docs_route_to_runtime",
              from_type: "docs_route",
              from: "/internal/health",
              to_type: "runtime_platform",
              to: "internal-eks",
              platform_kind: "eks",
              visibility: "internal"
            }
          ]
        },
        error: null,
        truth: null
      })
    } as unknown as EshuApiClient;

    const ingress = await loadExposureIngress(client, "internal-api");
    const chain = ingress.chains[0];
    expect(chain.hops[0].kind).toBe("network");
    expect(chain.hops[0].label).toBe("Network boundary");
    expect(chain.hops.some((hop) => hop.kind === "internet")).toBe(false);
    // No edge resource posture -> unproven, never optimistic.
    expect(ingress.posture.wafCoverage).toBe("unproven");
    expect(ingress.posture.tlsTermination).toBe("unproven");
  });

  it("returns an empty provenance when no network path is proven", async () => {
    const client = {
      get: async () => ({
        data: { name: "ghost", entrypoints: [], network_paths: [] },
        error: null,
        truth: null
      })
    } as unknown as EshuApiClient;

    const ingress = await loadExposureIngress(client, "ghost");
    expect(ingress.provenance).toBe("empty");
    expect(ingress.chains).toHaveLength(0);
  });

  it("defaults posture to unproven for unknown wire values", async () => {
    const client = {
      get: async () => ({
        data: {
          name: "svc",
          entrypoints: [{ type: "hostname", target: "svc.test", visibility: "public" }],
          network_paths: [
            { from_type: "hostname", from: "svc.test", to_type: "runtime_platform", to: "svc-eks", visibility: "public" }
          ],
          ingress_posture: { waf_coverage: "bogus", tls_termination: "" }
        },
        error: null,
        truth: null
      })
    } as unknown as EshuApiClient;

    const ingress = await loadExposureIngress(client, "svc");
    expect(ingress.posture.wafCoverage).toBe("unproven");
    expect(ingress.posture.tlsTermination).toBe("unproven");
  });

  it("returns unavailable with the error message on request failure", async () => {
    const client = {
      get: async () => {
        throw new Error("HTTP 503");
      }
    } as unknown as EshuApiClient;

    const ingress = await loadExposureIngress(client, "checkout");
    expect(ingress.provenance).toBe("unavailable");
    expect(ingress.error).toContain("503");
    expect(ingress.chains).toHaveLength(0);
  });

  it("requires a service name", async () => {
    const client = { get: async () => ({ data: null, error: null, truth: null }) } as unknown as EshuApiClient;
    const ingress = await loadExposureIngress(client, "   ");
    expect(ingress.provenance).toBe("unavailable");
    expect(ingress.error).toContain("service name");
  });
});
