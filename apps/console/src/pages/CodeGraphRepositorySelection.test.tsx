import { render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, useLocation } from "react-router-dom";

import { CodeGraphPage } from "./CodeGraphPage";
import type { EshuApiClient } from "../api/client";
import { demoRepositories } from "../api/demoFixtures";
import type { RepoListItem } from "../api/repoCatalog";
import { demoModel } from "../console/demoModel";
import type { ConsoleModel } from "../console/types";

const repositories: readonly RepoListItem[] = [
  repository("repository:r1", "service-one"),
  repository("repository:r2", "service-two"),
];

describe("CodeGraphPage repository selection", () => {
  it("maps demo findings onto the same catalog repository contract", async () => {
    render(
      <MemoryRouter initialEntries={["/code-graph"]}>
        <CodeGraphPage model={demoModel} repositories={demoRepositories} />
      </MemoryRouter>,
    );

    expect(screen.getByRole("combobox", { name: "Repository" })).toHaveValue(
      "repository:checkout-service",
    );
    expect(await screen.findByRole("combobox", { name: "Symbol" })).toHaveTextContent(
      "legacyDiscount",
    );
  });

  it("selects a catalog repository absent from the dead-code window", async () => {
    const calls: { readonly body: unknown; readonly path: string }[] = [];
    const client = {
      post: async (path: string, body: unknown) => {
        calls.push({ body, path });
        if (path === "/api/v0/code/structure/inventory") {
          return {
            data: {
              count: 1,
              limit: 100,
              next_offset: null,
              repo_id: "repository:r2",
              results: [
                {
                  end_line: 18,
                  entity_id: "content-entity:r2-entry",
                  entity_name: "serviceTwoEntry",
                  entity_type: "Function",
                  file_path: "src/service-two.ts",
                  language: "typescript",
                  repo_id: "repository:r2",
                  start_line: 12,
                },
              ],
              truncated: false,
            },
            error: null,
            truth: { freshness: { state: "fresh" }, level: "exact", profile: "production" },
          };
        }
        if (path === "/api/v0/code/relationships/story") {
          return {
            data: {
              entity_id: "content-entity:r2-entry",
              labels: ["Function"],
              name: "serviceTwoEntry",
              scope: { repo_id: "repository:r2" },
              target_resolution: {
                entity_id: "content-entity:r2-entry",
                repo_id: "repository:r2",
                status: "resolved",
              },
              relationships: [],
            },
            error: null,
            truth: null,
          };
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
          detail: "src/dead.ts · unused",
          entity: "service-one",
          entityId: "content-entity:r1-dead",
          filePath: "src/dead.ts",
          id: "dead-r1",
          repoId: "repository:r1",
          title: "Unreferenced symbol deadInServiceOne",
          truth: "derived",
          type: "Dead code",
        },
      ],
      source: "live",
    };

    render(
      <MemoryRouter initialEntries={["/code-graph?repo_id=repository%3Ar2"]}>
        <CodeGraphPage client={client} model={model} repositories={repositories} />
        <LocationProbe />
      </MemoryRouter>,
    );

    expect(screen.getByRole("combobox", { name: "Repository" })).toHaveValue("repository:r2");
    expect(screen.getByRole("combobox", { name: "Repository" })).toHaveTextContent("service-two");
    expect(await screen.findByRole("combobox", { name: "Symbol" })).toHaveValue(
      "content-entity:r2-entry",
    );
    expect(screen.getByRole("combobox", { name: "Symbol" })).toHaveTextContent("serviceTwoEntry");
    expect(await screen.findByRole("link", { name: "Open source" })).toHaveAttribute(
      "href",
      "/repositories/repository%3Ar2/source?path=src%2Fservice-two.ts&lineStart=12&lineEnd=18",
    );
    expect(
      screen.getByText("No modeled code relationships returned for service-two."),
    ).toBeInTheDocument();
    expect(screen.queryByText("deadInServiceOne")).not.toBeInTheDocument();
    await waitFor(() =>
      expect(screen.getByTestId("location")).toHaveTextContent(
        "/code-graph?repo_id=repository%3Ar2&entity_id=content-entity%3Ar2-entry",
      ),
    );
    expect(calls).toContainEqual({
      body: { inventory_kind: "entity", limit: 100, repo_id: "repository:r2" },
      path: "/api/v0/code/structure/inventory",
    });
    await waitFor(() =>
      expect(calls.some((call) => call.path === "/api/v0/code/relationships/story")).toBe(true),
    );
    expect(calls.some((call) => call.path === "/api/v0/code/dead-code")).toBe(false);
  });
});

function LocationProbe(): React.JSX.Element {
  const location = useLocation();
  return <output data-testid="location">{`${location.pathname}${location.search}`}</output>;
}

function repository(id: string, name: string): RepoListItem {
  return {
    groupKey: "source",
    groupKind: "source",
    groupReason: "fixture",
    groupSource: "fixture",
    groupTruth: "exact",
    id,
    isDependency: false,
    name,
    remoteUrl: "",
    repoSlug: `platform/${name}`,
  };
}
