import type { ServiceSpotlight } from "../api/serviceSpotlight";

export function ServiceTrustStrip({
  spotlight
}: {
  readonly spotlight: ServiceSpotlight;
}): React.JSX.Element {
  const coverage = spotlight.investigation.coverage;
  const families = spotlight.investigation.evidenceFamilies.length;
  const items = [
    {
      label: "Coverage",
      value: `${displayValue(coverage.state)} coverage`
    },
    {
      label: "Scope",
      value: `${coverage.repositoryCount} repositories`
    },
    {
      label: "Evidence",
      value: `${families} evidence families`
    },
    {
      label: "Truth",
      value: `${displayValue(truthDisplayValue(spotlight.trust))} truth`
    },
    {
      label: "Payload",
      value: coverage.truncated ? "Truncated" : "Not truncated"
    }
  ];
  return (
    <dl className="service-atlas-trust-strip" aria-label="Service evidence confidence">
      {items.map((item) => (
        <div key={item.label}>
          <dt>{item.label}</dt>
          <dd>{item.value}</dd>
        </div>
      ))}
    </dl>
  );
}

export function ServiceEvidenceRail({
  spotlight
}: {
  readonly spotlight: ServiceSpotlight;
}): React.JSX.Element {
  const coverage = spotlight.investigation.coverage;
  const findings = spotlight.investigation.findings.slice(0, 4);
  const nextCalls = spotlight.investigation.nextCalls.slice(0, 3);
  return (
    <aside aria-label="Evidence drilldown" className="service-evidence-rail">
      <div className="service-evidence-rail-heading">
        <span>{spotlight.trust.level}</span>
        <h3>Evidence drilldown</h3>
        <p>{coverage.reason}</p>
      </div>
      <section>
        <h4>Deployment lanes</h4>
        <div className="service-evidence-rail-list">
          {spotlight.lanes.map((lane) => (
            <article key={lane.label}>
              <strong>{lane.label}</strong>
              <span>{lane.evidenceCount} evidence items</span>
              <p>{lane.sourceRepos.join(", ") || "Source repositories pending."}</p>
            </article>
          ))}
        </div>
      </section>
      <section>
        <h4>Evidence families</h4>
        <EvidenceLabels
          fallback="evidence pending"
          labels={spotlight.investigation.evidenceFamilies.map(displayEvidenceFamily)}
        />
      </section>
      <section>
        <h4>What to inspect next</h4>
        <div className="service-evidence-rail-list">
          {findings.map((finding) => (
            <article key={`${finding.family}:${finding.path}`}>
              <strong>{displayEvidenceFamily(finding.family)}</strong>
              <p>{finding.summary}</p>
            </article>
          ))}
          {nextCalls.map((call) => (
            <article key={`${call.tool}:${call.reason}`}>
              <strong>{call.tool}</strong>
              <p>{call.reason}</p>
            </article>
          ))}
        </div>
      </section>
    </aside>
  );
}

function truthDisplayValue(trust: ServiceSpotlight["trust"]): string {
  if (trust.freshness === "unavailable") {
    return trust.level;
  }
  return trust.freshness;
}

function EvidenceLabels({
  fallback,
  labels
}: {
  readonly fallback: string;
  readonly labels: readonly string[];
}): React.JSX.Element {
  const visibleLabels = labels.length > 0 ? labels : [fallback];
  return (
    <div className="service-evidence-chips">
      {visibleLabels.map((label) => (
        <span key={label}>{label}</span>
      ))}
    </div>
  );
}

function displayValue(value: string): string {
  if (value.length === 0) {
    return "Unknown";
  }
  return `${value.slice(0, 1).toUpperCase()}${value.slice(1).replace(/_/g, " ")}`;
}

function displayEvidenceFamily(value: string): string {
  return value
    .split("_")
    .filter((part) => part.length > 0)
    .map(displayValue)
    .join(" ");
}
