import { render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";

import { CodeGraphPage } from "./CodeGraphPage";
import type { EshuApiClient } from "../api/client";
import type { RepoListItem } from "../api/repoCatalog";
import { demoModel } from "../console/demoModel";
import type { ConsoleModel } from "../console/types";

describe("CodeGraphPage inventory ownership", () => {
  it("loads symbols from structural inventory while retaining dead-code overlays", async () => {
    const model: ConsoleModel = {
      ...demoModel,
      findings: [
        {
          classification: "unused",
          detail: "src/unused.ts · unused",
          entity: "service-repo",
          entityId: "content-entity:e1",
          filePath: "src/unused.ts",
          id: "dead-1",
          repoId: "repository:r1",
          title: "Unreferenced symbol unusedRoute",
          truth: "derived",
          type: "Dead code",
        },
      ],
      source: "live",
    };
    const calls: string[] = [];
    const client = {
      get: async (path: string) => {
        calls.push(path);
        return { data: { repositories: [] }, error: null, truth: null };
      },
      post: async (path: string) => {
        calls.push(path);
        if (path === "/api/v0/code/structure/inventory") {
          return {
            data: {
              results: [
                {
                  entity_id: "content-entity:e1",
                  entity_name: "unusedRoute",
                  entity_type: "Function",
                  file_path: "src/unused.ts",
                  repo_id: "repository:r1",
                },
              ],
            },
            error: null,
            truth: null,
          };
        }
        if (path === "/api/v0/code/relationships/story") {
          return {
            data: {
              entity_id: "content-entity:e1",
              labels: ["Function"],
              name: "unusedRoute",
              relationships: [],
            },
            error: null,
            truth: null,
          };
        }
        return { data: {}, error: null, truth: null };
      },
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/code-graph"]}>
        <CodeGraphPage client={client} model={model} repositories={[repository()]} />
      </MemoryRouter>,
    );

    expect(await screen.findByRole("combobox", { name: "Symbol" })).toHaveTextContent(
      "unusedRoute",
    );
    expect(calls).toContain("/api/v0/code/structure/inventory");
    await waitFor(() => expect(calls).toContain("/api/v0/code/relationships/story"));
    expect(calls.some((path) => path.startsWith("/api/v0/repositories"))).toBe(false);
    expect(calls).not.toContain("/api/v0/code/dead-code");
  });

  it("shows an honest empty state when structural inventory has no symbols", async () => {
    const calls: string[] = [];
    const client = {
      get: async (path: string) => {
        calls.push(path);
        return { data: { repositories: [] }, error: null, truth: null };
      },
      post: async (path: string) => {
        calls.push(path);
        if (path === "/api/v0/code/structure/inventory") {
          return { data: { results: [] }, error: null, truth: null };
        }
        return { data: {}, error: null, truth: null };
      },
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/code-graph"]}>
        <CodeGraphPage
          client={client}
          model={{ ...demoModel, findings: [], source: "live" }}
          repositories={[repository()]}
        />
      </MemoryRouter>,
    );

    await waitFor(() => expect(calls).toContain("/api/v0/code/structure/inventory"));
    expect(
      screen.getByText("No modeled code symbols returned for service-repo."),
    ).toBeInTheDocument();
    expect(calls).not.toContain("/api/v0/code/dead-code");
  });

  it("retains the selected repository dead-code overlay when structural inventory is empty", async () => {
    const client = {
      post: async (path: string) => {
        if (path === "/api/v0/code/structure/inventory") {
          return { data: { results: [] }, error: null, truth: null };
        }
        if (path === "/api/v0/code/imports/investigate") {
          return { data: { cycles: [], truncated: false }, error: null, truth: null };
        }
        throw new Error(`unexpected request: ${path}`);
      },
    } as unknown as EshuApiClient;
    const model: ConsoleModel = {
      ...demoModel,
      findings: [
        {
          classification: "unused",
          detail: "src/dead.ts · unused",
          entity: "service-repo",
          entityId: "content-entity:dead",
          filePath: "src/dead.ts",
          id: "dead-1",
          repoId: "repository:r1",
          title: "Unreferenced symbol retainedDeadSymbol",
          truth: "derived",
          type: "Dead code",
        },
      ],
      source: "live",
    };

    render(
      <MemoryRouter initialEntries={["/code-graph?repo_id=repository%3Ar1"]}>
        <CodeGraphPage client={client} model={model} repositories={[repository()]} />
      </MemoryRouter>,
    );

    expect(
      await screen.findByText("No modeled code symbols returned for service-repo."),
    ).toBeInTheDocument();
    expect(screen.getByText("Dead in this repo · 1")).toBeInTheDocument();
    expect(screen.getByText("retainedDeadSymbol")).toBeInTheDocument();
  });

  it("keeps an explicit error when structural inventory fails", async () => {
    const client = {
      get: async () => {
        throw new Error("repository catalog unavailable");
      },
      post: async () => {
        throw new Error("structural inventory unavailable");
      },
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/code-graph"]}>
        <CodeGraphPage
          client={client}
          model={{ ...demoModel, findings: [], source: "live" }}
          repositories={[repository()]}
        />
      </MemoryRouter>,
    );

    expect(await screen.findByText("structural inventory unavailable")).toBeInTheDocument();
  });
});

function repository(): RepoListItem {
  return {
    groupKey: "source",
    groupKind: "source",
    groupReason: "fixture",
    groupSource: "fixture",
    groupTruth: "exact",
    id: "repository:r1",
    isDependency: false,
    name: "service-repo",
    remoteUrl: "",
    repoSlug: "platform/service-repo",
  };
}
