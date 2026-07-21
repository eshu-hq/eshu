import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";

import { ExplorerPage } from "./ExplorerPage";
import type { EshuApiClient } from "../api/client";
import { demoModel } from "../console/demoModel";
import type { ConsoleModel } from "../console/types";

const liveModel: ConsoleModel = { ...demoModel, source: "live" };

function renderExplorer(client: EshuApiClient, q: string): void {
  render(
    <MemoryRouter initialEntries={[`/explorer?q=${encodeURIComponent(q)}`]}>
      <ExplorerPage model={liveModel} client={client} />
    </MemoryRouter>,
  );
}

function renderSeededExplorer(client: EshuApiClient, defaultQuery: string): void {
  render(
    <MemoryRouter initialEntries={["/code-graph"]}>
      <ExplorerPage model={liveModel} client={client} defaultQuery={defaultQuery} />
    </MemoryRouter>,
  );
}

describe("ExplorerPage mode-by-kind (issue #1725)", () => {
  it("auto-selects Neighborhood for a service kind and loads the entity map", async () => {
    const calls: string[] = [];
    const client = {
      postJson: async () => ({
        entities: [
          {
            id: "workload:catalog-api",
            name: "catalog-api",
            labels: ["Workload"],
            type: "Workload",
          },
        ],
      }),
      post: async (path: string) => {
        calls.push(path);
        return {
          data: {
            from: "catalog-api",
            resolution: {
              candidates: [
                { id: "workload:catalog-api", name: "catalog-api", labels: ["Workload"] },
              ],
            },
            evidence: {
              relationships: [
                {
                  entity_id: "repository:r1",
                  entity_name: "items",
                  entity_labels: ["Repository"],
                  direction: "incoming",
                  relationship_type: "DEFINES",
                },
              ],
            },
          },
          error: null,
          truth: null,
        };
      },
    } as unknown as EshuApiClient;

    renderExplorer(client, "catalog-api");

    // The service search must route to impact/entity-map, never code/relationships.
    await waitFor(() => expect(calls).toContain("/api/v0/impact/entity-map"));
    expect(calls).not.toContain("/api/v0/code/relationships");
    // The Neighborhood toggle is now active.
    expect(screen.getByRole("button", { name: "Neighborhood" }).className).toContain("active");
  });

  it("renders the deployment story from service context before falling back to entity-map", async () => {
    const calls: string[] = [];
    const client = {
      postJson: async () => ({
        entities: [
          {
            id: "workload:svc-platform",
            name: "svc-platform",
            labels: ["Workload"],
            type: "Workload",
          },
        ],
      }),
      get: async (path: string) => {
        calls.push(path);
        return {
          data: {
            name: "svc-platform",
            repo_name: "svc-platform",
            deployment_evidence: {
              artifacts: [
                {
                  source_repo_id: "repository:r_dd626fe7",
                  source_repo_name: "iac-eks-argocd",
                  target_repo_id: "repository:r_078043f1",
                  target_repo_name: "svc-platform",
                  relationship_type: "DEPLOYS_FROM",
                  artifact_family: "kustomize",
                  path: "applicationsets/core-engineering/api-node/kustomization.yaml",
                },
                {
                  source_repo_id: "repository:r_66cd2d76",
                  source_repo_name: "helm-charts",
                  target_repo_id: "repository:r_078043f1",
                  target_repo_name: "svc-platform",
                  relationship_type: "DEPLOYS_FROM",
                  artifact_family: "helm",
                  path: "svc-platform/Chart.yaml",
                },
              ],
            },
          },
          error: null,
          truth: null,
        };
      },
      post: async (path: string) => {
        calls.push(path);
        if (path === "/api/v0/impact/trace-deployment-chain") {
          return { data: {}, error: null, truth: null };
        }
        throw new Error(`unexpected POST ${path}`);
      },
    } as unknown as EshuApiClient;

    renderExplorer(client, "svc-platform");

    expect(await screen.findByText("iac-eks-argocd")).toBeInTheDocument();
    expect(screen.getByText("helm-charts")).toBeInTheDocument();
    fireEvent.click(screen.getByText("iac-eks-argocd"));
    expect(screen.getByText("DEPLOYS_FROM → svc-platform")).toBeInTheDocument();
    expect(calls).toContain("/api/v0/services/svc-platform/context");
    expect(calls).not.toContain("/api/v0/impact/entity-map");
    expect(screen.queryByText(/RELATED/)).not.toBeInTheDocument();
  });

  it("opens the inline evidence panel when an inspector edge row is clicked", async () => {
    const client = {
      postJson: async () => ({
        entities: [
          {
            id: "workload:checkout-api",
            name: "checkout-api",
            labels: ["Workload"],
            type: "Workload",
          },
        ],
      }),
      get: async () => ({
        data: {
          name: "checkout-api",
          repo_name: "checkout-api",
          deployment_evidence: {
            artifacts: [
              {
                source_repo_id: "repository:r_dd626fe7",
                source_repo_name: "gitops-config",
                target_repo_id: "repository:r_078043f1",
                target_repo_name: "checkout-api",
                relationship_type: "DEPLOYS_FROM",
                artifact_family: "kustomize",
                path: "applicationsets/checkout/kustomization.yaml",
              },
            ],
          },
        },
        error: null,
        truth: null,
      }),
      post: async (path: string) => {
        if (path === "/api/v0/impact/trace-deployment-chain") {
          return { data: {}, error: null, truth: null };
        }
        throw new Error(`unexpected POST ${path}`);
      },
    } as unknown as EshuApiClient;

    renderExplorer(client, "checkout-api");

    fireEvent.click(await screen.findByText("gitops-config"));
    const edgeRow = screen.getByRole("button", { name: /DEPLOYS_FROM → checkout-api/i });
    fireEvent.click(edgeRow);

    const panel = screen.getByRole("region", { name: /Evidence for DEPLOYS_FROM/i });
    expect(panel).toBeInTheDocument();
    // The edge's evidence-derived facts (endpoints + layer) are now surfaced.
    expect(panel.querySelectorAll(".evp-row").length).toBeGreaterThan(0);
    fireEvent.click(screen.getByRole("button", { name: "Close" }));
    expect(screen.queryByRole("region", { name: /Evidence for DEPLOYS_FROM/i })).toBeNull();
  });

  it("keeps Direct for a code (Function) kind and loads code/relationships", async () => {
    const calls: string[] = [];
    const client = {
      postJson: async () => ({
        entities: [
          {
            id: "content-entity:e1",
            name: "createNewVersion",
            labels: ["Function"],
            type: "Function",
          },
        ],
      }),
      post: async (path: string) => {
        calls.push(path);
        return {
          data: {
            entity_id: "content-entity:e1",
            name: "createNewVersion",
            labels: ["Function"],
            incoming: [{ type: "CALLS", source_id: "content-entity:e2", source_name: "main" }],
            outgoing: [],
          },
          error: null,
          truth: null,
        };
      },
    } as unknown as EshuApiClient;

    renderExplorer(client, "createNewVersion");

    await waitFor(() => expect(calls).toContain("/api/v0/code/relationships"));
    expect(calls).not.toContain("/api/v0/impact/entity-map");
    expect(screen.getByRole("button", { name: "Direct" }).className).toContain("active");
  });

  it("links a direct code entity to its repository source location", async () => {
    const client = {
      postJson: async () => ({
        entities: [
          {
            id: "content-entity:e1",
            name: "searchByPortalId",
            labels: ["Function"],
            type: "Function",
          },
        ],
      }),
      post: async () => ({
        data: {
          entity_id: "content-entity:e1",
          name: "searchByPortalId",
          labels: ["Function"],
          repo_id: "repository:r_platform",
          repo_name: "svc-platform",
          file_path: "server/resources/listing/index.js",
          start_line: 1653,
          end_line: 1662,
          incoming: [],
          outgoing: [],
        },
        error: null,
        truth: null,
      }),
    } as unknown as EshuApiClient;

    renderExplorer(client, "searchByPortalId");

    expect(
      await screen.findByRole("link", { name: "server/resources/listing/index.js:1653-1662" }),
    ).toHaveAttribute(
      "href",
      "/repositories/repository%3Ar_platform/source?path=server%2Fresources%2Flisting%2Findex.js&lineStart=1653&lineEnd=1662",
    );
    expect(screen.getByRole("link", { name: "Open source" })).toHaveAttribute(
      "href",
      "/repositories/repository%3Ar_platform/source?path=server%2Fresources%2Flisting%2Findex.js&lineStart=1653&lineEnd=1662",
    );
  });

  it("uses a live default query when the page has no search parameter", async () => {
    const calls: string[] = [];
    const client = {
      postJson: async () => ({
        entities: [
          {
            id: "content-entity:e1",
            name: "createNewVersion",
            labels: ["Function"],
            type: "Function",
          },
        ],
      }),
      post: async (path: string) => {
        calls.push(path);
        return {
          data: {
            entity_id: "content-entity:e1",
            name: "createNewVersion",
            labels: ["Function"],
            incoming: [{ type: "CALLS", source_id: "content-entity:e2", source_name: "main" }],
            outgoing: [],
          },
          error: null,
          truth: null,
        };
      },
    } as unknown as EshuApiClient;

    renderSeededExplorer(client, "createNewVersion");

    await waitFor(() => expect(calls).toContain("/api/v0/code/relationships"));
    expect(screen.getByDisplayValue("createNewVersion")).toBeInTheDocument();
  });

  it("does not render an enabled expand action for the graph center", async () => {
    const client = {
      postJson: async () => ({
        entities: [
          {
            id: "workload:catalog-api",
            name: "catalog-api",
            labels: ["Workload"],
            type: "Workload",
          },
        ],
      }),
      post: async () => ({
        data: {
          from: "catalog-api",
          resolution: {
            candidates: [{ id: "workload:catalog-api", name: "catalog-api", labels: ["Workload"] }],
          },
          evidence: {
            relationships: [
              {
                entity_id: "repository:r1",
                entity_name: "repo-a",
                entity_labels: ["Repository"],
                direction: "incoming",
                relationship_type: "DEFINES",
              },
            ],
          },
        },
        error: null,
        truth: null,
      }),
    } as unknown as EshuApiClient;

    renderExplorer(client, "catalog-api");

    const centerButton = await screen.findByRole("button", { name: "Current center" });
    expect(centerButton).toBeDisabled();
    expect(
      screen.queryByRole("button", { name: "Expand relationships →" }),
    ).not.toBeInTheDocument();
  });

  it("renders edge endpoints by display name instead of raw graph id", async () => {
    const client = {
      postJson: async () => ({
        entities: [
          {
            id: "workload:catalog-api",
            name: "catalog-api",
            labels: ["Workload"],
            type: "Workload",
          },
        ],
      }),
      post: async () => ({
        data: {
          from: "catalog-api",
          resolution: {
            candidates: [{ id: "workload:catalog-api", name: "catalog-api", labels: ["Workload"] }],
          },
          evidence: {
            relationships: [
              {
                entity_id: "repository:r1",
                entity_name: "repo-a",
                entity_labels: ["Repository"],
                direction: "incoming",
                relationship_type: "DEFINES",
              },
            ],
          },
        },
        error: null,
        truth: null,
      }),
    } as unknown as EshuApiClient;

    renderExplorer(client, "catalog-api");

    expect(await screen.findByText("DEFINES ← repo-a")).toBeInTheDocument();
    expect(screen.queryByText("DEFINES ← repository:r1")).not.toBeInTheDocument();
  });

  it("centers a selected neighbor using its canonical graph id", async () => {
    const bodies: unknown[] = [];
    const client = {
      postJson: async () => ({
        entities: [
          {
            id: "workload:catalog-api",
            name: "catalog-api",
            labels: ["Workload"],
            type: "Workload",
          },
        ],
      }),
      post: async (_path: string, body: unknown) => {
        bodies.push(body);
        return {
          data: {
            from: "catalog-api",
            resolution: {
              candidates: [
                { id: "workload:catalog-api", name: "catalog-api", labels: ["Workload"] },
              ],
            },
            evidence: {
              relationships: [
                {
                  entity_id: "repository:r1",
                  entity_name: "repo-a",
                  entity_labels: ["Repository"],
                  direction: "incoming",
                  relationship_type: "DEFINES",
                },
              ],
            },
          },
          error: null,
          truth: null,
        };
      },
    } as unknown as EshuApiClient;

    renderExplorer(client, "catalog-api");

    await screen.findByText("repo-a");
    fireEvent.click(screen.getByText("repo-a"));
    fireEvent.click(screen.getByRole("button", { name: "Center graph here →" }));

    await waitFor(() => expect(bodies).toContainEqual({ from: "repository:r1", depth: 2 }));
    expect(screen.getByDisplayValue("repo-a")).toBeInTheDocument();
  });
});
