import {
  buildSourceCitationHref,
  emptyAnswerGraph,
  normalizeAnswerCompanion
} from "./answerPacket";
import type { EshuTruth } from "./envelope";
import type { GraphModel } from "../console/types";

describe("answer packet adapter", () => {
  it("normalizes answer packets, metadata, truth, and source citations", () => {
    const answer = normalizeAnswerCompanion({
      answerMetadata: {
        evidence_handles: [
          {
            evidence_family: "source",
            kind: "entity",
            entity_id: "entity:postLead",
            repo_id: "catalog-api",
            reason: "matched route handler"
          }
        ],
        limitations: ["bounded to 16 impact rows"],
        missing_evidence: ["runtime traces"],
        partial_reasons: ["transitive impact truncated"],
        truncated: true
      },
      answerPacket: {
        evidence_handles: [
          {
            end_line: 45,
            evidence_family: "source",
            kind: "file",
            reason: "route handler evidence",
            relative_path: "server/routes/leads.ts",
            repo_id: "catalog-api",
            start_line: 42
          }
        ],
        partial: true,
        primary_route: "POST /api/v0/impact/change-surface/investigate",
        primary_tool: "find_change_surface",
        question: "What changes when catalog-api changes?",
        recommended_next_calls: [
          {
            reason: "hydrate line citations",
            tool: "build_evidence_citation_packet"
          }
        ],
        summary: "catalog-api reaches two workloads and one repository.",
        supported: true,
        truth: routeTruth,
        truth_class: "derived",
        unsupported_reasons: ["transitive impact truncated"]
      },
      routeTruth
    });

    expect(answer.status).toBe("partial");
    expect(answer.summary).toBe("catalog-api reaches two workloads and one repository.");
    expect(answer.truth).toBe(routeTruth);
    expect(answer.truthClass).toBe("derived");
    expect(answer.evidenceHandles.map((handle) => handle.kind)).toEqual(["file", "entity"]);
    expect(answer.missingEvidence).toEqual(["runtime traces"]);
    expect(answer.limitations).toEqual(["bounded to 16 impact rows"]);
    expect(answer.partialReasons).toEqual(["transitive impact truncated"]);
    expect(answer.recommendedNextCalls[0]?.tool).toBe("build_evidence_citation_packet");
    expect(
      buildSourceCitationHref({
        relativePath: "server/routes/leads.ts",
        repoId: "catalog-api",
        startLine: 42
      })
    ).toBe("/repositories/catalog-api/source?path=server%2Froutes%2Fleads.ts&lineStart=42");
  });

  it("keeps unsupported answers explicit and refuses to surface confident prose", () => {
    const answer = normalizeAnswerCompanion({
      answerMetadata: {
        missing_evidence: ["no matching service graph target"]
      },
      answerPacket: {
        missing_evidence: ["no matching service graph target"],
        recommended_next_calls: [
          {
            args: { service_name: "catalog-api" },
            reason: "resolve service first",
            tool: "investigate_service"
          }
        ],
        summary: "catalog-api has no dependencies",
        supported: false,
        truth_class: "unsupported",
        unsupported_reasons: ["scope_not_found"]
      },
      routeTruth: null
    });

    expect(answer.status).toBe("unsupported");
    expect(answer.summary).toBe("");
    expect(answer.truthClass).toBe("unsupported");
    expect(answer.missingEvidence).toEqual(["no matching service graph target"]);
    expect(answer.unsupportedReasons).toEqual(["scope_not_found"]);
  });

  it("returns an explicit empty graph when no resolved answer subgraph exists", () => {
    const graph: GraphModel = emptyAnswerGraph();

    expect(graph.nodes).toEqual([]);
    expect(graph.edges).toEqual([]);
  });
});

const routeTruth: EshuTruth = {
  basis: "hybrid",
  capability: "platform_impact.change_surface",
  freshness: { state: "fresh" },
  level: "derived",
  profile: "local_authoritative",
  reason: "resolved from graph and content"
};
