import type {
  DeploymentConfigInfluence,
  DeploymentConfigItem,
  DeploymentConfigSection
} from "../api/deploymentConfigInfluence";

export function ServiceConfigInfluencePanel({
  influence
}: {
  readonly influence: DeploymentConfigInfluence | undefined;
}): React.JSX.Element | null {
  if (influence === undefined) {
    return null;
  }
  return (
    <section aria-label="Deployment configuration influence" className="service-config-influence">
      <div className="service-panel-heading">
        <div>
          <h3>Configuration influence</h3>
          <p>{influence.summary}</p>
        </div>
        <span>
          {influence.coverage.truncated ? "Truncated" : "Complete within limit"} · limit{" "}
          {influence.coverage.limit}
        </span>
      </div>
      <div className="service-config-layout">
        <RepositoryTrail repositories={influence.repositories} />
        <div className="service-config-sections">
          {influence.sections.map((section) => (
            <ConfigSection key={section.label} section={section} />
          ))}
        </div>
      </div>
    </section>
  );
}

function RepositoryTrail({
  repositories
}: {
  readonly repositories: DeploymentConfigInfluence["repositories"];
}): React.JSX.Element {
  return (
    <aside className="service-config-repositories">
      <h4>Influencing repositories</h4>
      {repositories.map((repository) => (
        <article key={repository.name}>
          <strong>{repository.name}</strong>
          <span>{repository.roles.map(prettyLabel).join(", ")}</span>
        </article>
      ))}
    </aside>
  );
}

function ConfigSection({
  section
}: {
  readonly section: DeploymentConfigSection;
}): React.JSX.Element {
  return (
    <section className="service-config-section">
      <div>
        <h4>{section.label}</h4>
        <span>{section.count} observed</span>
      </div>
      {section.items.length === 0 ? (
        <p>No evidence returned for this group.</p>
      ) : (
        section.items.slice(0, 4).map((item, index) => (
          <ConfigItem item={item} key={`${section.label}:${item.repoName}:${item.path}:${index}`} />
        ))
      )}
    </section>
  );
}

function ConfigItem({
  item
}: {
  readonly item: DeploymentConfigItem;
}): React.JSX.Element {
  return (
    <article>
      <div>
        <strong>{item.label}</strong>
        <span>{item.value || item.evidenceKind}</span>
      </div>
      <p>{[item.repoName, item.path].filter(Boolean).join(" · ")}</p>
      {item.action !== undefined ? (
        <small>
          {item.action}
          {item.line !== undefined ? ` from line ${item.line}` : ""}
        </small>
      ) : null}
    </article>
  );
}

function prettyLabel(value: string): string {
  return value.replace(/_/g, " ");
}
