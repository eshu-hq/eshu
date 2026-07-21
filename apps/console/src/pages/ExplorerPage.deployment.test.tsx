import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";

import { ExplorerPage } from "./ExplorerPage";
import type { EshuApiClient } from "../api/client";
import { demoModel } from "../console/demoModel";
import type { ConsoleModel } from "../console/types";

const liveModel: ConsoleModel = { ...demoModel, source: "live" };

describe("ExplorerPage bounded deployment detail", () => {
  it("resolves catalog service names through the bounded workload resolver", async () => {
    const bodies: unknown[] = [];
    const client = {
      postJson: async (_path: string, body: unknown) => {
        bodies.push(body);
        return {
          entities: [
            {
              id: "workload:checkout-service",
              labels: ["Workload"],
              name: "checkout-service",
              type: "Workload",
            },
          ],
        };
      },
      get: async () => ({ data: {}, error: null, truth: null }),
      post: async () => ({ data: {}, error: null, truth: null }),
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/explorer?q=checkout-service"]}>
        <ExplorerPage client={client} model={liveModel} />
      </MemoryRouter>,
    );

    await waitFor(() =>
      expect(bodies).toContainEqual({ limit: 1, name: "checkout-service", type: "workload" }),
    );
  });

  it("keeps endpoint-less source truth visible and expands the bounded instance family", async () => {
    const calls: string[] = [];
    const instances = Array.from({ length: 14 }, (_, index) => ({
      environment: `environment-${index}`,
      instance_id: `workload-instance:checkout-api:${index}`,
      platforms: [
        {
          platform_kind: index % 2 === 0 ? "ecs_service" : "kubernetes",
          platform_name: `platform-${index}`,
        },
      ],
    }));
    const client = {
      get: async (path: string) => {
        calls.push(path);
        return {
          data: {
            id: "workload:checkout-api",
            instances,
            name: "checkout-api",
            repo_id: "repository:r_checkout",
            repo_name: "checkout-api",
          },
          error: null,
          truth: null,
        };
      },
      post: async (path: string) => {
        calls.push(path);
        if (path === "/api/v0/impact/trace-deployment-chain") {
          return {
            data: {
              deployment_sources: [
                {
                  reason: "canonical_instance_deployment_source",
                  repo_id: "repository:r_runtime",
                  repo_name: "runtime-deploy",
                },
              ],
              instances,
              service_name: "checkout-api",
              workload_id: "workload:checkout-api",
            },
            error: null,
            truth: null,
          };
        }
        throw new Error(`unexpected POST ${path}`);
      },
      postJson: async () => ({
        entities: [
          {
            id: "workload:checkout-api",
            labels: ["Workload"],
            name: "checkout-api",
            repo_id: "repository:r_checkout",
            type: "Workload",
          },
        ],
      }),
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/explorer?q=checkout-api"]}>
        <ExplorerPage client={client} model={liveModel} />
      </MemoryRouter>,
    );

    expect(await screen.findByText("runtime-deploy")).toBeInTheDocument();
    expect(screen.queryByText("environment-13")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Show more deployment evidence" }));
    expect(await screen.findByText("environment-13")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Show less deployment evidence" })).toBeVisible();
    await waitFor(() =>
      expect(calls.filter((path) => path === "/api/v0/impact/trace-deployment-chain")).toHaveLength(
        2,
      ),
    );
  });
});
