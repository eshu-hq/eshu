import {
  Blocks,
  FolderGit2,
  GitBranch,
  Hexagon,
  KeyRound,
  ShipWheel,
  Workflow
} from "lucide-react";
import type {
  ServiceConsumer,
  ServiceDependency,
  ServiceDeploymentLane,
  ServiceRelationshipCluster,
  ServiceRelationshipRepository,
  ServiceSpotlight,
  ServiceTechnologyKind
} from "../api/serviceSpotlight";

export function ServiceRelationshipExplorer({
  clusters,
  dependencies,
  graphDependents,
  lanes,
  references,
  totals
}: {
  readonly clusters: readonly ServiceRelationshipCluster[];
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
        <LaneSourceList clusters={clusters} lanes={lanes} />
        <RelationshipClusterList clusters={clusters} />
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
  clusters,
  lanes
}: {
  readonly clusters: readonly ServiceRelationshipCluster[];
  readonly lanes: readonly ServiceDeploymentLane[];
}): React.JSX.Element {
  const deploymentRepositories = clusters.find((cluster) =>
    cluster.kind === "deployment"
  )?.repositories ?? [];
  return (
    <section
      aria-label="Deployment sources"
      className="service-relationship-group service-relationship-group-wide"
    >
      <div className="service-relationship-group-heading">
        <h4>Deployment sources</h4>
        <span>{lanes.length} lanes</span>
      </div>
      <div className="service-deployment-source-grid">
        {lanes.map((lane) => (
          <article key={lane.label}>
            <div className="service-tech-row">
              <TechnologyMark technology={technologyForLane(lane)} />
              <strong>{lane.label}</strong>
            </div>
            <EvidenceLabels labels={lane.relationshipTypes} fallback="deployment evidence" />
            <small>{lane.environments.join(", ") || "environment pending"}</small>
            <p>{lane.sourceRepos.join(", ") || "Source repository not observed."}</p>
          </article>
        ))}
      </div>
      {deploymentRepositories.length > 0 ? (
        <div className="service-deployment-source-repos" aria-label="Deployment source repositories">
          {deploymentRepositories.slice(0, 8).map((repository) => (
            <RepositoryEvidence key={repository.repository} repository={repository} />
          ))}
        </div>
      ) : null}
    </section>
  );
}

function RelationshipClusterList({
  clusters
}: {
  readonly clusters: readonly ServiceRelationshipCluster[];
}): React.JSX.Element {
  const nonDeploymentClusters = clusters.filter((cluster) => cluster.kind !== "deployment");
  return (
    <section
      aria-label="Config and dependency graph"
      className="service-relationship-group service-relationship-group-wide"
    >
      <div className="service-relationship-group-heading">
        <h4>Config and dependency graph</h4>
        <span>{nonDeploymentClusters.length} groups</span>
      </div>
      <div className="service-relationship-cluster-grid">
        {nonDeploymentClusters.map((cluster) => (
          <article className="service-relationship-cluster" key={cluster.kind}>
            <div className="service-tech-row">
              <TechnologyMark technology={cluster.technology} />
              <div>
                <strong>{cluster.label}</strong>
                <small>{cluster.description}</small>
              </div>
            </div>
            <EvidenceLabels labels={cluster.relationshipTypes} fallback="observed relationship" />
            <div className="service-relationship-repos">
              {cluster.repositories.slice(0, 6).map((repository) => (
                <RepositoryEvidence key={repository.repository} repository={repository} />
              ))}
            </div>
          </article>
        ))}
      </div>
    </section>
  );
}

function RepositoryEvidence({
  repository
}: {
  readonly repository: ServiceRelationshipRepository;
}): React.JSX.Element {
  return (
    <div className="service-relationship-repo">
      <div className="service-tech-row">
        <TechnologyMark technology={repository.technology} />
        <span>{repository.repository}</span>
      </div>
      <small>
        {repository.paths[0] ?? repository.evidenceKinds[0] ?? "Evidence observed"}
      </small>
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

function TechnologyMark({
  technology
}: {
  readonly technology: ServiceTechnologyKind;
}): React.JSX.Element {
  const Icon = technologyIcon(technology);
  return (
    <span className={`service-tech-mark service-tech-mark-${technology}`}>
      <Icon aria-hidden="true" size={16} strokeWidth={2.2} />
      <span>{technologyLabel(technology)}</span>
    </span>
  );
}

function technologyIcon(technology: ServiceTechnologyKind): typeof Blocks {
  switch (technology) {
    case "argocd":
      return GitBranch;
    case "config":
      return KeyRound;
    case "github_actions":
      return Workflow;
    case "helm":
      return ShipWheel;
    case "kubernetes":
      return Hexagon;
    case "terraform":
      return Blocks;
    default:
      return FolderGit2;
  }
}

function technologyForLane(lane: ServiceDeploymentLane): ServiceTechnologyKind {
  const label = lane.label.toLowerCase();
  if (label.includes("ecs") || label.includes("terraform")) {
    return "terraform";
  }
  if (label.includes("gitops")) {
    return "argocd";
  }
  if (label.includes("kubernetes")) {
    return "kubernetes";
  }
  return "repository";
}

function technologyLabel(technology: ServiceTechnologyKind): string {
  switch (technology) {
    case "argocd":
      return "ArgoCD";
    case "config":
      return "Config";
    case "github_actions":
      return "GitHub Actions";
    case "helm":
      return "Helm chart";
    case "kubernetes":
      return "Kubernetes";
    case "terraform":
      return "Terraform resource";
    default:
      return "Repository";
  }
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

function consumerLabels(consumer: ServiceConsumer): readonly string[] {
  return [...consumer.relationshipTypes, ...consumer.consumerKinds].map(prettyLabel);
}

function prettyLabel(label: string): string {
  return label === label.toUpperCase() ? label : label.replace(/_/g, " ");
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
