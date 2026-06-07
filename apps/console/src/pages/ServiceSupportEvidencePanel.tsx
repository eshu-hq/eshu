import type {
  ServiceSupportEvidence,
  ServiceSupportOverview
} from "../api/serviceSupportEvidence";

export function ServiceSupportEvidencePanel({
  support
}: {
  readonly support: ServiceSupportOverview | undefined;
}): React.JSX.Element | null {
  if (support === undefined) {
    return null;
  }
  return (
    <section aria-label="Incidents and issues" className="service-panel">
      <div className="service-panel-heading">
        <h3>Incidents and issues</h3>
        <span>{supportSummary(support)}</span>
      </div>
      <div className="service-chip-row" aria-label="Support evidence counts">
        <span>{countLabel(support.workItemCount, "Jira/work item", "Jira/work items")}</span>
        <span>{countLabel(support.incidentRoutingCount, "PagerDuty route", "PagerDuty routes")}</span>
        {support.ambiguousCount > 0 ? (
          <span>{countLabel(support.ambiguousCount, "ambiguous", "ambiguous")}</span>
        ) : null}
        {support.truncated ? <span>Truncated</span> : null}
      </div>
      {support.evidence.length > 0 ? (
        <div className="service-endpoint-list">
          {support.evidence.map((row) => (
            <SupportEvidenceCard key={`${row.factId}:${row.factKind}`} row={row} />
          ))}
        </div>
      ) : (
        <p>{support.missingEvidence.join(", ") || "No target-linked support evidence yet."}</p>
      )}
    </section>
  );
}

function SupportEvidenceCard({
  row
}: {
  readonly row: ServiceSupportEvidence;
}): React.JSX.Element {
  const chips = [row.provider, row.issueType, row.status, row.outcome]
    .filter((value): value is string => value !== undefined && value.length > 0);
  return (
    <article>
      <div>
        <strong>{row.label}</strong>
        <span>{sourceLabel(row)}</span>
      </div>
      {chips.length > 0 ? (
        <div className="service-chip-row">
          {chips.map((chip) => <span key={`${row.factId}:${chip}`}>{chip}</span>)}
        </div>
      ) : null}
      <p>{row.sourceUrlText ?? row.factKind}</p>
      <small>{row.scopeId ?? row.observedAt ?? row.factId}</small>
    </article>
  );
}

function supportSummary(support: ServiceSupportOverview): string {
  if (support.evidenceCount === 0) {
    return support.missingEvidence.join(", ") || "No target-linked support evidence yet";
  }
  return `${support.evidenceCount} target-linked evidence item(s) from bounded support read models`;
}

function sourceLabel(row: ServiceSupportEvidence): string {
  return row.sourceSystem ?? row.factKind.split(".")[0] ?? "support";
}

function countLabel(count: number, singular: string, plural: string): string {
  return `${count} ${count === 1 ? singular : plural}`;
}
