import { useEffect, useState } from "react";
import { EshuApiClient } from "../api/client";
import {
  loadCodeTopicInvestigation,
  type CodeTopicInvestigation,
  type CodeTopicNextCall
} from "../api/codeTopic";
import type { ServiceSpotlight } from "../api/serviceSpotlight";
import { loadConsoleEnvironment } from "../config/environment";

export function ServiceCodeInvestigationPanel({
  spotlight
}: {
  readonly spotlight: ServiceSpotlight;
}): React.JSX.Element | null {
  const [investigation, setInvestigation] = useState<CodeTopicInvestigation | undefined>();

  useEffect(() => {
    const environment = loadConsoleEnvironment();
    if (environment.mode !== "private") {
      return;
    }
    const client = new EshuApiClient({
      apiKey: environment.apiKey,
      baseUrl: environment.apiBaseUrl
    });
    let active = true;
    void loadCodeTopicInvestigation({
      client,
      repoName: spotlight.repoName,
      serviceName: spotlight.name
    })
      .then((loaded) => {
        if (active) {
          setInvestigation(loaded);
        }
      })
      .catch(() => {
        if (active) {
          setInvestigation(undefined);
        }
      });
    return () => {
      active = false;
    };
  }, [spotlight.name, spotlight.repoName]);

  if (investigation === undefined || investigation.coverage.empty) {
    return null;
  }

  return (
    <section aria-label="Code investigation" className="service-code-investigation">
      <div className="service-code-investigation-copy">
        <span className="entity-kind">Code investigation</span>
        <h2>Code paths Eshu found</h2>
        <p>
          Eshu searched the service repository for behavior-level evidence before asking
          for exact symbols. Use this when the question starts with a flow, not a function name.
        </p>
      </div>
      <div className="service-code-investigation-summary">
        <dl>
          <div>
            <dt>Matches</dt>
            <dd>{investigation.coverage.returnedCount}</dd>
          </div>
          <div>
            <dt>Symbols</dt>
            <dd>{investigation.matchedSymbols.length}</dd>
          </div>
          <div>
            <dt>Files</dt>
            <dd>{investigation.matchedFiles.length}</dd>
          </div>
        </dl>
        <div className="service-chip-row">
          {investigation.coverage.searchedTerms.slice(0, 6).map((term) => (
            <span key={term}>{term}</span>
          ))}
        </div>
      </div>
      <div className="service-code-investigation-grid">
        {investigation.evidenceGroups.slice(0, 4).map((group) => (
          <article key={`${group.rank}:${group.relativePath}:${group.entityName}`}>
            <div>
              <strong>{group.entityName || group.relativePath}</strong>
              <span>{group.entityType || group.sourceKind}</span>
            </div>
            <p>{group.relativePath}</p>
            <small>{codeTopicDetail(group.language, group.matchedTerms)}</small>
          </article>
        ))}
      </div>
      <NextCallStrip calls={investigation.nextCalls} />
    </section>
  );
}

function NextCallStrip({
  calls
}: {
  readonly calls: readonly CodeTopicNextCall[];
}): React.JSX.Element | null {
  if (calls.length === 0) {
    return null;
  }
  return (
    <div className="service-code-next-calls" aria-label="Code investigation next calls">
      <h3>Next proof calls</h3>
      {calls.slice(0, 3).map((call, index) => (
        <article key={`${call.tool}:${index}`}>
          <strong>{humanToolLabel(call.tool)}</strong>
          <small>{argumentSummary(call.args)}</small>
        </article>
      ))}
    </div>
  );
}

function codeTopicDetail(language: string, terms: readonly string[]): string {
  const termList = terms.join(", ");
  return termList.length > 0 ? `${language}; matched ${termList}` : language;
}

function argumentSummary(argumentsValue: Record<string, unknown>): string {
  const path = stringArgument(argumentsValue, "relative_path");
  const startLine = stringArgument(argumentsValue, "start_line");
  const endLine = stringArgument(argumentsValue, "end_line");
  if (path.length > 0 && startLine.length > 0 && endLine.length > 0) {
    return `${path}:${startLine}-${endLine}`;
  }
  if (path.length > 0) {
    return path;
  }
  const direction = stringArgument(argumentsValue, "direction");
  const limit = stringArgument(argumentsValue, "limit");
  if (direction.length > 0 && limit.length > 0) {
    return `${direction} relationships, ${limit} max`;
  }
  const summary = Object.entries(argumentsValue)
    .slice(0, 3)
    .map(([key, value]) => `${key}: ${String(value)}`)
    .join(", ");
  return summary.length > 0 ? summary : "No extra arguments";
}

function stringArgument(argumentsValue: Record<string, unknown>, key: string): string {
  const value = argumentsValue[key];
  return value === undefined || value === null ? "" : String(value);
}

function humanToolLabel(tool: string): string {
  const labels: Record<string, string> = {
    get_code_relationship_story: "Code relationship story",
    get_file_lines: "Source lines",
    find_symbol: "Symbol lookup",
    search_file_content: "Content search"
  };
  return labels[tool] ?? tool.replace(/_/g, " ");
}
