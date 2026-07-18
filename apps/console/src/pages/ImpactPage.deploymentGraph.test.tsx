import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";

import { ImpactPage } from "./ImpactPage";
import type { EshuApiClient } from "../api/client";
import { emptySnapshot, modelFromSnapshot } from "../console/liveModel";

describe("ImpactPage deployment topology", () => {
  it("renders exact graph composition, structured evidence, narrative, and pivots", async () => {
    const client = {
      post: async (path: string) => {
        if (path === "/api/v0/impact/change-surface/investigate") {
          return { data: zeroChangeSurface(), error: null, truth: truth("derived", "fresh") };
        }
        if (path === "/api/v0/impact/trace-deployment-chain") {
          return { data: deploymentTrace(), error: null, truth: truth("exact", "stale") };
        }
        throw new Error(`unexpected path ${path}`);
      },
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/impact?kind=service&target=catalog-api"]}>
        <ImpactPage client={client} model={modelFromSnapshot(emptySnapshot("live"))} />
      </MemoryRouter>,
    );

    expect(await screen.findByText("Deployment topology")).toBeInTheDocument();
    const composition = screen.getByLabelText("Graph composition evidence");
    expect(within(composition).getByText("deployment trace")).toBeInTheDocument();
    expect(within(composition).getByText("truth exact")).toBeInTheDocument();
    expect(within(composition).getByText("basis authoritative_graph")).toBeInTheDocument();
    expect(within(composition).getByText("freshness stale")).toBeInTheDocument();
    expect(within(composition).getByText("12/12 nodes")).toBeInTheDocument();
    expect(within(composition).getByText("9/9 edges")).toBeInTheDocument();
    expect(within(composition).getByText(/composition \d+\.\d{3} ms/)).toBeInTheDocument();
    expect(document.querySelector(".gnode-instance")).not.toBeNull();
    expect(document.querySelector(".gnode-platform")).not.toBeNull();

    expect(screen.getByText("Full deployment narrative")).toBeInTheDocument();
    expect(
      screen.getByText("catalog-api runs on ECS and Kubernetes through deployment-config."),
    ).toBeInTheDocument();
    expect(screen.getByText("2 runtime instances")).toBeInTheDocument();
    expect(screen.getByText("Runtime instances and platforms")).toBeInTheDocument();
    expect(screen.getByText(/platform:ecs:catalog-ecs/)).toBeInTheDocument();
    expect(screen.getByText(/platform:kubernetes:catalog-eks/)).toBeInTheDocument();
    expect(screen.getByText("Deployment facts")).toBeInTheDocument();
    expect(screen.getByText("DEPLOYS FROM")).toBeInTheDocument();
    expect(screen.getByText("Cloud resources")).toBeInTheDocument();
    expect(screen.getByText("Kubernetes resources")).toBeInTheDocument();

    await waitFor(() => {
      expect(screen.getByRole("link", { name: "Service story" })).toHaveAttribute(
        "href",
        "/service-story/catalog-api",
      );
    });
    expect(screen.getByRole("link", { name: "Workload context" })).toHaveAttribute(
      "href",
      "/workspace/services/workload%3Acatalog-api",
    );
    expect(screen.getByRole("link", { name: "Repository source" })).toHaveAttribute(
      "href",
      "/repositories/repository%3Ar_catalog/source",
    );
    expect(screen.getByRole("link", { name: "deployment-config" })).toHaveAttribute(
      "href",
      "/repositories/repository%3Ar_config/source",
    );
    expect(screen.getByRole("button", { name: "Inspect prod environment" })).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Inspect instance:catalog:prod" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Inspect catalog-ecs platform" }),
    ).toBeInTheDocument();
    const selectedPanel = screen.getByText("Selected entity").closest("section");
    expect(selectedPanel).not.toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Inspect lead-events" }));
    expect(within(selectedPanel as HTMLElement).getByText("cloud:queue")).toBeInTheDocument();
  });
});

function deploymentTrace(): Record<string, unknown> {
  return {
    cloud_resources: [{ id: "cloud:queue", name: "lead-events", resource_type: "aws_sqs_queue" }],
    deployment_sources: [
      {
        confidence: 0.98,
        reason: "canonical deployment source",
        relationship_type: "DEPLOYS_FROM",
        repo_id: "repository:r_config",
        repo_name: "deployment-config",
        source_id: "repository:r_config",
        target_id: "repository:r_catalog",
      },
    ],
    deployment_facts: [
      {
        confidence: 0.98,
        reason: "canonical deployment source",
        target: "deployment-config",
        target_id: "repository:r_config",
        type: "DEPLOYS_FROM",
      },
    ],
    instances: [
      {
        environment: "prod",
        instance_id: "instance:catalog:prod",
        platforms: [
          {
            platform_id: "platform:ecs:catalog-ecs",
            platform_kind: "ecs",
            platform_name: "catalog-ecs",
          },
          {
            platform_id: "platform:kubernetes:catalog-eks",
            platform_kind: "kubernetes",
            platform_name: "catalog-eks",
          },
        ],
      },
      {
        environment: "stage",
        instance_id: "instance:catalog:stage",
        platforms: [
          {
            platform_id: "platform:kubernetes:catalog-stage-eks",
            platform_kind: "kubernetes",
            platform_name: "catalog-stage-eks",
          },
        ],
      },
    ],
    k8s_resources: [{ entity_id: "k8s:catalog", entity_name: "catalog-api", kind: "Deployment" }],
    repo_id: "repository:r_catalog",
    repo_name: "catalog-api",
    service_name: "catalog-api",
    story: "catalog-api runs on ECS and Kubernetes through deployment-config.",
    workload_id: "workload:catalog-api",
  };
}

function truth(level: string, freshness: string): Record<string, unknown> {
  return {
    basis: "authoritative_graph",
    capability: "platform_impact.deployment_chain",
    freshness: { state: freshness },
    level,
    profile: "local_authoritative",
  };
}

function zeroChangeSurface(): Record<string, unknown> {
  return {
    code_surface: {
      changed_files: [],
      matched_file_count: 0,
      source_backends: [],
      symbol_count: 0,
      touched_symbols: [],
    },
    coverage: {
      direct_count: 0,
      limit: 25,
      max_depth: 4,
      query_shape: "resolved_change_surface_traversal",
      transitive_count: 0,
      truncated: false,
    },
    direct_impact: [],
    impact_summary: { direct_count: 0, total_count: 0, transitive_count: 0 },
    scope: { limit: 25, max_depth: 4, target: "catalog-api", target_type: "service" },
    source_backend: "authoritative_graph",
    target_resolution: {
      input: "catalog-api",
      selected: { id: "workload:catalog-api", labels: ["Workload"], name: "catalog-api" },
      status: "resolved",
      target_type: "service",
      truncated: false,
    },
    transitive_impact: [],
    truncated: false,
  };
}
