import type { ServiceInvestigation } from "../api/serviceInvestigation";

export function ServiceInvestigationPanel({
  investigation
}: {
  readonly investigation: ServiceInvestigation;
}): React.JSX.Element | null {
  if (!hasInvestigation(investigation)) {
    return null;
  }
  const coverageDetail = `${investigation.coverage.repositoriesWithEvidence} with evidence of ${investigation.coverage.repositoryCount} checked`;
  return (
    <section aria-label="Investigation coverage" className="service-investigation">
      <div className="service-investigation-summary">
        <div>
          <span className="entity-kind">Investigation</span>
          <h2>Investigation coverage</h2>
          <p>{coverageNarrative(investigation)}</p>
        </div>
        <dl>
          <div>
            <dt>Coverage</dt>
            <dd>{coverageLabel(investigation.coverage.state)}</dd>
          </div>
          <div>
            <dt>Repositories</dt>
            <dd>{coverageDetail}</dd>
          </div>
        </dl>
      </div>
      <div className="service-investigation-grid">
        <EvidenceFamilies families={investigation.evidenceFamilies} />
        <InvestigationFindings findings={investigation.findings} />
        <RecommendedCalls calls={investigation.nextCalls} />
        <RepositoryScope repositories={investigation.repositories} />
      </div>
    </section>
  );
}

function EvidenceFamilies({
  families
}: {
  readonly families: readonly string[];
}): React.JSX.Element {
  return (
    <section>
      <h3>What Eshu checked</h3>
      <div className="service-chip-row">
        {families.slice(0, 8).map((family) => (
          <span key={family}>{humanLabel(family)}</span>
        ))}
      </div>
    </section>
  );
}

function InvestigationFindings({
  findings
}: {
  readonly findings: readonly ServiceInvestigation["findings"][number][];
}): React.JSX.Element {
  return (
    <section>
      <h3>What it found</h3>
      <div className="service-investigation-list">
        {findings.slice(0, 4).map((finding) => (
          <article key={`${finding.family}:${finding.path}`}>
            <strong>{humanLabel(finding.family)}</strong>
            <p>{finding.summary}</p>
            {finding.path.length > 0 ? <small>{finding.path}</small> : null}
          </article>
        ))}
      </div>
    </section>
  );
}

function RecommendedCalls({
  calls
}: {
  readonly calls: readonly ServiceInvestigation["nextCalls"][number][];
}): React.JSX.Element {
  return (
    <section>
      <h3>Next drilldowns</h3>
      <div className="service-investigation-list">
        {dedupeCalls(calls).slice(0, 4).map((call) => (
          <article key={`${call.tool}:${JSON.stringify(call.arguments)}`}>
            <strong>{call.reason}</strong>
            <p>{humanToolLabel(call.tool)}</p>
            <small>{argumentSummary(call.arguments)}</small>
          </article>
        ))}
      </div>
    </section>
  );
}

function RepositoryScope({
  repositories
}: {
  readonly repositories: readonly ServiceInvestigation["repositories"][number][];
}): React.JSX.Element {
  return (
    <section>
      <h3>Repos in scope</h3>
      <div className="service-investigation-list">
        {repositories.slice(0, 4).map((repository) => (
          <article key={repository.name}>
            <strong>{repository.name}</strong>
            <p>{humanList(repository.roles) || "Evidence repository"}</p>
            <small>{humanList(repository.evidenceFamilies)}</small>
          </article>
        ))}
      </div>
    </section>
  );
}

function argumentSummary(argumentsValue: Record<string, unknown>): string {
  const summary = Object.entries(argumentsValue)
    .map(([key, value]) => `${key}: ${String(value)}`)
    .join(", ");
  return summary.length > 0 ? summary : "No extra arguments";
}

function dedupeCalls(
  calls: readonly ServiceInvestigation["nextCalls"][number][]
): readonly ServiceInvestigation["nextCalls"][number][] {
  const seen = new Set<string>();
  return calls.filter((call) => {
    const key = `${call.tool}:${JSON.stringify(call.arguments)}`;
    if (seen.has(key)) {
      return false;
    }
    seen.add(key);
    return true;
  });
}

function hasInvestigation(investigation: ServiceInvestigation): boolean {
  return investigation.coverage.repositoryCount > 0 ||
    investigation.evidenceFamilies.length > 0 ||
    investigation.findings.length > 0 ||
    investigation.nextCalls.length > 0;
}

function coverageNarrative(investigation: ServiceInvestigation): string {
  const state = coverageLabel(investigation.coverage.state).toLowerCase();
  if (investigation.coverage.reason.length === 0) {
    return `Eshu marked this investigation as ${state} after checking the related repositories it could prove from indexed evidence.`;
  }
  return investigation.coverage.reason;
}

function coverageLabel(state: string): string {
  const normalized = state.trim().toLowerCase();
  if (normalized === "complete") {
    return "Complete";
  }
  if (normalized === "partial") {
    return "Partial";
  }
  return "Unknown";
}

function humanToolLabel(tool: string): string {
  const labels: Record<string, string> = {
    get_code_relationship_story: "Code relationship story",
    get_relationship_evidence: "Relationship evidence",
    get_service_context: "Service context",
    get_service_story: "Service story",
    investigate_service: "Service investigation",
    trace_deployment_chain: "Deployment chain"
  };
  return labels[tool] ?? humanLabel(tool);
}

function humanList(values: readonly string[]): string {
  return values.map(humanLabel).filter((value) => value.length > 0).join(", ");
}

function humanLabel(value: string): string {
  return value
    .replace(/_/g, " ")
    .replace(/\b\w/g, (letter) => letter.toUpperCase())
    .replace(/\bApi\b/g, "API")
    .replace(/\bMcp\b/g, "MCP")
    .replace(/\bEcs\b/g, "ECS")
    .replace(/\bEks\b/g, "EKS");
}
