import { Link } from "react-router-dom";

import type {
  DeploymentSourceLimits,
  DeploymentTraceEntity,
  DeploymentTracePlatform,
  DeploymentTraceResult,
  DeploymentTraceTopologyEdge,
  ImpactGraphPresentation,
} from "../api/impactReviewTypes";

export function ImpactGraphProvenance({
  presentation,
}: {
  readonly presentation: ImpactGraphPresentation;
}): React.JSX.Element {
  return (
    <div className="impact-graph-provenance" aria-label="Graph composition evidence">
      <div className="impact-mini-stats">
        <span>{presentation.mode.replace(/_/g, " ")}</span>
        {presentation.truthLevel ? <span>truth {presentation.truthLevel}</span> : null}
        {presentation.truthBasis ? <span>basis {presentation.truthBasis}</span> : null}
        {presentation.freshness ? <span>freshness {presentation.freshness}</span> : null}
        <span>composition {presentation.compositionDurationMs.toFixed(3)} ms</span>
        <span>
          {presentation.renderedNodes}/{presentation.inputNodes} nodes
        </span>
        <span>
          {presentation.renderedEdges}/{presentation.inputEdges} edges
        </span>
        <span>
          bounds {presentation.nodeLimit}/{presentation.edgeLimit}
        </span>
        <span>
          {presentation.completeness === "unverified"
            ? "completeness unverified"
            : presentation.completeness === "truncated"
              ? "truncated"
              : "complete within bounds"}
        </span>
      </div>
      <p className="mono impact-source">
        {presentation.sourceApis.join(" · ") || "No source API selected"}
      </p>
      {presentation.duplicateNodes +
        presentation.duplicateEdges +
        presentation.omittedNodes +
        presentation.omittedEdges >
      0 ? (
        <p className="t-mut">
          {presentation.duplicateNodes} duplicate nodes · {presentation.duplicateEdges} duplicate
          edges · {presentation.omittedNodes} omitted nodes · {presentation.omittedEdges} omitted
          edges
        </p>
      ) : null}
      {presentation.limitations.map((limitation) => (
        <p className="inline-state" key={limitation}>
          {limitation}
        </p>
      ))}
    </div>
  );
}

export function DeploymentTraceSummary({
  canInspectEntity,
  onInspectEntity,
  trace,
}: {
  readonly canInspectEntity: (entityId: string) => boolean;
  readonly onInspectEntity: (entityId: string) => void;
  readonly trace: DeploymentTraceResult;
}): React.JSX.Element {
  return (
    <div className="impact-summary-block">
      <div className="impact-mini-stats">
        <span>{trace.instances.length} runtime instances</span>
        <span>{trace.provisionedPlatforms.length} provisioned platforms</span>
        <span>{trace.deploymentSources.length} deployment sources</span>
        <span>{trace.cloudResources.length} cloud resources</span>
        <span>{trace.k8sResources.length} Kubernetes resources</span>
      </div>
      {trace.deploymentSourceLimits === null ? (
        <p className="inline-state">
          Deployment source coverage unavailable; deployment topology completeness is unverified.
        </p>
      ) : trace.deploymentSourceLimits.truncated ? (
        <p className="inline-state">{deploymentSourceLimitation(trace.deploymentSourceLimits)}</p>
      ) : null}

      <div className="impact-pivots" aria-label="Deployment pivots">
        {trace.serviceName ? (
          <Link to={`/service-story/${encodeURIComponent(trace.serviceName)}`}>Service story</Link>
        ) : null}
        {trace.workloadId ? (
          <Link to={`/workspace/services/${encodeURIComponent(trace.workloadId)}`}>
            Workload context
          </Link>
        ) : null}
        {trace.repoId ? (
          <Link to={`/repositories/${encodeURIComponent(trace.repoId)}/source`}>
            Repository source
          </Link>
        ) : null}
      </div>

      <details className="impact-narrative" open>
        <summary>Full deployment narrative</summary>
        <p>{trace.story}</p>
      </details>

      <section className="impact-trace-group">
        <div className="section-label">Subject relationship evidence</div>
        {trace.topologyEdges.length === 0 ? (
          <p className="empty">No exact repository-to-workload relationship backbone returned.</p>
        ) : (
          <TopologyEdgeEvidence edges={trace.topologyEdges} />
        )}
      </section>

      <DeploymentSourceGroup trace={trace} />

      <section className="impact-trace-group">
        <div className="section-label">Deployment facts</div>
        {trace.deploymentFacts.length === 0 ? (
          <p className="empty">No normalized deployment facts returned.</p>
        ) : (
          <div className="impact-entity-list">
            {trace.deploymentFacts.map((fact, index) => (
              <article key={`${fact.type}:${fact.targetId ?? fact.target}:${index}`}>
                <strong>{fact.type.replace(/_/g, " ")}</strong>
                <span>{fact.target}</span>
                {fact.targetId ? <span className="mono">{fact.targetId}</span> : null}
                {fact.reason ? <span>{fact.reason}</span> : null}
                {fact.targetId ? (
                  <EntityPivot
                    canInspectEntity={canInspectEntity}
                    entityId={fact.targetId}
                    label={`Inspect ${fact.target}`}
                    onInspectEntity={onInspectEntity}
                  />
                ) : null}
              </article>
            ))}
          </div>
        )}
      </section>

      <section className="impact-trace-group">
        <div className="section-label">Runtime instances and platforms</div>
        {trace.instances.length === 0 ? (
          <p className="empty">No materialized runtime instances returned.</p>
        ) : (
          <div className="impact-entity-list">
            {trace.instances.map((instance) => (
              <article key={instance.id || `missing:${instance.environment ?? "unknown"}`}>
                <strong>
                  Environment attribute: {instance.environment ?? "environment unavailable"}
                </strong>
                <span className="mono">
                  {instance.id || "canonical instance identity unavailable"}
                </span>
                <div className="impact-pivots">
                  {instance.id ? (
                    <EntityPivot
                      canInspectEntity={canInspectEntity}
                      entityId={instance.id}
                      label={
                        instance.environment
                          ? `Inspect ${instance.environment} runtime instance`
                          : `Inspect ${instance.id}`
                      }
                      onInspectEntity={onInspectEntity}
                    />
                  ) : null}
                </div>
                {instance.platforms.length > 0 ? (
                  <div className="impact-entity-list">
                    {instance.platforms.map((platform, index) => (
                      <PlatformEvidence
                        canInspectEntity={canInspectEntity}
                        key={`${platform.id ?? platform.name}:${index}`}
                        onInspectEntity={onInspectEntity}
                        platform={platform}
                      />
                    ))}
                  </div>
                ) : (
                  <span>No exact platform relationship returned</span>
                )}
              </article>
            ))}
          </div>
        )}
      </section>

      <section className="impact-trace-group">
        <div className="section-label">Repository-provisioned platforms</div>
        {trace.provisionedPlatforms.length === 0 ? (
          <p className="empty">No exact repository-level provisioning topology returned.</p>
        ) : (
          <div className="impact-entity-list">
            {trace.provisionedPlatforms.map((platform, index) => (
              <PlatformEvidence
                canInspectEntity={canInspectEntity}
                key={`${platform.id ?? platform.name}:${index}`}
                onInspectEntity={onInspectEntity}
                platform={platform}
              />
            ))}
          </div>
        )}
      </section>

      <TraceEntityGroup
        empty="No exact cloud-resource relationships returned."
        graphLinks
        label="Cloud resources"
        canInspectEntity={canInspectEntity}
        onInspectEntity={onInspectEntity}
        rows={trace.cloudResources}
      />
      <TraceEntityGroup
        empty="No Kubernetes resources returned."
        graphLinks
        label="Kubernetes resources"
        canInspectEntity={canInspectEntity}
        onInspectEntity={onInspectEntity}
        rows={trace.k8sResources}
      />

      {trace.imageRefs.length > 0 ? (
        <section className="impact-trace-group">
          <div className="section-label">Image references</div>
          <p className="mono t-mut">{trace.imageRefs.join(" · ")}</p>
        </section>
      ) : null}
    </div>
  );
}

function PlatformEvidence({
  canInspectEntity,
  onInspectEntity,
  platform,
}: {
  readonly canInspectEntity: (entityId: string) => boolean;
  readonly onInspectEntity: (entityId: string) => void;
  readonly platform: DeploymentTracePlatform;
}): React.JSX.Element {
  return (
    <article>
      <strong>{platform.name}</strong>
      <span>{platform.kind ?? "platform"}</span>
      {platform.id ? (
        <>
          <span className="mono">{platform.id}</span>
          <EntityPivot
            canInspectEntity={canInspectEntity}
            entityId={platform.id}
            label={`Inspect ${platform.name} platform`}
            onInspectEntity={onInspectEntity}
          />
        </>
      ) : (
        <span>Canonical identity unavailable</span>
      )}
      <TopologyEdgeEvidence edges={platform.topologyEdges} />
    </article>
  );
}

function TopologyEdgeEvidence({
  edges,
}: {
  readonly edges: readonly DeploymentTraceTopologyEdge[];
}): React.JSX.Element | null {
  if (edges.length === 0) return null;
  return (
    <div className="impact-entity-list">
      {edges.map((edge, index) => (
        <span
          className="t-mut"
          key={`${edge.relationshipType}:${edge.sourceId}:${edge.targetId}:${index}`}
        >
          {[
            edge.relationshipType,
            edge.sourceId && edge.targetId ? `${edge.sourceId} -> ${edge.targetId}` : undefined,
            edge.evidenceSource,
            edge.sourceTool,
            edge.reason,
            edge.confidence === undefined ? undefined : `confidence ${edge.confidence}`,
          ]
            .filter((value): value is string => value !== undefined && value.length > 0)
            .join(" · ")}
        </span>
      ))}
    </div>
  );
}

function deploymentSourceLimitation(limits: DeploymentSourceLimits): string {
  const observed = limits.observedCountIsLowerBound
    ? `at least ${limits.observedCount}`
    : String(limits.observedCount);
  return `Deployment sources truncated: showing ${limits.returnedCount} of ${observed} observed relationships (${limits.limit}-result limit).`;
}

function DeploymentSourceGroup({
  trace,
}: {
  readonly trace: DeploymentTraceResult;
}): React.JSX.Element {
  return (
    <section className="impact-trace-group">
      <div className="section-label">Deployment sources</div>
      {trace.deploymentSources.length === 0 ? (
        <p className="empty">No canonical deployment-source repositories returned.</p>
      ) : (
        <div className="impact-entity-list" role="list">
          {trace.deploymentSources.map((source, index) => {
            const relationship = deploymentSourceRelationship(source, trace);
            return (
              <article
                key={`${source.id ?? source.name}:${relationship.family}:${index}`}
                role="listitem"
              >
                {source.id ? (
                  <Link to={`/repositories/${encodeURIComponent(source.id)}/source`}>
                    <strong>{source.name}</strong>
                  </Link>
                ) : (
                  <strong>{source.name}</strong>
                )}
                <span>{relationship.family}</span>
                <span>
                  {relationship.verb}: {relationship.source} → {relationship.target}
                </span>
                {source.id ? <span className="mono">{source.id}</span> : null}
                {source.detail ? <span>{source.detail}</span> : null}
              </article>
            );
          })}
        </div>
      )}
    </section>
  );
}

function deploymentSourceRelationship(
  source: import("../api/impactReviewTypes").DeploymentTraceSource,
  trace: DeploymentTraceResult,
): {
  readonly family: string;
  readonly source: string;
  readonly target: string;
  readonly verb: string;
} {
  if (source.relationshipType === "DEPLOYS_FROM") {
    return {
      family: "DEPLOYS_FROM",
      source: source.name,
      target: trace.repoName || trace.repoId,
      verb: "deploys from",
    };
  }
  if (source.relationshipType === "DEPLOYMENT_SOURCE") {
    return {
      family: "DEPLOYMENT_SOURCE",
      source: source.sourceId ?? "source identity unavailable",
      target: source.name,
      verb: "deployment source",
    };
  }
  return {
    family: "relationship family unavailable",
    source: source.sourceId ?? "source identity unavailable",
    target: source.targetId ?? source.name,
    verb: "relationship unavailable",
  };
}

function TraceEntityGroup({
  canInspectEntity,
  empty,
  graphLinks = false,
  label,
  onInspectEntity,
  repositoryLinks = false,
  rows,
}: {
  readonly canInspectEntity?: (entityId: string) => boolean;
  readonly empty: string;
  readonly graphLinks?: boolean;
  readonly label: string;
  readonly onInspectEntity?: (entityId: string) => void;
  readonly repositoryLinks?: boolean;
  readonly rows: readonly DeploymentTraceEntity[];
}): React.JSX.Element {
  return (
    <section className="impact-trace-group">
      <div className="section-label">{label}</div>
      {rows.length === 0 ? (
        <p className="empty">{empty}</p>
      ) : (
        <div className="impact-entity-list">
          {rows.map((row, index) => (
            <article key={`${row.id ?? row.name}:${index}`}>
              {repositoryLinks && row.id ? (
                <Link to={`/repositories/${encodeURIComponent(row.id)}/source`}>
                  <strong>{row.name}</strong>
                </Link>
              ) : (
                <strong>{row.name}</strong>
              )}
              {row.id ? <span className="mono">{row.id}</span> : null}
              {row.detail ? <span>{row.detail}</span> : null}
              {graphLinks && row.id && onInspectEntity ? (
                <EntityPivot
                  canInspectEntity={canInspectEntity}
                  entityId={row.id}
                  label={`Inspect ${row.name}`}
                  onInspectEntity={onInspectEntity}
                />
              ) : null}
            </article>
          ))}
        </div>
      )}
    </section>
  );
}

function EntityPivot({
  canInspectEntity,
  entityId,
  label,
  onInspectEntity,
}: {
  readonly canInspectEntity?: (entityId: string) => boolean;
  readonly entityId: string;
  readonly label: string;
  readonly onInspectEntity: (entityId: string) => void;
}): React.JSX.Element {
  if (canInspectEntity !== undefined && !canInspectEntity(entityId)) {
    return <span className="t-mut">Outside bounded graph</span>;
  }
  return (
    <button
      aria-label={label}
      className="btn-ghost"
      onClick={() => onInspectEntity(entityId)}
      type="button"
    >
      Inspect graph
    </button>
  );
}
