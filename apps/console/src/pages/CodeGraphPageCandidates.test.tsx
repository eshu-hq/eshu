import { render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";

import { CodeGraphPage } from "./CodeGraphPage";
import type { EshuApiClient } from "../api/client";
import { demoModel } from "../console/demoModel";
import type { ConsoleModel } from "../console/types";

describe("CodeGraphPage candidate reuse", () => {
  it("uses non-empty bootstrap dead-code candidates without reloading the catalog or scan", async () => {
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
        <CodeGraphPage client={client} model={model} />
      </MemoryRouter>,
    );

    await screen.findByText("unusedRoute");
    await waitFor(() => expect(calls).toContain("/api/v0/code/relationships/story"));
    expect(calls.some((path) => path.startsWith("/api/v0/repositories"))).toBe(false);
    expect(calls).not.toContain("/api/v0/code/dead-code");
  });

  it("still fetches live candidates when the bootstrap snapshot has none", async () => {
    const calls: string[] = [];
    const client = {
      get: async (path: string) => {
        calls.push(path);
        return { data: { repositories: [] }, error: null, truth: null };
      },
      post: async (path: string) => {
        calls.push(path);
        if (path === "/api/v0/code/dead-code") {
          return { data: { results: [] }, error: null, truth: null };
        }
        return { data: {}, error: null, truth: null };
      },
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/code-graph"]}>
        <CodeGraphPage client={client} model={{ ...demoModel, findings: [], source: "live" }} />
      </MemoryRouter>,
    );

    await waitFor(() => expect(calls).toContain("/api/v0/code/dead-code"));
    expect(screen.getByText("No dead-code entity selected.")).toBeInTheDocument();
    expect(screen.queryByText(/Failed to load live dead-code candidates/)).not.toBeInTheDocument();
  });

  it("keeps an explicit error when the required empty-snapshot fallback fails", async () => {
    const client = {
      get: async () => {
        throw new Error("repository catalog unavailable");
      },
      post: async () => {
        throw new Error("dead-code scan unavailable");
      },
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/code-graph"]}>
        <CodeGraphPage client={client} model={{ ...demoModel, findings: [], source: "live" }} />
      </MemoryRouter>,
    );

    expect(
      await screen.findByText(
        "Failed to load live dead-code candidates: dead-code scan unavailable",
      ),
    ).toBeInTheDocument();
  });
});
