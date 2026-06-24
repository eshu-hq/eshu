import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";

import { DependenciesPage } from "./DependenciesPage";
import type { EshuApiClient } from "../api/client";

function renderWithRouter(ui: React.ReactElement, initialEntries: string[] = ["/dependencies"]) {
  return render(ui, { wrapper: ({ children }) => <MemoryRouter initialEntries={initialEntries}>{children}</MemoryRouter> });
}

const forwardEnvelope = {
  data: {
    dependencies: [
      { direction: "forward", anchor_package: "@eshu/core", anchor_package_id: "npm://r/@eshu/core", declaring_version: "1.0.0", related_package: "left-pad", related_package_id: "npm://r/left-pad", related_ecosystem: "npm", dependency_range: "^1.3.0", dependency_type: "runtime", optional: false, edge_id: "edge-1" }
    ],
    direction: "forward",
    truncated: false
  },
  error: null,
  truth: { profile: "production", level: "exact", capability: "dependencies.list", basis: "authoritative_graph", freshness: { state: "fresh" } }
};

describe("DependenciesPage", () => {
  it("renders forward dependency rows and the truth chip from the live envelope", async () => {
    const client = { get: async () => forwardEnvelope } as unknown as EshuApiClient;

    renderWithRouter(<DependenciesPage client={client} />);

    expect(screen.getByLabelText("Package graph workbench")).toBeInTheDocument();
    expect(await screen.findByText("left-pad")).toBeInTheDocument();
    expect(screen.getByText("@eshu/core")).toBeInTheDocument();
    expect(screen.getByText("^1.3.0")).toBeInTheDocument();
    expect(screen.getByTitle("Truth: exact")).toBeInTheDocument();
  });

  it("requires a package anchor before issuing a reverse lookup", async () => {
    let calls = 0;
    const client = {
      get: async (path: string) => {
        calls += 1;
        if (path.includes("direction=reverse")) {
          return { data: { dependencies: [], direction: "reverse", truncated: false }, error: null, truth: null };
        }
        return forwardEnvelope;
      }
    } as unknown as EshuApiClient;

    renderWithRouter(<DependenciesPage client={client} />);
    await screen.findByText("left-pad");
    const callsAfterForward = calls;

    fireEvent.click(screen.getByRole("button", { name: "Dependents of" }));

    // Switching to reverse with no package anchor must not fire a request.
    await waitFor(() => expect(screen.getByText("Enter a package name to find its dependents.")).toBeInTheDocument());
    expect(calls).toBe(callsAfterForward);
    expect(screen.queryByText(/Failed to load/)).not.toBeInTheDocument();
  });

  it("shows an honest empty state when the package graph has no edges", async () => {
    const client = {
      get: async () => ({ data: { dependencies: [], direction: "forward", truncated: false }, error: null, truth: null })
    } as unknown as EshuApiClient;

    renderWithRouter(<DependenciesPage client={client} />);

    expect(await screen.findByText(/No package dependencies in the indexed package graph/)).toBeInTheDocument();
  });

  it("renders a repo dependency chain with the publisher leg labeled inferred when deep-linked by repo", async () => {
    const chainEnvelope = {
      data: {
        chains: [
          {
            consumer_repository_id: "repo-consumer",
            package_id: "pkg:npm://registry.example/team-api",
            package_name: "@acme/team-api",
            ecosystem: "npm",
            dependency_range: "^1.2.0",
            consumption_correlation_id: "consume-1",
            consumption_provenance_only: false,
            consumption_canonical_writes: 1,
            ambiguous: false,
            publishers: [
              { correlation_id: "publish-1", relationship_kind: "publication", repository_id: "repo-publisher", repository_name: "team-api", provenance_only: true, canonical_writes: 0 }
            ]
          }
        ],
        repository_id: "repo-consumer",
        truncated: false
      },
      error: null,
      truth: { profile: "production", level: "exact", capability: "package_registry.dependency_chains.list", basis: "semantic_facts", freshness: { state: "fresh" } }
    };
    let chainPath = "";
    const client = {
      get: async (path: string) => {
        if (path.includes("dependency-chains")) { chainPath = path; return chainEnvelope; }
        return { data: { dependencies: [], direction: "forward", truncated: false }, error: null, truth: null };
      }
    } as unknown as EshuApiClient;

    renderWithRouter(<DependenciesPage client={client} />, ["/dependencies?repo=repo-consumer"]);

    // The publisher repo name and an inferred (provenance-only) label must appear.
    expect(await screen.findByText("team-api")).toBeInTheDocument();
    expect(screen.getByText("@acme/team-api")).toBeInTheDocument();
    expect(screen.getByTitle("Truth: inferred")).toBeInTheDocument();
    expect(chainPath).toContain("repository_id=repo-consumer");
  });
});
