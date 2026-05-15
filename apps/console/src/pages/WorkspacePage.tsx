import { useEffect, useState } from "react";
import { useParams } from "react-router-dom";
import { EshuApiClient } from "../api/client";
import { loadWorkspaceStory } from "../api/repository";
import type { WorkspaceStory } from "../api/mockData";
import { loadConsoleEnvironment } from "../config/environment";
import { EvidenceGrid } from "../grid/EvidenceGrid";
import { DeploymentGraphView } from "../visualization/DeploymentGraphView";
import { ServiceSpotlightPanel } from "./ServiceSpotlightPanel";

export function WorkspacePage(): React.JSX.Element {
  const { entityId, entityKind } = useParams();
  const [story, setStory] = useState<WorkspaceStory | null>(null);
  const [loadState, setLoadState] = useState<"loading" | "ready" | "unavailable">(
    "loading"
  );

  useEffect(() => {
    if (
      entityKind !== "repositories" &&
      entityKind !== "services" &&
      entityKind !== "workloads"
    ) {
      setLoadState("unavailable");
      return;
    }
    const environment = loadConsoleEnvironment();
    const client =
      environment.mode === "private"
        ? new EshuApiClient({
          apiKey: environment.apiKey,
          baseUrl: environment.apiBaseUrl
        })
        : undefined;
    void loadWorkspaceStory({
      client,
      entityId: entityId ?? "",
      entityKind,
      mode: environment.mode
    })
      .then((loadedStory) => {
        setStory(loadedStory);
        setLoadState(loadedStory === null ? "unavailable" : "ready");
      })
      .catch(() => {
        setStory(null);
        setLoadState("unavailable");
      });
  }, [entityId, entityKind]);

  if (loadState === "loading") {
    return (
      <section className="page-shell">
        <h1>Loading workspace</h1>
        <p>Loading live data.</p>
      </section>
    );
  }

  if (story === null) {
    return (
      <section className="page-shell">
        <h1>Workspace unavailable</h1>
        <p>The selected entity is not available from the local Eshu API.</p>
      </section>
    );
  }
  const hasServiceDossier = story.serviceSpotlight !== undefined;

  return (
    <section className={`workspace-page${hasServiceDossier ? " service-workspace-page" : ""}`}>
      {!hasServiceDossier ? (
        <div className="workspace-summary">
          <div>
            <h1>{story.title}</h1>
            <p className="entity-kind">{story.kind}</p>
            <p>{story.story}</p>
          </div>
          <dl className="truth-list" aria-label="Truth and freshness">
            <div>
              <dt>Truth</dt>
              <dd>{story.truth.level}</dd>
            </div>
            <div>
              <dt>Freshness</dt>
              <dd>{story.truth.freshness.state}</dd>
            </div>
            <div>
              <dt>Profile</dt>
              <dd>{story.truth.profile}</dd>
            </div>
          </dl>
        </div>
      ) : null}

      {!hasServiceDossier ? (
        <dl className="overview-list">
          {story.overviewStats.map((stat) => (
            <div key={stat.label} title={stat.detail}>
              <dt>{stat.label}</dt>
              <dd>{stat.value}</dd>
            </div>
          ))}
        </dl>
      ) : null}

      <div className="workspace-grid">
        {hasServiceDossier ? (
          <ServiceSpotlightPanel spotlight={story.serviceSpotlight} />
        ) : null}
        <section className="workspace-panel-wide workspace-evidence-graph">
          <WorkspaceSectionHeading
            description={
              story.serviceSpotlight !== undefined
                ? "Raw deployment evidence behind the service story, kept visible for audit."
                : "Deployment relationships found from repository, workflow, and infrastructure evidence."
            }
            title="Evidence graph"
          />
          {story.deploymentGraph.nodes.length > 1 ? (
            <DeploymentGraphView
              detailTitle="Evidence index"
              graph={story.deploymentGraph}
            />
          ) : (
            <p className="inline-state">No deployment graph is available yet.</p>
          )}
        </section>
        <section className="workspace-panel-wide workspace-evidence-story">
          <WorkspaceSectionHeading
            description="Readable claims first, with the source and basis close enough to verify."
            title="Evidence story"
          />
          <EvidenceGrid rows={story.evidence} />
        </section>
        <div className="workspace-columns">
          <section className="workspace-support-panel">
            <WorkspaceSectionHeading
              description="Actionable cleanup, drift, or confidence issues for this entity."
              title="Findings"
            />
            {story.findings.length > 0 ? (
              <ul className="workspace-signal-list">
                {story.findings.map((finding) => (
                  <li key={finding}>{finding}</li>
                ))}
              </ul>
            ) : (
              <p className="inline-state">No findings reported for this entity.</p>
            )}
          </section>
          <section className="workspace-support-panel">
            <WorkspaceSectionHeading
              description="Known gaps, inferred edges, or coverage limits to keep in mind."
              title="Known gaps"
            />
            {story.limitations.length > 0 ? (
              <ul className="workspace-signal-list">
                {story.limitations.map((limitation) => (
                  <li key={limitation}>{limitation}</li>
                ))}
              </ul>
            ) : (
              <p className="inline-state">No known gaps reported for this entity.</p>
            )}
          </section>
        </div>
      </div>
    </section>
  );
}

function WorkspaceSectionHeading({
  description,
  title
}: {
  readonly description: string;
  readonly title: string;
}): React.JSX.Element {
  return (
    <div className="workspace-section-heading">
      <h2>{title}</h2>
      <p>{description}</p>
    </div>
  );
}
