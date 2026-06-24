import { render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";

import { CodeGraphPage } from "./CodeGraphPage";
import type { EshuApiClient } from "../api/client";
import { demoModel } from "../console/demoModel";
import type { ConsoleModel } from "../console/types";

describe("CodeGraphPage relationship truth labels", () => {
  it("renders relationship-story confidence tiers, truth states, and coverage", async () => {
    const model: ConsoleModel = {
      ...demoModel,
      findings: [{
        id: "dead-1",
        type: "Dead code",
        entity: "api-platform",
        title: "Unreferenced symbol post",
        detail: "server/handlers/install.ts · unused",
        truth: "derived",
        entityId: "content-entity:e1",
        filePath: "server/handlers/install.ts",
        repoId: "repository:r_platform"
      }]
    };
    const calls: { readonly path: string; readonly body: unknown }[] = [];
    const client = {
      post: async (path: string, body: unknown) => {
        calls.push({ path, body });
        return {
          data: {
            relationships: [
              {
                direction: "incoming",
                type: "CALLS",
                source_id: "content-entity:caller",
                source_name: "caller",
                target_id: "content-entity:e1",
                provenance: {
                  confidence_tier: "high",
                  truth_state: "derived",
                  source_family: "code_edge",
                  method: "scip",
                  confidence: 0.99
                }
              },
              {
                direction: "outgoing",
                type: "REFERENCES",
                source_id: "content-entity:e1",
                target_id: "content-entity:config",
                target_name: "configValue",
                provenance: {
                  confidence_tier: "low",
                  truth_state: "heuristic",
                  source_family: "correlation_edge",
                  method: "evidence_constant",
                  confidence: 0.55
                }
              },
              {
                direction: "outgoing",
                type: "IMPORTS",
                source_id: "content-entity:e1",
                target_id: "content-entity:legacy",
                target_name: "legacyImport",
                provenance: {
                  confidence_tier: "unsupported",
                  truth_state: "unsupported",
                  source_family: "unsupported",
                  method: "unsupported"
                }
              }
            ],
            coverage: {
              missing_edge_reason: "truncated_by_limit",
              truncation_state: "count",
              evidence_explanation: "Returned the first page of relationship rows."
            }
          },
          error: null,
          truth: null
        };
      }
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/code-graph"]}>
        <CodeGraphPage model={model} client={client} />
      </MemoryRouter>
    );

    await waitFor(() => expect(calls).toContainEqual({
      path: "/api/v0/code/relationships/story",
      body: {
        entity_id: "content-entity:e1",
        direction: "both",
        relationship_types: ["CALLS", "IMPORTS", "REFERENCES", "INHERITS", "OVERRIDES", "TAINT_FLOWS_TO"],
        limit: 50
      }
    }));
    expect(screen.getByText("Relationship truth")).toBeInTheDocument();
    expect(screen.getByText("high · derived")).toBeInTheDocument();
    expect(screen.getByText("low · heuristic")).toBeInTheDocument();
    expect(screen.getByText("unsupported · unsupported")).toBeInTheDocument();
    expect(screen.getByText("truncated_by_limit")).toBeInTheDocument();
    expect(screen.getByText("count")).toBeInTheDocument();
    expect(screen.getByText("Returned the first page of relationship rows.")).toBeInTheDocument();
  });
});
