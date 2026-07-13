// api/demoFreshnessFixture.ts
// Wire-shaped fixture for GET /api/v0/repositories/{id}/freshness (issue
// #5143). Split out of demoFixtures.ts and dynamically imported from
// demoClient.ts's fetcher (only when a /freshness request is actually made)
// rather than statically, because demoClient.ts and demoFixtures.ts are
// imported eagerly from App.tsx: a static import here would add this
// fixture's weight to the console's tightly budgeted main bundle
// (scripts/console-bundle-budget.mjs) even though most demo sessions never
// hit this endpoint.
//
// Covers the three verdicts a demo session can reach: checkout-service is
// "current" (fully drained), payments-api is "building" (projector stage
// still draining), and any other repository id is "unobserved" (a push
// landed but indexing has not started -- the console never fabricates
// progress for a repository outside the demo corpus). Timestamps are
// relative to call time so the "current" copy's relative observed_at reads
// correctly for any session length.
export function demoFreshnessWire(repoId: string): Record<string, unknown> {
  const now = Date.now();
  const ago = (minutes: number): string => new Date(now - minutes * 60_000).toISOString();
  const base = {
    scope_id: `scope:${repoId}`,
    verdict: "unobserved",
    observed_commit: "",
    observed_at: null as string | null,
    generation: null as Record<string, unknown> | null,
    stages: { collected: false, reduced: false, projected: false, materialized: false },
    outstanding_by_stage: [] as readonly Record<string, unknown>[],
    shared_enrichment: { pending: false, pending_domains: [] },
    unobserved_push: null as Record<string, unknown> | null,
    as_of: new Date(now).toISOString(),
    scoped: false,
  };
  if (repoId === "repository:checkout-service") {
    return {
      ...base,
      verdict: "current",
      observed_commit: "d34db33fd34db33fd34db33fd34db33fd34db33f",
      observed_at: ago(2),
      generation: {
        id: "generation:demo-checkout-3",
        status: "active",
        trigger_kind: "webhook",
        is_delta: true,
        activated_at: ago(2),
      },
      stages: { collected: true, reduced: true, projected: true, materialized: true },
    };
  }
  if (repoId === "repository:payments-api") {
    return {
      ...base,
      verdict: "building",
      observed_commit: "cafef00dcafef00dcafef00dcafef00dcafef00d",
      observed_at: ago(1),
      generation: {
        id: "generation:demo-payments-5",
        status: "reducing",
        trigger_kind: "webhook",
        is_delta: true,
        activated_at: ago(1),
      },
      stages: { collected: true, reduced: true, projected: false, materialized: false },
      outstanding_by_stage: [{ stage: "project", status: "running", count: 7 }],
    };
  }
  return {
    ...base,
    unobserved_push: {
      target_sha: "f005ba11f005ba11f005ba11f005ba11f005ba1",
      ref: "refs/heads/main",
      received_at: ago(0.5),
    },
  };
}
