import { describe, expect, it } from "vitest";
import type { EshuApiClient } from "./client";
import { loadStatusOverview, type StatusCollectorState } from "./statusOverview";

// Each bounded status read is mocked independently so the join logic, the
// Stalled / Catching Up / Up To Date classification, and the overall indexing
// percentage are exercised without a live backend.
interface MockResponses {
  readonly readiness?: unknown;
  readonly indexStatus?: unknown;
  readonly freshness?: unknown;
  readonly ingesters?: unknown;
  readonly throwReadiness?: boolean;
}

function mockClient(responses: MockResponses): EshuApiClient {
  const calls: string[] = [];
  const client = {
    calls,
    get: async (path: string) => {
      calls.push(path);
      if (path.includes("/status/collector-readiness")) {
        if (responses.throwReadiness) throw new Error("collector readiness offline");
        return { data: responses.readiness ?? { readiness: [] }, error: null, truth: { profile: "production", level: "exact", capability: "status.collector_readiness", freshness: { state: "fresh" } } };
      }
      if (path.includes("/status/freshness-causality")) {
        return { data: responses.freshness ?? null, error: null, truth: null };
      }
      throw new Error(`unexpected get ${path}`);
    },
    getJson: async (path: string) => {
      calls.push(path);
      if (path.includes("/index-status")) {
        if (responses.indexStatus === undefined) throw new Error("index status offline");
        return responses.indexStatus;
      }
      if (path.includes("/status/ingesters")) {
        if (responses.ingesters === undefined) throw new Error("ingester status offline");
        return responses.ingesters;
      }
      throw new Error(`unexpected getJson ${path}`);
    }
  };
  return client as unknown as EshuApiClient;
}

const readinessTwoCollectors = {
  readiness: [
    { collector_kind: "git", display_name: "Git", instance_id: "git-primary", claim_state: "claim_driven", promotion_state: "implemented" },
    { collector_kind: "aws", display_name: "AWS Cloud", instance_id: "aws-main", promotion_state: "implemented" }
  ]
};

describe("loadStatusOverview", () => {
  it("uses only the bounded status read paths", async () => {
    const client = mockClient({ indexStatus: { coordinator: { collector_instances: [] } }, freshness: { state: "fresh", generations: { active: 1, pending: 0, completed: 1, superseded: 0, failed: 0 }, pending_projection: { outstanding: 0, dead_letter: 0, domains: 0 } } });
    await loadStatusOverview(client);
    const calls = (client as unknown as { calls: string[] }).calls;
    expect(calls).toContain("/api/v0/status/collector-readiness");
    expect(calls).toContain("/api/v0/index-status");
    expect(calls).toContain("/api/v0/status/freshness-causality");
    // No unbounded catalog/aggregate reads.
    expect(calls.every((c) => !c.includes("/catalog") && !c.includes("limit=2000"))).toBe(true);
  });

  it("classifies a collector with backpressure as catching up with a progress bar", async () => {
    const now = "2026-06-21T12:00:00Z";
    const client = mockClient({
      readiness: readinessTwoCollectors,
      indexStatus: {
        status: "indexing",
        repository_count: 900,
        coordinator: {
          collector_instances: [
            { instance_id: "git-primary", collector_kind: "git", enabled: true, last_observed_at: "2026-06-21T11:59:20Z" },
            { instance_id: "aws-main", collector_kind: "aws", enabled: true, last_observed_at: "2026-06-21T11:59:00Z" }
          ],
          collector_backpressure: [
            { collector_kind: "git", collector_instance_id: "git-primary", pending: 34, claimed: 2, dead_letter: 0 }
          ]
        }
      },
      freshness: { state: "building", generations: { active: 2, pending: 3, completed: 5, superseded: 0, failed: 0 }, pending_projection: { outstanding: 12, dead_letter: 0, domains: 2 } }
    });
    const overview = await loadStatusOverview(client, () => Date.parse(now));
    const git = overview.collectors.find((c) => c.kind === "git");
    const aws = overview.collectors.find((c) => c.kind === "aws");
    expect(git?.state).toBe<StatusCollectorState>("catching_up");
    expect(git?.workItems).toBe(36); // pending + claimed
    expect(git?.progress).toBeGreaterThan(0);
    expect(git?.progress).toBeLessThan(1);
    expect(aws?.state).toBe<StatusCollectorState>("up_to_date");
    expect(aws?.workItems).toBe(0);
    expect(aws?.progress).toBe(1);
  });

  it("classifies dead-lettered or disabled collectors as stalled", async () => {
    const now = "2026-06-21T12:00:00Z";
    const client = mockClient({
      readiness: {
        readiness: [
          { collector_kind: "sbom_attestation", display_name: "SBOM", instance_id: "sbom-1", promotion_state: "failed" },
          { collector_kind: "synthetics", display_name: "Synthetics", instance_id: "syn-1", promotion_state: "stale" }
        ]
      },
      indexStatus: {
        coordinator: {
          collector_instances: [
            { instance_id: "sbom-1", collector_kind: "sbom_attestation", enabled: true, last_observed_at: "2026-06-21T11:59:00Z" },
            { instance_id: "syn-1", collector_kind: "synthetics", enabled: false, deactivated_at: "2026-06-20T00:00:00Z", last_observed_at: "2026-06-20T00:00:00Z" }
          ],
          collector_backpressure: [
            { collector_kind: "sbom_attestation", collector_instance_id: "sbom-1", pending: 8, dead_letter: 3 }
          ]
        }
      },
      freshness: { state: "stale", generations: { active: 1, pending: 0, completed: 0, superseded: 2, failed: 1 }, pending_projection: { outstanding: 0, dead_letter: 3, domains: 1 } }
    });
    const overview = await loadStatusOverview(client, () => Date.parse(now));
    const sbom = overview.collectors.find((c) => c.kind === "sbom_attestation");
    const syn = overview.collectors.find((c) => c.kind === "synthetics");
    expect(sbom?.state).toBe<StatusCollectorState>("stalled"); // dead letters
    expect(syn?.state).toBe<StatusCollectorState>("stalled"); // disabled/deactivated
  });

  it("computes overall indexing percent as 100 when caught up and lower under backlog", async () => {
    const caughtUp = mockClient({
      readiness: readinessTwoCollectors,
      indexStatus: { coordinator: { collector_instances: [], collector_backpressure: [] }, queue: { outstanding: 0, in_flight: 0, succeeded: 1000, dead_letter: 0 } },
      freshness: { state: "fresh", generations: { active: 4, pending: 0, completed: 9, superseded: 0, failed: 0 }, pending_projection: { outstanding: 0, dead_letter: 0, domains: 0 } }
    });
    const a = await loadStatusOverview(caughtUp);
    expect(a.indexingPercent).toBe(100);

    const backlog = mockClient({
      readiness: readinessTwoCollectors,
      indexStatus: { coordinator: { collector_instances: [], collector_backpressure: [] }, queue: { outstanding: 500, in_flight: 0, succeeded: 500, dead_letter: 0 } },
      freshness: { state: "building", generations: { active: 2, pending: 6, completed: 2, superseded: 0, failed: 0 }, pending_projection: { outstanding: 500, dead_letter: 0, domains: 3 } }
    });
    const b = await loadStatusOverview(backlog);
    expect(b.indexingPercent).toBeLessThan(100);
    expect(b.indexingPercent).toBeGreaterThanOrEqual(0);
  });

  it("renders a human last-run age and source volume per collector", async () => {
    const now = "2026-06-21T12:00:00Z";
    const client = mockClient({
      readiness: readinessTwoCollectors,
      indexStatus: {
        coordinator: {
          collector_instances: [
            { instance_id: "git-primary", collector_kind: "git", enabled: true, last_observed_at: "2026-06-21T11:59:20Z" }
          ]
        }
      },
      ingesters: { ingesters: [{ name: "git-primary", kind: "git", fact_count: 142900, health: "healthy" }] },
      freshness: { state: "fresh", generations: { active: 1, pending: 0, completed: 1, superseded: 0, failed: 0 }, pending_projection: { outstanding: 0, dead_letter: 0, domains: 0 } }
    });
    const overview = await loadStatusOverview(client, () => Date.parse(now));
    const git = overview.collectors.find((c) => c.kind === "git");
    expect(git?.lastRunLabel).toBe("40s ago");
    expect(git?.volume).toBe(142900);
  });

  it("fails closed to an unavailable overview when readiness is unreachable", async () => {
    const client = mockClient({ throwReadiness: true });
    const overview = await loadStatusOverview(client);
    expect(overview.provenance).toBe("unavailable");
    expect(overview.collectors).toEqual([]);
  });

  it("derives the collector schedule label from kind", async () => {
    const client = mockClient({
      readiness: { readiness: [{ collector_kind: "git", display_name: "Git", instance_id: "git-1" }] },
      indexStatus: { coordinator: { collector_instances: [{ instance_id: "git-1", collector_kind: "git", enabled: true, last_observed_at: "2026-06-21T11:59:20Z" }] } },
      freshness: { state: "fresh", generations: { active: 1, pending: 0, completed: 1, superseded: 0, failed: 0 }, pending_projection: { outstanding: 0, dead_letter: 0, domains: 0 } }
    });
    const overview = await loadStatusOverview(client, () => Date.parse("2026-06-21T12:00:00Z"));
    const git = overview.collectors.find((c) => c.kind === "git");
    expect(git?.schedule.length).toBeGreaterThan(0);
  });

  // P1 regression: coordinator.collector_backpressure was omitted from the
  // coordinatorToMap() response in go/internal/query/status.go, so the field
  // was always an empty array in the live API.  The Go fix adds it; the TS join
  // then reads pending + claimed + retrying correctly.
  it("(P1) reads per-collector work-item counts from coordinator.collector_backpressure", async () => {
    const client = mockClient({
      readiness: { readiness: [
        { collector_kind: "aws", display_name: "AWS", instance_id: "aws-1", promotion_state: "implemented" }
      ]},
      indexStatus: {
        coordinator: {
          collector_instances: [
            { instance_id: "aws-1", collector_kind: "aws", enabled: true, last_observed_at: "2026-06-21T11:59:00Z" }
          ],
          // Previously this field was absent from the live response; the fix ensures it is exposed.
          collector_backpressure: [
            { collector_kind: "aws", collector_instance_id: "aws-1", pending: 20, claimed: 5, retrying: 2, dead_letter: 0 }
          ]
        }
      },
      freshness: { state: "building", generations: { active: 1, pending: 2, completed: 0, superseded: 0, failed: 0 }, pending_projection: { outstanding: 0, dead_letter: 0, domains: 0 } }
    });
    const overview = await loadStatusOverview(client, () => Date.parse("2026-06-21T12:00:00Z"));
    const aws = overview.collectors.find((c) => c.kind === "aws");
    // workItems = pending(20) + claimed(5) + retrying(2)
    expect(aws?.workItems).toBe(27);
    expect(aws?.state).toBe<StatusCollectorState>("catching_up");
    // Without the backpressure field the workItems would be 0 and state would be up_to_date.
  });

  // P2 regression: promotion_state values that are not actively-running
  // ("unsupported", "gated", "disabled", etc.) were classified as "up_to_date"
  // because failedPromotion only tested for "failed" and "stale".
  it("(P2) classifies unsupported/gated/disabled promotion states as stalled", async () => {
    const client = mockClient({
      readiness: { readiness: [
        { collector_kind: "oci_registry", display_name: "OCI", instance_id: "oci-1", promotion_state: "unsupported" },
        { collector_kind: "grafana", display_name: "Grafana", instance_id: "grafana-1", promotion_state: "gated" },
        { collector_kind: "loki", display_name: "Loki", instance_id: "loki-1", promotion_state: "disabled" },
        { collector_kind: "git", display_name: "Git", instance_id: "git-1", promotion_state: "implemented" }
      ]},
      indexStatus: {
        coordinator: {
          collector_instances: [
            { instance_id: "oci-1", collector_kind: "oci_registry", enabled: true, last_observed_at: "2026-06-21T11:59:00Z" },
            { instance_id: "grafana-1", collector_kind: "grafana", enabled: true, last_observed_at: "2026-06-21T11:59:00Z" },
            { instance_id: "loki-1", collector_kind: "loki", enabled: true, last_observed_at: "2026-06-21T11:59:00Z" },
            { instance_id: "git-1", collector_kind: "git", enabled: true, last_observed_at: "2026-06-21T11:59:00Z" }
          ],
          collector_backpressure: []
        }
      },
      freshness: { state: "fresh", generations: { active: 1, pending: 0, completed: 1, superseded: 0, failed: 0 }, pending_projection: { outstanding: 0, dead_letter: 0, domains: 0 } }
    });
    const overview = await loadStatusOverview(client, () => Date.parse("2026-06-21T12:00:00Z"));
    const oci = overview.collectors.find((c) => c.kind === "oci_registry");
    const grafana = overview.collectors.find((c) => c.kind === "grafana");
    const loki = overview.collectors.find((c) => c.kind === "loki");
    const git = overview.collectors.find((c) => c.kind === "git");
    // P2: non-active promotion states must NOT be classified as up_to_date.
    expect(oci?.state).toBe<StatusCollectorState>("stalled");
    expect(grafana?.state).toBe<StatusCollectorState>("stalled");
    expect(loki?.state).toBe<StatusCollectorState>("stalled");
    // "implemented" stays up_to_date (no backpressure, not disabled).
    expect(git?.state).toBe<StatusCollectorState>("up_to_date");
  });
});
