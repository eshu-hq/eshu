import { fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import type { EshuApiClient } from "../api/client";
import { demoModel } from "../console/demoModel";
import type { ConsoleModel } from "../console/types";
import { CodeGraphPage } from "./CodeGraphPage";

describe("CodeGraphPage source-link hydration", () => {
  it("keeps story truth labels while hydrating related-symbol source links", async () => {
    const model: ConsoleModel = {
      ...demoModel,
      findings: [{
        id: "dead-1",
        type: "Dead code",
        entity: "svc-platform",
        title: "Unreferenced symbol post",
        detail: "server/handlers/install.ts · unused",
        truth: "derived",
        entityId: "content-entity:e1",
        filePath: "server/handlers/install.ts",
        startLine: 17,
        endLine: 54,
        language: "typescript",
        labels: ["Function"],
        classification: "unused",
        repoId: "repository:r_platform"
      }]
    };
    const calls: { readonly path: string; readonly body: unknown }[] = [];
    const client = {
      post: async (path: string, body: unknown) => {
        calls.push({ path, body });
        if (path === "/api/v0/code/relationships/story") {
          return {
            data: {
              entity_id: "content-entity:e1",
              name: "post",
              labels: ["Function"],
              relationships: [{
                direction: "incoming",
                type: "CALLS",
                source_id: "content-entity:e2",
                source_name: "caller",
                provenance: {
                  confidence_tier: "high",
                  truth_state: "derived",
                  source_family: "code_edge",
                  method: "scip"
                }
              }]
            },
            error: null,
            truth: null
          };
        }
        return {
          data: {
            entity_id: "content-entity:e1",
            name: "post",
            labels: ["Function"],
            incoming: [{
              type: "CALLS",
              source_id: "content-entity:e2",
              source_name: "caller",
              source_repo_id: "repository:r_platform",
              source_repo_name: "svc-platform",
              source_file_path: "server/handlers/caller.ts",
              source_start_line: 12,
              source_end_line: 18,
              source_type: "Function"
            }],
            outgoing: []
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

    const callerNode = await screen.findByText("caller");
    expect(calls).toContainEqual({
      path: "/api/v0/code/relationships",
      body: { entity_id: "content-entity:e1", max_depth: 1 }
    });
    fireEvent.click(callerNode);

    expect(screen.getByText("high · derived")).toBeInTheDocument();
    const href = "/repositories/repository%3Ar_platform/source?path=server%2Fhandlers%2Fcaller.ts&lineStart=12&lineEnd=18";
    expect(screen.getByRole("link", { name: "server/handlers/caller.ts:12-18" })).toHaveAttribute("href", href);
    expect(screen.getByRole("link", { name: "Open source" })).toHaveAttribute("href", href);
  });
});
