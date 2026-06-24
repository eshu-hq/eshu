import { render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";

import { CodeGraphPage } from "./CodeGraphPage";
import type { EshuApiClient } from "../api/client";
import { demoModel } from "../console/demoModel";
import type { ConsoleModel } from "../console/types";

function cycleModel(): ConsoleModel {
  return {
    ...demoModel,
    findings: [
      {
        id: "dead-1",
        type: "Dead code",
        entity: "platform-api",
        title: "Unreferenced symbol handler",
        detail: "src/handler.py · unused",
        truth: "derived",
        entityId: "content-entity:e1",
        filePath: "src/handler.py",
        startLine: 10,
        language: "python",
        classification: "unused",
        repoId: "repository:r_platform"
      }
    ]
  };
}

describe("CodeGraphPage import cycles", () => {
  it("loads and renders source-backed import cycle rows for the selected repository", async () => {
    const calls: { readonly path: string; readonly body: unknown }[] = [];
    const client = {
      post: async (path: string, body: unknown) => {
        calls.push({ path, body });
        if (path === "/api/v0/code/imports/investigate") {
          return {
            data: {
              cycles: [
                {
                  repo_id: "repository:r_platform",
                  repo_name: "platform-api",
                  source_file: "src/module_a.py",
                  target_file: "src/module_b.py",
                  source_module: "module_a",
                  target_module: "module_b",
                  source_line_number: 4,
                  back_edge_line_number: 7,
                  relationship_type: "IMPORTS",
                  cycle_path: ["src/module_a.py", "src/module_b.py", "src/module_a.py"],
                  cycle_edges: [
                    { relationship_type: "IMPORTS", source_file: "src/module_a.py", target_file: "src/module_b.py", line_number: 4 },
                    { relationship_type: "IMPORTS", source_file: "src/module_b.py", target_file: "src/module_a.py", line_number: 7 }
                  ]
                }
              ],
              count: 1,
              truncated: true,
              next_offset: 6
            },
            error: null,
            truth: { level: "exact", freshness: { state: "fresh" }, profile: "production" }
          };
        }
        return {
          data: {
            entity_id: "content-entity:e1",
            name: "handler",
            labels: ["Function"],
            incoming: [],
            outgoing: []
          },
          error: null,
          truth: null
        };
      }
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/code-graph"]}>
        <CodeGraphPage model={cycleModel()} client={client} />
      </MemoryRouter>
    );

    await waitFor(() => expect(calls).toContainEqual({
      path: "/api/v0/code/imports/investigate",
      body: { query_type: "file_import_cycles", repo_id: "repository:r_platform", limit: 6 }
    }));
    expect(await screen.findByText("Import cycles · 1")).toBeInTheDocument();
    expect(screen.getByText("src/module_a.py → src/module_b.py → src/module_a.py")).toBeInTheDocument();
    expect(screen.getByText("IMPORTS · platform-api")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "src/module_a.py:4" })).toHaveAttribute(
      "href",
      "/repositories/repository%3Ar_platform/source?path=src%2Fmodule_a.py&lineStart=4"
    );
    expect(screen.getByText("More import cycles are available at offset 6.")).toBeInTheDocument();
    expect(screen.queryByText(/not reported/)).not.toBeInTheDocument();
  });

  it("distinguishes an empty source-backed cycle response", async () => {
    const client = {
      post: async (path: string) => ({
        data: path === "/api/v0/code/imports/investigate"
          ? { cycles: [], count: 0, truncated: false }
          : { entity_id: "content-entity:e1", name: "handler", labels: ["Function"], incoming: [], outgoing: [] },
        error: null,
        truth: null
      })
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/code-graph"]}>
        <CodeGraphPage model={cycleModel()} client={client} />
      </MemoryRouter>
    );

    expect(await screen.findByText("No source-backed import cycles returned for this repository.")).toBeInTheDocument();
    expect(screen.queryByText(/not reported/)).not.toBeInTheDocument();
  });

  it("surfaces unavailable cycle analysis separately from empty results", async () => {
    const client = {
      post: async (path: string) => {
        if (path === "/api/v0/code/imports/investigate") throw new Error("Eshu API request failed with HTTP 503");
        return {
          data: { entity_id: "content-entity:e1", name: "handler", labels: ["Function"], incoming: [], outgoing: [] },
          error: null,
          truth: null
        };
      }
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/code-graph"]}>
        <CodeGraphPage model={cycleModel()} client={client} />
      </MemoryRouter>
    );

    expect(await screen.findByText("Import cycle analysis unavailable: Eshu API request failed with HTTP 503")).toBeInTheDocument();
  });
});
