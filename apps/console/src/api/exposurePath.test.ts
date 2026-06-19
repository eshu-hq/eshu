import { describe, expect, it } from "vitest";
import type { EshuApiClient } from "./client";
import { loadExposureFinding } from "./exposurePath";

// loadExposureFinding maps the POST /api/v0/impact/trace-exposure-path wire shape
// into the console view-model. It must keep types in lockstep with the Go
// contract, and it must never fabricate a path: an unresolved finding keeps its
// empty path set and honest coverage reason.
describe("loadExposureFinding", () => {
  it("maps a reachable-sink finding with path, sink, severity, and derived label", async () => {
    const client = {
      post: async (path: string, body: unknown) => {
        expect(path).toBe("/api/v0/impact/trace-exposure-path");
        expect(body).toMatchObject({ source: "createWidgetHandler", repo_id: "repository:r_example", max_depth: 5 });
        return {
          data: reachableWire(),
          error: null,
          truth: {
            capability: "platform_impact.exposure_path",
            level: "derived",
            profile: "production",
            freshness: { state: "fresh" }
          }
        };
      }
    } as unknown as EshuApiClient;

    const finding = await loadExposureFinding(client, {
      source: "createWidgetHandler",
      repoId: "repository:r_example",
      maxDepth: 5
    });

    expect(finding.provenance).toBe("live");
    expect(finding.state).toBe("exact");
    expect(finding.exposureRank).toBe("internet_exposed");
    expect(finding.truthLabel).toBe("derived");
    expect(finding.paths).toHaveLength(1);
    const [first] = finding.paths;
    expect(first.severity).toBe("critical");
    expect(first.sink.kind).toBe("iam_privileged_action");
    expect(first.sink.displayName).toBe("IAM privileged action");
    expect(first.nodes.map((n) => n.name)).toEqual([
      "createWidgetHandler",
      "persistWidget",
      "assumeAdminRole"
    ]);
    expect(first.reason).toContain("internet-exposed handler");
    expect(finding.coverage.pathsFound).toBe(1);
    expect(finding.truth?.level).toBe("derived");
  });

  it("preserves an unresolved finding's coverage reason and never fabricates a path", async () => {
    const client = {
      post: async () => ({
        data: unresolvedWire(),
        error: null,
        truth: {
          capability: "platform_impact.exposure_path",
          level: "derived",
          profile: "production",
          freshness: { state: "fresh" }
        }
      })
    } as unknown as EshuApiClient;

    const finding = await loadExposureFinding(client, { source: "drainQueueHandler" });

    expect(finding.provenance).toBe("live");
    expect(finding.state).toBe("unresolved");
    expect(finding.paths).toHaveLength(0);
    expect(finding.coverage.unresolvedReason).toBe(
      "code-to-cloud bridge edge is not materialized; cloud-sink segment is unresolved"
    );
  });

  it("downgrades an empty-path finding to unresolved even if the wire state claims exact", async () => {
    const client = {
      post: async () => ({
        data: { ...unresolvedWire(), state: "exact", paths: [] },
        error: null,
        truth: null
      })
    } as unknown as EshuApiClient;

    const finding = await loadExposureFinding(client, { sourceEntityId: "entity:e_handler" });
    expect(finding.state).toBe("unresolved");
    expect(finding.paths).toHaveLength(0);
  });

  it("returns an unavailable finding (no path) when the request fails", async () => {
    const client = {
      post: async () => {
        throw new Error("HTTP 503");
      }
    } as unknown as EshuApiClient;

    const finding = await loadExposureFinding(client, { source: "createWidgetHandler" });
    expect(finding.provenance).toBe("unavailable");
    expect(finding.error).toContain("503");
    expect(finding.paths).toHaveLength(0);
  });

  it("sends source_entity_id and clamps max_depth to the backend bound", async () => {
    let sent: Record<string, unknown> = {};
    const client = {
      post: async (_path: string, body: unknown) => {
        sent = body as Record<string, unknown>;
        return { data: unresolvedWire(), error: null, truth: null };
      }
    } as unknown as EshuApiClient;

    await loadExposureFinding(client, { sourceEntityId: "entity:e_handler", maxDepth: 99 });
    expect(sent.source_entity_id).toBe("entity:e_handler");
    expect(sent.max_depth).toBe(10);
    expect(sent).not.toHaveProperty("source");
  });
});

function reachableWire(): Record<string, unknown> {
  return {
    source: {
      entity_id: "entity:e_handler",
      name: "createWidgetHandler",
      labels: ["Function", "HttpHandler"]
    },
    source_kind: "http_handler",
    exposure_rank: "internet_exposed",
    truth_label: "derived",
    state: "exact",
    paths: [
      {
        nodes: [
          { entity_id: "entity:e_handler", name: "createWidgetHandler", labels: ["Function"] },
          { entity_id: "entity:e_persist", name: "persistWidget", labels: ["Function"] },
          { entity_id: "entity:e_iam", name: "assumeAdminRole", labels: ["Function"] }
        ],
        sink: {
          kind: "iam_privileged_action",
          display_name: "IAM privileged action",
          node: { entity_id: "cloud:c_iam", name: "admin-policy", labels: ["IamPolicy"] }
        },
        depth: 3,
        state: "exact",
        severity: "critical",
        reason:
          "internet-exposed handler transitively reaches a privileged IAM action; no authentication gate is modeled in Level 1"
      }
    ],
    coverage: {
      max_depth: 5,
      paths_found: 1,
      truncated: false,
      unresolved_reason: ""
    }
  };
}

function unresolvedWire(): Record<string, unknown> {
  return {
    source: {
      entity_id: "entity:e_drain",
      name: "drainQueueHandler",
      labels: ["Function", "MessageConsumer"]
    },
    source_kind: "message_consumer",
    exposure_rank: "network_reachable",
    truth_label: "derived",
    state: "unresolved",
    paths: [],
    coverage: {
      max_depth: 5,
      paths_found: 0,
      truncated: false,
      unresolved_reason:
        "code-to-cloud bridge edge is not materialized; cloud-sink segment is unresolved"
    }
  };
}
