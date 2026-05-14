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
