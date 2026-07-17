import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";

import { CodeGraphPage } from "./CodeGraphPage";
import type { EshuApiClient } from "../api/client";
import type { RepoListItem } from "../api/repoCatalog";
import { demoModel } from "../console/demoModel";

describe("CodeGraphPage legacy candidate resolution", () => {
  it("does not choose a repository when a legacy label is ambiguous", () => {
    const inventoryCalls: string[] = [];
    const client = {
      post: async (_path: string, body: unknown) => {
        inventoryCalls.push(String((body as { readonly repo_id?: string }).repo_id));
        return { data: { results: [] }, error: null, truth: null };
      },
    } as unknown as EshuApiClient;
    const repositories = [
      repository("repository:r1", "shared-service", "group-one/shared-service"),
      repository("repository:r2", "shared-service", "group-two/shared-service"),
    ];

    render(
      <MemoryRouter initialEntries={["/code-graph?candidate=legacy-shared"]}>
        <CodeGraphPage
          client={client}
          model={{
            ...demoModel,
            findings: [
              {
                detail: "src/shared.ts · unused",
                entity: "shared-service",
                id: "legacy-shared",
                title: "Unreferenced symbol sharedSymbol",
                truth: "derived",
                type: "Dead code",
              },
            ],
            source: "live",
          }}
          repositories={repositories}
        />
      </MemoryRouter>,
    );

    expect(screen.getByText(/repository label matches multiple repositories/i)).toBeInTheDocument();
    expect(screen.getByRole("combobox", { name: "Repository" })).toHaveValue("");
    expect(inventoryCalls).toEqual([]);
  });
});

function repository(id: string, name: string, repoSlug: string): RepoListItem {
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
    repoSlug,
  };
}
