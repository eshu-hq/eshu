import { useEffect, useState } from "react";
import { useParams } from "react-router-dom";
import { EshuApiClient } from "../api/client";
import { loadWorkspaceStory } from "../api/repository";
import type { WorkspaceStory } from "../api/mockData";
import { loadConsoleEnvironment } from "../config/environment";
import { EvidenceGrid } from "../grid/EvidenceGrid";
import { DeploymentGraphView } from "../visualization/DeploymentGraphView";

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

  return (
    <section className="workspace-page">
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

      <dl className="overview-list">
        {story.overviewStats.map((stat) => (
          <div key={stat.label} title={stat.detail}>
            <dt>{stat.label}</dt>
            <dd>{stat.value}</dd>
          </div>
        ))}
      </dl>

      <div className="workspace-grid">
        <section className="workspace-panel-wide">
          <h2>Deployment graph</h2>
          {story.deploymentGraph.nodes.length > 1 ? (
            <DeploymentGraphView graph={story.deploymentGraph} />
          ) : (
            <p className="inline-state">No deployment graph is available yet.</p>
          )}
        </section>
        <section className="workspace-panel-wide">
          <h2>Evidence story</h2>
          <EvidenceGrid rows={story.evidence} />
        </section>
        <div className="workspace-columns">
          <section>
            <h2>Findings</h2>
            <ul>
              {story.findings.map((finding) => (
                <li key={finding}>{finding}</li>
              ))}
            </ul>
          </section>
          <section>
            <h2>Limitations</h2>
            <ul>
              {story.limitations.map((limitation) => (
                <li key={limitation}>{limitation}</li>
              ))}
            </ul>
          </section>
        </div>
      </div>
    </section>
  );
}
