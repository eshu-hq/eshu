import { fireEvent, render, screen } from "@testing-library/react";

import { CodeGraphSelectors } from "./CodeGraphSelectors";
import type { RepoListItem } from "../api/repoCatalog";
import type { FindingRow } from "../console/types";

describe("CodeGraphSelectors", () => {
  it("searches repository and symbol controls independently", () => {
    render(
      <CodeGraphSelectors
        loading={false}
        onEntityChange={vi.fn()}
        onRepositoryChange={vi.fn()}
        repositories={[
          repository("repository:r1", "service-one"),
          repository("repository:r2", "service-two"),
          repository("repository:r3", "service-three"),
        ]}
        selectedEntityId="content-entity:alpha"
        selectedRepositoryId="repository:r1"
        symbols={[
          symbol("content-entity:alpha", "alphaSymbol"),
          symbol("content-entity:beta", "betaSymbol"),
          symbol("content-entity:gamma", "gammaSymbol"),
        ]}
      />,
    );

    fireEvent.change(screen.getByRole("searchbox", { name: "Search repositories" }), {
      target: { value: "three" },
    });
    expect(screen.getByRole("option", { name: "service-one" })).toBeInTheDocument();
    expect(screen.getByRole("option", { name: "service-three" })).toBeInTheDocument();
    expect(screen.queryByRole("option", { name: "service-two" })).not.toBeInTheDocument();

    const symbolSearch = screen.getByRole("searchbox", { name: "Search symbols" });
    fireEvent.change(symbolSearch, { target: { value: "beta" } });
    expect(screen.getByRole("option", { name: "alphaSymbol" })).toBeInTheDocument();
    expect(screen.getByRole("option", { name: "betaSymbol" })).toBeInTheDocument();
    expect(screen.queryByRole("option", { name: "gammaSymbol" })).not.toBeInTheDocument();

    fireEvent.change(screen.getByRole("combobox", { name: "Repository" }), {
      target: { value: "repository:r3" },
    });
    expect(symbolSearch).toHaveValue("");
  });

  it("clears a prior repository symbol query when history changes repository scope", () => {
    const props = {
      loading: false,
      onEntityChange: vi.fn(),
      onRepositoryChange: vi.fn(),
      repositories: [
        repository("repository:r1", "service-one"),
        repository("repository:r2", "service-two"),
      ],
      selectedEntityId: "content-entity:alpha",
      symbols: [symbol("content-entity:alpha", "alphaSymbol")],
    } as const;
    const { rerender } = render(
      <CodeGraphSelectors {...props} selectedRepositoryId="repository:r1" />,
    );
    const symbolSearch = screen.getByRole("searchbox", { name: "Search symbols" });
    fireEvent.change(symbolSearch, { target: { value: "alpha" } });

    rerender(
      <CodeGraphSelectors
        {...props}
        selectedEntityId="content-entity:beta"
        selectedRepositoryId="repository:r2"
        symbols={[symbol("content-entity:beta", "betaSymbol")]}
      />,
    );

    expect(symbolSearch).toHaveValue("");
  });
});

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

function symbol(id: string, name: string): FindingRow {
  return {
    detail: `src/${name}.ts`,
    entity: "service-one",
    entityId: id,
    id,
    repoId: "repository:r1",
    title: name,
    truth: "exact",
    type: "Code symbol",
  };
}
