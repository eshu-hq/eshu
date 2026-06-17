import { EshuApiHttpError, type EshuApiClient } from "./client";
import type { EshuError, EshuTruth } from "./envelope";

// ServiceReportResult is the source-backed payload behind the report-mode page.
// On any API failure it carries the error with an empty investigation, so the
// page renders an explicit failure state instead of stale or invented findings.
export interface ServiceReportResult {
  readonly contextPath: string;
  readonly error: EshuError | null;
  readonly investigation: ServiceInvestigation;
  readonly serviceName: string;
  readonly storyPath: string;
  readonly truth: EshuTruth | null;
}

// loadServiceInvestigation fetches the bounded service investigation packet and
// normalizes it. It is read-only and performs no synthesis; a non-2xx envelope
// returns the error with `emptyServiceInvestigation`.
export async function loadServiceInvestigation(
  client: EshuApiClient,
  serviceName: string
): Promise<ServiceReportResult> {
  const trimmed = serviceName.trim();
  try {
    const env = await client.get<ServiceInvestigationResponse>(
      `/api/v0/investigations/services/${encodeURIComponent(trimmed)}`
    );
    if (env.error !== null || env.data === null) {
      return reportError(trimmed, env.error ?? requestFailedError());
    }
    return {
      contextPath: env.data.service_context_path ?? "",
      error: null,
      investigation: normalizeServiceInvestigation(env.data),
      serviceName: trimmed,
      storyPath: env.data.service_story_path ?? "",
      truth: env.truth
    };
  } catch (error) {
    // The client throws EshuApiHttpError on non-2xx (404/400/409/5xx) and on
    // timeouts. Convert it to an explicit error result so the page renders the
    // failure state instead of leaving a previously loaded report on screen.
    return reportError(trimmed, eshuErrorFromThrown(error));
  }
}

function reportError(serviceName: string, error: EshuError): ServiceReportResult {
  return {
    contextPath: "",
    error,
    investigation: emptyServiceInvestigation,
    serviceName,
    storyPath: "",
    truth: null
  };
}

function eshuErrorFromThrown(error: unknown): EshuError {
  if (error instanceof EshuApiHttpError) {
    return error.error ?? { code: `http_${error.status}`, message: error.message };
  }
  return requestFailedError(error);
}

function requestFailedError(error?: unknown): EshuError {
  return {
    code: "request_failed",
    message: error instanceof Error ? error.message : "service investigation request failed"
  };
}

export interface ServiceInvestigation {
  readonly coverage: {
    readonly reason: string;
    readonly repositoryCount: number;
    readonly repositoriesWithEvidence: number;
    readonly state: string;
    readonly truncated: boolean;
  };
  readonly evidenceFamilies: readonly string[];
  readonly findings: readonly ServiceInvestigationFinding[];
  readonly nextCalls: readonly ServiceInvestigationNextCall[];
  readonly repositories: readonly ServiceInvestigationRepository[];
}

export interface ServiceInvestigationFinding {
  readonly family: string;
  readonly path: string;
  readonly summary: string;
}

export interface ServiceInvestigationNextCall {
  readonly arguments: Record<string, unknown>;
  readonly reason: string;
  readonly tool: string;
}

export interface ServiceInvestigationRepository {
  readonly evidenceFamilies: readonly string[];
  readonly name: string;
  readonly roles: readonly string[];
}

export interface ServiceInvestigationResponse {
  readonly coverage_summary?: {
    readonly reason?: string;
    readonly repositories_with_evidence_count?: number;
    readonly repository_count?: number;
    readonly state?: string;
    readonly truncated?: boolean;
  };
  readonly evidence_families_found?: readonly string[];
  readonly investigation_findings?: readonly InvestigationFindingRecord[];
  readonly recommended_next_calls?: readonly InvestigationNextCallRecord[];
  readonly repositories_considered?: readonly InvestigationRepositoryRecord[];
  readonly repositories_with_evidence?: readonly InvestigationRepositoryRecord[];
  readonly service_context_path?: string;
  readonly service_story_path?: string;
}

interface InvestigationFindingRecord {
  readonly evidence_path?: string;
  readonly family?: string;
  readonly summary?: string;
}

interface InvestigationNextCallRecord {
  readonly arguments?: Record<string, unknown>;
  readonly reason?: string;
  readonly tool?: string;
}

interface InvestigationRepositoryRecord {
  readonly evidence_families?: readonly string[];
  readonly repo_name?: string;
  readonly roles?: readonly string[];
}

export const emptyServiceInvestigation: ServiceInvestigation = {
  coverage: {
    reason: "Coverage has not been reported for this service.",
    repositoryCount: 0,
    repositoriesWithEvidence: 0,
    state: "unknown",
    truncated: false
  },
  evidenceFamilies: [],
  findings: [],
  nextCalls: [],
  repositories: []
};

export function normalizeServiceInvestigation(
  response: ServiceInvestigationResponse | undefined
): ServiceInvestigation {
  if (response === undefined) {
    return emptyServiceInvestigation;
  }
  const coverage = response.coverage_summary ?? {};
  const repositories = repositoryRows(
    response.repositories_with_evidence ?? response.repositories_considered ?? []
  );
  return {
    coverage: {
      reason: nonEmpty(coverage.reason, emptyServiceInvestigation.coverage.reason),
      repositoryCount: coverage.repository_count ?? repositories.length,
      repositoriesWithEvidence: coverage.repositories_with_evidence_count ?? repositories.length,
      state: nonEmpty(coverage.state, "unknown"),
      truncated: coverage.truncated ?? false
    },
    evidenceFamilies: response.evidence_families_found ?? [],
    findings: (response.investigation_findings ?? []).map((finding) => ({
      family: nonEmpty(finding.family, "evidence"),
      path: nonEmpty(finding.evidence_path),
      summary: nonEmpty(finding.summary, "Evidence observed.")
    })),
    nextCalls: (response.recommended_next_calls ?? []).map((call) => ({
      arguments: call.arguments ?? {},
      reason: nonEmpty(call.reason, "Use this call for drilldown evidence."),
      tool: nonEmpty(call.tool, "tool")
    })),
    repositories
  };
}

function repositoryRows(
  repositories: readonly InvestigationRepositoryRecord[]
): readonly ServiceInvestigationRepository[] {
  return repositories.map((repository) => ({
    evidenceFamilies: repository.evidence_families ?? [],
    name: nonEmpty(repository.repo_name, "repository"),
    roles: repository.roles ?? []
  }));
}

function nonEmpty(...values: readonly (string | undefined)[]): string {
  for (const value of values) {
    if (value !== undefined && value.trim().length > 0) {
      return value;
    }
  }
  return "";
}
