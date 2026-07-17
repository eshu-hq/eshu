import { fireEvent, render, screen, within } from "@testing-library/react";
import { vi } from "vitest";

import { SemanticRepositorySelector } from "./SemanticRepositorySelector";
import type { RepoListItem } from "../api/repoCatalog";
import { readyRepositoryCatalog } from "../repositoryCatalogLifecycle";

describe("SemanticRepositorySelector", () => {
  it("includes canonical IDs when duplicate names and slugs need disambiguation", () => {
    const onChange = vi.fn();
    render(
      <SemanticRepositorySelector
        catalog={readyRepositoryCatalog([
          repository("repository:r_one", "shared-service", "team/shared-service"),
          repository("repository:r_two", "shared-service", "team/shared-service"),
        ])}
        onChange={onChange}
        searchHint=""
        selectedRepositoryId=""
      />,
    );

    const selector = screen.getByRole("combobox", { name: "Repository" });
    expect(
      within(selector).getByRole("option", {
        name: "shared-service — team/shared-service — repository:r_one",
      }),
    ).toBeInTheDocument();
    fireEvent.change(selector, { target: { value: "repository:r_two" } });
    expect(onChange).toHaveBeenCalledWith("repository:r_two");
  });

  it("filters a full 887-repository session catalog while preserving the selection", () => {
    const repositories = Array.from({ length: 887 }, (_, index) =>
      repository(
        `repository:r_${String(index).padStart(4, "0")}`,
        `service-${String(index).padStart(4, "0")}`,
        `team/service-${String(index).padStart(4, "0")}`,
      ),
    );
    const selected = repositories[886]?.id ?? "";
    render(
      <SemanticRepositorySelector
        catalog={readyRepositoryCatalog(repositories)}
        onChange={vi.fn()}
        searchHint=""
        selectedRepositoryId={selected}
      />,
    );

    fireEvent.change(screen.getByRole("searchbox", { name: "Search repositories" }), {
      target: { value: "no-match" },
    });

    const selector = screen.getByRole("combobox", { name: "Repository" });
    expect(selector).toHaveValue(selected);
    expect(within(selector).getAllByRole("option")).toHaveLength(2);
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
