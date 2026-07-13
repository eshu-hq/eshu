// api/demoOperationsFixture.ts
// operationsBoardWire is the wire-shaped fixture for GET
// /api/v0/status/operations (issue #5137). Timestamps are computed relative
// to call time (rather than baked in) so collector heartbeat freshness
// (fresh/lagging/stale) reads correctly however long a demo session runs.
//
// Split out of demoFixtures.ts and dynamically imported from demoClient.ts's
// fetcher (issue #5139): the operations board route itself is already
// React.lazy-loaded, but this fixture was still a static import reachable
// from the eager fetcher dispatch table, so its bytes counted against the
// console's tightly budgeted main bundle (scripts/console-bundle-budget.mjs)
// even though most demo sessions never open the operations board.
function demoCollectorWire(
  instanceId: string,
  kind: string,
  mode: string,
  displayName: string,
  health: string,
  lastObservedAt: string,
): Record<string, unknown> {
  return {
    instance_id: instanceId,
    collector_kind: kind,
    mode,
    display_name: displayName,
    health,
    last_observed_at: lastObservedAt,
  };
}

function demoActivityWire(
  workItemId: string,
  stage: string,
  status: string,
  repo: string,
  leaseOwner: string,
  attemptCount: number,
  ageSeconds: number,
  collectorKind: string,
  sourceSystem: string,
): Record<string, unknown> {
  return {
    work_item_id: workItemId,
    stage,
    status,
    domain: `repository:${repo}`,
    lease_owner: leaseOwner,
    claim_until: null,
    attempt_count: attemptCount,
    updated_at: null,
    created_at: null,
    age_seconds: ageSeconds,
    scope_kind: "repository",
    collector_kind: collectorKind,
    source_system: sourceSystem,
    // source_key: opaque hash like the live backend (#5137 follow-up).
    // source_display: operator-facing name from the scope payload.
    source_key: `r_${workItemId}`,
    source_display: `acme/${repo}`,
  };
}

export function operationsBoardWire(): Record<string, unknown> {
  const now = Date.now();
  const minutesAgo = (minutes: number): string => new Date(now - minutes * 60_000).toISOString();
  return {
    version: "demo-fixture",
    as_of: new Date(now).toISOString(),
    scoped: false,
    health: { state: "degraded", reasons: ["queue_backlog"] },
    collectors: [
      demoCollectorWire("git-1", "git", "poll", "Git", "healthy", minutesAgo(0.3)),
      demoCollectorWire("sbom-1", "sbom_attestation", "claim", "SBOM", "degraded", minutesAgo(45)),
    ],
    stage_summaries: [
      {
        stage: "reducer",
        pending: 12,
        claimed: 3,
        running: 2,
        retrying: 1,
        succeeded: 940,
        failed: 0,
        dead_letter: 1,
      },
      {
        stage: "projector",
        pending: 4,
        claimed: 1,
        running: 1,
        retrying: 0,
        succeeded: 512,
        failed: 0,
        dead_letter: 0,
      },
    ],
    queue: {
      outstanding: 16,
      in_flight: 5,
      retrying: 1,
      succeeded: 1452,
      dead_letter: 1,
      failed: 0,
      overdue_claims: 0,
    },
    live_activity: [
      demoActivityWire(
        "wi-demo-1",
        "reducer",
        "running",
        "checkout-service",
        "reducer-1",
        1,
        90,
        "git",
        "github",
      ),
      demoActivityWire(
        "wi-demo-2",
        "projector",
        "claimed",
        "payments-api",
        "projector-2",
        1,
        30,
        "git",
        "github",
      ),
      demoActivityWire(
        "wi-demo-3",
        "reducer",
        "retrying",
        "checkout-service",
        "reducer-2",
        3,
        360,
        "sbom_attestation",
        "sbom",
      ),
    ],
    truncated: false,
    limit: 50,
  };
}
