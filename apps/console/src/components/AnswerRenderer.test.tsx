import { render, screen, within } from "@testing-library/react";
import type { GraphModel } from "../console/types";
import type { EshuTruth } from "../api/envelope";
import {
  normalizeAnswerCompanion,
  normalizeCitationPacket,
  type EvidenceCitationPacket
} from "../api/answerPacket";
import { normalizeVisualizationPacket, type VisualizationPacket } from "../api/answerVisualization";
import { AnswerRenderer } from "./AnswerRenderer";

describe("AnswerRenderer", () => {
  it("renders summary, resolved subgraph, citations, and truth labels together", () => {
    render(
      <AnswerRenderer
        answer={supportedAnswerFixture()}
        graph={answerGraph}
        title="Answer evidence"
      />
    );

    expect(screen.getByRole("heading", { name: "Answer evidence" })).toBeInTheDocument();
    expect(
      screen.getByText("catalog-api reaches the lead queue through postLead.")
    ).toBeInTheDocument();
    expect(screen.getAllByTitle("Truth: derived").length).toBeGreaterThan(0);
    expect(screen.getAllByTitle("Freshness: fresh").length).toBeGreaterThan(0);
    expect(screen.getByText("derived answer")).toBeInTheDocument();

    const sourceLink = screen.getAllByRole("link", { name: "server/routes/leads.ts:42" })[0];
    expect(sourceLink).toHaveAttribute(
      "href",
      "/repositories/catalog-api/source?path=server%2Froutes%2Fleads.ts&lineStart=42"
    );
    expect(screen.getAllByText("entity:postLead").length).toBeGreaterThan(0);

    const graph = screen.getByText("3 nodes · 2 edges").closest(".gcanvas");
    expect(graph).not.toBeNull();
    expect(within(graph as HTMLElement).getAllByText("catalog-api").length).toBeGreaterThan(0);
    expect(within(graph as HTMLElement).getByText("lead-queue")).toBeInTheDocument();
  });

  it("renders unsupported and missing-evidence states without bare confident prose", () => {
    const answer = normalizeAnswerCompanion({
      answerMetadata: {
        missing_evidence: ["no graph target resolved"]
      },
      answerPacket: {
        missing_evidence: ["no graph target resolved"],
        summary: "catalog-api has no downstream impact",
        supported: false,
        truth_class: "unsupported",
        unsupported_reasons: ["scope_not_found"]
      },
      routeTruth: null
    });

    render(<AnswerRenderer answer={answer} title="Answer evidence" />);

    expect(screen.getByText("Insufficient evidence")).toBeInTheDocument();
    expect(screen.queryByText("catalog-api has no downstream impact")).not.toBeInTheDocument();
    expect(screen.getAllByText("no graph target resolved").length).toBeGreaterThan(0);
    expect(screen.getByText("scope_not_found")).toBeInTheDocument();
    expect(screen.getByText("No graph rows returned from this source yet.")).toBeInTheDocument();
  });

  it("renders a grouped evidence packet reader with bounds, source hops, reducer decisions, and semantic labels", () => {
    render(
      <AnswerRenderer
        answer={packetReaderAnswerFixture()}
        citationPacket={packetReaderCitationFixture()}
        title="Answer evidence"
        visualizationPacket={packetReaderVisualizationFixture()}
      />
    );

    const reader = screen.getByRole("region", { name: "Evidence packet reader" });
    expect(within(reader).getByRole("heading", { name: "Evidence packet reader" })).toBeInTheDocument();

    expect(within(reader).getByText("5 of 12 citations resolved")).toBeInTheDocument();
    expect(within(reader).getByText("truncated packet")).toBeInTheDocument();
    expect(within(reader).getByText("2 nodes dropped")).toBeInTheDocument();
    expect(within(reader).getByText("1 edge dropped")).toBeInTheDocument();

    const sourceFacts = within(reader).getByRole("region", { name: "Source facts" });
    expect(within(sourceFacts).getByRole("link", { name: "server/routes/leads.ts:42" })).toHaveAttribute(
      "href",
      "/repositories/catalog-api/source?path=server%2Froutes%2Fleads.ts&lineStart=42"
    );
    expect(within(sourceFacts).getByText("docs/runbook.md:7")).toBeInTheDocument();

    const reducerDecisions = within(reader).getByRole("region", { name: "Reducer decisions" });
    expect(within(reducerDecisions).getByText("reducer admission")).toBeInTheDocument();
    expect(within(reducerDecisions).getByText("workload-linked")).toBeInTheDocument();

    const queryTruth = within(reader).getByRole("region", { name: "Query truth and freshness" });
    expect(within(queryTruth).getByText("platform_impact.change_surface")).toBeInTheDocument();
    expect(within(queryTruth).getByTitle("Truth: derived")).toBeInTheDocument();
    expect(within(queryTruth).getByTitle("Freshness: fresh")).toBeInTheDocument();

    const semanticLabels = within(reader).getByRole("region", { name: "Semantic labels" });
    expect(within(semanticLabels).getByText("semantic route label")).toBeInTheDocument();
    expect(within(semanticLabels).getByText("policy-gated semantic observation")).toBeInTheDocument();

    const missingEvidence = within(reader).getByRole("region", { name: "Missing evidence" });
    expect(within(missingEvidence).getByText("runtime traces")).toBeInTheDocument();
    expect(within(missingEvidence).getByText("image provenance")).toBeInTheDocument();
  });

  it("marks missing-only citation packets as bounded instead of complete", () => {
    render(
      <AnswerRenderer
        answer={supportedAnswerFixture()}
        citationPacket={missingOnlyCitationFixture()}
        title="Answer evidence"
      />
    );

    const reader = screen.getByRole("region", { name: "Evidence packet reader" });
    expect(within(reader).getByText("bounded")).toBeInTheDocument();
    expect(within(reader).queryByText("complete")).not.toBeInTheDocument();
    expect(within(reader).getByText("1 missing handle")).toBeInTheDocument();
    expect(within(reader).getByText("unresolved deployment evidence")).toBeInTheDocument();
  });
});

const answerGraph: GraphModel = {
  edges: [
    { layer: "runtime", s: "service:catalog-api", t: "workload:catalog-api", verb: "DEPLOYS_TO" },
    { layer: "ops", s: "workload:catalog-api", t: "queue:lead", verb: "PUBLISHES_TO" }
  ],
  nodes: [
    {
      col: 0,
      hero: true,
      id: "service:catalog-api",
      kind: "service",
      label: "catalog-api",
      sub: "service"
    },
    {
      col: 1,
      id: "workload:catalog-api",
      kind: "workload",
      label: "catalog-api",
      sub: "runtime"
    },
    {
      col: 2,
      id: "queue:lead",
      kind: "datastore",
      label: "lead-queue",
      sub: "ops"
    }
  ]
};

function supportedAnswerFixture() {
  return normalizeAnswerCompanion({
    answerMetadata: {
      evidence_handles: [
        {
          entity_id: "entity:postLead",
          evidence_family: "source",
          kind: "entity",
          repo_id: "catalog-api",
          reason: "route handler entity"
        }
      ]
    },
    answerPacket: {
      evidence_handles: [
        {
          evidence_family: "source",
          kind: "file",
          reason: "route handler evidence",
          relative_path: "server/routes/leads.ts",
          repo_id: "catalog-api",
          start_line: 42
        }
      ],
      primary_tool: "find_change_surface",
      recommended_next_calls: [
        {
          reason: "cite the source rows",
          tool: "build_evidence_citation_packet"
        }
      ],
      summary: "catalog-api reaches the lead queue through postLead.",
      supported: true,
      truth: {
        basis: "hybrid",
        capability: "platform_impact.change_surface",
        freshness: { state: "fresh" },
        level: "derived",
        profile: "local_authoritative"
      },
      truth_class: "derived"
    },
    routeTruth: {
      basis: "hybrid",
      capability: "platform_impact.change_surface",
      freshness: { state: "fresh" },
      level: "derived",
      profile: "local_authoritative"
    }
  });
}

function packetReaderAnswerFixture() {
  return normalizeAnswerCompanion({
    answerMetadata: {
      evidence_handles: [
        {
          entity_id: "reducer_admission:workload-linked",
          evidence_family: "reducer_decision",
          kind: "decision",
          reason: "reducer admission"
        },
        {
          entity_id: "semantic:route-handler",
          evidence_family: "semantic_label",
          kind: "semantic",
          reason: "semantic route label"
        }
      ],
      limitations: ["policy-gated semantic observation"],
      missing_evidence: ["runtime traces"],
      truncated: true
    },
    answerPacket: {
      evidence_handles: [
        {
          evidence_family: "source_fact",
          kind: "file",
          reason: "route handler source fact",
          relative_path: "server/routes/leads.ts",
          repo_id: "catalog-api",
          start_line: 42
        }
      ],
      partial: true,
      primary_route: "POST /api/v0/impact/change-surface/investigate",
      summary: "catalog-api reaches the lead queue through postLead.",
      supported: true,
      truth: routeTruth,
      truth_class: "derived",
      truncated: true
    },
    routeTruth
  });
}

function packetReaderCitationFixture(): EvidenceCitationPacket {
  return normalizeCitationPacket(
    {
      citations: [
        {
          citation_id: "citation-source-1",
          evidence_family: "source_fact",
          kind: "file",
          reason: "route handler source fact",
          relative_path: "server/routes/leads.ts",
          repo_id: "catalog-api",
          start_line: 42
        },
        {
          citation_id: "citation-source-2",
          evidence_family: "source_fact",
          kind: "file",
          reason: "operator runbook source fact",
          relative_path: "docs/runbook.md",
          repo_id: "catalog-api",
          start_line: 7
        }
      ],
      coverage: {
        input_handle_count: 12,
        limit: 5,
        missing_count: 1,
        query_shape: "evidence_citation_packet",
        resolved_count: 5,
        source_backend: "content_store",
        truncated: true
      },
      missing_handles: [
        {
          entity_id: "image provenance",
          evidence_family: "source_fact",
          kind: "entity",
          reason: "image provenance"
        }
      ]
    },
    routeTruth
  );
}

function missingOnlyCitationFixture(): EvidenceCitationPacket {
  return normalizeCitationPacket(
    {
      citations: [
        {
          citation_id: "citation-source-1",
          evidence_family: "source_fact",
          kind: "file",
          reason: "route handler source fact",
          relative_path: "server/routes/leads.ts",
          repo_id: "catalog-api",
          start_line: 42
        }
      ],
      coverage: {
        input_handle_count: 2,
        limit: 5,
        missing_count: 1,
        query_shape: "evidence_citation_packet",
        resolved_count: 1,
        source_backend: "content_store",
        truncated: false
      },
      missing_handles: [
        {
          entity_id: "unresolved deployment evidence",
          evidence_family: "source_fact",
          kind: "entity",
          reason: "unresolved deployment evidence"
        }
      ]
    },
    routeTruth
  );
}

function packetReaderVisualizationFixture(): VisualizationPacket {
  return normalizeVisualizationPacket(
    {
      visualization_packet: {
        supported: true,
        title: "catalog-api proof",
        truth: routeTruth,
        truncation: {
          dropped_edge_count: 1,
          dropped_node_count: 2,
          truncated: true
        },
        view: "evidence_citation"
      }
    },
    routeTruth
  ) as VisualizationPacket;
}

const routeTruth: EshuTruth = {
  basis: "hybrid",
  capability: "platform_impact.change_surface",
  freshness: { state: "fresh" },
  level: "derived",
  profile: "local_authoritative"
};
