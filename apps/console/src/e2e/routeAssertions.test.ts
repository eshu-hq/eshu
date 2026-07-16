import {
  defaultNetworkAllowList,
  evaluateNetworkObservations,
  evaluateRoute,
  minMainContentChars,
  summarizeGate,
  type ConsoleRoute,
  type NetworkAllowRule,
  type RouteResult,
  type RouteSignals,
} from "./routeAssertions";

const sampleRoute: ConsoleRoute = { path: "/dashboard", label: "Dashboard", area: "dashboard" };

function healthySignals(overrides: Partial<RouteSignals> = {}): RouteSignals {
  return {
    route: sampleRoute,
    connected: true,
    sourceMode: "live",
    demoBannerPresent: false,
    mainContentChars: minMainContentChars + 100,
    consoleErrors: [],
    workflow: null,
    network: [
      {
        url: "https://host/eshu-api/api/v0/index-status",
        method: "GET",
        status: 200,
        failureText: null,
      },
    ],
    durationMs: 1234,
    ...overrides,
  };
}

describe("evaluateRoute", () => {
  it("passes a fully healthy live route", () => {
    const result = evaluateRoute(healthySignals());
    expect(result.passed).toBe(true);
    expect(result.failures).toHaveLength(0);
    expect(result.durationMs).toBe(1234);
  });

  it("fails when the source is not connected", () => {
    const result = evaluateRoute(
      healthySignals({ connected: false, sourceMode: "needs-connection" }),
    );
    expect(result.passed).toBe(false);
    expect(result.failures.map((f) => f.code)).toContain("not_connected");
  });

  it("fails on demo fallback by source mode", () => {
    const result = evaluateRoute(healthySignals({ sourceMode: "demo" }));
    expect(result.failures.map((f) => f.code)).toContain("demo_fallback");
  });

  it("fails on demo fallback by visible demo banner", () => {
    const result = evaluateRoute(healthySignals({ demoBannerPresent: true }));
    expect(result.failures.map((f) => f.code)).toContain("demo_fallback");
  });

  it("fails a blank render below the substance threshold", () => {
    const result = evaluateRoute(healthySignals({ mainContentChars: minMainContentChars - 1 }));
    expect(result.failures.map((f) => f.code)).toContain("blank_render");
  });

  it("accepts an explicit empty/unavailable state above the threshold", () => {
    // Empty-but-healthy is acceptable proof: substantive status copy, no error.
    const result = evaluateRoute(healthySignals({ mainContentChars: minMainContentChars + 1 }));
    expect(result.passed).toBe(true);
  });

  it("fails on any browser console error", () => {
    const result = evaluateRoute(healthySignals({ consoleErrors: ["TypeError: boom"] }));
    expect(result.failures.map((f) => f.code)).toContain("console_error");
  });

  it("redacts URL query values from browser errors retained in the report", () => {
    const result = evaluateRoute(
      healthySignals({
        consoleErrors: [
          "request failed at https://host/eshu-api/api/v0/search?token=private-token",
        ],
      }),
    );

    expect(JSON.stringify(result.failures)).toContain("/eshu-api/api/v0/search");
    expect(JSON.stringify(result.failures)).not.toContain("private-token");
  });

  it("fails on an unexpected non-2xx network response", () => {
    const result = evaluateRoute(
      healthySignals({
        network: [
          {
            url: "https://host/eshu-api/api/v0/catalog",
            method: "GET",
            status: 500,
            failureText: null,
          },
        ],
      }),
    );
    expect(result.failures.map((f) => f.code)).toContain("unexpected_network");
  });

  it("redacts request query values from failure details retained in the report", () => {
    const result = evaluateRoute(
      healthySignals({
        network: [
          {
            url: "https://host/eshu-api/api/v0/catalog?token=private-token",
            method: "GET",
            status: 500,
            failureText: null,
          },
        ],
      }),
    );

    expect(JSON.stringify(result.failures)).toContain("/eshu-api/api/v0/catalog");
    expect(JSON.stringify(result.failures)).not.toContain("private-token");
  });

  it("fails on a transport-level network failure even with status 0", () => {
    const result = evaluateRoute(
      healthySignals({
        network: [
          {
            url: "https://host/eshu-api/api/v0/catalog",
            method: "GET",
            status: 0,
            failureText: "net::ERR_CONNECTION_REFUSED",
          },
        ],
      }),
    );
    expect(result.failures.map((f) => f.code)).toContain("unexpected_network");
  });

  it("treats 3xx as ok", () => {
    const result = evaluateRoute(
      healthySignals({
        network: [
          {
            url: "https://host/eshu-api/api/v0/redirect",
            method: "GET",
            status: 304,
            failureText: null,
          },
        ],
      }),
    );
    expect(result.passed).toBe(true);
  });

  it("allows a justified non-2xx via the allow list and records it", () => {
    const allowList: readonly NetworkAllowRule[] = [
      {
        method: "GET",
        pathname: "/eshu-api/api/v0/optional-feature",
        status: 404,
        reason: "feature endpoint absent on the no-provider local stack",
      },
    ];
    const result = evaluateRoute(
      healthySignals({
        network: [
          {
            url: "https://host/eshu-api/api/v0/optional-feature",
            method: "GET",
            status: 404,
            failureText: null,
          },
        ],
      }),
      allowList,
    );
    expect(result.passed).toBe(true);
    expect(result.allowedNonOk).toHaveLength(1);
    expect(result.allowedNonOk[0].reason).toContain("no-provider");
  });

  it("allows only the exact shared-key browser-session fallback handshake", () => {
    const result = evaluateRoute(
      healthySignals({
        network: [
          {
            url: "http://127.0.0.1/eshu-api/api/v0/auth/browser-session",
            method: "GET",
            status: 401,
            failureText: null,
          },
          {
            url: "http://127.0.0.1/eshu-api/api/v0/auth/browser-session",
            method: "POST",
            status: 400,
            failureText: null,
          },
        ],
      }),
      defaultNetworkAllowList,
    );

    expect(result.passed).toBe(true);
    expect(result.allowedNonOk).toHaveLength(2);
  });

  it("rejects browser-session allow-list near misses", () => {
    for (const observation of [
      {
        url: "http://127.0.0.1/eshu-api/api/v0/auth/browser-session/context",
        method: "GET",
        status: 401,
        failureText: null,
      },
      {
        url: "http://127.0.0.1/eshu-api/api/v0/auth/browser-session",
        method: "POST",
        status: 401,
        failureText: null,
      },
      {
        url: "http://127.0.0.1/eshu-api/api/v0/auth/browser-session",
        method: "GET",
        status: 400,
        failureText: null,
      },
    ]) {
      const result = evaluateRoute(
        healthySignals({ network: [observation] }),
        defaultNetworkAllowList,
      );
      expect(result.failures.map((failure) => failure.code)).toContain("unexpected_network");
    }
  });

  it("fails the owning route when API requests did not settle", () => {
    const result = evaluateRoute(
      healthySignals({
        apiQuiet: { settled: false, inFlight: 2, waitedMs: 18_000 },
      }),
    );

    expect(result.failures.map((failure) => failure.code)).toContain("api_not_quiet");
  });

  it("does not allow a transport failure even if an allow rule matches the status", () => {
    const allowList: readonly NetworkAllowRule[] = [
      {
        method: "GET",
        pathname: "/eshu-api/api/v0/optional",
        status: 0,
        reason: "should not mask transport failures",
      },
    ];
    const result = evaluateRoute(
      healthySignals({
        network: [
          {
            url: "https://host/eshu-api/api/v0/optional",
            method: "GET",
            status: 0,
            failureText: "net::ERR_ABORTED",
          },
        ],
      }),
      allowList,
    );
    expect(result.failures.map((f) => f.code)).toContain("unexpected_network");
  });

  it("accumulates multiple independent failures", () => {
    const result = evaluateRoute(
      healthySignals({
        connected: false,
        sourceMode: "demo",
        consoleErrors: ["boom"],
      }),
    );
    const codes = result.failures.map((f) => f.code);
    expect(codes).toContain("not_connected");
    expect(codes).toContain("demo_fallback");
    expect(codes).toContain("console_error");
  });

  it("fails when a route-specific workflow probe was not completed", () => {
    const workflowRoute: ConsoleRoute = {
      path: "/status",
      label: "Status",
      area: "operations",
      workflow: {
        id: "status-live-overview",
        kind: "state",
        anySelectors: [".status-hero"],
        forbiddenText: "Status is unavailable from this source.",
      },
    };
    const result = evaluateRoute(healthySignals({ route: workflowRoute, workflow: null }));

    expect(result.failures.map((failure) => failure.code)).toContain("workflow_failed");
  });

  it("passes a completed route-specific workflow probe", () => {
    const workflowRoute: ConsoleRoute = {
      path: "/status",
      label: "Status",
      area: "operations",
      workflow: {
        id: "status-live-overview",
        kind: "state",
        anySelectors: [".status-hero"],
        forbiddenText: "Status is unavailable from this source.",
      },
    };
    const result = evaluateRoute(
      healthySignals({
        route: workflowRoute,
        workflow: {
          id: "status-live-overview",
          passed: true,
          detail: "rendered live overall indexing state",
        },
      }),
    );

    expect(result.passed).toBe(true);
  });
});

describe("summarizeGate", () => {
  function resultFor(path: string, passed: boolean): RouteResult {
    return {
      route: { path, label: path, area: "dashboard" },
      passed,
      failures: passed ? [] : [{ code: "blank_render", detail: "x" }],
      allowedNonOk: [],
      durationMs: 10,
    };
  }

  it("passes only when every route passed", () => {
    const summary = summarizeGate([resultFor("/a", true), resultFor("/b", true)]);
    expect(summary.passed).toBe(true);
    expect(summary.passedCount).toBe(2);
    expect(summary.failedCount).toBe(0);
  });

  it("fails when any route failed", () => {
    const summary = summarizeGate([resultFor("/a", true), resultFor("/b", false)]);
    expect(summary.passed).toBe(false);
    expect(summary.passedCount).toBe(1);
    expect(summary.failedCount).toBe(1);
  });

  it("fails on an empty result set rather than vacuously passing", () => {
    const summary = summarizeGate([]);
    expect(summary.passed).toBe(false);
  });

  it("fails when bootstrap traffic contains a server or transport failure", () => {
    const bootstrap = evaluateNetworkObservations(
      "bootstrap",
      [
        {
          url: "https://host/eshu-api/api/v0/status",
          method: "GET",
          status: 500,
          failureText: null,
        },
        {
          url: "https://host/eshu-api/api/v0/repositories",
          method: "GET",
          status: 0,
          failureText: "net::ERR_ABORTED",
        },
      ],
      defaultNetworkAllowList,
    );

    const summary = summarizeGate([resultFor("/a", true)], [bootstrap]);

    expect(bootstrap.failures).toHaveLength(2);
    expect(summary.passed).toBe(false);
    expect(summary.preflightFailureCount).toBe(2);
  });

  it("allows only the explicit bootstrap auth fallback handshakes", () => {
    const bootstrap = evaluateNetworkObservations(
      "bootstrap",
      [
        {
          url: "https://host/eshu-api/api/v0/auth/browser-session",
          method: "GET",
          status: 401,
          failureText: null,
        },
        {
          url: "https://host/eshu-api/api/v0/auth/browser-session",
          method: "POST",
          status: 400,
          failureText: null,
        },
      ],
      defaultNetworkAllowList,
    );

    const summary = summarizeGate([resultFor("/a", true)], [bootstrap]);

    expect(bootstrap.passed).toBe(true);
    expect(bootstrap.allowedNonOk).toHaveLength(2);
    expect(summary.passed).toBe(true);
    expect(summary.preflightFailureCount).toBe(0);
  });
});
