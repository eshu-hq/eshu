import { createDemoApiClient } from "./demoClient";

describe("demoClient", () => {
  it("returns checkout demo evidence for the supported impact scope", async () => {
    const client = createDemoApiClient();

    const env = await client.post<Record<string, unknown>>(
      "/api/v0/impact/trace-deployment-chain",
      { max_depth: 4, service_name: "checkout-service" },
    );

    expect(env.error).toBeNull();
    expect(env.data?.story).toBe(
      "Demo fixture traces checkout-service from repository workflow to image, workload, and cloud resources.",
    );
    expect(env.truth?.basis).toBe("demo_fixture");
    expect(env.truth?.profile).toBe("demo_fixture");
  });

  it("rejects impact scopes outside the demo fixture corpus", async () => {
    const client = createDemoApiClient();

    const env = await client.post<Record<string, unknown>>(
      "/api/v0/impact/trace-deployment-chain",
      { max_depth: 4, service_name: "other-service" },
    );

    expect(env.data).toBeNull();
    expect(env.error?.code).toBe("demo_fixture_scope_not_covered");
  });

  it("rejects runtime drift scopes outside the demo fixture corpus", async () => {
    const client = createDemoApiClient();

    const env = await client.post<Record<string, unknown>>("/api/v0/aws/runtime-drift/findings", {
      account_id: "other-account",
      limit: 50,
      offset: 0,
      region: "us-east-1",
      scope_id: "aws:demo-account",
    });

    expect(env.data).toBeNull();
    expect(env.error?.code).toBe("demo_fixture_scope_not_covered");
  });

  it("rejects CI/CD query scopes outside the demo fixture corpus", async () => {
    const client = createDemoApiClient();

    const env = await client.get<Record<string, unknown>>(
      "/api/v0/ci-cd/run-correlations?repository_id=repository:other&environment=prod&limit=25",
    );

    expect(env.data).toBeNull();
    expect(env.error?.code).toBe("demo_fixture_scope_not_covered");
  });

  it("serves a wire-shaped live operations board fixture (issue #5137)", async () => {
    const client = createDemoApiClient();

    const env = await client.get<Record<string, unknown>>("/api/v0/status/operations?limit=25");

    expect(env.error).toBeNull();
    expect(env.truth?.basis).toBe("demo_fixture");
    expect(Array.isArray(env.data?.collectors)).toBe(true);
    expect(Array.isArray(env.data?.live_activity)).toBe(true);
    const rows = env.data?.live_activity as readonly Record<string, unknown>[];
    const firstRow = rows[0];
    // source_display is the operator-facing repo name (#5137 follow-up),
    // distinct from the opaque source_key the live backend stores.
    expect(firstRow?.source_display).toBe("acme/checkout-service");
    expect(firstRow?.source_key).not.toBe(firstRow?.source_display);
    // generation_state (#5138): the demo fixture carries one retrying row
    // from a superseded generation to demo the console's dimmed/badged
    // rendering, alongside ordinary "active" rows.
    expect(firstRow?.generation_state).toBe("active");
    expect(rows.some((row) => row.generation_state === "stale")).toBe(true);
  });

  it("serves a wire-shaped current freshness fixture for checkout-service (issue #5143)", async () => {
    const client = createDemoApiClient();

    const env = await client.get<Record<string, unknown>>(
      "/api/v0/repositories/repository%3Acheckout-service/freshness",
    );

    expect(env.error).toBeNull();
    expect(env.truth?.basis).toBe("demo_fixture");
    expect(env.data?.verdict).toBe("current");
    expect(env.data?.observed_commit).not.toBe("");
    expect(env.data?.stages).toEqual({
      collected: true,
      reduced: true,
      projected: true,
      materialized: true,
    });
  });

  it("serves a wire-shaped building freshness fixture for payments-api (issue #5143)", async () => {
    const client = createDemoApiClient();

    const env = await client.get<Record<string, unknown>>(
      "/api/v0/repositories/repository%3Apayments-api/freshness",
    );

    expect(env.data?.verdict).toBe("building");
    expect(Array.isArray(env.data?.outstanding_by_stage)).toBe(true);
    expect((env.data?.outstanding_by_stage as readonly Record<string, unknown>[])[0]?.stage).toBe(
      "project",
    );
  });

  it("serves a wire-shaped unobserved freshness fixture for repos outside the demo corpus (issue #5143)", async () => {
    const client = createDemoApiClient();

    const env = await client.get<Record<string, unknown>>(
      "/api/v0/repositories/repository%3Alegacy-batch/freshness",
    );

    expect(env.data?.verdict).toBe("unobserved");
    expect(env.data?.observed_commit).toBe("");
    expect(env.data?.unobserved_push).not.toBeNull();
  });
});
