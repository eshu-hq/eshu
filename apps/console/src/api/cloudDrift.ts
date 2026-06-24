// api/cloudDrift.ts
// Typed read-only loaders for the cloud runtime drift and IaC management
// surfaces. These endpoints are bounded POST readbacks; callers must provide a
// scope/account/project/subscription and must preserve safety/refusal posture.

import type { EshuApiClient } from "./client";
import type {
  AwsRuntimeDriftFinding,
  AwsRuntimeDriftPage,
  CloudDriftExactQuery,
  CloudDriftQuery,
  CloudDriftSafetyGate,
  CloudDriftTruth,
  CloudRuntimeDriftFinding,
  CloudRuntimeDriftPage,
  DriftListWire,
  EvidenceGroupWire,
  ExplanationWire,
  IaCManagementEvidenceGroup,
  IaCManagementExplanation,
  ImportCandidateWire,
  ImportPlanWire,
  RuntimeDriftFindingWire,
  SafetyGateWire,
  TerraformImportPlanCandidate,
  TerraformImportPlanPage,
  UnmanagedCloudResourceFinding,
  UnmanagedCloudResourcesPage,
  UnmanagedFindingWire,
  UnmanagedListWire
} from "./cloudDriftTypes";
import type { EshuTruth } from "./envelope";
import { EshuEnvelopeError, unwrapEnvelope } from "./envelope";
export type {
  AwsRuntimeDriftFinding,
  AwsRuntimeDriftPage,
  CloudDriftExactQuery,
  CloudDriftProvider,
  CloudDriftQuery,
  CloudRuntimeDriftPage,
  IaCManagementExplanation,
  TerraformImportPlanCandidate,
  TerraformImportPlanPage,
  UnmanagedCloudResourceFinding,
  UnmanagedCloudResourcesPage
} from "./cloudDriftTypes";

export async function loadCloudRuntimeDriftFindings(
  client: EshuApiClient,
  query: CloudDriftQuery
): Promise<CloudRuntimeDriftPage> {
  const env = await client.post<DriftListWire>("/api/v0/cloud/runtime-drift/findings", cloudRuntimeRequest(query));
  if (env.error) throw new EshuEnvelopeError(env.error);
  const { data, truth } = unwrapEnvelope(env);
  return {
    analysisStatus: str(data.analysis_status),
    findings: (data.drift_findings ?? []).map(cloudRuntimeFindingFromWire),
    limit: numberOr(data.limit, query.limit),
    nextOffset: nullableNumber(data.next_offset),
    offset: numberOr(data.offset, query.offset),
    story: str(data.story),
    totalFindingsCount: numberOr(data.total_findings_count, 0),
    truncated: data.truncated === true,
    truth: truthFromEnvelope(truth)
  };
}

export async function loadAwsRuntimeDriftFindings(
  client: EshuApiClient,
  query: CloudDriftQuery
): Promise<AwsRuntimeDriftPage> {
  const env = await client.post<DriftListWire>("/api/v0/aws/runtime-drift/findings", awsRuntimeRequest(query));
  if (env.error) throw new EshuEnvelopeError(env.error);
  const { data, truth } = unwrapEnvelope(env);
  return {
    findings: (data.drift_findings ?? []).map(awsRuntimeFindingFromWire),
    limit: numberOr(data.limit, query.limit),
    nextOffset: nullableNumber(data.next_offset),
    offset: numberOr(data.offset, query.offset),
    story: str(data.story),
    totalFindingsCount: numberOr(data.total_findings_count, 0),
    truncated: data.truncated === true,
    truth: truthFromEnvelope(truth)
  };
}

export async function loadUnmanagedCloudResources(
  client: EshuApiClient,
  query: CloudDriftQuery
): Promise<UnmanagedCloudResourcesPage> {
  const env = await client.post<UnmanagedListWire>("/api/v0/iac/unmanaged-resources", awsRuntimeRequest(query));
  if (env.error) throw new EshuEnvelopeError(env.error);
  const { data, truth } = unwrapEnvelope(env);
  return {
    findings: (data.findings ?? []).map(unmanagedFindingFromWire),
    limit: numberOr(data.limit, query.limit),
    nextOffset: nullableNumber(data.next_offset),
    offset: numberOr(data.offset, query.offset),
    story: str(data.story),
    totalFindingsCount: numberOr(data.total_findings_count, 0),
    truncated: data.truncated === true,
    truth: truthFromEnvelope(truth)
  };
}

export async function loadTerraformImportPlanCandidates(
  client: EshuApiClient,
  query: CloudDriftQuery
): Promise<TerraformImportPlanPage> {
  const env = await client.post<ImportPlanWire>("/api/v0/iac/terraform-import-plan/candidates", awsRuntimeRequest(query));
  if (env.error) throw new EshuEnvelopeError(env.error);
  const { data, truth } = unwrapEnvelope(env);
  return {
    candidates: (data.candidates ?? []).map(importCandidateFromWire),
    limit: numberOr(data.limit, query.limit),
    nextOffset: nullableNumber(data.next_offset),
    offset: numberOr(data.offset, query.offset),
    readyCount: numberOr(data.ready_count, 0),
    refusedCount: numberOr(data.refused_count, 0),
    story: str(data.story),
    totalFindingsCount: numberOr(data.total_findings_count, 0),
    truncated: data.truncated === true,
    truth: truthFromEnvelope(truth)
  };
}

export async function loadIaCManagementExplanation(
  client: EshuApiClient,
  query: CloudDriftExactQuery
): Promise<IaCManagementExplanation> {
  const env = await client.post<ExplanationWire>("/api/v0/iac/management-status/explain", {
    account_id: clean(query.accountId),
    arn: query.arn,
    finding_kinds: query.findingKinds,
    region: clean(query.region),
    scope_id: clean(query.scopeId)
  });
  if (env.error) throw new EshuEnvelopeError(env.error);
  const { data } = unwrapEnvelope(env);
  return {
    arn: str(data.arn) || query.arn,
    evidenceGroups: (data.evidence_groups ?? []).map(evidenceGroupFromWire),
    safetyOutcome: safetyGateFromWire(data.safety_gate).outcome,
    story: str(data.story)
  };
}

function cloudRuntimeRequest(query: CloudDriftQuery): Record<string, unknown> {
  return compactRequest({
    account_id: clean(query.accountId),
    finding_kinds: query.findingKinds,
    limit: query.limit,
    offset: query.offset,
    project_id: clean(query.projectId),
    provider: clean(query.provider),
    scope_id: clean(query.scopeId),
    subscription_id: clean(query.subscriptionId)
  }, ["finding_kinds"]);
}

function awsRuntimeRequest(query: CloudDriftQuery): Record<string, unknown> {
  return compactRequest({
    account_id: clean(query.accountId),
    arn: clean(query.arn),
    finding_kinds: query.findingKinds,
    limit: query.limit,
    offset: query.offset,
    region: clean(query.region),
    scope_id: clean(query.scopeId)
  }, ["finding_kinds"]);
}

function cloudRuntimeFindingFromWire(wire: RuntimeDriftFindingWire): CloudRuntimeDriftFinding {
  const gate = safetyGateFromWire(wire.safety_gate);
  return {
    canonicalResourceId: str(wire.cloud_resource_uid),
    confidence: numberOr(wire.confidence, 0),
    findingKind: str(wire.finding_kind),
    generationId: str(wire.generation_id),
    id: str(wire.fact_id),
    managementStatus: str(wire.management_status),
    matchedTerraformStateAddress: str(wire.matched_terraform_state_address),
    missingEvidence: stringList(wire.missing_evidence),
    provider: str(wire.provider),
    recommendedAction: str(wire.recommended_action),
    safetyOutcome: gate.outcome,
    scopeId: str(wire.scope_id),
    sourceState: str(wire.source_state)
  };
}

function awsRuntimeFindingFromWire(wire: RuntimeDriftFindingWire): AwsRuntimeDriftFinding {
  const gate = safetyGateFromWire(wire.safety_gate);
  return {
    accountId: str(wire.account_id),
    arn: str(wire.arn),
    confidence: numberOr(wire.confidence, 0),
    findingKind: str(wire.finding_kind),
    id: str(wire.id),
    managementStatus: str(wire.management_status),
    missingEvidence: stringList(wire.missing_evidence),
    outcome: str(wire.outcome),
    promotionOutcome: str(wire.promotion_outcome),
    promotionReason: str(wire.promotion_reason),
    provider: str(wire.provider),
    region: str(wire.region),
    safetyOutcome: gate.outcome
  };
}

function unmanagedFindingFromWire(wire: UnmanagedFindingWire): UnmanagedCloudResourceFinding {
  const gate = safetyGateFromWire(wire.safety_gate);
  return {
    accountId: str(wire.account_id),
    arn: str(wire.arn),
    confidence: numberOr(wire.confidence, 0),
    findingKind: str(wire.finding_kind),
    id: str(wire.id),
    managementStatus: str(wire.management_status),
    missingEvidence: stringList(wire.missing_evidence),
    provider: str(wire.provider),
    recommendedAction: str(wire.recommended_action),
    region: str(wire.region),
    resourceId: str(wire.resource_id),
    resourceType: str(wire.resource_type),
    safetyOutcome: gate.outcome,
    warningFlags: stringList(wire.warning_flags)
  };
}

function importCandidateFromWire(wire: ImportCandidateWire): TerraformImportPlanCandidate {
  const gate = safetyGateFromWire(wire.safety_gate);
  return {
    accountId: str(wire.account_id),
    arn: str(wire.arn),
    cloudResourceType: str(wire.cloud_resource_type),
    destinationHint: str(wire.destination_hint),
    findingId: str(wire.finding_id),
    id: str(wire.id),
    importId: str(wire.import_id),
    provider: str(wire.provider),
    refusalReasons: stringList(wire.refusal_reasons),
    region: str(wire.region),
    safetyOutcome: gate.outcome,
    status: str(wire.status),
    suggestedResourceAddress: str(wire.suggested_resource_address),
    terraformResourceType: str(wire.terraform_resource_type),
    warnings: stringList(wire.warnings)
  };
}

function evidenceGroupFromWire(wire: EvidenceGroupWire): IaCManagementEvidenceGroup {
  return {
    count: numberOr(wire.count, 0),
    evidence: (wire.evidence ?? []).map((item) => ({
      evidenceType: str(item.evidence_type),
      id: str(item.id),
      key: str(item.key),
      value: str(item.value)
    })),
    layer: str(wire.layer)
  };
}

function safetyGateFromWire(wire: SafetyGateWire | undefined): CloudDriftSafetyGate {
  return {
    auditExpectation: str(wire?.audit_expectation),
    outcome: str(wire?.outcome),
    readOnly: wire?.read_only === true,
    redactions: stringList(wire?.redactions),
    refusedActions: stringList(wire?.refused_actions),
    reviewRequired: wire?.review_required === true,
    warnings: stringList(wire?.warnings)
  };
}

function truthFromEnvelope(truth: EshuTruth): CloudDriftTruth {
  return {
    capability: truth.capability,
    freshness: truth.freshness.state,
    level: truth.level,
    profile: truth.profile
  };
}

function clean(value: string | undefined): string | undefined {
  const trimmed = value?.trim() ?? "";
  return trimmed === "" ? undefined : trimmed;
}

function str(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function numberOr(value: unknown, fallback: number): number {
  return typeof value === "number" && Number.isFinite(value) ? value : fallback;
}

function nullableNumber(value: unknown): number | null {
  return typeof value === "number" && Number.isFinite(value) ? value : null;
}

function stringList(value: readonly string[] | undefined): readonly string[] {
  if (!Array.isArray(value)) return [];
  // Array.isArray narrows to `any` per its signature, so retype to the
  // declared element type before any string-only operations.
  const list = value as readonly string[];
  return list.filter((item) => item.trim() !== "");
}

function compactRequest(
  input: Record<string, unknown>,
  keepUndefined: readonly string[] = []
): Record<string, unknown> {
  const keep = new Set(keepUndefined);
  const out: Record<string, unknown> = {};
  for (const [key, value] of Object.entries(input)) {
    if (value !== undefined || keep.has(key)) {
      out[key] = value;
    }
  }
  return out;
}
