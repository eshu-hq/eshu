// api/demoFixtures.ts
// Core demo-mode fixture data that MUST be available synchronously: the app
// shell (App.tsx), route table (appRoutes.tsx), and a few pages read
// demoApiBaseUrl/demoDefaults/demoRepositories eagerly at render time, before
// any request is made. Everything else -- the actual response bodies each
// demo endpoint returns -- lives in per-surface modules (demoCloudFixtures.ts,
// demoImpactFixtures.ts, demoCicdFixtures.ts, demoSupplyChainFixtures.ts,
// demoOperationsFixture.ts, demoFreshnessFixture.ts) that demoClient.ts's
// fetcher loads via dynamic import() only when a matching request is made.
// This keeps demo-only response payloads out of the console's tightly
// budgeted main bundle (scripts/console-bundle-budget.mjs) for the common
// case where a demo session never touches most surfaces (issue #5139).
import type { RepoListItem } from "./repoCatalog";

export const demoDigest = "sha256:abc1234567890def";

export const demoApiBaseUrl = "/demo-api/";

export const demoDefaults = {
  cicd: {
    environment: "prod",
    repositoryId: "repository:checkout-service",
  },
  cloudDrift: {
    accountId: "demo-account",
    provider: "aws",
    region: "us-east-1",
    scopeId: "aws:demo-account",
  },
  impact: {
    environment: "prod-us-east-1",
    kind: "service",
    target: "checkout-service",
  },
} as const;

export const demoRepositories: readonly RepoListItem[] = [
  {
    groupKey: "sample",
    groupKind: "product",
    groupReason: "prospect demo fixture",
    groupSource: "demo_fixture",
    groupTruth: "exact",
    id: "repository:checkout-service",
    isDependency: false,
    name: "checkout-service",
    remoteUrl: "https://git.example.test/sample/checkout-service",
    repoSlug: "sample/checkout-service",
  },
  {
    groupKey: "sample",
    groupKind: "product",
    groupReason: "prospect demo fixture",
    groupSource: "demo_fixture",
    groupTruth: "exact",
    id: "repository:payments-api",
    isDependency: false,
    name: "payments-api",
    remoteUrl: "https://git.example.test/sample/payments-api",
    repoSlug: "sample/payments-api",
  },
];

export function demoRepositoriesWire(): readonly Record<string, unknown>[] {
  return demoRepositories.map((repo) => ({
    group_key: repo.groupKey,
    group_kind: repo.groupKind,
    group_reason: repo.groupReason,
    group_source: repo.groupSource,
    group_truth: repo.groupTruth,
    id: repo.id,
    is_dependency: repo.isDependency,
    name: repo.name,
    remote_url: repo.remoteUrl,
    repo_slug: repo.repoSlug,
  }));
}
