import { render, screen, waitFor } from "@testing-library/react";
import { StrictMode } from "react";
import { describe, expect, it, vi } from "vitest";

import { DashboardPage } from "./DashboardPage";
import type { EshuApiClient } from "../api/client";
import { emptySnapshot, modelFromSnapshot } from "../console/liveModel";
import { loadingRepositoryCatalog } from "../repositoryCatalogLifecycle";

describe("DashboardPage atlas lifecycle", () => {
  it("reuses one atlas discovery owner during StrictMode replay", async () => {
    const resolveCalls: unknown[] = [];
    const entityMapCalls: unknown[] = [];
    const client = {
      postJson: vi.fn(async (_path: string, body: unknown) => {
        resolveCalls.push(body);
        return {
          entities: [
            {
              entity_id: "workload:checkout-service",
              labels: ["Workload"],
              name: "checkout-service",
            },
          ],
        };
      }),
      post: vi.fn(async (_path: string, body: unknown) => {
        entityMapCalls.push(body);
        return {
          data: {
            evidence: {
              relationships: [
                {
                  direction: "outgoing",
                  entity_id: "workload:payments-api",
                  entity_labels: ["Workload"],
                  entity_name: "payments-api",
                  relationship_type: "DEPENDS_ON",
                },
                {
                  direction: "outgoing",
                  entity_id: "workload:orders-api",
                  entity_labels: ["Workload"],
                  entity_name: "orders-api",
                  relationship_type: "CALLS",
                },
              ],
            },
            from: "checkout-service",
            resolution: {
              candidates: [
                {
                  id: "workload:checkout-service",
                  labels: ["Workload"],
                  name: "checkout-service",
                },
              ],
            },
          },
          error: null,
          truth: null,
        };
      }),
    } as unknown as EshuApiClient;

    render(
      <StrictMode>
        <DashboardPage
          client={client}
          model={modelFromSnapshot({
            ...emptySnapshot(),
            provenance: { services: "live" },
            services: [
              {
                environments: ["prod"],
                freshness: "fresh",
                id: "svc-checkout",
                kind: "service",
                name: "checkout-service",
                repo: "checkout",
                truth: "exact",
              },
            ],
          })}
          repositoryCatalog={loadingRepositoryCatalog}
        />
      </StrictMode>,
    );

    await waitFor(() => expect(screen.getByText("3 nodes · 2 edges")).toBeInTheDocument());
    expect(resolveCalls).toHaveLength(1);
    expect(entityMapCalls).toHaveLength(1);
  });

  it("starts a new atlas owner when a seed keeps its identity but changes scope", async () => {
    const resolveBodies: unknown[] = [];
    const client = {
      postJson: vi.fn(async (_path: string, body: unknown) => {
        resolveBodies.push(body);
        return { entities: [] };
      }),
      post: vi.fn(async () => ({
        data: { evidence: { relationships: [] }, resolution: { candidates: [] } },
        error: null,
        truth: null,
      })),
    } as unknown as EshuApiClient;
    const { rerender } = render(<DashboardPage client={client} model={liveSeedModel("service")} />);

    await waitFor(() =>
      expect(resolveBodies).toEqual([{ limit: 1, name: "shared-seed", type: "workload" }]),
    );

    rerender(<DashboardPage client={client} model={liveSeedModel("repo")} />);

    await waitFor(() =>
      expect(resolveBodies).toEqual([
        { limit: 1, name: "shared-seed", type: "workload" },
        { limit: 1, name: "shared-seed", repo_id: "seed:shared" },
      ]),
    );
  });
});

function liveSeedModel(kind: string) {
  return modelFromSnapshot({
    ...emptySnapshot(),
    provenance: { services: "live" },
    services: [
      {
        environments: [],
        freshness: "fresh",
        id: "seed:shared",
        kind,
        name: "shared-seed",
        repo: "",
        truth: "exact",
      },
    ],
  });
}
