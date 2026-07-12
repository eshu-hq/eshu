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
    expect((env.data?.live_activity as readonly Record<string, unknown>[])[0]?.source_key).toBe(
      "sample/checkout-service",
    );
  });
});
