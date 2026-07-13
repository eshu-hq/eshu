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
  generationState: "active" | "stale" = "active",
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
    // source_key: the repository catalog id, matching the live backend
    // (#5137 follow-up) where a repository scope's source_key IS the
    // repository catalog id (both derive from
    // repositoryidentity.CanonicalRepositoryID). Must match demoRepositories'
    // `id` and demoFreshnessWire's keyed repoId ("repository:<repo>",
    // demoFixtures.ts / demoFreshnessFixture.ts) exactly -- #5171's
    // repositorySourceHref() links this value straight to
    // /repositories/:id/source, so a mismatched demo source_key would land
    // on an uncovered demo repository page instead of the matching one.
    // source_display: operator-facing name from the scope payload.
    source_key: `repository:${repo}`,
    source_display: `acme/${repo}`,
    // generation_state (#5138): "stale" demos a retrying row from a
    // superseded generation rendering dimmed with a badge.
    generation_state: generationState,
  };
}

// demoDomainBacklogWire mirrors go/internal/query/aws_materialization_status.go's
// domainBacklogToMap wire shape (issue #5172). pending is derived the same
// way the backend's pendingDomainWork helper computes it (outstanding minus
// in-flight minus retrying, floored at zero) so the fixture stays internally
// consistent with the real read model rather than picking an unrelated
// number.
function demoDomainBacklogWire(
  domain: string,
  outstanding: number,
  inFlight: number,
  retrying: number,
  deadLetter: number,
  failed: number,
  oldestAgeSeconds: number,
): Record<string, unknown> {
  return {
    domain,
    outstanding,
    pending: Math.max(0, outstanding - inFlight - retrying),
    in_flight: inFlight,
    blocked: 0,
    retrying,
    dead_letter: deadLetter,
    failed,
    oldest_age: oldestAgeSeconds,
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
    // domain_backlogs (#5172 cold-review P2-2): kept coherent with the
    // live_activity rows below and the queue totals above rather than a
    // separate, contradictory picture -- checkout-service carries the
    // in-flight running row (wi-demo-1), the stale retrying row (wi-demo-3,
    // oldest_age matches its age_seconds), and the queue's one dead-letter
    // item; payments-api carries the claimed row (wi-demo-2). Outstanding
    // sums to 16, matching queue.outstanding, and dead_letter sums to 1,
    // matching queue.dead_letter, so a demo session never shows a busy board
    // next to an empty "Top domain backlogs" panel.
    domain_backlogs: [
      demoDomainBacklogWire("repository:checkout-service", 10, 1, 1, 1, 0, 360),
      demoDomainBacklogWire("repository:payments-api", 6, 1, 0, 0, 0, 30),
    ],
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
        "stale",
      ),
    ],
    truncated: false,
    limit: 50,
  };
}
