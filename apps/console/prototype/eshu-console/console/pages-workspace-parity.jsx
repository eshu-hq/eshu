/* Workspace dossier parity for the standalone prototype. */
(function () {
  const { useEffect: useEffectWP, useMemo: useMemoWP, useState: useStateWP } = React;

  function workspaceParts() {
    const raw = String(location.hash || "#workspace/services/svc-catalog").slice(1).split("?")[0];
    const parts = raw.split("/");
    return {
      entityKind: parts[1] || "services",
      entityId: decodeURIComponent(parts.slice(2).join("/") || "svc-catalog")
    };
  }

  function validKind(kind) {
    return kind === "repositories" || kind === "services" || kind === "workloads";
  }

  function storyPath(kind, id) {
    const escaped = encodeURIComponent(id);
    return kind === "repositories" ? "/api/v0/repositories/" + escaped + "/story" : "/api/v0/services/" + escaped + "/story";
  }

  function text(value, fallback) {
    return typeof value === "string" && value.trim() ? value.trim() : fallback;
  }

  function envelopeErrorMessage(error) {
    if (!error) return "";
    if (typeof error === "string") return error;
    if (typeof error !== "object") return "api error";
    return String(error.message || error.code || "api error");
  }

  function apiData(env) {
    const message = envelopeErrorMessage(env && env.error);
    if (message) throw new Error(message);
    return env && env.data ? env.data : {};
  }

  function subjectTitle(story, fallback) {
    if (typeof story.subject === "string") return text(story.subject, fallback);
    if (story.subject && typeof story.subject === "object") return text(story.subject.name, text(story.subject.id, fallback));
    return text((story.repository || {}).name, text(story.service_name, fallback));
  }

  function serviceName(story, fallback) {
    return text((story.service_identity || {}).service_name, text(story.service_name, subjectTitle(story, fallback)));
  }

  async function optionalGet(client, path) {
    if (!path) return null;
    try {
      const env = await client.get(path);
      return apiData(env);
    } catch (_) {
      return null;
    }
  }

  async function loadLiveWorkspace(client, kind, id) {
    if (!validKind(kind)) return null;
    const env = await client.get(storyPath(kind, id));
    const story = apiData(env);
    let context = null;
    if (kind === "repositories") {
      context = await optionalGet(client, ((story.drilldowns || {}).context_path));
      const workload = (((story.deployment_overview || {}).workloads) || [])[0];
      if (!context && workload) {
        context = await optionalGet(client, "/api/v0/services/" + encodeURIComponent(workload) + "/context");
      }
    } else {
      const name = serviceName(story, id);
      context = name ? await optionalGet(client, "/api/v0/services/" + encodeURIComponent(name) + "/context") : null;
    }
    return workspaceFromStory(kind, id, story, context, "live");
  }

  function demoService(D, kind, id) {
    const clean = id.replace(/^repository:/, "").replace(/^workload:/, "");
    if (kind === "repositories") {
      return D.services.find((svc) => svc.repo && (svc.repo.endsWith("/" + clean) || svc.repo === clean || svc.id === clean || svc.name === clean));
    }
    return D.servicesById[clean] || D.services.find((svc) => svc.name === clean || svc.id === clean);
  }

  function loadDemoWorkspace(D, kind, id) {
    const svc = demoService(D, kind, id) || D.servicesById["svc-catalog"] || D.services[0];
    if (!svc) return null;
    const title = kind === "repositories" ? (svc.repo || svc.name).split("/").pop() : svc.name;
    const context = {
      deployment_evidence: { artifacts: [
        { artifact_family: "argocd", relationship_type: "DEPLOYS_FROM", source_repo_name: "iac-eks-argocd", target_repo_name: "helm-charts", path: "apps/" + svc.name + ".yaml" },
        { artifact_family: "helm", relationship_type: "DEPLOYS_FROM", source_repo_name: "helm-charts", target_repo_name: title, path: "charts/" + svc.name + "/Chart.yaml" }
      ] }
    };
    return workspaceFromStory(kind, id, {
      repository: { name: title },
      service_name: svc.name,
      story: svc.story,
      deployment_overview: {
        direct_story: ["iac-eks-argocd deploys helm-charts", "helm-charts deploys " + title, title + " runs as " + svc.name],
        workloads: [svc.name],
        workload_count: 1
      },
      semantic_overview: { language_counts: { [svc.lang || "unknown"]: 1 }, entity_count: svc.calls || 0 },
      support_overview: { dependency_count: (svc.deps || []).length, topology_signal_count: svc.callers || 0 },
      story_sections: [
        { title: "Service story", summary: svc.story || (svc.name + " is indexed from bundled prototype facts.") },
        { title: "Deployment", summary: "ArgoCD and Helm deployment evidence connect source to workload." }
      ],
      limitations: []
    }, context, "demo");
  }

  function workspaceFromStory(kind, id, story, context, source) {
    const title = subjectTitle(story, id);
    const graph = deploymentGraph(story, context, title);
    const evidence = evidenceRows(story, context);
    return {
      id,
      kind,
      source,
      title,
      story: text(story.story, humanStory(story, context, title)),
      stats: overviewStats(story, context, graph, evidence),
      graph,
      evidence,
      findings: [],
      limitations: Array.isArray(story.limitations) ? story.limitations : []
    };
  }

  function overviewStats(story, context, graph, evidence) {
    const overview = story.deployment_overview || {};
    const semantic = story.semantic_overview || {};
    const support = story.support_overview || {};
    return [
      { label: "Workloads", value: overview.workload_count || (overview.workloads || []).length || (graph.nodes.length ? 1 : 0), detail: "deployment_overview.workloads" },
      { label: "Evidence rows", value: evidence.length, detail: "story_sections + deployment_evidence.artifacts" },
      { label: "Entities", value: semantic.entity_count || 0, detail: "semantic_overview.entity_count" },
      { label: "Dependencies", value: support.dependency_count || ((context || {}).dependency_count) || 0, detail: "support_overview.dependency_count" }
    ];
  }

  function humanStory(story, context, title) {
    const direct = ((story.deployment_overview || {}).direct_story || []).filter(Boolean);
    if (direct.length) return direct.join(". ") + ".";
    const families = (((context || {}).deployment_evidence || {}).artifact_families || []).join(", ");
    return title + (families ? " has deployment evidence from " + families + "." : " has no deployment story returned yet.");
  }

  function evidenceRows(story, context) {
    const rows = [];
    const artifacts = (((context || {}).deployment_evidence || {}).artifacts) || [];
    artifacts.slice(0, 8).forEach((artifact) => {
      rows.push({
        title: artifact.artifact_family === "argocd" ? "Deployed by ArgoCD" : artifact.artifact_family === "helm" ? "Deployed from Helm" : "Deployment artifact",
        source: text(artifact.source_repo_name, text((artifact.source_location || {}).repo_name, "deployment")),
        basis: text(artifact.relationship_type, text(artifact.evidence_kind, "deployment_evidence")),
        detail: text((artifact.source_location || {}).path, text(artifact.path, text(artifact.name, "")))
      });
    });
    (story.story_sections || []).slice(0, 6).forEach((section) => {
      rows.push({
        title: text(section.title, "Story"),
        source: "repository_story",
        basis: "story_section",
        detail: text(section.summary, "")
      });
    });
    return rows;
  }

  function repoFromArtifact(artifact, side) {
    const id = text(side === "source" ? artifact.source_repo_id : artifact.target_repo_id, "");
    const name = text(side === "source" ? artifact.source_repo_name : artifact.target_repo_name, "");
    if (!id && !name) return null;
    return { id: id || "repository:" + name, name: name || id };
  }

  function deploymentGraph(story, context, title) {
    const artifacts = ((((context || {}).deployment_evidence || {}).artifacts) || [])
      .filter((artifact) => text(artifact.relationship_type, "").toUpperCase() === "DEPLOYS_FROM");
    if (!artifacts.length) {
      return { nodes: [{ id: "entity:" + title, kind: "repo", label: title, sub: "Workspace entity", col: 1, hero: true }], edges: [] };
    }
    const sourceRepo = artifacts.map((artifact) => repoFromArtifact(artifact, "target")).find((repo) => repo && repo.name === title)
      || { id: "repository:" + title, name: title };
    const workload = (((story.deployment_overview || {}).workloads) || [])[0] || serviceName(story, title);
    const nodes = new Map();
    const edges = [];
    addNode(nodes, { id: sourceRepo.id, kind: "repo", label: sourceRepo.name, sub: "Source repository", col: 2 });
    addNode(nodes, { id: "workload:" + workload, kind: "workload", label: workload, sub: "Workload", col: 3, hero: true });
    uniqueRepos(artifacts.filter(isHelm).map((artifact) => repoFromArtifact(artifact, "source"))).forEach((repo) => {
      addNode(nodes, { id: repo.id, kind: "repo", label: repo.name, sub: "Helm chart", col: 1 });
      edges.push({ s: repo.id, t: sourceRepo.id, verb: "PACKAGES", layer: "deploy" });
    });
    const chartIds = new Set(Array.from(nodes.values()).filter((node) => node.sub === "Helm chart").map((node) => node.id));
    uniqueRepos(artifacts.filter(isController).map((artifact) => repoFromArtifact(artifact, "source"))).forEach((repo) => {
      if (repo.id === sourceRepo.id || chartIds.has(repo.id)) return;
      addNode(nodes, { id: repo.id, kind: "repo", label: repo.name, sub: "Deployment controller", col: 0 });
      const charts = Array.from(nodes.values()).filter((node) => node.sub === "Helm chart");
      if (charts.length) charts.forEach((chart) => edges.push({ s: repo.id, t: chart.id, verb: "DEPLOYS_HELM", layer: "deploy" }));
      else edges.push({ s: repo.id, t: sourceRepo.id, verb: "DEPLOYS_FROM", layer: "deploy" });
    });
    edges.push({ s: sourceRepo.id, t: "workload:" + workload, verb: "DEPLOYS_FROM", layer: "deploy" });
    return { nodes: Array.from(nodes.values()), edges };
  }

  function addNode(nodes, value) {
    if (!nodes.has(value.id)) nodes.set(value.id, value);
  }

  function uniqueRepos(repos) {
    const seen = new Set();
    return repos.filter((repo) => {
      if (!repo || seen.has(repo.id)) return false;
      seen.add(repo.id);
      return true;
    });
  }

  function isHelm(artifact) {
    const family = text(artifact.artifact_family, "").toLowerCase();
    const path = text(artifact.path, "").toLowerCase();
    const repo = text(artifact.source_repo_name, "").toLowerCase();
    return family === "helm" && (path.indexOf("chart.yaml") >= 0 || repo.indexOf("helm") >= 0 || repo.indexOf("chart") >= 0);
  }

  function isController(artifact) {
    const family = text(artifact.artifact_family, "").toLowerCase();
    return family === "argocd" || family === "kustomize";
  }

  function nodeLabel(graph, id) {
    const node = (graph.nodes || []).find((candidate) => candidate.id === id);
    return node ? node.label : id;
  }

  function typedDeploymentEdges(graph) {
    return (graph.edges || []).filter((edge) => edge.layer === "deploy" && edge.verb);
  }

  function Workspace({ data, client, onOpenNode }) {
    const D = data || ESHU;
    const [{ entityKind, entityId }, setRoute] = useStateWP(workspaceParts);
    const [state, setState] = useStateWP({ status: "loading", story: null, error: "" });
    useEffectWP(() => {
      function onHash() { setRoute(workspaceParts()); }
      window.addEventListener("hashchange", onHash);
      return () => window.removeEventListener("hashchange", onHash);
    }, []);
    useEffectWP(() => {
      let cancelled = false;
      setState({ status: "loading", story: null, error: "" });
      const load = client ? loadLiveWorkspace(client, entityKind, entityId) : Promise.resolve(loadDemoWorkspace(D, entityKind, entityId));
      load.then((story) => {
        if (cancelled) return;
        setState({ status: story ? "ready" : "unavailable", story, error: "" });
      }).catch((e) => {
        if (cancelled) return;
        setState({ status: "unavailable", story: null, error: (e && e.message) || "unavailable" });
      });
      return () => { cancelled = true; };
    }, [D, client, entityKind, entityId]);

    const story = state.story;
    const graph = useMemoWP(() => story ? story.graph : { nodes: [], edges: [] }, [story]);
    const deployEdges = useMemoWP(() => typedDeploymentEdges(graph), [graph]);
    if (state.status === "loading") {
      return <div className="page"><div className="page-intro"><h2>Loading workspace</h2><p>Loading workspace dossier.</p></div></div>;
    }
    if (!story) {
      return <div className="page"><div className="page-intro"><h2>Workspace unavailable</h2><p>{state.error || "The selected entity is not available from the Eshu API."}</p></div></div>;
    }
    return (
      <div className="page" style={{ maxWidth: "none" }}>
        <div className="page-intro">
          <h2>{story.title}</h2>
          <p><span className="mono">{story.kind}</span> dossier from {story.source === "live" ? "live Eshu API" : "bundled prototype facts"}.</p>
          <p>{story.story}</p>
        </div>
        <div className="grid g-4">
          {story.stats.map((stat) => <StatTile key={stat.label} label={stat.label} value={stat.value} sub={stat.detail} color="var(--teal)" />)}
        </div>
        <Panel className="flush mt" title="Deployment evidence map" sub="Repository, chart, controller, and workload evidence" glyph={<Icon.graph />}>
          <GraphCanvas graph={graph} data={D} layout="layered" height={420} onSelect={(node) => onOpenNode && onOpenNode(node, graph)} />
          {deployEdges.length ? (
            <div className="insp-evi mt">
              <div className="insp-kind">Typed deployment chain</div>
              {deployEdges.map((edge) => (
                <div className="insp-evi-row" key={edge.s + edge.verb + edge.t}>
                  <span>{nodeLabel(graph, edge.s)}</span>
                  <Badge tone="teal">{edge.verb}</Badge>
                  <span>{nodeLabel(graph, edge.t)}</span>
                </div>
              ))}
            </div>
          ) : null}
        </Panel>
        <Panel className="flush mt" title="Evidence story" sub="Readable claims with source and basis">
          <table className="tbl">
            <thead><tr><th>Claim</th><th>Source</th><th>Basis</th><th>Detail</th></tr></thead>
            <tbody>{story.evidence.map((row, index) => (
              <tr key={index}><td className="t-name">{row.title}</td><td className="mono">{row.source}</td><td><Badge tone="teal">{row.basis}</Badge></td><td>{row.detail}</td></tr>
            ))}</tbody>
          </table>
          {!story.evidence.length ? <p className="empty">No evidence rows returned for this workspace.</p> : null}
        </Panel>
        <div className="grid g-2 mt">
          <Panel title="Findings">{story.findings.length ? story.findings.map((row) => <div className="insp-evi-row" key={row}>{row}</div>) : <p className="empty">No findings reported for this entity.</p>}</Panel>
          <Panel title="Known gaps">{story.limitations.length ? story.limitations.map((row) => <div className="insp-evi-row" key={row}>{row}</div>) : <p className="empty">No known gaps reported for this entity.</p>}</Panel>
        </div>
      </div>
    );
  }

  window.Workspace = Workspace;
})();
