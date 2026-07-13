// api/demoCicdFixtures.ts
// CI/CD run-correlation demo fixtures (count/inventory/list). Split out of
// demoFixtures.ts and dynamically imported from demoClient.ts's fetcher
// (issue #5139) so this surface's payload weight only lands in a session
// that actually queries the CI/CD run-correlations page. The isDemoCicdQuery
// scope guard travels with the fixtures it guards for the same reason.
import { demoDefaults, demoDigest } from "./demoFixtures";
import { optionalParamMatches } from "./demoRequestMatch";

// Scope-guard rejection message, colocated with the guard it explains (issue
// #5139) and shared by all three run-correlation endpoints (count, inventory,
// list), which return identical demo-corpus scope text.
export const cicdScopeMessage =
  "Demo CI/CD fixtures only cover repository:checkout-service in prod.";

export function isDemoCicdQuery(params: URLSearchParams): boolean {
  return (
    params.get("repository_id") === demoDefaults.cicd.repositoryId &&
    params.get("environment") === demoDefaults.cicd.environment &&
    optionalParamMatches(params, "provider", "github_actions") &&
    optionalParamMatches(params, "outcome", "exact") &&
    optionalParamMatches(params, "provider_run_id", "1234") &&
    optionalParamMatches(params, "run_id", "1234") &&
    optionalParamMatches(params, "artifact_digest", demoDigest) &&
    optionalParamMatches(params, "commit_sha", "abc123def456") &&
    optionalParamMatches(params, "image_ref", `registry.example/sample/checkout@${demoDigest}`)
  );
}

export const cicdCount = {
  by_environment: { prod: 2, staging: 1 },
  by_outcome: { exact: 2, provenance_only: 1 },
  by_provider: { github_actions: 3 },
  scope: { repository_id: "repository:checkout-service" },
  total_correlations: 3,
} as const;

export const cicdInventory = {
  buckets: [
    { count: 2, dimension: "outcome", value: "exact" },
    { count: 1, dimension: "outcome", value: "provenance_only" },
  ],
  count: 2,
  group_by: "outcome",
  limit: 25,
  offset: 0,
  scope: { repository_id: "repository:checkout-service" },
  truncated: false,
} as const;

export const cicdList = {
  correlations: [
    {
      artifact_digest: demoDigest,
      canonical_target: "checkout-service",
      canonical_writes: 4,
      commit_sha: "abc123def456",
      correlation_id: "cicd:checkout:1234",
      correlation_kind: "workflow_to_image_to_workload",
      environment: "prod",
      evidence_fact_ids: ["fact:workflow:checkout", "fact:image:checkout"],
      image_ref: `registry.example/sample/checkout@${demoDigest}`,
      outcome: "exact",
      provider: "github_actions",
      provenance_only: false,
      reason: "workflow artifact digest matched deployed checkout image",
      repository_id: "repository:checkout-service",
      run_attempt: "1",
      run_id: "1234",
    },
  ],
  count: 1,
  evidence_summary: {
    live_run_correlations: {
      count: 1,
      reason: "workflow run linked to image digest",
      state: "present",
      truncated: false,
    },
    missing_evidence: [],
    reason: "Demo fixture has workflow, artifact, and deployment evidence.",
    run_artifact_evidence: {
      ambiguous_count: 0,
      artifact_digest_count: 1,
      count: 1,
      image_ref_count: 1,
      reason: "digest match",
      state: "present",
      truncated: false,
    },
    static_workflow_artifacts: {
      ambiguous_count: 0,
      count: 1,
      evidence_class: "github_actions",
      image_ref_count: 1,
      paths: [".github/workflows/deploy.yml"],
      reason: "workflow publishes OCI image",
      state: "present",
      truncated: false,
      unresolved_count: 0,
    },
  },
  limit: 25,
  truncated: false,
} as const;
