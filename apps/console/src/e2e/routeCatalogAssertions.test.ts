import { consoleRoutes, type ConsoleRoute } from "./routeAssertions";
import { APP_ROUTE_PATHS, missingProductionRoutePaths } from "../appRoutePaths";
import { NAV_ITEMS } from "../i18n/navigation";

describe("consoleRoutes catalog", () => {
  it("covers every route exposed by the sidebar navigation", () => {
    const catalogPaths = new Set(consoleRoutes.map((route) => route.path));
    const missingPaths = NAV_ITEMS.map((item) => item.to).filter((path) => !catalogPaths.has(path));

    expect(missingPaths).toEqual([]);
  });

  it("enumerates the major console surfaces without duplicates", () => {
    const paths = consoleRoutes.map((route) => route.path);
    expect(new Set(paths).size).toBe(paths.length);
    for (const required of [
      "/dashboard",
      "/repositories",
      "/catalog",
      "/explorer",
      "/cloud",
      "/observability",
      "/operations",
      "/findings",
      "/ask",
    ]) {
      expect(paths).toContain(required);
    }
  });

  it("covers every acceptance-criteria surface area", () => {
    const areas = new Set(consoleRoutes.map((route) => route.area));
    for (const area of [
      "dashboard",
      "repositories",
      "service",
      "graph",
      "cloud",
      "observability",
      "operations",
      "security",
      "ask",
      "system",
    ]) {
      expect(areas.has(area as ConsoleRoute["area"])).toBe(true);
    }
  });

  it("assigns bounded workflow probes to the newly covered and high-value live routes", () => {
    const workflowsByPath = new Map(
      consoleRoutes.map((route) => [route.path, route.workflow?.kind] as const),
    );

    expect(
      Object.fromEntries(
        [
          "/status",
          "/cloud",
          "/ask",
          "/semantic-search",
          "/relationships",
          "/nodes",
          "/profile",
          "/admin",
          "/operations",
          "/surface-inventory",
          "/dead-code",
          "/code-graph",
          "/vulnerabilities",
          "/cloud-drift",
        ].map((path) => [path, workflowsByPath.get(path)]),
      ),
    ).toEqual({
      "/status": "state",
      "/cloud": "state",
      "/ask": "askExactCount",
      "/semantic-search": "submit",
      "/relationships": "state",
      "/nodes": "fill",
      "/profile": "state",
      "/admin": "click",
      "/operations": "state",
      "/surface-inventory": "fill",
      "/dead-code": "exactKind",
      "/code-graph": "state",
      "/vulnerabilities": "tabs",
      "/cloud-drift": "submit",
    });
  });

  it("gives every live catalog route a visible, surface-specific workflow", () => {
    const routesWithoutWorkflows = consoleRoutes
      .filter((route) => route.workflow === undefined)
      .map((route) => route.path);

    expect(routesWithoutWorkflows).toEqual([]);
    expect(new Set(consoleRoutes.map((route) => route.workflow?.id)).size).toBe(
      consoleRoutes.length,
    );
  });

  it("does not accept a generic page shell as route-specific proof", () => {
    const genericSelectors = new Set([".page", ".page-shell", ".seg-page", ".srp-page"]);
    const genericProofs = consoleRoutes.flatMap((route) => {
      const workflow = route.workflow;
      if (!workflow || workflow.kind !== "state") return [];
      return workflow.anySelectors
        .filter((selector) => genericSelectors.has(selector))
        .map((selector) => `${route.path}:${selector}`);
    });

    expect(genericProofs).toEqual([]);
  });

  it("binds every state workflow to production response ownership", () => {
    const unownedStateWorkflows = consoleRoutes.flatMap((route) => {
      const workflow = route.workflow;
      if (!workflow || workflow.kind !== "state") return [];
      const responseCount =
        (workflow.requiredResponses?.length ?? 0) +
        (workflow.requiredBootstrapResponses?.length ?? 0) +
        (workflow.retainedDataRequiredResponses?.length ?? 0) +
        (workflow.retainedDataRequiredBootstrapResponses?.length ?? 0);
      if (responseCount > 0 || workflow.nonNetworkAuthority !== undefined) return [];
      return [`${route.path}:${workflow.id}`];
    });

    expect(unownedStateWorkflows).toEqual([]);
  });

  it("requires every non-network state authority to explain its source of truth", () => {
    const unexplainedAuthorities = consoleRoutes.flatMap((route) => {
      const workflow = route.workflow;
      if (!workflow || workflow.kind !== "state" || !workflow.nonNetworkAuthority) return [];
      return workflow.nonNetworkAuthority.reason.trim() === ""
        ? [`${route.path}:${workflow.nonNetworkAuthority.kind}`]
        : [];
    });

    expect(unexplainedAuthorities).toEqual([]);
  });

  it("keeps the live catalog in parity with every production route pattern", () => {
    const catalogPatterns = new Set(
      consoleRoutes.flatMap((route) => route.productionPaths ?? [route.path]),
    );

    expect(missingProductionRoutePaths([...catalogPatterns])).toEqual([]);
  });

  it("executes every parameterized route with a real retained-data anchor", () => {
    const repositories = consoleRoutes.find((route) => route.path === "/repositories");
    const incidents = consoleRoutes.find((route) => route.path === "/incidents");
    const vulnerabilities = consoleRoutes.find((route) => route.path === "/vulnerabilities");

    expect(repositories).toMatchObject({
      productionPaths: [
        APP_ROUTE_PATHS.repositories,
        APP_ROUTE_PATHS.repositorySource,
        APP_ROUTE_PATHS.workspace,
      ],
      workflow: { kind: "repositoryDetails" },
    });
    expect(incidents).toMatchObject({
      productionPaths: [APP_ROUTE_PATHS.incidents, APP_ROUTE_PATHS.incidentContext],
      workflow: {
        kind: "submit",
        fields: [{ valueEnv: "ESHU_E2E_INCIDENT_ID" }],
        expectedPagePath: "/incidents/${ESHU_E2E_INCIDENT_ID}/context",
      },
    });
    expect(vulnerabilities).toMatchObject({
      productionPaths: [APP_ROUTE_PATHS.vulnerabilities, APP_ROUTE_PATHS.vulnerabilityDetail],
      workflow: {
        kind: "tabs",
        followLink: { expectedPathPrefix: "/vulnerabilities/" },
      },
    });
  });

  it("proves Cloud Drift from response-backed rows instead of its always-rendered shell", () => {
    const workflow = consoleRoutes.find((route) => route.path === "/cloud-drift")?.workflow;

    expect(workflow).toMatchObject({
      kind: "submit",
      outcomeSelector: ".evidence-workbench > .panel:first-child tbody tr",
      additionalExpectedRequests: [
        { path: "/api/v0/aws/runtime-drift/findings" },
        { path: "/api/v0/iac/unmanaged-resources" },
        { path: "/api/v0/iac/terraform-import-plan/candidates" },
      ],
    });
  });

  it("binds Findings to its three bootstrap snapshot responses without route borrowing", () => {
    const workflow = consoleRoutes.find((route) => route.path === "/findings")?.workflow;

    expect(workflow).toMatchObject({
      kind: "state",
      requiredBootstrapResponses: [
        { path: "/api/v0/code/dead-code", method: "POST", acceptedStatuses: [200] },
        {
          path: "/api/v0/supply-chain/impact/findings",
          method: "GET",
          acceptedStatuses: [200],
          query: { impact_status: "affected_exact" },
        },
        {
          path: "/api/v0/supply-chain/impact/findings",
          method: "GET",
          acceptedStatuses: [200],
          query: { impact_status: "affected_derived" },
        },
      ],
    });
    expect(workflow).not.toHaveProperty("requiredResponses");
  });

  it("fails parity when a production-only parameterized route is missing from the catalog", () => {
    const withoutWorkspace = Object.values(APP_ROUTE_PATHS).filter(
      (path) => path !== APP_ROUTE_PATHS.workspace,
    );

    expect(missingProductionRoutePaths(withoutWorkspace)).toEqual([APP_ROUTE_PATHS.workspace]);
  });

  it("requires the exact retained Trait kind without fallback", () => {
    const workflow = consoleRoutes.find((route) => route.path === "/dead-code")?.workflow;

    expect(workflow).toMatchObject({
      kind: "exactKind",
      preferredName: "Trait",
      outcomeCellSelector: ".evidence-workbench tbody tr.cloud-row td:nth-child(2)",
    });
  });

  it("requires retained semantic-search anchors and visible result rows", () => {
    const workflow = consoleRoutes.find((route) => route.path === "/semantic-search")?.workflow;

    expect(workflow).toMatchObject({
      kind: "submit",
      fields: [
        { valueEnv: "ESHU_E2E_SEMANTIC_REPOSITORY_ID" },
        { valueEnv: "ESHU_E2E_SEMANTIC_QUERY" },
      ],
      outcomeSelector: ".sem-result-row",
      forbiddenSelectors: [".src-err"],
    });
  });
});
