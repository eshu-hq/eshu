import { Link } from "react-router-dom";

import type {
  DeploymentTraceEntity,
  DeploymentTraceResult,
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
        {presentation.freshness ? <span>freshness {presentation.freshness}</span> : null}
        <span>
          {presentation.renderedNodes}/{presentation.inputNodes} nodes
        </span>
        <span>
          {presentation.renderedEdges}/{presentation.inputEdges} edges
        </span>
        <span>
          bounds {presentation.nodeLimit}/{presentation.edgeLimit}
        </span>
        <span>{presentation.truncated ? "truncated" : "complete within bounds"}</span>
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
  trace,
}: {
  readonly trace: DeploymentTraceResult;
}): React.JSX.Element {
  return (
    <div className="impact-summary-block">
      <div className="impact-mini-stats">
        <span>{trace.instances.length} runtime instances</span>
        <span>{trace.deploymentSources.length} deployment sources</span>
        <span>{trace.cloudResources.length} cloud resources</span>
        <span>{trace.k8sResources.length} Kubernetes resources</span>
      </div>

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

      <TraceEntityGroup
        empty="No canonical deployment-source repositories returned."
        label="Deployment sources"
        rows={trace.deploymentSources}
        repositoryLinks
      />

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
                <strong>{instance.environment ?? "environment unavailable"}</strong>
                <span className="mono">
                  {instance.id || "canonical instance identity unavailable"}
                </span>
                <span>
                  {instance.platforms.length > 0
                    ? instance.platforms
                        .map((platform) => `${platform.name} (${platform.kind ?? "platform"})`)
                        .join(" · ")
                    : "No exact platform relationship returned"}
                </span>
              </article>
            ))}
          </div>
        )}
      </section>

      <TraceEntityGroup
        empty="No exact cloud-resource relationships returned."
        label="Cloud resources"
        rows={trace.cloudResources}
      />
      <TraceEntityGroup
        empty="No Kubernetes resources returned."
        label="Kubernetes resources"
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

function TraceEntityGroup({
  empty,
  label,
  repositoryLinks = false,
  rows,
}: {
  readonly empty: string;
  readonly label: string;
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
            </article>
          ))}
        </div>
      )}
    </section>
  );
}
