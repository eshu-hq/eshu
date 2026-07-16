import { fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";

import { CodeGraphPage } from "./CodeGraphPage";
import type { EshuApiClient } from "../api/client";
import { demoModel } from "../console/demoModel";
import type { ConsoleModel } from "../console/types";

describe("CodeGraphPage source links", () => {
  it("uses relationship-story truth and source metadata without a redundant graph read", async () => {
    const model: ConsoleModel = {
      ...demoModel,
      findings: [
        {
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
          repoId: "repository:r_platform",
        },
      ],
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
              relationships: [
                {
                  direction: "incoming",
                  type: "CALLS",
                  source_id: "content-entity:e2",
                  source_name: "caller",
                  source_repo_id: "repository:r_platform",
                  source_repo_name: "svc-platform",
                  source_file_path: "server/handlers/caller.ts",
                  source_start_line: 12,
                  source_end_line: 18,
                  source_type: "Function",
                  provenance: {
                    confidence_tier: "high",
                    truth_state: "derived",
                    source_family: "code_edge",
                    method: "scip",
                  },
                },
              ],
            },
            error: null,
            truth: null,
          };
        }
        throw new Error(`unexpected POST ${path}`);
      },
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/code-graph"]}>
        <CodeGraphPage model={model} client={client} />
      </MemoryRouter>,
    );

    const callerNode = await screen.findByText("caller");
    expect(calls.filter((call) => call.path.includes("/code/relationships"))).toHaveLength(1);
    fireEvent.click(callerNode);

    expect(screen.getByText("high · derived")).toBeInTheDocument();
    const href =
      "/repositories/repository%3Ar_platform/source?path=server%2Fhandlers%2Fcaller.ts&lineStart=12&lineEnd=18";
    expect(screen.getByRole("link", { name: "server/handlers/caller.ts:12-18" })).toHaveAttribute(
      "href",
      href,
    );
    expect(screen.getByRole("link", { name: "Open source" })).toHaveAttribute("href", href);
  });
});
