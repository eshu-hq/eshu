// api/demoCloudFixtures.ts
// Cloud inventory, runtime-drift, and IaC (unmanaged resource / import
// candidate / management explanation) demo fixtures. Split out of
// demoFixtures.ts and dynamically imported from demoClient.ts's fetcher
// (issue #5139) so this surface's payload weight only lands in a session
// that actually visits the cloud/IaC pages. The scope-guard predicates
// (isDemoCloudRuntimeRequest/isDemoAwsRuntimeRequest) travel with the
// fixtures they guard for the same reason.
import { demoDefaults } from "./demoFixtures";
import { field, objectBody, optionalFieldMatches } from "./demoRequestMatch";

const demoAccountId = "demo-account";
const demoRegion = "us-east-1";
const demoScopeId = "aws:demo-account";

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

const unmanagedFindings = [
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

const importCandidates = [
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

// Scope-guard rejection messages, colocated with the guard they explain
// (issue #5139) so their text lives in this lazily loaded chunk rather than
// the eager main bundle. driftScopeMessage covers both /cloud and /aws
// runtime-drift endpoints, which share identical demo-corpus scope text.
export const driftScopeMessage = "Demo drift fixtures only cover aws:demo-account in us-east-1.";
export const unmanagedResourcesScopeMessage =
  "Demo unmanaged-resource fixtures only cover aws:demo-account in us-east-1.";
export const importPlanScopeMessage =
  "Demo import-plan fixtures only cover aws:demo-account in us-east-1.";
export const managementStatusScopeMessage =
  "Demo management-status fixtures only cover aws:demo-account in us-east-1.";

export function isDemoCloudRuntimeRequest(body: unknown): boolean {
  const record = objectBody(body);
  return (
    record !== null &&
    field(record, "account_id") === demoDefaults.cloudDrift.accountId &&
    field(record, "provider") === demoDefaults.cloudDrift.provider &&
    field(record, "scope_id") === demoDefaults.cloudDrift.scopeId &&
    optionalFieldMatches(record, "region", demoDefaults.cloudDrift.region)
  );
}

export function isDemoAwsRuntimeRequest(body: unknown): boolean {
  const record = objectBody(body);
  return (
    record !== null &&
    field(record, "account_id") === demoDefaults.cloudDrift.accountId &&
    field(record, "region") === demoDefaults.cloudDrift.region &&
    field(record, "scope_id") === demoDefaults.cloudDrift.scopeId &&
    optionalFieldMatches(record, "provider", demoDefaults.cloudDrift.provider)
  );
}

export function filterCloudResources(params: URLSearchParams): readonly Record<string, string>[] {
  return cloudResources.filter(
    (resource) =>
      matchesParam(params, "provider", resource.provider) &&
      matchesParam(params, "resource_type", resource.resource_type) &&
      matchesParam(params, "region", resource.region) &&
      matchesParam(params, "account_id", resource.account_id),
  );
}

function driftList(story: string): Record<string, unknown> {
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

// Response builders below own their narrative "story" text and envelope
// shape, not just the underlying arrays, so that text stays in this lazily
// loaded chunk instead of the demoClient.ts dispatcher (issue #5139).
export function cloudDriftResponse(): Record<string, unknown> {
  return driftList("Provider-neutral demo drift for checkout cloud resources.");
}

export function awsDriftResponse(): Record<string, unknown> {
  return driftList("AWS demo drift for unmanaged checkout identity and load balancer resources.");
}

export function unmanagedResourcesResponse(): Record<string, unknown> {
  return {
    findings: unmanagedFindings,
    limit: 50,
    offset: 0,
    story: "Demo fixture shows one unmanaged IAM role with read-only import context.",
    total_findings_count: unmanagedFindings.length,
    truncated: false,
  };
}

export function importPlanCandidatesResponse(): Record<string, unknown> {
  return {
    candidates: importCandidates,
    limit: 50,
    offset: 0,
    ready_count: 1,
    refused_count: 0,
    story: "Demo fixture import packet is ready for review only.",
    total_findings_count: importCandidates.length,
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
