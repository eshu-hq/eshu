import { useMemo, useState } from "react";
import type {
  ServiceEndpoint,
  ServiceHostname,
  ServiceDeploymentLane,
  ServiceSpotlight
} from "../api/serviceSpotlight";
import { ServiceEvidenceRail, ServiceTrustStrip } from "./ServiceAtlasEvidence";
import { ServiceChangeSurfacePanel } from "./ServiceChangeSurfacePanel";
import { ServiceCodeInvestigationPanel } from "./ServiceCodeInvestigationPanel";
import { ServiceConfigInfluencePanel } from "./ServiceConfigInfluencePanel";
import { ServiceInvestigationPanel } from "./ServiceInvestigationPanel";
import { ServiceRelationshipExplorer } from "./ServiceRelationshipExplorer";
import { ServiceRelationshipWorkbench } from "./ServiceRelationshipWorkbench";
import { ServiceTrafficPathPanel } from "./ServiceTrafficPathPanel";

type ServiceAtlasTab = "map" | "traffic" | "impact" | "api";

export function ServiceSpotlightPanel({
  spotlight
}: {
  readonly spotlight: ServiceSpotlight;
}): React.JSX.Element {
  const [activeTab, setActiveTab] = useState<ServiceAtlasTab>("map");

  return (
    <section aria-label="Service Atlas" className="service-spotlight service-atlas">
      <div className="service-atlas-header">
        <div className="service-atlas-copy">
          <span className="entity-kind">Service</span>
          <h2>Service Atlas</h2>
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

      <ServiceTrustStrip spotlight={spotlight} />

      <div className="service-section-tabs" aria-label="Service atlas sections">
        {serviceTabs.map((tab) => (
          <button
            aria-label={tab.label}
            aria-pressed={activeTab === tab.id}
            key={tab.id}
            onClick={() => setActiveTab(tab.id)}
            type="button"
          >
            <span>{tab.label}</span>
            <small>{tab.description}</small>
          </button>
        ))}
      </div>

      {activeTab === "map" ? (
        <div className="service-atlas-tab-panel service-atlas-workbench">
          <div className="service-atlas-workbench-main">
            <div className="service-deployment-board">
              <section aria-label="Deployment story" className="service-panel service-map-panel">
                <PanelHeading
                  detail={relationshipMapSentence(spotlight)}
                  title="Service flow"
                />
                <ServiceRelationshipWorkbench spotlight={spotlight} />
              </section>
              <LaneCards lanes={spotlight.lanes} />
            </div>
          </div>
          <ServiceEvidenceRail spotlight={spotlight} />
        </div>
      ) : null}

      {activeTab === "traffic" ? (
        <div className="service-atlas-tab-panel">
          <EntryPointStrip hostnames={spotlight.hostnames} />
          <ServiceTrafficPathPanel paths={spotlight.trafficPaths} serviceName={spotlight.name} />
          <ServiceConfigInfluencePanel influence={spotlight.configInfluence} />
        </div>
      ) : null}

      {activeTab === "impact" ? (
        <div className="service-atlas-tab-panel">
          <ServiceInvestigationPanel investigation={spotlight.investigation} />
          <ServiceChangeSurfacePanel spotlight={spotlight} />
          <ServiceCodeInvestigationPanel spotlight={spotlight} />
        </div>
      ) : null}

      {activeTab === "api" ? (
        <div className="service-atlas-tab-panel service-operating-grid">
          <EndpointTable
            endpointCount={spotlight.api.endpointCount}
            endpoints={spotlight.api.endpoints}
          />
          <RelationshipList
            clusters={spotlight.relationshipClusters}
            dependencies={spotlight.dependencies}
            graphDependents={spotlight.graphDependents}
            lanes={spotlight.lanes}
            references={spotlight.consumers}
            totals={spotlight.relationshipCounts}
          />
        </div>
      ) : null}
    </section>
  );
}

const serviceTabs: readonly {
  readonly description: string;
  readonly id: ServiceAtlasTab;
  readonly label: string;
}[] = [
  { description: "Deployment truth", id: "map", label: "Map" },
  { description: "Entry, traffic, config", id: "traffic", label: "Traffic and config" },
  { description: "Change and code proof", id: "impact", label: "Impact review" },
  { description: "Endpoints and consumers", id: "api", label: "API and relationships" }
];

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

function relationshipMapSentence(spotlight: ServiceSpotlight): string {
  const lanes = spotlight.lanes.map((lane) => lane.label).join(" and ");
  if (lanes.length === 0) {
    return "Drag the map and click relationships to inspect evidence as it arrives.";
  }
  return `Explore ${lanes} plus config dependencies. Drag nodes and click edges for proof.`;
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
        {filteredEndpoints.map((endpoint, index) => (
          <article key={endpointKey(endpoint, index)}>
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

function endpointKey(endpoint: ServiceEndpoint, index: number): string {
  return [
    endpoint.path,
    endpoint.methods.join(","),
    endpoint.operationIds.join(","),
    endpoint.sourcePaths.join(","),
    index
  ].join(":");
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

const RelationshipList = ServiceRelationshipExplorer;

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
