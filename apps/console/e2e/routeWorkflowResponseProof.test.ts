import type { Page } from "playwright";
import { describe, expect, it, vi } from "vitest";

import {
  consoleRoutes,
  type NetworkObservation,
  type RouteWorkflowSpec,
} from "../src/e2e/routeAssertions";
import { executeRouteWorkflow } from "./routeWorkflowProbes";
import { locatorStub } from "./routeWorkflowProbesTestSupport";

const findingsPath = "/api/v0/supply-chain/impact/findings";

function findingResponse(impactStatus: string): NetworkObservation {
  return {
    method: "GET",
    status: 200,
    url: `http://host/eshu-api${findingsPath}?limit=100&impact_status=${impactStatus}`,
    failureText: null,
  };
}

function findingsWorkflow(): Extract<RouteWorkflowSpec, { readonly kind: "state" }> {
  return {
    id: "findings-live",
    kind: "state" as const,
    anySelectors: ["[data-finding-row]"],
    requiredBootstrapResponses: [
      {
        path: findingsPath,
        method: "GET" as const,
        acceptedStatuses: [200],
        query: { impact_status: "affected_exact" },
      },
      {
        path: findingsPath,
        method: "GET" as const,
        acceptedStatuses: [200],
        query: { impact_status: "affected_derived" },
      },
    ],
  };
}

describe("executeRouteWorkflow response proof", () => {
  it("accepts a visible Dashboard atlas only from its exact bootstrap responses", async () => {
    const workflow = consoleRoutes.find((route) => route.path === "/")?.workflow;
    if (!workflow || workflow.kind !== "state") {
      throw new Error("Dashboard state workflow is missing");
    }
    const atlas = locatorStub();
    const absent = locatorStub({ count: vi.fn().mockResolvedValue(0) });
    const page = {
      locator: vi.fn((selector: string) =>
        selector === ".dashboard-atlas-panel .gcanvas-svg" ? atlas : absent,
      ),
    } as unknown as Page;
    const bootstrap = [
      ["GET", "/api/v0/catalog"],
      ["GET", "/api/v0/repositories"],
      ["POST", "/api/v0/entities/resolve"],
      ["POST", "/api/v0/impact/entity-map"],
    ].map(([method, path]) => ({
      method: method ?? "GET",
      status: 200,
      url: `http://host/eshu-api${path ?? ""}`,
      failureText: null,
    }));

    const result = await executeRouteWorkflow(page, workflow, vi.fn(), [], bootstrap);

    expect(result.passed).toBe(true);
    expect(result.requests).toHaveLength(4);
    expect(result.requests?.every((request) => request.phase === "bootstrap")).toBe(true);
  });

  it("rejects a visible Service Catalog selector when its production fetch is absent", async () => {
    const workflow = consoleRoutes.find((route) => route.path === "/catalog")?.workflow;
    if (!workflow || workflow.kind !== "state") {
      throw new Error("Service Catalog state workflow is missing");
    }
    const rows = locatorStub();
    const absent = locatorStub({ count: vi.fn().mockResolvedValue(0) });
    const page = {
      locator: vi.fn((selector: string) => (selector === ".tbl tbody .t-name" ? rows : absent)),
    } as unknown as Page;

    const result = await executeRouteWorkflow(page, workflow, vi.fn(), [
      {
        method: "GET",
        status: 200,
        url: "http://host/eshu-api/api/v0/status/collector-readiness",
        failureText: null,
      },
    ]);

    expect(result.passed).toBe(false);
    expect(result.detail).toContain("required bootstrap response GET /api/v0/catalog");
  });

  it("accepts a truthful empty Topology from catalog ownership without story requests", async () => {
    const workflow = consoleRoutes.find((route) => route.path === "/topology")?.workflow;
    if (!workflow || workflow.kind !== "state") {
      throw new Error("Topology state workflow is missing");
    }
    const absent = locatorStub({ count: vi.fn().mockResolvedValue(0) });
    const empty = locatorStub({
      allTextContents: vi.fn().mockResolvedValue(["No services are available from this source."]),
    });
    const page = {
      locator: vi.fn((selector: string) =>
        selector === ".topology-page > .empty" ? empty : absent,
      ),
    } as unknown as Page;

    const result = await executeRouteWorkflow(
      page,
      workflow,
      vi.fn(),
      [],
      [
        {
          method: "GET",
          status: 200,
          url: "http://host/eshu-api/api/v0/catalog?limit=2000&offset=0",
          failureText: null,
        },
      ],
    );

    expect(result.passed).toBe(true);
    expect(result.detail).toContain("truthful empty state");
  });

  it("passes a state workflow only when its required response and live selector are present", async () => {
    const main = locatorStub();
    const liveState = locatorStub();
    const page = {
      locator: vi.fn((selector: string) => (selector === ".main" ? main : liveState)),
    } as unknown as Page;

    const result = await executeRouteWorkflow(
      page,
      {
        id: "status-live",
        kind: "state",
        anySelectors: [".status-hero"],
        requiredResponses: [
          {
            path: "/api/v0/status/collector-readiness",
            method: "GET",
            acceptedStatuses: [200],
          },
        ],
      },
      vi.fn(),
      [
        {
          method: "GET",
          status: 200,
          url: "http://host/eshu-api/api/v0/status/collector-readiness",
          failureText: null,
        },
      ],
    );

    expect(result.passed).toBe(true);
    expect(liveState.count).toHaveBeenCalledOnce();
  });

  it("rejects a generic state shell without its required successful API response", async () => {
    const main = locatorStub();
    const shell = locatorStub();
    const page = {
      locator: vi.fn((selector: string) => (selector === ".main" ? main : shell)),
    } as unknown as Page;

    const result = await executeRouteWorkflow(
      page,
      {
        id: "relationships-live",
        kind: "state",
        anySelectors: [".rel-verb-row"],
        requiredResponses: [
          {
            path: "/api/v0/relationships/catalog",
            method: "POST",
            acceptedStatuses: [200],
          },
        ],
      },
      vi.fn(),
      [],
    );

    expect(result.passed).toBe(false);
    expect(result.detail).toContain("required route response");
  });

  it("accepts a response-backed state with retained result cardinality", async () => {
    const main = locatorStub();
    const rows = locatorStub({ count: vi.fn().mockResolvedValue(16) });
    const page = {
      locator: vi.fn((selector: string) => (selector === ".main" ? main : rows)),
    } as unknown as Page;

    const result = await executeRouteWorkflow(
      page,
      {
        id: "relationships-live",
        kind: "state",
        anySelectors: [".rel-verb-row"],
        requiredResponses: [
          {
            path: "/api/v0/relationships/catalog",
            method: "POST",
            acceptedStatuses: [200],
          },
        ],
      },
      vi.fn(),
      [
        {
          method: "POST",
          status: 200,
          url: "http://host/eshu-api/api/v0/relationships/catalog",
          failureText: null,
        },
      ],
    );

    expect(result.passed).toBe(true);
    expect(result.dataShapes).toEqual([{ selector: ".rel-verb-row", visibleCount: 16 }]);
  });

  it("rejects a snapshot-backed state when one required bootstrap response is missing", async () => {
    const rows = locatorStub();
    const page = { locator: vi.fn(() => rows) } as unknown as Page;
    const result = await executeRouteWorkflow(
      page,
      findingsWorkflow(),
      vi.fn(),
      [],
      [findingResponse("affected_exact")],
    );

    expect(result.passed).toBe(false);
    expect(result.detail).toContain("required bootstrap response GET");
  });

  it("rejects duplicate exact findings responses when derived findings are absent", async () => {
    const rows = locatorStub();
    const page = { locator: vi.fn(() => rows) } as unknown as Page;

    const result = await executeRouteWorkflow(
      page,
      findingsWorkflow(),
      vi.fn(),
      [],
      [findingResponse("affected_exact"), findingResponse("affected_exact")],
    );

    expect(result.passed).toBe(false);
    expect(result.detail).toContain("required bootstrap response GET");
  });

  it("accepts exact and derived findings without exposing query values in the report", async () => {
    const rows = locatorStub();
    const page = { locator: vi.fn(() => rows) } as unknown as Page;

    const result = await executeRouteWorkflow(
      page,
      findingsWorkflow(),
      vi.fn(),
      [],
      [findingResponse("affected_exact"), findingResponse("affected_derived")],
    );

    expect(result.passed).toBe(true);
    expect(result.requests).toEqual([
      {
        method: "GET",
        pathname: `/eshu-api${findingsPath}`,
        phase: "bootstrap",
        status: 200,
      },
      {
        method: "GET",
        pathname: `/eshu-api${findingsPath}`,
        phase: "bootstrap",
        status: 200,
      },
    ]);
    const recordedRequests = JSON.stringify(result.requests);
    expect(recordedRequests).not.toContain("impact_status");
    expect(recordedRequests).not.toContain("affected_");
  });
});
