// api/demoImpactFixtures.ts
// Blast-radius, change-surface, and deployment-trace demo fixtures. Split out
// of demoFixtures.ts and dynamically imported from demoClient.ts's fetcher
// (issue #5139) so this surface's payload weight only lands in a session that
// actually queries the impact pages. The isDemoImpactRequest scope guard
// travels with the fixtures it guards for the same reason.
import { demoDefaults, demoDigest } from "./demoFixtures";
import { field, objectBody, optionalFieldMatches } from "./demoRequestMatch";

// Scope-guard rejection messages, colocated with the guard they explain
// (issue #5139). impactScopeMessage covers blast-radius and change-surface,
// which share identical demo-corpus scope text; deployment-chain tracing has
// its own wording.
export const impactScopeMessage = "Demo impact fixtures only cover checkout-service.";
export const deploymentTraceScopeMessage =
  "Demo deployment-trace fixtures only cover checkout-service.";

export function isDemoImpactRequest(body: unknown): boolean {
  const record = objectBody(body);
  if (record === null) {
    return false;
  }
  if (!optionalFieldMatches(record, "environment", demoDefaults.impact.environment)) {
    return false;
  }
  if (!optionalFieldMatches(record, "repo_id", demoDefaults.cicd.repositoryId)) {
    return false;
  }
  const candidates = [
    field(record, "service_name"),
    field(record, "target"),
    field(record, "resource_id"),
    field(record, "query"),
    field(record, "topic"),
  ].filter((value) => value.length > 0);
  return candidates.length > 0 && candidates.every((value) => value === demoDefaults.impact.target);
}

export const blastRadius = {
  affected: [
    {
      claim: "runtime dependency",
      hops: 1,
      repo: "payments-api",
      repo_id: "repository:payments-api",
      risk: "high",
      tier: "tier-1",
    },
    {
      claim: "ledger write path",
      hops: 2,
      repo: "ledger-service",
      repo_id: "repository:ledger-service",
      risk: "medium",
      tier: "tier-1",
    },
  ],
  affected_count: 2,
  limit: 25,
  target: "checkout-service",
  target_type: "repository",
  truncated: false,
} as const;

export const changeSurface = {
  code_surface: {
    coverage: {
      changed_path_count: 2,
      limit: 25,
      query_shape: "service_name",
      returned_symbols: 2,
      truncated: false,
    },
    evidence_groups: [
      {
        entity_name: "createCheckout",
        entity_type: "function",
        language: "TypeScript",
        matched_terms: ["checkout"],
        relative_path: "src/checkout.ts",
        source_kind: "code",
      },
    ],
    matched_file_count: 2,
    source_backends: ["content_store"],
    symbol_count: 2,
    topic:
      "checkout-service API routes, deployment, dependencies, consumers, and infrastructure changes",
    touched_symbols: [
      {
        entity_id: "symbol:createCheckout",
        kind: "function",
        language: "TypeScript",
        name: "createCheckout",
        relative_path: "src/checkout.ts",
      },
    ],
    truncated: false,
  },
  coverage: {
    code_symbol_count: 2,
    direct_count: 2,
    limit: 25,
    max_depth: 4,
    query_shape: "service_name",
    transitive_count: 1,
    truncated: false,
  },
  direct_impact: [
    {
      depth: 1,
      environment: "prod-us-east-1",
      id: "svc:payments",
      labels: ["Service"],
      name: "payments-api",
      repo_id: "repository:payments-api",
    },
    {
      depth: 1,
      environment: "prod-us-east-1",
      id: "cloud:frontend-lb",
      labels: ["CloudResource"],
      name: "aws_lb.frontend",
      repo_id: "repository:checkout-service",
    },
  ],
  impact_summary: { direct_count: 2, total_count: 3, transitive_count: 1 },
  recommended_next_calls: [
    { args: { service_name: "checkout-service" }, tool: "trace_deployment_chain" },
  ],
  scope: {
    changed_paths: ["src/checkout.ts", "deploy/checkout.yaml"],
    environment: "prod-us-east-1",
    limit: 25,
    max_depth: 4,
    repo_id: "repository:checkout-service",
    target: "checkout-service",
    target_type: "service",
    topic: "checkout",
  },
  source_backend: "demo_fixture",
  target_resolution: {
    input: "checkout-service",
    selected: {
      depth: 0,
      environment: "prod-us-east-1",
      id: "svc:checkout",
      labels: ["Service"],
      name: "checkout-service",
      repo_id: "repository:checkout-service",
    },
    status: "resolved",
    target_type: "service",
    truncated: false,
  },
  transitive_impact: [
    {
      depth: 2,
      environment: "prod-us-east-1",
      id: "svc:ledger",
      labels: ["Service"],
      name: "ledger-service",
      repo_id: "repository:ledger-service",
    },
  ],
  truncated: false,
} as const;

export const deploymentTrace = {
  cloud_resources: [
    { id: "cloud:frontend-lb", name: "aws_lb.frontend", resource_type: "aws_lb" },
    {
      id: "cloud:checkout-task-role",
      name: "aws_iam_role.checkout_task",
      resource_type: "aws_iam_role",
    },
  ],
  deployment_overview: { environment: "prod-us-east-1", strategy: "rolling" },
  deployment_sources: [
    {
      path: ".github/workflows/deploy.yml",
      relationship_type: "workflow",
      repo_name: "sample/checkout-service",
    },
    {
      path: "deploy/checkout.yaml",
      relationship_type: "kubernetes_manifest",
      repo_name: "sample/checkout-service",
    },
  ],
  image_refs: [`registry.example/sample/checkout@${demoDigest}`],
  k8s_resources: [{ entity_name: "Deployment/checkout", kind: "Deployment" }],
  service_name: "checkout-service",
  story:
    "Demo fixture traces checkout-service from repository workflow to image, workload, and cloud resources.",
  workload_id: "workload:checkout",
} as const;
