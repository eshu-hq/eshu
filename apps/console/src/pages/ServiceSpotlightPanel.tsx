import { useMemo, useState } from "react";
import type {
  ServiceConsumer,
  ServiceDependency,
  ServiceEndpoint,
  ServiceHostname,
  ServiceDeploymentLane,
  ServiceSpotlight
} from "../api/serviceSpotlight";
import { ServiceDeploymentLaneMap } from "../visualization/ServiceDeploymentLaneMap";
import { ServiceCodeInvestigationPanel } from "./ServiceCodeInvestigationPanel";
import { ServiceInvestigationPanel } from "./ServiceInvestigationPanel";

export function ServiceSpotlightPanel({
  spotlight
}: {
  readonly spotlight: ServiceSpotlight;
}): React.JSX.Element {
  return (
    <section aria-label="Service intelligence" className="service-spotlight">
      <div className="service-brief">
        <div className="service-brief-copy">
          <span className="entity-kind">Service</span>
          <h1>{spotlight.name}</h1>
          <p>{humanSummary(spotlight)}</p>
          <div className="service-storyline" aria-label="Service story highlights">
            <StoryPill label="API" value={`${spotlight.api.endpointCount} endpoints`} />
            <StoryPill label="Runtime" value={deploymentHeadline(spotlight)} />
            <StoryPill label="Entry" value={`${spotlight.hostnames.length} hostnames`} />
            <StoryPill
              label="Impact"
              value={`${spotlight.relationshipCounts.downstream} downstream`}
            />
          </div>
        </div>
        <MetricList spotlight={spotlight} />
      </div>

      <div className="service-deployment-board">
        <section aria-label="Deployment story" className="service-panel service-map-panel">
          <PanelHeading
            detail={deploymentSentence(spotlight)}
            title="Deployment"
          />
          <ServiceDeploymentLaneMap spotlight={spotlight} />
        </section>
        <LaneCards lanes={spotlight.lanes} />
      </div>

      <EntryPointStrip hostnames={spotlight.hostnames} />
      <ServiceInvestigationPanel investigation={spotlight.investigation} />
      <ServiceCodeInvestigationPanel spotlight={spotlight} />

      <div className="service-operating-grid">
        <EndpointTable
          endpointCount={spotlight.api.endpointCount}
          endpoints={spotlight.api.endpoints}
        />
        <RelationshipList
          dependencies={spotlight.dependencies}
          graphDependents={spotlight.graphDependents}
          lanes={spotlight.lanes}
          references={spotlight.consumers}
          totals={spotlight.relationshipCounts}
        />
      </div>
    </section>
  );
}

function MetricList({
  spotlight
}: {
  readonly spotlight: ServiceSpotlight;
}): React.JSX.Element {
  const metrics = [
    { label: "API", value: `${spotlight.api.endpointCount} endpoints` },
    { label: "Methods", value: `${spotlight.api.methodCount} methods` },
    { label: "Deployments", value: `${spotlight.lanes.length} lanes` },
    { label: "References", value: `${spotlight.relationshipCounts.references} references` },
    {
      label: "Typed dependents",
      value: `${spotlight.relationshipCounts.graphDependents} typed dependents`
    }
  ];
  return (
    <dl className="service-metrics" aria-label="Service facts">
      {metrics.map((metric) => (
        <div key={metric.label}>
          <dt>{metric.label}</dt>
          <dd>{metric.value}</dd>
        </div>
      ))}
    </dl>
  );
}

function StoryPill({
  label,
  value
}: {
  readonly label: string;
  readonly value: string;
}): React.JSX.Element {
  return (
    <span>
      <strong>{label}</strong>
      {value}
    </span>
  );
}

function deploymentSentence(spotlight: ServiceSpotlight): string {
  const lanes = spotlight.lanes.map((lane) => lane.label).join(" and ");
  const environments = [
    ...new Set(spotlight.lanes.flatMap((lane) => lane.environments))
  ];
  if (lanes.length === 0 || environments.length === 0) {
    return "Deployment evidence is still being gathered.";
  }
  return `Runs in ${lanes} across ${environments.join(", ")}. Click a lane to inspect the source repos and relationship verbs.`;
}

function deploymentHeadline(spotlight: ServiceSpotlight): string {
  if (spotlight.lanes.length > 1) {
    return `Dual deployment: ${spotlight.lanes.map((lane) => lane.label).join(" + ")}`;
  }
  return spotlight.lanes[0]?.label ?? "Deployment pending";
}

function EndpointTable({
  endpointCount,
  endpoints
}: {
  readonly endpointCount: number;
  readonly endpoints: readonly ServiceEndpoint[];
}): React.JSX.Element {
  const [query, setQuery] = useState("");
  const filteredEndpoints = useMemo(
    () => filterEndpoints(endpoints, query),
    [endpoints, query]
  );
  return (
    <section aria-label="API endpoints" className="service-panel">
      <PanelHeading
        detail={`${filteredEndpoints.length} shown of ${endpointCount} observed`}
        title="API endpoints"
      />
      <label className="service-search">
        <span>Search</span>
        <input
          aria-label="Search API endpoints"
          onChange={(event) => setQuery(event.target.value)}
          placeholder="/listing, get, specs"
          type="search"
          value={query}
        />
      </label>
      <div className="service-endpoint-list">
        {filteredEndpoints.map((endpoint) => (
          <article key={`${endpoint.path}:${endpoint.methods.join(",")}`}>
            <div>
              <strong>{endpoint.path}</strong>
              <span>{endpoint.methods.join(", ") || "method pending"}</span>
            </div>
            <p>{endpoint.operationIds.join(", ") || endpoint.sourcePaths.join(", ")}</p>
          </article>
        ))}
      </div>
    </section>
  );
}

function filterEndpoints(
  endpoints: readonly ServiceEndpoint[],
  query: string
): readonly ServiceEndpoint[] {
  const normalized = query.trim().toLowerCase();
  if (normalized.length === 0) {
    return endpoints;
  }
  return endpoints.filter((endpoint) =>
    [
      endpoint.path,
      ...endpoint.methods,
      ...endpoint.operationIds,
      ...endpoint.sourcePaths
    ].some((value) => value.toLowerCase().includes(normalized))
  );
}

function RelationshipList({
  dependencies,
  graphDependents,
  lanes,
  references,
  totals
}: {
  readonly dependencies: readonly ServiceDependency[];
  readonly graphDependents: readonly ServiceConsumer[];
  readonly lanes: readonly ServiceDeploymentLane[];
  readonly references: readonly ServiceConsumer[];
  readonly totals: ServiceSpotlight["relationshipCounts"];
}): React.JSX.Element {
  return (
    <section aria-label="Service relationships" className="service-panel">
      <PanelHeading
        detail={`${totals.downstream} downstream, ${totals.upstream} upstream`}
        title="Relationships"
      />
      <div className="service-relationship-groups">
        <LaneSourceList lanes={lanes} />
        <ConsumerList
          consumers={references}
          count={totals.references}
          heading="Repos that mention it"
        />
        <ConsumerList
          consumers={graphDependents}
          count={totals.graphDependents}
          heading="Typed dependents"
        />
        <DependencyList dependencies={dependencies} total={totals.upstream} />
      </div>
    </section>
  );
}

function LaneSourceList({
  lanes
}: {
  readonly lanes: readonly ServiceDeploymentLane[];
}): React.JSX.Element {
  return (
    <div className="service-relationship-group service-relationship-group-wide">
      <div className="service-relationship-group-heading">
        <h4>What deploys or provisions it</h4>
        <span>{lanes.length} lanes</span>
      </div>
      {lanes.map((lane) => (
        <article key={lane.label}>
          <strong>{lane.label}</strong>
          <EvidenceLabels labels={lane.relationshipTypes} fallback="deployment evidence" />
          <small>{lane.environments.join(", ") || "environment pending"}</small>
          <p>{lane.sourceRepos.join(", ") || "Source repository not observed."}</p>
        </article>
      ))}
    </div>
  );
}

function ConsumerList({
  consumers,
  count,
  heading
}: {
  readonly consumers: readonly ServiceConsumer[];
  readonly count: number;
  readonly heading: string;
}): React.JSX.Element {
  return (
    <div className="service-relationship-group">
      <div className="service-relationship-group-heading">
        <h4>{heading}</h4>
        <span>{count} observed</span>
      </div>
      {consumers.slice(0, 8).map((consumer, index) => (
        <article key={`${heading}:${consumer.repository}:${index}`}>
          <strong>{consumer.repository}</strong>
          <EvidenceLabels labels={consumerLabels(consumer)} fallback="observed reference" />
          <p>{consumer.samplePaths[0] ?? consumer.matchedValues[0] ?? "Evidence observed."}</p>
        </article>
      ))}
    </div>
  );
}

function consumerLabels(consumer: ServiceConsumer): readonly string[] {
  return [...consumer.relationshipTypes, ...consumer.consumerKinds].map(prettyLabel);
}

function DependencyList({
  dependencies,
  total
}: {
  readonly dependencies: readonly ServiceDependency[];
  readonly total: number;
}): React.JSX.Element {
  return (
    <div className="service-relationship-group">
      <div className="service-relationship-group-heading">
        <h4>Upstream relationships</h4>
        <span>{total} observed</span>
      </div>
      {dependencies.slice(0, 8).map((dependency, index) => (
        <article key={`${dependency.type}:${dependency.targetName}:${index}`}>
          <strong>{dependency.targetName}</strong>
          <EvidenceLabels labels={[dependency.type]} fallback="relationship evidence" />
          <p>{dependency.rationale}</p>
        </article>
      ))}
    </div>
  );
}

function LaneCards({
  lanes
}: {
  readonly lanes: readonly ServiceDeploymentLane[];
}): React.JSX.Element {
  return (
    <section aria-label="Deployment lane summary" className="service-panel service-lane-cards">
      <PanelHeading
        detail={`${lanes.length} observed`}
        title="Lanes"
      />
      {lanes.map((lane) => (
        <article key={lane.label}>
          <div>
            <strong>{lane.label}</strong>
            <span>{lane.evidenceCount} items</span>
          </div>
          <p>{lane.environments.join(", ") || "No environments observed yet"}</p>
          <div className="service-chip-row">
            {lane.relationshipTypes.map((type) => (
              <span key={`${lane.label}:${type}`}>{type}</span>
            ))}
          </div>
          <small>{lane.sourceRepos.join(", ") || "Source repos pending"}</small>
        </article>
      ))}
    </section>
  );
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

function prettyLabel(label: string): string {
  return label === label.toUpperCase() ? label : label.replace(/_/g, " ");
}

function EntryPointStrip({
  hostnames
}: {
  readonly hostnames: readonly ServiceHostname[];
}): React.JSX.Element | null {
  if (hostnames.length === 0) {
    return null;
  }
  return (
    <section aria-label="Service entrypoints" className="service-entrypoints">
      <PanelHeading
        detail={`${hostnames.length} observed`}
        title="Entrypoints"
      />
      <div>
        {hostnames.slice(0, 6).map((hostname) => (
          <article key={`${hostname.hostname}:${hostname.environment}`}>
            <strong>{hostname.hostname}</strong>
            <span>{hostname.environment}</span>
            {hostname.path.length > 0 ? <small>{hostname.path}</small> : null}
          </article>
        ))}
      </div>
    </section>
  );
}

function PanelHeading({
  detail,
  title
}: {
  readonly detail: string;
  readonly title: string;
}): React.JSX.Element {
  return (
    <div className="service-panel-heading">
      <h3>{title}</h3>
      <span>{detail}</span>
    </div>
  );
}

function humanSummary(spotlight: ServiceSpotlight): string {
  return `${spotlight.name} is an API service with ${spotlight.api.endpointCount} endpoint(s), ${deploymentHeadline(spotlight).toLowerCase()}, and ${spotlight.relationshipCounts.downstream} downstream relationship(s) across typed graph evidence and content references.`;
}
