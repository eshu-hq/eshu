import { render, screen, within } from "@testing-library/react";
import type { GraphModel } from "../console/types";
import { normalizeAnswerCompanion } from "../api/answerPacket";
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
    expect(screen.getByTitle("Truth: derived")).toBeInTheDocument();
    expect(screen.getByTitle("Freshness: fresh")).toBeInTheDocument();
    expect(screen.getByText("derived answer")).toBeInTheDocument();

    const sourceLink = screen.getByRole("link", { name: "server/routes/leads.ts:42" });
    expect(sourceLink).toHaveAttribute(
      "href",
      "/repositories/catalog-api/source?path=server%2Froutes%2Fleads.ts&lineStart=42"
    );
    expect(screen.getByText("entity:postLead")).toBeInTheDocument();

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
    expect(screen.getByText("no graph target resolved")).toBeInTheDocument();
    expect(screen.getByText("scope_not_found")).toBeInTheDocument();
    expect(screen.getByText("No graph rows returned from this source yet.")).toBeInTheDocument();
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
