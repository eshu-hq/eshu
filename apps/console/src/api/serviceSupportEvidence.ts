/** Service support evidence normalized for the Service Atlas UI. */
export interface ServiceSupportOverview {
  readonly ambiguousCount: number;
  readonly evidence: readonly ServiceSupportEvidence[];
  readonly evidenceCount: number;
  readonly incidentRoutingCount: number;
  readonly missingEvidence: readonly string[];
  readonly truncated: boolean;
  readonly workItemCount: number;
}

/** One bounded support-evidence fact row safe for display without links. */
export interface ServiceSupportEvidence {
  readonly factId: string;
  readonly factKind: string;
  readonly issueType?: string;
  readonly label: string;
  readonly observedAt?: string;
  readonly outcome?: string;
  readonly provider?: string;
  readonly scopeId?: string;
  readonly sourceSystem?: string;
  readonly sourceUrlText?: string;
  readonly status?: string;
}

/** Raw service-story support evidence returned by the Eshu API. */
export interface ServiceSupportRecord {
  readonly ambiguous_count?: number;
  readonly coverage?: {
    readonly truncated?: boolean;
  };
  readonly evidence?: readonly ServiceSupportEvidenceRecord[];
  readonly evidence_count?: number;
  readonly incident_routing_count?: number;
  readonly missing_evidence?: readonly string[];
  readonly work_item_count?: number;
}

/** Raw support evidence fact record embedded under a service story target. */
export interface ServiceSupportEvidenceRecord {
  readonly fact_id?: string;
  readonly fact_kind?: string;
  readonly observed_at?: string;
  readonly payload?: ServiceSupportEvidencePayload;
  readonly scope_id?: string;
  readonly source_system?: string;
}

interface ServiceSupportEvidencePayload {
  readonly issue_type_name?: string;
  readonly outcome?: string;
  readonly provider?: string;
  readonly provider_work_item_id?: string;
  readonly service_id?: string;
  readonly source_class?: string;
  readonly status?: string;
  readonly status_name?: string;
  readonly url_redacted?: string;
  readonly work_item_key?: string;
}

/** Converts API support evidence into the console's display contract. */
export function serviceSupportFromRecord(
  record: ServiceSupportRecord | undefined
): ServiceSupportOverview | undefined {
  if (record === undefined) {
    return undefined;
  }
  const evidence = (record.evidence ?? []).slice(0, 10).map(supportEvidenceRow);
  const missingEvidence = record.missing_evidence ?? [];
  if (evidence.length === 0 && missingEvidence.length === 0 && (record.evidence_count ?? 0) === 0) {
    return undefined;
  }
  return {
    ambiguousCount: record.ambiguous_count ?? 0,
    evidence,
    evidenceCount: record.evidence_count ?? evidence.length,
    incidentRoutingCount: record.incident_routing_count ?? countFamily(evidence, "incident_routing."),
    missingEvidence,
    truncated: record.coverage?.truncated ?? false,
    workItemCount: record.work_item_count ?? countFamily(evidence, "work_item.")
  };
}

function supportEvidenceRow(record: ServiceSupportEvidenceRecord): ServiceSupportEvidence {
  const payload = record.payload ?? {};
  const factKind = nonEmpty(record.fact_kind, "support.evidence");
  return {
    factId: nonEmpty(record.fact_id, factKind),
    factKind,
    issueType: optional(payload.issue_type_name),
    label: supportEvidenceLabel(factKind, payload),
    observedAt: optional(record.observed_at),
    outcome: optional(payload.outcome),
    provider: optional(payload.provider),
    scopeId: optional(record.scope_id),
    sourceSystem: optional(record.source_system),
    sourceUrlText: optional(payload.url_redacted),
    status: optional(payload.status_name, payload.status)
  };
}

function supportEvidenceLabel(factKind: string, payload: ServiceSupportEvidencePayload): string {
  if (factKind.startsWith("work_item.")) {
    return `${providerLabel(payload.provider, "Work item")} ${nonEmpty(
      payload.work_item_key,
      payload.provider_work_item_id,
      "evidence"
    )}`;
  }
  if (factKind.startsWith("incident_routing.")) {
    return `${providerLabel(payload.provider, "PagerDuty")} routing`;
  }
  return factKind.replaceAll("_", " ");
}

function providerLabel(provider: string | undefined, fallback: string): string {
  const normalized = nonEmpty(provider).toLowerCase();
  if (normalized.includes("jira")) {
    return "Jira";
  }
  if (normalized.includes("pagerduty")) {
    return "PagerDuty";
  }
  return fallback;
}

function countFamily(evidence: readonly ServiceSupportEvidence[], prefix: string): number {
  return evidence.filter((row) => row.factKind.startsWith(prefix)).length;
}

function optional(...values: readonly (string | undefined)[]): string | undefined {
  const value = nonEmpty(...values);
  return value.length > 0 ? value : undefined;
}

function nonEmpty(...values: readonly (string | undefined)[]): string {
  for (const value of values) {
    if (value !== undefined && value.trim().length > 0) {
      return value;
    }
  }
  return "";
}
