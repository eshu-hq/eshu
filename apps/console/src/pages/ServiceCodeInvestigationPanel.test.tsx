import { fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter, useLocation } from "react-router";
import { describe, expect, it, vi } from "vitest";

import { ServiceCodeInvestigationPanel } from "./ServiceCodeInvestigationPanel";
import type { ServiceSpotlight } from "../api/serviceSpotlight";
import { loadCodeTopicInvestigation } from "../api/codeTopic";

vi.mock("../api/codeTopic", () => ({
  loadCodeTopicInvestigation: vi.fn(),
}));
vi.mock("../config/environment", () => ({
  loadConsoleEnvironment: () => ({ mode: "private", apiBaseUrl: "http://localhost" }),
}));

function LocationProbe(): React.JSX.Element {
  const location = useLocation();
  return (
    <>
      <output aria-label="Route path">{location.pathname + location.search}</output>
      <output aria-label="Route state">{JSON.stringify(location.state)}</output>
    </>
  );
}

describe("ServiceCodeInvestigationPanel", () => {
  it("offers file content search when the investigation found no code paths", async () => {
    vi.mocked(loadCodeTopicInvestigation).mockResolvedValue({
      callGraphHandles: [],
      coverage: { empty: true, limit: 25, returnedCount: 0, searchedTerms: [], truncated: false },
      evidenceGroups: [],
      matchedFiles: [],
      matchedSymbols: [],
      nextCalls: [
        {
          tool: "search_file_content",
          args: { query: "private-term", repo_id: "repo-a", limit: 25 },
        },
      ],
      topic: "checkout",
    });

    render(
      <MemoryRouter>
        <LocationProbe />
        <ServiceCodeInvestigationPanel
          spotlight={{ name: "checkout", repoName: "repo-a" } as ServiceSpotlight}
        />
      </MemoryRouter>,
    );

    expect(loadCodeTopicInvestigation).toHaveBeenCalled();
    const loaded = await vi.mocked(loadCodeTopicInvestigation).mock.results[0].value;
    expect(loaded.nextCalls).toHaveLength(1);
    const link = await screen.findByRole("link", { name: "Open file content search" });
    expect(link).toHaveAttribute("href", "/content-search");
    expect(screen.getByText("No code paths found for this service")).toBeInTheDocument();
    fireEvent.click(link);
    expect(screen.getByLabelText("Route path")).toHaveTextContent("/content-search");
    expect(screen.getByLabelText("Route path")).not.toHaveTextContent("private-term");
    expect(screen.getByLabelText("Route state")).toHaveTextContent("private-term");
    expect(screen.getByLabelText("Route state")).toHaveTextContent("repo-a");
  });
});
