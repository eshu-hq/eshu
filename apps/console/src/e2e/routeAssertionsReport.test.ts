import {
  buildNetworkReportObservation,
  buildRouteReportObservation,
  type RouteSignals,
} from "./routeAssertions";

function healthySignals(overrides: Partial<RouteSignals> = {}): RouteSignals {
  return {
    connected: true,
    consoleErrors: [],
    demoBannerPresent: false,
    durationMs: 1234,
    mainContentChars: 140,
    network: [],
    route: { path: "/dashboard", label: "Dashboard", area: "dashboard" },
    sourceMode: "live",
    workflow: null,
    ...overrides,
  };
}

describe("buildRouteReportObservation", () => {
  it("preserves request status and visible data-shape proof without query strings or bodies", () => {
    const observation = buildRouteReportObservation(
      healthySignals({
        route: {
          path: "/semantic-search",
          label: "Semantic Search",
          area: "ask",
          workflow: {
            id: "semantic-search",
            kind: "state",
            anySelectors: [".sem-result-announce"],
          },
        },
        network: [
          {
            url: "https://host/eshu-api/api/v0/search/semantic?q=private-token",
            method: "POST",
            status: 200,
            failureText: null,
          },
        ],
        workflow: {
          id: "semantic-search",
          passed: true,
          detail: "rendered a visible semantic result",
          dataShapes: [{ selector: ".sem-result-announce", visibleCount: 1 }],
          requests: [{ method: "POST", pathname: "/eshu-api/api/v0/search/semantic", status: 200 }],
        },
      }),
    );

    expect(observation.network).toEqual([
      {
        failureText: null,
        method: "POST",
        pathname: "/eshu-api/api/v0/search/semantic",
        status: 200,
      },
    ]);
    expect(observation.workflow?.dataShapes).toEqual([
      { selector: ".sem-result-announce", visibleCount: 1 },
    ]);
    expect(JSON.stringify(observation)).not.toContain("private-token");
  });

  it("bounds retained request observations while marking truncation", () => {
    const network = Array.from({ length: 205 }, (_, index) => ({
      failureText: null,
      method: "GET",
      status: 200,
      url: `https://host/eshu-api/api/v0/entities/${index}`,
    }));

    const observation = buildRouteReportObservation(healthySignals({ network }));

    expect(observation.network).toHaveLength(200);
    expect(observation.networkTruncated).toBe(true);
  });

  it("redacts URL query values from retained transport failure text", () => {
    const observation = buildRouteReportObservation(
      healthySignals({
        network: [
          {
            failureText: "failed at https://host/eshu-api/api/v0/entities?credential=private-token",
            method: "GET",
            status: 0,
            url: "https://host/eshu-api/api/v0/entities",
          },
        ],
      }),
    );

    expect(JSON.stringify(observation)).not.toContain("private-token");
  });
});

describe("buildNetworkReportObservation", () => {
  it("retains bounded bootstrap transport proof without query values", () => {
    const observations = Array.from({ length: 205 }, (_, index) => ({
      failureText:
        index === 0
          ? "failed at https://host/eshu-api/api/v0/status?credential=private-token"
          : null,
      method: "GET",
      status: index === 0 ? 0 : 200,
      url: `https://host/eshu-api/api/v0/entities/${index}?credential=private-token`,
    }));

    const report = buildNetworkReportObservation(observations);

    expect(report.network).toHaveLength(200);
    expect(report.networkTruncated).toBe(true);
    expect(report.network[0].pathname).toBe("/eshu-api/api/v0/entities/0");
    expect(JSON.stringify(report)).not.toContain("private-token");
  });
});
