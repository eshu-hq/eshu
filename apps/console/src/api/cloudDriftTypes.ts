import type { FreshnessState, TruthLevel } from "./envelope";

export type CloudDriftProvider = "aws" | "gcp" | "azure" | "";

export interface CloudDriftQuery {
  readonly accountId?: string;
  readonly arn?: string;
  readonly findingKinds?: readonly string[];
  readonly limit: number;
  readonly offset: number;
  readonly projectId?: string;
  readonly provider?: CloudDriftProvider;
  readonly region?: string;
  readonly scopeId?: string;
  readonly subscriptionId?: string;
}

export interface CloudDriftExactQuery {
  readonly accountId?: string;
  readonly arn: string;
  readonly findingKinds?: readonly string[];
  readonly region?: string;
  readonly scopeId?: string;
}

export interface CloudDriftTruth {
  readonly capability: string;
  readonly freshness: FreshnessState;
  readonly level: TruthLevel;
  readonly profile: string;
}

export interface CloudDriftSafetyGate {
  readonly auditExpectation: string;
  readonly outcome: string;
  readonly readOnly: boolean;
  readonly redactions: readonly string[];
  readonly refusedActions: readonly string[];
  readonly reviewRequired: boolean;
  readonly warnings: readonly string[];
}

export interface CloudRuntimeDriftFinding {
  readonly canonicalResourceId: string;
  readonly confidence: number;
  readonly findingKind: string;
  readonly generationId: string;
  readonly id: string;
  readonly managementStatus: string;
  readonly matchedTerraformStateAddress: string;
  readonly missingEvidence: readonly string[];
  readonly provider: string;
  readonly recommendedAction: string;
  readonly safetyOutcome: string;
  readonly scopeId: string;
  readonly sourceState: string;
}

export interface AwsRuntimeDriftFinding {
  readonly accountId: string;
  readonly arn: string;
  readonly confidence: number;
  readonly findingKind: string;
  readonly id: string;
  readonly managementStatus: string;
  readonly missingEvidence: readonly string[];
  readonly outcome: string;
  readonly promotionOutcome: string;
  readonly promotionReason: string;
  readonly provider: string;
  readonly region: string;
  readonly safetyOutcome: string;
}

export interface UnmanagedCloudResourceFinding {
  readonly accountId: string;
  readonly arn: string;
  readonly confidence: number;
  readonly findingKind: string;
  readonly id: string;
  readonly managementStatus: string;
  readonly missingEvidence: readonly string[];
  readonly provider: string;
  readonly recommendedAction: string;
  readonly region: string;
  readonly resourceId: string;
  readonly resourceType: string;
  readonly safetyOutcome: string;
  readonly warningFlags: readonly string[];
}

export interface TerraformImportPlanCandidate {
  readonly accountId: string;
  readonly arn: string;
  readonly cloudResourceType: string;
  readonly destinationHint: string;
  readonly findingId: string;
  readonly id: string;
  readonly importId: string;
  readonly provider: string;
  readonly refusalReasons: readonly string[];
  readonly region: string;
  readonly safetyOutcome: string;
  // Status is the backend-reported string union plus an open string fallback
  // for forward compatibility; narrowed unions ("ready" / "refused") would be
  // redundant because `string` already covers every literal.
  readonly status: string;
  readonly suggestedResourceAddress: string;
  readonly terraformResourceType: string;
  readonly warnings: readonly string[];
}

export interface IaCManagementEvidenceRow {
  readonly evidenceType: string;
  readonly id: string;
  readonly key: string;
  readonly value: string;
}

export interface IaCManagementEvidenceGroup {
  readonly count: number;
  readonly evidence: readonly IaCManagementEvidenceRow[];
  readonly layer: string;
}

export interface IaCManagementExplanation {
  readonly arn: string;
  readonly evidenceGroups: readonly IaCManagementEvidenceGroup[];
  readonly safetyOutcome: string;
  readonly story: string;
}

export interface CloudRuntimeDriftPage {
  readonly analysisStatus: string;
  readonly findings: readonly CloudRuntimeDriftFinding[];
  readonly limit: number;
  readonly nextOffset: number | null;
  readonly offset: number;
  readonly story: string;
  readonly totalFindingsCount: number;
  readonly truncated: boolean;
  readonly truth: CloudDriftTruth;
}

export interface AwsRuntimeDriftPage {
  readonly findings: readonly AwsRuntimeDriftFinding[];
  readonly limit: number;
  readonly nextOffset: number | null;
  readonly offset: number;
  readonly story: string;
  readonly totalFindingsCount: number;
  readonly truncated: boolean;
  readonly truth: CloudDriftTruth;
}

export interface UnmanagedCloudResourcesPage {
  readonly findings: readonly UnmanagedCloudResourceFinding[];
  readonly limit: number;
  readonly nextOffset: number | null;
  readonly offset: number;
  readonly story: string;
  readonly totalFindingsCount: number;
  readonly truncated: boolean;
  readonly truth: CloudDriftTruth;
}

export interface TerraformImportPlanPage {
  readonly candidates: readonly TerraformImportPlanCandidate[];
  readonly limit: number;
  readonly nextOffset: number | null;
  readonly offset: number;
  readonly readyCount: number;
  readonly refusedCount: number;
  readonly story: string;
  readonly totalFindingsCount: number;
  readonly truncated: boolean;
  readonly truth: CloudDriftTruth;
}

export interface DriftListWire {
  readonly analysis_status?: string;
  readonly drift_findings?: readonly RuntimeDriftFindingWire[];
  readonly limit?: number;
  readonly next_offset?: number | null;
  readonly offset?: number;
  readonly story?: string;
  readonly total_findings_count?: number;
  readonly truncated?: boolean;
}

export interface RuntimeDriftFindingWire {
  readonly account_id?: string;
  readonly arn?: string;
  readonly cloud_resource_uid?: string;
  readonly confidence?: number;
  readonly finding_kind?: string;
  readonly fact_id?: string;
  readonly generation_id?: string;
  readonly id?: string;
  readonly management_status?: string;
  readonly matched_terraform_state_address?: string;
  readonly missing_evidence?: readonly string[];
  readonly outcome?: string;
  readonly promotion_outcome?: string;
  readonly promotion_reason?: string;
  readonly provider?: string;
  readonly recommended_action?: string;
  readonly region?: string;
  readonly safety_gate?: SafetyGateWire;
  readonly scope_id?: string;
  readonly source_state?: string;
}

export interface UnmanagedListWire {
  readonly findings?: readonly UnmanagedFindingWire[];
  readonly limit?: number;
  readonly next_offset?: number | null;
  readonly offset?: number;
  readonly story?: string;
  readonly total_findings_count?: number;
  readonly truncated?: boolean;
}

export interface UnmanagedFindingWire {
  readonly account_id?: string;
  readonly arn?: string;
  readonly confidence?: number;
  readonly finding_kind?: string;
  readonly id?: string;
  readonly management_status?: string;
  readonly missing_evidence?: readonly string[];
  readonly provider?: string;
  readonly recommended_action?: string;
  readonly region?: string;
  readonly resource_id?: string;
  readonly resource_type?: string;
  readonly safety_gate?: SafetyGateWire;
  readonly warning_flags?: readonly string[];
}

export interface ImportPlanWire {
  readonly candidates?: readonly ImportCandidateWire[];
  readonly limit?: number;
  readonly next_offset?: number | null;
  readonly offset?: number;
  readonly ready_count?: number;
  readonly refused_count?: number;
  readonly story?: string;
  readonly total_findings_count?: number;
  readonly truncated?: boolean;
}

export interface ImportCandidateWire {
  readonly account_id?: string;
  readonly arn?: string;
  readonly cloud_resource_type?: string;
  readonly destination_hint?: string;
  readonly finding_id?: string;
  readonly id?: string;
  readonly import_id?: string;
  readonly provider?: string;
  readonly refusal_reasons?: readonly string[];
  readonly region?: string;
  readonly safety_gate?: SafetyGateWire;
  readonly status?: string;
  readonly suggested_resource_address?: string;
  readonly terraform_resource_type?: string;
  readonly warnings?: readonly string[];
}

export interface ExplanationWire {
  readonly arn?: string;
  readonly evidence_groups?: readonly EvidenceGroupWire[];
  readonly safety_gate?: SafetyGateWire;
  readonly story?: string;
}

export interface EvidenceGroupWire {
  readonly count?: number;
  readonly evidence?: readonly EvidenceWire[];
  readonly layer?: string;
}

export interface EvidenceWire {
  readonly evidence_type?: string;
  readonly id?: string;
  readonly key?: string;
  readonly value?: string;
}

export interface SafetyGateWire {
  readonly audit_expectation?: string;
  readonly outcome?: string;
  readonly read_only?: boolean;
  readonly redactions?: readonly string[];
  readonly refused_actions?: readonly string[];
  readonly review_required?: boolean;
  readonly warnings?: readonly string[];
}
