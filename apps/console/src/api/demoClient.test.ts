import { createDemoApiClient } from "./demoClient";
import { demoRepositories } from "./demoFixtures";

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
    expect(env.truth?.capability).toBe("operations.status");
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

  // #5171 regression: a repository-scope row's source_key must resolve to a
  // repository catalog id the demo corpus actually covers -- otherwise the
  // "Now processing" repo label's link (repositorySourceHref in
  // operationsBoard.ts) lands on an uncovered demo repository page instead of
  // one with real freshness data. The live backend's source_key IS the
  // repository catalog id for a repository scope, so the demo fixture must
  // mirror that identity, not invent an unrelated opaque value.
  it("keys every repository-scope live_activity row's source_key to a demo repository id", async () => {
    const client = createDemoApiClient();

    const env = await client.get<Record<string, unknown>>("/api/v0/status/operations?limit=25");

    const rows = env.data?.live_activity as readonly Record<string, unknown>[];
    const demoRepoIds = new Set(demoRepositories.map((repo) => repo.id));
    const repositoryScopeRows = rows.filter((row) => row.scope_kind === "repository");
    expect(repositoryScopeRows.length).toBeGreaterThan(0);
    for (const row of repositoryScopeRows) {
      expect(demoRepoIds.has(row.source_key as string)).toBe(true);
    }
    expect(rows.map((row) => row.source_key)).toEqual([
      "repository:checkout-service",
      "repository:payments-api",
      "repository:checkout-service",
    ]);
  });

  // domain_backlogs (#5172 cold-review P2-2): the demo fixture's queue and
  // live_activity already show a busy board (outstanding=16, a running row,
  // a claimed row, a stale retrying row, one dead-lettered item). Before this
  // fix domain_backlogs was absent, so the "Top domain backlogs" panel
  // rendered its empty "pipeline idle" state right next to that visibly busy
  // board -- a false-idle read. These rows must stay coherent with that
  // picture, not just present.
  it("serves domain_backlogs rows coherent with the live_activity/queue picture (issue #5172)", async () => {
    const client = createDemoApiClient();

    const env = await client.get<Record<string, unknown>>("/api/v0/status/operations?limit=25");

    expect(env.error).toBeNull();
    const rows = env.data?.domain_backlogs as readonly Record<string, unknown>[];
    expect(Array.isArray(rows)).toBe(true);
    expect(rows.length).toBeGreaterThan(0);
    // Same two repository domains the live_activity fixture uses -- not an
    // unrelated demo-only domain -- and server-sorted outstanding desc.
    expect(rows.map((row) => row.domain)).toEqual([
      "repository:checkout-service",
      "repository:payments-api",
    ]);
    // outstanding sums to the queue's outstanding total, and dead_letter
    // sums to the queue's dead_letter total, so the panel and the rest of
    // the board never disagree about how much work is backlogged.
    const queue = env.data?.queue as Record<string, number>;
    const outstandingSum = rows.reduce((sum, row) => sum + (row.outstanding as number), 0);
    const deadLetterSum = rows.reduce((sum, row) => sum + (row.dead_letter as number), 0);
    expect(outstandingSum).toBe(queue.outstanding);
    expect(deadLetterSum).toBe(queue.dead_letter);
    // checkout-service is the domain carrying the dead-lettered item and the
    // oldest outstanding age (its stale retrying row, wi-demo-3).
    expect(rows[0]).toMatchObject({
      domain: "repository:checkout-service",
      dead_letter: 1,
      oldest_age: 360,
    });
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
