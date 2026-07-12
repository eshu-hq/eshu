import type { RepoListItem } from "./repoCatalog";

const demoAccountId = "demo-account";
const demoRegion = "us-east-1";
const demoScopeId = "aws:demo-account";
export const demoDigest = "sha256:abc1234567890def";

export const demoApiBaseUrl = "/demo-api/";

export const demoDefaults = {
  cicd: {
    environment: "prod",
    repositoryId: "repository:checkout-service",
  },
  cloudDrift: {
    accountId: demoAccountId,
    provider: "aws",
    region: demoRegion,
    scopeId: demoScopeId,
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

const cloudResources = [
  cloudResource("aws_lb.frontend", "aws_lb", "frontend", "active"),
  cloudResource("aws_iam_role.checkout_task", "aws_iam_role", "checkout-task", "unmanaged"),
  cloudResource("aws_s3_bucket.assets", "aws_s3_bucket", "checkout-assets", "active"),
  cloudResource("aws_security_group.checkout", "aws_security_group", "checkout-sg", "active"),
] as const;

export const cloudInventory = cloudResources.map((resource) => ({
  cloud_resource_uid: resource.id,
  evidence: {
    applied: resource.state !== "unmanaged",
    declared: resource.name !== "aws_iam_role.checkout_task",
    observed: true,
  },
  generation_id: "generation:demo-cloud",
  management_origin: resource.state === "unmanaged" ? "observed_only" : "terraform",
  provider: resource.provider,
  resource_type: resource.resource_type,
  scope_id: demoScopeId,
  source_state: resource.state === "unmanaged" ? "observed_only" : "exact",
  tag_value_fingerprints: { service: "checkout-service" },
}));

const safetyGate = {
  audit_expectation: "read_only",
  outcome: "review_required",
  read_only: true,
  redactions: [],
  refused_actions: [],
  review_required: true,
  warnings: ["demo_fixture"],
} as const;

export const unmanagedFindings = [
  {
    account_id: demoAccountId,
    arn: "arn:aws:iam::123456789012:role/checkout-task",
    confidence: 0.94,
    finding_kind: "observed_without_declaration",
    id: "drift:checkout-task-role",
    management_status: "unmanaged",
    missing_evidence: ["terraform_declaration"],
    provider: "aws",
    recommended_action: "review_import_candidate",
    region: demoRegion,
    resource_id: "checkout-task-role",
    resource_type: "aws_iam_role",
    safety_gate: safetyGate,
    warning_flags: ["least_privilege_review_required"],
  },
] as const;

export const importCandidates = [
  {
    account_id: demoAccountId,
    arn: "arn:aws:iam::123456789012:role/checkout-task",
    cloud_resource_type: "aws_iam_role",
    destination_hint: "modules/checkout/iam.tf",
    finding_id: "drift:checkout-task-role",
    id: "import:checkout-task-role",
    import_id: "checkout-task",
    provider: "aws",
    refusal_reasons: [],
    region: demoRegion,
    safety_gate: safetyGate,
    status: "ready",
    suggested_resource_address: "module.checkout.aws_iam_role.checkout_task",
    terraform_resource_type: "aws_iam_role",
    warnings: ["manual_review_required"],
  },
] as const;

export const managementExplanation = {
  arn: "arn:aws:iam::123456789012:role/checkout-task",
  evidence_groups: [
    {
      count: 2,
      evidence: [
        {
          evidence_type: "cloud_observation",
          id: "obs:checkout-task",
          key: "provider",
          value: "aws",
        },
        {
          evidence_type: "iac_search",
          id: "iac:checkout-module",
          key: "matched_module",
          value: "checkout",
        },
      ],
      layer: "identity",
    },
  ],
  safety_gate: safetyGate,
  story:
    "Observed IAM role has runtime evidence but no matching Terraform declaration in the demo corpus.",
} as const;

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

export const imageList = {
  count: 1,
  images: [
    {
      artifact_type: "",
      config_digest: "sha256:cfg9876543210",
      digest: demoDigest,
      id: `oci-image://registry.example/sample/checkout@${demoDigest}`,
      media_type: "application/vnd.oci.image.manifest.v1+json",
      name: "checkout",
      registry: "registry.example",
      repository: "sample/checkout",
      repository_id: "oci-registry://registry.example/sample/checkout",
      size_bytes: 28475610,
      source_system: "oci_registry",
      tag: "1.4.2",
    },
  ],
  limit: 50,
  offset: 0,
  truncated: false,
} as const;

export const sbomCount = {
  by_artifact_kind: { attestation: 1, sbom: 2 },
  by_attachment_status: { attached_unverified: 1, attached_verified: 2 },
  total_attachments: 3,
} as const;

export const sbomInventory = {
  buckets: [{ count: 3, dimension: "subject_digest", value: demoDigest }],
  group_by: "subject_digest",
  truncated: false,
} as const;

export const sbomAttachments = {
  attachments: [
    {
      artifact_kind: "sbom",
      attachment_id: "sbom:checkout:1",
      attachment_scope: "image",
      attachment_status: "attached_verified",
      component_count: 2,
      component_evidence: [
        {
          component_id: "pkg:npm/sample-lib@1.0.0",
          name: "sample-lib",
          purl: "pkg:npm/sample-lib@1.0.0",
          version: "1.0.0",
        },
        {
          component_id: "pkg:npm/left-pad@1.3.0",
          name: "left-pad",
          purl: "pkg:npm/left-pad@1.3.0",
          version: "1.3.0",
        },
      ],
      document_id: "doc:checkout-sbom",
      format: "spdx",
      missing_evidence: [],
      reason: "image digest matched demo checkout deployment evidence",
      repository_ids: ["repository:checkout-service"],
      service_ids: ["checkout-service"],
      source_confidence: "high",
      source_freshness: "active",
      spec_version: "2.3",
      subject_digest: demoDigest,
      verification_status: "verified",
      warning_summaries: [],
      workload_ids: ["workload:checkout"],
    },
  ],
  truncated: false,
} as const;

export const dependencyList = {
  dependencies: [
    {
      anchor_package: "sample-lib",
      anchor_package_id: "pkg:npm/sample-lib",
      declaring_version: "1.0.0",
      dependency_range: "^1.3.0",
      dependency_type: "runtime",
      direction: "forward",
      edge_id: "dep:sample-lib:left-pad",
      optional: false,
      related_ecosystem: "npm",
      related_package: "left-pad",
      related_package_id: "pkg:npm/left-pad",
    },
  ],
  direction: "forward",
  truncated: false,
} as const;

export function filterCloudResources(params: URLSearchParams): readonly Record<string, string>[] {
  return cloudResources.filter(
    (resource) =>
      matchesParam(params, "provider", resource.provider) &&
      matchesParam(params, "resource_type", resource.resource_type) &&
      matchesParam(params, "region", resource.region) &&
      matchesParam(params, "account_id", resource.account_id),
  );
}

export function driftList(story: string): Record<string, unknown> {
  return {
    analysis_status: "complete",
    drift_findings: unmanagedFindings.map((finding) => ({
      ...finding,
      cloud_resource_uid: "cloud:aws:demo-account:us-east-1:aws-iam-role-checkout-task",
      fact_id: finding.id,
      generation_id: "generation:demo-cloud",
      matched_terraform_state_address: "",
      outcome: "needs_review",
      promotion_outcome: "accepted_for_review",
      promotion_reason: "observed runtime identity lacks declaration",
      recommended_action: "review_import_candidate",
      scope_id: demoScopeId,
      source_state: "observed_only",
    })),
    limit: 50,
    offset: 0,
    story,
    total_findings_count: unmanagedFindings.length,
    truncated: false,
  };
}

function cloudResource(
  name: string,
  resourceType: string,
  serviceName: string,
  state: string,
): Record<string, string> {
  const slug = name.replace(/[^a-zA-Z0-9]+/g, "-");
  return {
    account_id: demoAccountId,
    arn: `arn:aws:demo:${demoRegion}:123456789012:${slug}`,
    id: `cloud:aws:${demoAccountId}:${demoRegion}:${slug}`,
    name,
    provider: "aws",
    region: demoRegion,
    resource_type: resourceType,
    service_name: serviceName,
    state,
  };
}

function matchesParam(params: URLSearchParams, name: string, value: string): boolean {
  const expected = params.get(name);
  return expected === null || expected === "" || expected === value;
}

// operationsBoardWire is the wire-shaped fixture for GET
// /api/v0/status/operations (issue #5137). Timestamps are computed relative
// to call time (rather than baked in) so collector heartbeat freshness
// (fresh/lagging/stale) reads correctly however long a demo session runs.
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
